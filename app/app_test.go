package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func newTestServer(t *testing.T) (*httptest.Server, *store) {
	t.Helper()
	db, err := openDB(filepath.Join(t.TempDir(), "test.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	a, err := newApp(db)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(a.routes())
	t.Cleanup(srv.Close)
	return srv, a.store
}

func get(t *testing.T, url string, header ...string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, string(body)
}

func exec(t *testing.T, s *store, query string, args ...any) {
	t.Helper()
	if _, err := s.db.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}

func TestMigrationsRunOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "twice.sqlite3")
	for range 2 {
		db, err := openDB(path)
		if err != nil {
			t.Fatal(err)
		}
		db.Close()
	}
}

func TestGroupSearch(t *testing.T) {
	srv, s := newTestServer(t)
	exec(t, s, `INSERT INTO StudyGroups (Name, ShortDescription, LongDescription) VALUES
		('Calc II', 'integrals every week', ''),
		('Databases', 'sql practice', 'we meet at 100% capacity')`)

	code, body := get(t, srv.URL+"/studygroups/?query=CALC")
	if code != http.StatusOK || !strings.Contains(body, "Calc II") || strings.Contains(body, "Databases") {
		t.Fatalf("search by name: %d %s", code, body)
	}

	_, body = get(t, srv.URL+"/studygroups/?query=100%25")
	if !strings.Contains(body, "Databases") || strings.Contains(body, "Calc II") {
		t.Fatalf("percent sign should match literally: %s", body)
	}

	_, body = get(t, srv.URL+"/studygroups/?query=sql", "HX-Request", "true")
	if strings.Contains(body, "<html") || !strings.Contains(body, "Databases") {
		t.Fatalf("htmx request should get only rows: %s", body)
	}
}

func TestInterestSearch(t *testing.T) {
	srv, s := newTestServer(t)
	exec(t, s, `INSERT INTO Interests (Name) VALUES ('math'), ('art')`)

	_, body := get(t, srv.URL+"/interests/?query=MAT", "HX-Request", "true")
	if !strings.Contains(body, "math") || strings.Contains(body, "art") {
		t.Fatalf("got %s", body)
	}
	_, body = get(t, srv.URL+"/interests/?query=zzz", "HX-Request", "true")
	if !strings.Contains(body, "No interests found.") {
		t.Fatalf("got %s", body)
	}
}

func TestPagesAndStatic(t *testing.T) {
	srv, _ := newTestServer(t)
	for _, path := range []string{"/", "/about/", "/up", "/static/style.css", "/static/js/htmx.min.js"} {
		if code, _ := get(t, srv.URL+path); code != http.StatusOK {
			t.Errorf("%s: got %d", path, code)
		}
	}
	if code, _ := get(t, srv.URL+"/nope"); code != http.StatusNotFound {
		t.Errorf("unknown path: got %d", code)
	}
}
