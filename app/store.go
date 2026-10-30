package main

import (
	"context"
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
}

type Interest struct {
	ID     int64
	Name   string
	People int
	Mine   bool
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

func (s *store) searchGroups(ctx context.Context, query string) ([]StudyGroup, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT Id, Name, ShortDescription, LongDescription FROM StudyGroups
		WHERE instr(lower(Name), lower(?1)) > 0 OR instr(lower(ShortDescription), lower(?1)) > 0 OR instr(lower(LongDescription), lower(?1)) > 0
		ORDER BY Name`, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var groups []StudyGroup
	for rows.Next() {
		var g StudyGroup
		if err := rows.Scan(&g.ID, &g.Name, &g.ShortDescription, &g.LongDescription); err != nil {
			return nil, err
		}
		groups = append(groups, g)
	}
	return groups, rows.Err()
}

// searchInterests also counts people per interest and marks the ones userID
// has; pass 0 for a guest.
func (s *store) searchInterests(ctx context.Context, query string, userID int64) ([]Interest, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT i.Id, i.Name, COUNT(ui.UserID), COALESCE(MAX(ui.UserID = ?2), 0)
		FROM Interests i LEFT JOIN UserInterests ui ON ui.InterestID = i.Id
		WHERE instr(lower(i.Name), lower(?1)) > 0
		GROUP BY i.Id ORDER BY i.Name`, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var interests []Interest
	for rows.Next() {
		var i Interest
		if err := rows.Scan(&i.ID, &i.Name, &i.People, &i.Mine); err != nil {
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
