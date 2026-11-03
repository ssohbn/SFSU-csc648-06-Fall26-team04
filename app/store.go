package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/mattn/go-sqlite3"
)

type store struct {
	db *sql.DB
}

type StudyGroup struct {
	ID               int64
	Name             string
	ShortDescription string
	LongDescription  string
	Mode             string
	SchoolDomain     string
	MeetingPlace     string
	MeetingTime      string
	Pin              *Pin // nil until the organizer drops one on the map
	Members          int
	Interests        []Interest
	Role             string // the viewer's: "", "USER" or "ADMIN"
	inviteCode       string // unexported so no template can print it by accident
}

// Pin is where a group meets on the map.
type Pin struct {
	Lat, Lng float64
}

// columns is the pin as the database stores it, NULLs when there's no pin.
func (p *Pin) columns() (lat, lng any) {
	if p == nil {
		return nil, nil
	}
	return p.Lat, p.Lng
}

func (g StudyGroup) ModeLabel() string {
	c, _ := modeInfo(g.Mode)
	return c.Label
}

type Member struct {
	ID       int64
	Username string
	Role     string
}

type Interest struct {
	ID   int64
	Name string
	Mine bool
}

// User never carries the password hash, so views can't leak it.
type User struct {
	ID       int64
	Username string
	Email    string
	Bio      string
}

var (
	errUsernameTaken = errors.New("username taken")
	errEmailTaken    = errors.New("email taken")
)

// takenErr tells which unique Users column an insert or update ran into.
func takenErr(err error) error {
	var se sqlite3.Error
	if errors.As(err, &se) && se.ExtendedCode == sqlite3.ErrConstraintUnique {
		switch {
		case strings.Contains(se.Error(), "Users.Username"):
			return errUsernameTaken
		case strings.Contains(se.Error(), "Users.Email"):
			return errEmailTaken
		}
	}
	return err
}

// searchInterests also marks the ones userID has; pass 0 for a guest.
func (s *store) searchInterests(ctx context.Context, query string, userID int64) ([]Interest, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT i.Id, i.Name, EXISTS (SELECT 1 FROM UserInterests WHERE UserID = ?2 AND InterestID = i.Id)
		FROM Interests i
		WHERE instr(lower(i.Name), lower(?1)) > 0
		ORDER BY i.Name`, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var interests []Interest
	for rows.Next() {
		var i Interest
		if err := rows.Scan(&i.ID, &i.Name, &i.Mine); err != nil {
			return nil, err
		}
		interests = append(interests, i)
	}
	return interests, rows.Err()
}

func (s *store) userInterests(ctx context.Context, userID int64) ([]Interest, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT i.Id, i.Name FROM UserInterests ui JOIN Interests i ON i.Id = ui.InterestID
		WHERE ui.UserID = ? ORDER BY i.Name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var interests []Interest
	for rows.Next() {
		i := Interest{Mine: true}
		if err := rows.Scan(&i.ID, &i.Name); err != nil {
			return nil, err
		}
		interests = append(interests, i)
	}
	return interests, rows.Err()
}

func (s *store) addUserInterest(ctx context.Context, userID, interestID int64) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO UserInterests (UserID, InterestID)
		SELECT ?, Id FROM Interests WHERE Id = ?`, userID, interestID)
	return err
}

func (s *store) removeUserInterest(ctx context.Context, userID, interestID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM UserInterests WHERE UserID = ? AND InterestID = ?`, userID, interestID)
	return err
}

// createUserInterest adds an interest by name, creating it if nobody has yet.
func (s *store) createUserInterest(ctx context.Context, userID int64, name string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO Interests (Name) VALUES (?) ON CONFLICT DO NOTHING`, name); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO UserInterests (UserID, InterestID)
		SELECT ?, Id FROM Interests WHERE Name = ?`, userID, name); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *store) createUser(ctx context.Context, username, email, passwordHash string) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO Users (Username, Email, PasswordHash) VALUES (?, ?, ?)`, username, email, passwordHash)
	if err != nil {
		return 0, takenErr(err)
	}
	return res.LastInsertId()
}

