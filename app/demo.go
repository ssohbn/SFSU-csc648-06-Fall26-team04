package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// This opt-in seed appends labelled fixtures to an existing database atomically.
// Repeat runs reuse only our demo identities and never change their profiles.
func seedMatchingDemo(ctx context.Context, s *store) error {
	people := []struct {
		name, bio string
		topics    []string
	}{
		{"avery", "Web development student; prefers small project sessions.", []string{"CSC 667", "Web Development", "Software Engineering"}},
		{"jae", "Operating systems student; likes tracing processes and memory.", []string{"CSC 415", "Operating Systems"}},
		{"noor", "Practices SQL and database design with peers.", []string{"CSC 675", "Databases"}},
		{"mateo", "Studies software engineering and web applications.", []string{"CSC 648", "CSC 667", "Software Engineering"}},
		{"lina", "Works through calculus and physics problems together.", []string{"Calculus", "Physics"}},
		{"kai", "Interested in artificial intelligence and linear algebra.", []string{"CSC 665", "Machine Learning", "Linear Algebra"}},
		{"riley", "Likes operating systems labs and collaborative debugging.", []string{"CSC 415", "Data Structures"}},
		{"zoe", "Practices academic writing and Spanish conversation.", []string{"Writing", "Spanish"}},
	}
	groups := []struct {
		owner                         int
		name, description, time, mode string
		topics                        []string
		members                       []int
	}{
		{0, "CSC 667 Thursday web lab", "CSC 667 web application practice: HTTP, forms and backend debugging. Small group.", "Thursdays 13:00–14:00 America/Los_Angeles", modeOpen, []string{"CSC 667", "Web Development"}, []int{3, 6}},
		{1, "CSC 415 Thursday OS practice", "CSC 415 processes, threads, memory and operating systems lab exercises.", "Thursdays 14:00–15:00 America/Los_Angeles", modeOpen, []string{"CSC 415", "Operating Systems"}, []int{6, 0}},
		{6, "CSC 415 Thursday online lab", "CSC 415 collaborative operating systems lab debugging in a small online group.", "Thursdays 13:30–14:30 America/Los_Angeles", modeAccount, []string{"CSC 415", "Operating Systems"}, []int{1, 3}},
		{3, "CSC 667 evening web workshop", "CSC 667 web development project practice and backend code review.", "Tuesdays 18:00–19:00 America/Los_Angeles", modeOpen, []string{"CSC 667", "Web Development"}, []int{0, 2}},
		{1, "CSC 415 Friday systems circle", "CSC 415 exam preparation covering scheduling, synchronization and virtual memory.", "Fridays 10:00–11:00 America/Los_Angeles", modeOpen, []string{"CSC 415", "Operating Systems"}, []int{6, 5}},
		{2, "CSC 675 Thursday SQL practice", "CSC 675 quiet SQL query practice, joins and database workshop preparation.", "Thursdays 13:00–14:00 America/Los_Angeles", modeOpen, []string{"CSC 675", "Databases"}, []int{0, 3}},
		{3, "CSC 648 software workshop", "CSC 648 team project planning, software engineering and testing practice.", "Wednesdays 16:00–17:00 America/Los_Angeles", modeAccount, []string{"CSC 648", "Software Engineering"}, []int{0, 1, 2}},
		{5, "CSC 665 AI reading circle", "CSC 665 artificial intelligence exercises and discussion of search and reasoning.", "Saturdays 11:00–12:00 America/Los_Angeles", modeOpen, []string{"CSC 665", "Machine Learning"}, []int{2, 6}},
		{4, "Calculus Thursday problem hour", "Calculus differentiation, integration and weekly problem sets at a quiet pace.", "Thursdays 13:00–14:00 America/Los_Angeles", modeOpen, []string{"Calculus"}, []int{5, 2}},
		{4, "Physics Saturday study hall", "Physics mechanics and calculus problem solving with peers.", "Saturdays 14:00–15:00 America/Los_Angeles", modeOpen, []string{"Physics", "Calculus"}, []int{5, 1}},
		{7, "Friday writing practice", "Bring an essay outline for academic writing practice and peer feedback.", "Fridays 15:00–16:00 America/Los_Angeles", modeOpen, []string{"Writing"}, []int{0, 4}},
		{7, "Sunday Spanish conversation", "Spanish conversation practice with a relaxed small group.", "Sundays 11:00–12:00 America/Los_Angeles", modeInterest, []string{"Spanish"}, []int{3, 4}},
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(rand.Text()), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	topicID := func(name string) (int64, error) {
		if _, err := tx.ExecContext(ctx, `INSERT INTO Interests (Name) VALUES (?) ON CONFLICT DO NOTHING`, name); err != nil {
			return 0, err
		}
		var id int64
		err := tx.QueryRowContext(ctx, `SELECT Id FROM Interests WHERE Name = ?`, name).Scan(&id)
		return id, err
	}
	ids := make([]int64, len(people))
	for i, p := range people {
		name, email := "demo."+p.name, "demo."+p.name+"@studysync.invalid"
		var existingEmail, bio string
		err := tx.QueryRowContext(ctx, `SELECT Id, Email, Bio FROM Users WHERE Username = ?`, name).Scan(&ids[i], &existingEmail, &bio)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			result, err := tx.ExecContext(ctx, `INSERT INTO Users (Username, Email, PasswordHash, Bio) VALUES (?,?,?,?)`, name, email, string(hash), "Demo profile for matching tests. "+p.bio)
			if err != nil {
				return err
			}
			ids[i], err = result.LastInsertId()
			if err != nil {
				return err
			}
		case err != nil:
			return err
		case existingEmail != email || bio != "Demo profile for matching tests. "+p.bio:
			return fmt.Errorf("demo username %s belongs to another profile; no demo data added", name)
		}
		for _, name := range p.topics {
			id, err := topicID(name)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO UserInterests VALUES (?,?)`, ids[i], id); err != nil {
				return err
			}
		}
	}
	for _, g := range groups {
		var id int64
		name := "Demo · " + g.name
		err := tx.QueryRowContext(ctx, `SELECT g.Id FROM StudyGroups g JOIN StudyGroupEnrollment e ON e.StudyGroupID = g.Id WHERE g.Name = ? AND e.UserID = ? AND e.MemberType = 'ADMIN'`, name, ids[g.owner]).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			result, err := tx.ExecContext(ctx, `INSERT INTO StudyGroups (Name, ShortDescription, LongDescription, Mode, MeetingPlace, MeetingTime) VALUES (?,?,?,?,?,?)`, name, g.description, "Demo group for testing StudySync matching. These are sample students and meetings, not an organized event.", g.mode, "Online (demo)", g.time)
			if err != nil {
				return err
			}
			id, err = result.LastInsertId()
			if err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO StudyGroupEnrollment VALUES (?,?,'ADMIN')`, ids[g.owner], id); err != nil {
			return err
		}
		for _, name := range g.topics {
			topic, err := topicID(name)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO StudyGroupInterests VALUES (?,?)`, id, topic); err != nil {
				return err
			}
		}
		for _, member := range g.members {
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO StudyGroupEnrollment VALUES (?,?,'USER')`, ids[member], id); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}
