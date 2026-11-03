package main

import (
	"net/url"
	"os"
	"strings"
	"testing"
)

func TestGeminiLiveMatching(t *testing.T) {
	if os.Getenv("STUDYSYNC_GEMINI_LIVE") != "1" {
		t.Skip("set STUDYSYNC_GEMINI_LIVE=1 for one real Gemini request")
	}
	m := newGeminiMatcher(os.Getenv("GEMINI_API_KEY"), os.Getenv("GEMINI_MODEL"))
	if m == nil {
		t.Fatal("GEMINI_API_KEY is required for live verification")
	}
	c, a := matchingFixture(t, m)
	exec(t, a.store, `INSERT INTO Interests (Id,Name) VALUES (1,'Databases')`)
	exec(t, a.store, `INSERT INTO UserInterests VALUES (1,1)`)
	exec(t, a.store, `INSERT INTO StudyGroups (Id,Name,ShortDescription,Mode,MeetingTime) VALUES
		(1,'SQL Thursday practice','CSC 675 SQL queries and database workshop preparation','OPEN','Every Thursday 16:00–17:00 America/Los_Angeles'),
		(2,'Conflicting SQL practice','CSC 675 SQL queries and database workshop preparation','OPEN','Every Thursday 20:00–21:00 America/Los_Angeles')`)
	exec(t, a.store, `INSERT INTO StudyGroupInterests VALUES (1,1),(2,1)`)
	code, body, _ := c.post("/matches", url.Values{
		"courses":      {"CSC 675 Databases"},
		"availability": {"I am available ONLY Thursdays 15:00–18:00 America/Los_Angeles. I am unavailable outside this window."},
		"preferences":  {"Practice SQL queries and prepare for database workshops."},
	})
	if code != 200 {
		t.Fatalf("live local POST /matches returned HTTP %d; successful live verification is incomplete", code)
	}
	if !strings.Contains(body, `href="/groups/1"`) || strings.Contains(body, `href="/groups/2"`) {
		t.Fatal("live matching must recommend the eligible SQL group and exclude the explicit time conflict")
	}
	t.Log("Real authenticated local HTTP -> SQLite -> Gemini -> rendered eligible group passed; explicit schedule conflict excluded")
}