// passwordHash finds an account by username or email; usernames can't
// contain "@", so the two never collide.
func (s *store) passwordHash(ctx context.Context, login string) (int64, string, error) {
	var id int64
	var hash string
	err := s.db.QueryRowContext(ctx,
		`SELECT Id, PasswordHash FROM Users WHERE Username = ?1 OR Email = ?1`, login).Scan(&id, &hash)
	return id, hash, err
}

func (s *store) updateProfile(ctx context.Context, userID int64, email, bio string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE Users SET Email = ?, Bio = ? WHERE Id = ?`, email, bio, userID)
	return takenErr(err)
}

func (s *store) createSession(ctx context.Context, tokenHash []byte, userID int64, expires time.Time) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM Sessions WHERE ExpiresAt <= ?`, time.Now().Unix()); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO Sessions (TokenHash, UserID, ExpiresAt) VALUES (?, ?, ?)`, tokenHash, userID, expires.Unix())
	return err
}

func (s *store) userBySession(ctx context.Context, tokenHash []byte, now time.Time) (User, error) {
	var u User
	err := s.db.QueryRowContext(ctx, `
		SELECT u.Id, u.Username, u.Email, u.Bio FROM Sessions s JOIN Users u ON u.Id = s.UserID
		WHERE s.TokenHash = ? AND s.ExpiresAt > ?`, tokenHash, now.Unix()).Scan(&u.ID, &u.Username, &u.Email, &u.Bio)
	return u, err
}

func (s *store) deleteSession(ctx context.Context, tokenHash []byte) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM Sessions WHERE TokenHash = ?`, tokenHash)
	return err
}

