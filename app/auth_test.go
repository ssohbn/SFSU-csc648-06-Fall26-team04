package main

import (
	"bytes"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

func TestSignupSigninSignout(t *testing.T) {
	srv, s := newTestServer(t)
	c := newClient(t, srv)

	if code, _, loc := c.get("/me"); code != http.StatusSeeOther || loc != "/signin?next=%2Fme" {
		t.Fatalf("guest /me: %d %q", code, loc)
	}

	code, _, loc := c.post("/signup", url.Values{
		"username": {"Maeve"}, "email": {" Maeve@SFSU.edu "}, "password": {"correct horse"}, "confirm": {"correct horse"},
	})
	if code != http.StatusSeeOther || loc != "/interests/?ok=welcome" {
		t.Fatalf("signup: %d %q", code, loc)
	}
	if _, body, _ := c.get("/me"); !strings.Contains(body, "maeve@sfsu.edu") || !strings.Contains(body, "Sign out") {
		t.Fatalf("profile after signup: %s", body)
	}

	// the cookie is a random token and only its hash is stored
	var token string
	base, _ := url.Parse(srv.URL)
	for _, ck := range c.http.Jar.Cookies(base) {
		if ck.Name == sessionCookie {
			token = ck.Value
		}
	}
	var stored []byte
	if err := s.db.QueryRow(`SELECT TokenHash FROM Sessions`).Scan(&stored); err != nil || token == "" || !bytes.Equal(stored, hashToken(token)) {
		t.Fatalf("session storage: token %q, err %v", token, err)
	}
	var hash string
	s.db.QueryRow(`SELECT PasswordHash FROM Users`).Scan(&hash)
	if !strings.HasPrefix(hash, "$2a$") {
		t.Fatalf("password should be a bcrypt hash, got %q", hash)
	}

	if code, _, loc := c.post("/signout", nil); code != http.StatusSeeOther || loc != "/?ok=signedout" {
		t.Fatalf("signout: %d %q", code, loc)
	}
	if code, _, _ := c.get("/me"); code != http.StatusSeeOther {
		t.Fatalf("still signed in after signout: %d", code)
	}
	var sessions int
	s.db.QueryRow(`SELECT COUNT(*) FROM Sessions`).Scan(&sessions)
	if sessions != 0 {
		t.Fatalf("signout should delete the session, %d left", sessions)
	}

	for _, login := range []string{"maeve", "MAEVE@sfsu.edu"} {
		c := newClient(t, srv)
		code, _, loc := c.post("/signin", url.Values{"login": {login}, "password": {"correct horse"}, "next": {"/interests/"}})
		if code != http.StatusSeeOther || loc != "/interests/" {
			t.Fatalf("signin as %s: %d %q", login, code, loc)
		}
	}

	code, body, _ := newClient(t, srv).post("/signin", url.Values{"login": {"maeve"}, "password": {"wrong password"}})
	if code != http.StatusUnprocessableEntity || !strings.Contains(body, "Wrong username, email or password.") || !strings.Contains(body, `value="maeve"`) {
		t.Fatalf("wrong password: %d %s", code, body)
	}
	code, body, _ = newClient(t, srv).post("/signin", url.Values{"login": {"nobody"}, "password": {"correct horse"}})
	if code != http.StatusUnprocessableEntity || !strings.Contains(body, "Wrong username, email or password.") {
		t.Fatalf("unknown user: %d %s", code, body)
	}
}

func TestSignupValidation(t *testing.T) {
	srv, _ := newTestServer(t)
	newClient(t, srv).signup("henry", "henry@sfsu.edu")

	for _, tc := range []struct {
		form url.Values
		want string
	}{
		{url.Values{"username": {"hi"}, "email": {"a@b.co"}, "password": {"password1"}, "confirm": {"password1"}}, "Usernames are 3 to 30"},
		{url.Values{"username": {"has space"}, "email": {"a@b.co"}, "password": {"password1"}, "confirm": {"password1"}}, "Usernames are 3 to 30"},
		{url.Values{"username": {"jerald"}, "email": {"not-an-email"}, "password": {"password1"}, "confirm": {"password1"}}, "valid email"},
		{url.Values{"username": {"jerald"}, "email": {"Jerald <j@b.co>"}, "password": {"password1"}, "confirm": {"password1"}}, "valid email"},
		{url.Values{"username": {"jerald"}, "email": {"j@b.co"}, "password": {"short"}, "confirm": {"short"}}, "at least 8"},
		{url.Values{"username": {"jerald"}, "email": {"j@b.co"}, "password": {strings.Repeat("x", 73)}, "confirm": {strings.Repeat("x", 73)}}, "too long"},
		{url.Values{"username": {"jerald"}, "email": {"j@b.co"}, "password": {"password1"}, "confirm": {"password2"}}, "don&#39;t match"},
		{url.Values{"username": {"HENRY"}, "email": {"j@b.co"}, "password": {"password1"}, "confirm": {"password1"}}, "username is taken"},
		{url.Values{"username": {"jerald"}, "email": {"Henry@SFSU.edu"}, "password": {"password1"}, "confirm": {"password1"}}, "already exists"},
	} {
		code, body, _ := newClient(t, srv).post("/signup", tc.form)
		if code != http.StatusUnprocessableEntity || !strings.Contains(body, tc.want) {
			t.Errorf("%v: want %q, got %d %s", tc.form, tc.want, code, body)
		}
		if strings.Contains(body, "password1") {
			t.Errorf("%v: password echoed back into the form", tc.form)
		}
	}
}

func TestSafeNext(t *testing.T) {
	for next, want := range map[string]string{
		"/groups/3":            "/groups/3",
		"/interests/?query=go": "/interests/?query=go",
		"":                     "",
		"https://evil.example": "",
		"//evil.example":       "",
		"///evil.example":      "",
		`/\evil.example`:       "",
		"/\t/evil.example":     "",
		"groups/3":             "",
	} {
		if got := safeNext(next); got != want {
			t.Errorf("safeNext(%q) = %q, want %q", next, got, want)
		}
	}

	srv, _ := newTestServer(t)
	newClient(t, srv).signup("simran", "simran@sfsu.edu")
	code, _, loc := newClient(t, srv).post("/signin", url.Values{"login": {"simran"}, "password": {"correct horse"}, "next": {"//evil.example"}})
	if code != http.StatusSeeOther || loc != "/" {
		t.Fatalf("open redirect: %d %q", code, loc)
	}
}

func TestCrossSiteFormsAreBlocked(t *testing.T) {
	srv, _ := newTestServer(t)
	c := newClient(t, srv)
	c.signup("shreya", "shreya@sfsu.edu")

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/signout", nil)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	if code, body, _ := c.do(req); code != http.StatusForbidden {
		t.Fatalf("cross-site post: %d %s", code, body)
	}
	if code, _, _ := c.get("/me"); code != http.StatusOK {
		t.Fatalf("blocked signout should keep the session: %d", code)
	}
}

