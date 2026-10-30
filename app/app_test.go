package main

import (
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
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

// client is a browser stand-in: it keeps cookies and stops at redirects so
// tests can check where they point.
type client struct {
	t    *testing.T
	base string
	http *http.Client
}

func newClient(t *testing.T, srv *httptest.Server) *client {
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &client{t: t, base: srv.URL, http: &http.Client{
		Jar:           jar,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

// do returns the status, body and redirect target of one request.
func (c *client) do(req *http.Request) (int, string, string) {
	c.t.Helper()
	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		c.t.Fatal(err)
	}
	return resp.StatusCode, string(body), resp.Header.Get("Location")
}

func (c *client) get(path string) (int, string, string) {
	c.t.Helper()
	req, err := http.NewRequest(http.MethodGet, c.base+path, nil)
	if err != nil {
		c.t.Fatal(err)
	}
	return c.do(req)
}

func (c *client) post(path string, form url.Values) (int, string, string) {
	c.t.Helper()
	req, err := http.NewRequest(http.MethodPost, c.base+path, strings.NewReader(form.Encode()))
	if err != nil {
		c.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return c.do(req)
}

// signup creates an account and leaves the client signed in.
func (c *client) signup(username, email string) {
	c.t.Helper()
	code, body, _ := c.post("/signup", url.Values{
		"username": {username}, "email": {email}, "password": {"correct horse"}, "confirm": {"correct horse"},
	})
	if code != http.StatusSeeOther {
		c.t.Fatalf("signup %s: %d %s", username, code, body)
	}
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

	code, body := get(t, srv.URL+"/?query=CALC")
	if code != http.StatusOK || !strings.Contains(body, "Calc II") || strings.Contains(body, "Databases") {
		t.Fatalf("search by name: %d %s", code, body)
	}
	if !strings.Contains(body, `value="CALC"`) {
		t.Fatalf("search box should keep the query: %s", body)
	}

	_, body = get(t, srv.URL+"/?query=100%25")
	if !strings.Contains(body, "Databases") || strings.Contains(body, "Calc II") {
		t.Fatalf("percent sign should match literally: %s", body)
	}

	_, body = get(t, srv.URL+"/?query=sql", "HX-Request", "true")
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
	code, body := get(t, srv.URL+"/nope")
	if code != http.StatusNotFound || !strings.Contains(body, "Page not found") || !strings.Contains(body, "site-header") {
		t.Errorf("unknown path should get the styled 404: %d %s", code, body)
	}
	if _, body := get(t, srv.URL+"/about/"); !strings.Contains(body, "<title>About us - StudySync</title>") {
		t.Errorf("about title: %s", body)
	}
}
