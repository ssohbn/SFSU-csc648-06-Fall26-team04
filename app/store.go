package main

import (
	"context"
	"database/sql"
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
	ID   int64
	Name string
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

func (s *store) searchInterests(ctx context.Context, query string) ([]Interest, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT Id, Name FROM Interests WHERE instr(lower(Name), lower(?)) > 0 ORDER BY Name`, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var interests []Interest
	for rows.Next() {
		var i Interest
		if err := rows.Scan(&i.ID, &i.Name); err != nil {
			return nil, err
		}
		interests = append(interests, i)
	}
	return interests, rows.Err()
}