func TestBigFormsAreNotRead(t *testing.T) {
	srv, _ := newTestServer(t)
	// past 1 MiB the form isn't parsed at all, so this signup has no username
	code, body, _ := newClient(t, srv).post("/signup", url.Values{
		"username": {"maeve"}, "email": {"maeve@sfsu.edu"}, "password": {"correct horse"}, "confirm": {"correct horse"},
		"pad": {strings.Repeat("x", 2<<20)},
	})
	if code != http.StatusUnprocessableEntity || !strings.Contains(body, "Usernames are 3 to 30") {
		t.Fatalf("got %d %.200s", code, body)
	}
}

func TestProfileAndInterests(t *testing.T) {
	srv, s := newTestServer(t)
	exec(t, s, `INSERT INTO Interests (Name) VALUES ('Calculus'), ('Databases')`)
	c := newClient(t, srv)
	c.signup("aleks", "aleks@sfsu.edu")

	if code, _, loc := c.post("/me", url.Values{"email": {"aleks@mail.sfsu.edu"}, "bio": {"likes sql"}}); code != http.StatusSeeOther || loc != "/me?ok=saved" {
		t.Fatalf("save profile: %d %q", code, loc)
	}
	if _, body, _ := c.get("/me?ok=saved"); !strings.Contains(body, "Profile saved.") || !strings.Contains(body, "likes sql") || !strings.Contains(body, "aleks@mail.sfsu.edu") {
		t.Fatalf("saved profile: %s", body)
	}
	if code, body, _ := c.post("/me", url.Values{"email": {"nope"}, "bio": {""}}); code != http.StatusUnprocessableEntity || !strings.Contains(body, "valid email") {
		t.Fatalf("bad email: %d %s", code, body)
	}
	if code, body, _ := c.post("/me", url.Values{"email": {"a@b.co"}, "bio": {strings.Repeat("x", 501)}}); code != http.StatusUnprocessableEntity || !strings.Contains(body, "500 characters") {
		t.Fatalf("long bio: %d %s", code, body)
	}

	var calc int64
	s.db.QueryRow(`SELECT Id FROM Interests WHERE Name = 'Calculus'`).Scan(&calc)
	add := "/interests/" + strconv.FormatInt(calc, 10) + "/add"
	if code, _, loc := c.post(add, url.Values{"next": {"/interests/?query=calc"}}); code != http.StatusSeeOther || loc != "/interests/?query=calc" {
		t.Fatalf("add interest: %d %q", code, loc)
	}
	c.post(add, nil) // adding twice is a no-op
	if _, body, _ := c.get("/me"); !strings.Contains(body, "Calculus") || strings.Contains(body, "Databases") {
		t.Fatalf("profile interests: %s", body)
	}
	var people int
	s.db.QueryRow(`SELECT COUNT(*) FROM UserInterests`).Scan(&people)
	if people != 1 {
		t.Fatalf("want 1 user interest, got %d", people)
	}

	if code, _, loc := c.post("/interests/", url.Values{"name": {"  Linear   Algebra "}}); code != http.StatusSeeOther || loc != "/interests/?ok=interest&query=Linear+Algebra" {
		t.Fatalf("create interest: %d %q", code, loc)
	}
	c.post("/interests/", url.Values{"name": {"linear algebra"}}) // same interest, different case
	var created int
	s.db.QueryRow(`SELECT COUNT(*) FROM Interests WHERE Name = 'linear algebra'`).Scan(&created)
	if created != 1 {
		t.Fatalf("want one Linear Algebra interest, got %d", created)
	}
	if code, body, _ := c.post("/interests/", url.Values{"name": {"x"}}); code != http.StatusUnprocessableEntity || !strings.Contains(body, "2 to 40") {
		t.Fatalf("short interest name: %d %s", code, body)
	}

	_, body, _ := c.get("/interests/?query=a")
	if !strings.Contains(body, "Remove") || !strings.Contains(body, "Add") {
		t.Fatalf("interest buttons: %s", body)
	}
	if _, body, _ := c.get("/interests/?query=Graph+Theory"); !strings.Contains(body, "Create &ldquo;Graph Theory&rdquo;") {
		t.Fatalf("create offer: %s", body)
	}

	c.post("/interests/"+strconv.FormatInt(calc, 10)+"/remove", url.Values{"next": {"/me"}})
	if _, body, _ := c.get("/me"); strings.Contains(body, "Calculus") {
		t.Fatalf("interest should be removed: %s", body)
	}

	guest := newClient(t, srv)
	if code, _, loc := guest.post(add, url.Values{"next": {"/interests/"}}); code != http.StatusSeeOther || loc != "/signin?next=%2Finterests%2F" {
		t.Fatalf("guest add: %d %q", code, loc)
	}
	if _, body, _ := guest.get("/interests/"); strings.Contains(body, "Add</button>") || !strings.Contains(body, "Sign in</a> to add interests") {
		t.Fatalf("guest interests page: %s", body)
	}
}