// groups loads every group, or only group id when id isn't 0, with its member
// count, topics and viewerID's role. Callers decide what to show with canView.
func (s *store) groups(ctx context.Context, viewerID, id int64) ([]StudyGroup, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT g.Id, g.Name, g.ShortDescription, g.LongDescription, g.Mode, g.SchoolDomain,
			COALESCE(g.InviteCode, ''), g.MeetingPlace, g.MeetingTime, g.Latitude, g.Longitude,
			(SELECT COUNT(*) FROM StudyGroupEnrollment e WHERE e.StudyGroupID = g.Id),
			COALESCE((SELECT e.MemberType FROM StudyGroupEnrollment e WHERE e.StudyGroupID = g.Id AND e.UserID = ?1), '')
		FROM StudyGroups g WHERE ?2 = 0 OR g.Id = ?2
		ORDER BY g.Name COLLATE NOCASE`, viewerID, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var groups []StudyGroup
	index := map[int64]int{}
	for rows.Next() {
		var g StudyGroup
		var lat, lng sql.NullFloat64
		if err := rows.Scan(&g.ID, &g.Name, &g.ShortDescription, &g.LongDescription, &g.Mode, &g.SchoolDomain,
			&g.inviteCode, &g.MeetingPlace, &g.MeetingTime, &lat, &lng, &g.Members, &g.Role); err != nil {
			return nil, err
		}
		if lat.Valid && lng.Valid {
			g.Pin = &Pin{lat.Float64, lng.Float64}
		}
		index[g.ID] = len(groups)
		groups = append(groups, g)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	topics, err := s.db.QueryContext(ctx, `
		SELECT gi.StudyGroupID, i.Id, i.Name FROM StudyGroupInterests gi JOIN Interests i ON i.Id = gi.InterestID
		WHERE ?1 = 0 OR gi.StudyGroupID = ?1 ORDER BY i.Name`, id)
	if err != nil {
		return nil, err
	}
	defer topics.Close()
	for topics.Next() {
		var groupID int64
		var i Interest
		if err := topics.Scan(&groupID, &i.ID, &i.Name); err != nil {
			return nil, err
		}
		if n, ok := index[groupID]; ok {
			groups[n].Interests = append(groups[n].Interests, i)
		}
	}
	return groups, topics.Err()
}

// createGroup saves a new group with ownerID as its admin.
func (s *store) createGroup(ctx context.Context, ownerID int64, g StudyGroup, topics []string) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	lat, lng := g.Pin.columns()
	res, err := tx.ExecContext(ctx, `
		INSERT INTO StudyGroups (Name, ShortDescription, LongDescription, Mode, SchoolDomain, InviteCode,
			MeetingPlace, MeetingTime, Latitude, Longitude)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		g.Name, g.ShortDescription, g.LongDescription, g.Mode, g.SchoolDomain, rand.Text(),
		g.MeetingPlace, g.MeetingTime, lat, lng)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO StudyGroupEnrollment (UserID, StudyGroupID, MemberType) VALUES (?, ?, 'ADMIN')`, ownerID, id); err != nil {
		return 0, err
	}
	if err := setTopics(ctx, tx, id, topics); err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

func (s *store) updateGroup(ctx context.Context, g StudyGroup, topics []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	lat, lng := g.Pin.columns()
	// groups added straight to the database have no invite code yet
	res, err := tx.ExecContext(ctx, `
		UPDATE StudyGroups SET Name = ?, ShortDescription = ?, LongDescription = ?, Mode = ?, SchoolDomain = ?,
			MeetingPlace = ?, MeetingTime = ?, Latitude = ?, Longitude = ?, InviteCode = COALESCE(InviteCode, ?)
		WHERE Id = ?`,
		g.Name, g.ShortDescription, g.LongDescription, g.Mode, g.SchoolDomain,
		g.MeetingPlace, g.MeetingTime, lat, lng, rand.Text(), g.ID)
	if err != nil {
		return err
	}
	// the group was deleted after the form loaded
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return sql.ErrNoRows
	}
	if err := setTopics(ctx, tx, g.ID, topics); err != nil {
		return err
	}
	return tx.Commit()
}

// setTopics replaces a group's topics, creating interests nobody has used yet.
func setTopics(ctx context.Context, tx *sql.Tx, groupID int64, topics []string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM StudyGroupInterests WHERE StudyGroupID = ?`, groupID); err != nil {
		return err
	}
	for _, name := range topics {
		if _, err := tx.ExecContext(ctx, `INSERT INTO Interests (Name) VALUES (?) ON CONFLICT DO NOTHING`, name); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO StudyGroupInterests (StudyGroupID, InterestID)
			SELECT ?, Id FROM Interests WHERE Name = ?`, groupID, name); err != nil {
			return err
		}
	}
	return nil
}

// deleteGroup also drops its memberships and topics, through ON DELETE CASCADE.
func (s *store) deleteGroup(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM StudyGroups WHERE Id = ?`, id)
	return err
}

// joinGroup does nothing if the group was deleted after its page loaded.
func (s *store) joinGroup(ctx context.Context, userID, groupID int64) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO StudyGroupEnrollment (UserID, StudyGroupID)
		SELECT ?, Id FROM StudyGroups WHERE Id = ?`, userID, groupID)
	return err
}

// removeMember never removes the admin, so a group always keeps its owner.
func (s *store) removeMember(ctx context.Context, userID, groupID int64) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM StudyGroupEnrollment WHERE UserID = ? AND StudyGroupID = ? AND MemberType = 'USER'`, userID, groupID)
	return err
}

func (s *store) members(ctx context.Context, groupID int64) ([]Member, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT u.Id, u.Username, e.MemberType FROM StudyGroupEnrollment e JOIN Users u ON u.Id = e.UserID
		WHERE e.StudyGroupID = ? ORDER BY e.MemberType = 'ADMIN' DESC, u.Username`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var members []Member
	for rows.Next() {
		var m Member
		if err := rows.Scan(&m.ID, &m.Username, &m.Role); err != nil {
			return nil, err
		}
		members = append(members, m)
	}
	return members, rows.Err()
}

// resetInvite swaps in a new invite code, so old links stop working.
func (s *store) resetInvite(ctx context.Context, groupID int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE StudyGroups SET InviteCode = ? WHERE Id = ?`, rand.Text(), groupID)
	return err
}
