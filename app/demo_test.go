package main

import (
	"context"
	"net/url"
	"strings"
	"testing"
)

func TestMatchingDemoPreservesExistingDataAndIsRepeatable(t *testing.T) {
	srv, s := newTestServer(t)
	c := newClient(t, srv)
	c.signup("real_student", "real@example.com")
	path := c.createGroup(url.Values{"name": {"My existing group"}, "topics": {"Databases"}})
	counts := func() [4]int {
		var result [4]int
		for i, table := range []string{"Users", "StudyGroups", "StudyGroupEnrollment", "Interests"} {
			if err := s.db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&result[i]); err != nil {
				t.Fatal(err)
			}
		}
		return result
	}
	if err := seedMatchingDemo(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	first := counts()
	if first[0] != 9 || first[1] != 13 {
		t.Fatalf("expected 8 demo users and 12 demo groups alongside existing data: %v", first)
	}
	if err := seedMatchingDemo(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	if after := counts(); after != first {
		t.Fatalf("repeat seed duplicated data: %v -> %v", first, after)
	}
	if code, body, _ := c.get(path); code != 200 || !strings.Contains(body, "My existing group") || !strings.Contains(body, "Edit group") {
		t.Fatal("existing group or ownership changed")
	}
	if code, body, _ := c.get("/?q=CSC+667"); code != 200 || !strings.Contains(body, "Demo · CSC 667 Thursday web lab") {
		t.Fatal("seeded course group is not discoverable")
	}
	var bad int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM Users WHERE Username LIKE 'demo.%' AND (Email NOT LIKE '%@studysync.invalid' OR Bio NOT LIKE 'Demo profile%')`).Scan(&bad); err != nil || bad != 0 {
		t.Fatalf("unlabelled demo identities: %d %v", bad, err)
	}
}

func TestMatchingDemoRollsBackOnExistingIdentityCollision(t *testing.T) {
	_, s := newTestServer(t)
	exec(t, s, `INSERT INTO Users (Username,Email,PasswordHash,Bio) VALUES ('demo.noor','real@example.com','original','real student bio')`)
	if err := seedMatchingDemo(context.Background(), s); err == nil {
		t.Fatal("must not adopt a real account using a demo username")
	}
	var users, groups, interests int
	if err := s.db.QueryRow(`SELECT (SELECT COUNT(*) FROM Users), (SELECT COUNT(*) FROM StudyGroups), (SELECT COUNT(*) FROM Interests)`).Scan(&users, &groups, &interests); err != nil {
		t.Fatal(err)
	}
	if users != 1 || groups != 0 || interests != 0 {
		t.Fatalf("failed seed left partial data: %d users, %d groups, %d interests", users, groups, interests)
	}
	var bio string
	if err := s.db.QueryRow(`SELECT Bio FROM Users`).Scan(&bio); err != nil || bio != "real student bio" {
		t.Fatalf("real profile changed: %q %v", bio, err)
	}
}
