package main

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// createGroup posts the new group form, filling in what form leaves out, and
// returns the group's page.
func (c *client) createGroup(form url.Values) string {
	c.t.Helper()
	full := url.Values{"name": {"Study group"}, "short": {"we study"}, "mode": {modeOpen}}
	for k, v := range form {
		full[k] = v
	}
	code, body, loc := c.post("/groups/new", full)
	if code != http.StatusSeeOther || !strings.HasSuffix(loc, "?ok=created") {
		c.t.Fatalf("create group: %d %q %s", code, loc, body)
	}
	return strings.TrimSuffix(loc, "?ok=created")
}

func TestSchoolDomain(t *testing.T) {
	for email, want := range map[string]string{
		"a@sfsu.edu":      "sfsu.edu",
		"a@mail.SFSU.edu": "sfsu.edu",
		"a@gmail.com":     "",
		"a@edu":           "",
		"a@.edu":          "",
		"a@sfsu.edu.au":   "",
	} {
		if got := schoolDomain(email); got != want {
			t.Errorf("schoolDomain(%q) = %q, want %q", email, got, want)
		}
	}
}

func TestAccessRules(t *testing.T) {
	sid := &User{ID: 1, Email: "sid@mail.sfsu.edu"}
	gus := &User{ID: 2, Email: "gus@gmail.com"}
	school := StudyGroup{Mode: modeSchool, SchoolDomain: "sfsu.edu"}
	for _, tt := range []struct {
		name       string
		group      StudyGroup
		access     access
		view, join bool
	}{
		{"open guest", StudyGroup{Mode: modeOpen}, access{}, true, false},
		{"open user", StudyGroup{Mode: modeOpen}, access{user: gus}, true, true},
		{"account guest", StudyGroup{Mode: modeAccount}, access{}, false, false},
		{"account user", StudyGroup{Mode: modeAccount}, access{user: gus}, true, true},
		{"invite without code", StudyGroup{Mode: modeInvite}, access{user: gus}, false, false},
		{"invite with code", StudyGroup{Mode: modeInvite}, access{user: gus, inviteOK: true}, true, true},
		{"invite guest with code", StudyGroup{Mode: modeInvite}, access{inviteOK: true}, true, false},
		{"interest without topic", StudyGroup{Mode: modeInterest}, access{user: gus}, true, false},
		{"interest with topic", StudyGroup{Mode: modeInterest}, access{user: gus, sharesTopic: true}, true, true},
		{"school same domain", school, access{user: sid}, true, true},
		{"school other domain", school, access{user: gus}, false, false},
		{"school guest", school, access{}, false, false},
		{"school with no domain", StudyGroup{Mode: modeSchool}, access{user: gus}, false, false},
		{"unknown mode", StudyGroup{Mode: "NOPE"}, access{user: gus}, false, false},
	} {
		if got := canView(tt.group, tt.access); got != tt.view {
			t.Errorf("%s: canView = %v", tt.name, got)
		}
		if got := canJoin(tt.group, tt.access); got != tt.join {
			t.Errorf("%s: canJoin = %v", tt.name, got)
		}
	}
	for _, mode := range []string{modeAccount, modeInvite, modeSchool} {
		if !canView(StudyGroup{Mode: mode, SchoolDomain: "sfsu.edu", Role: "USER"}, access{user: gus}) {
			t.Errorf("%s: members should always see their group", mode)
		}
	}
}

func TestGroupVisibility(t *testing.T) {
	srv, s := newTestServer(t)
	owner := newClient(t, srv)
	owner.signup("olive", "olive@mail.sfsu.edu")
	paths := map[string]string{}
	for _, mode := range []string{modeOpen, modeAccount, modeInvite, modeInterest, modeSchool} {
		paths[mode] = owner.createGroup(url.Values{"name": {"Group " + mode}, "mode": {mode}, "topics": {"graph theory"}})
	}

	guest := newClient(t, srv)
	gus := newClient(t, srv)
	gus.signup("gus", "gus@gmail.com")
	sid := newClient(t, srv)
	sid.signup("sid", "sid@sfsu.edu")
	viewers := []*client{guest, gus, sid}

	// page status for guest, gus and sid; search lists exactly the 200s
	for mode, want := range map[string][3]int{
		modeOpen:     {200, 200, 200},
		modeAccount:  {303, 200, 200},
		modeInvite:   {404, 404, 404},
		modeInterest: {200, 200, 200},
		modeSchool:   {303, 404, 200},
	} {
		for i, c := range viewers {
			code, _, loc := c.get(paths[mode])
			if code != want[i] {
				t.Errorf("%s viewer %d: got %d, want %d", mode, i, code, want[i])
			}
			if code == http.StatusSeeOther && loc != "/signin?next="+url.QueryEscape(paths[mode]) {
				t.Errorf("%s viewer %d: redirect to %q", mode, i, loc)
			}
			_, home, _ := c.get("/")
			if listed := strings.Contains(home, ">Group "+mode+"</a>"); listed != (want[i] == 200) {
				t.Errorf("%s viewer %d: listed in search = %v", mode, i, listed)
			}
		}
	}

	var code string
	if err := s.db.QueryRow(`SELECT InviteCode FROM StudyGroups WHERE Mode = 'INVITE'`).Scan(&code); err != nil {
		t.Fatal(err)
	}
	if status, body, _ := guest.get(paths[modeInvite] + "?invite=" + code); status != http.StatusOK || !strings.Contains(body, "Sign up to join") {
		t.Errorf("guest with the invite link: %d", status)
	}
	if status, body, _ := gus.get(paths[modeInvite] + "?invite=" + code); status != http.StatusOK || !strings.Contains(body, `name="invite" value="`+code+`"`) {
		t.Errorf("user with the invite link should get a join button carrying the code: %d %s", status, body)
	}
	if status, _, _ := gus.get(paths[modeInvite] + "?invite=WRONG"); status != http.StatusNotFound {
		t.Errorf("wrong invite code: %d", status)
	}
	if _, body, _ := owner.get(paths[modeInvite]); !strings.Contains(body, "?invite="+code) {
		t.Errorf("the owner should see the invite link")
	}
	if _, body, _ := gus.get(paths[modeInvite] + "?invite=" + code); strings.Contains(body, "Invite link") {
		t.Errorf("only the owner sees the invite panel")
	}
	if _, body, _ := owner.get("/"); !strings.Contains(body, ">Group INVITE</a>") {
		t.Errorf("members should find their invite-only groups in search")
	}
}

func TestGroupMembership(t *testing.T) {
	srv, s := newTestServer(t)
	owner := newClient(t, srv)
	owner.signup("olive", "olive@sfsu.edu")
	open := owner.createGroup(url.Values{"name": {"Open group"}})
	interest := owner.createGroup(url.Values{"name": {"Graphs"}, "mode": {modeInterest}, "topics": {"Graph Theory"}})
	invite := owner.createGroup(url.Values{"name": {"Secret"}, "mode": {modeInvite}})
	gus := newClient(t, srv)
	gus.signup("gus", "gus@gmail.com")
	members := func(path string) int {
		t.Helper()
		var n int
		id := strings.TrimPrefix(path, "/groups/")
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM StudyGroupEnrollment WHERE StudyGroupID = ?`, id).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	if code, _, loc := newClient(t, srv).post(open+"/join", nil); code != http.StatusSeeOther || !strings.HasPrefix(loc, "/signin?next=") {
		t.Errorf("guest join should go to sign in: %d %q", code, loc)
	}
	for range 2 {
		if code, _, loc := gus.post(open+"/join", nil); code != http.StatusSeeOther || !strings.HasPrefix(loc, open) {
			t.Fatalf("join: %d %q", code, loc)
		}
	}
	if n := members(open); n != 2 {
		t.Errorf("joining twice should count once: %d members", n)
	}
	if _, body, _ := gus.get(open); !strings.Contains(body, "Leave group") || !strings.Contains(body, "olive") {
		t.Errorf("members see the leave button and the member list: %s", body)
	}

	code, body, _ := gus.post(interest+"/join", nil)
	if code != http.StatusUnprocessableEntity || !strings.Contains(body, "Add one of this group&#39;s topics") {
		t.Errorf("interest group without a shared topic: %d", code)
	}
	var topicID string
	if err := s.db.QueryRow(`SELECT Id FROM Interests WHERE Name = 'graph theory'`).Scan(&topicID); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, `action="/interests/`+topicID+`/add"`) {
		t.Errorf("the page should offer to add the missing topic")
	}
	gus.post("/interests/"+topicID+"/add", url.Values{"next": {interest}})
	if code, _, _ := gus.post(interest+"/join", nil); code != http.StatusSeeOther {
		t.Errorf("join after adding the topic: %d", code)
	}

	var inviteCode string
	if err := s.db.QueryRow(`SELECT InviteCode FROM StudyGroups WHERE Name = 'Secret'`).Scan(&inviteCode); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := gus.post(invite+"/join", nil); code != http.StatusNotFound {
		t.Errorf("invite join without the code: %d", code)
	}
	if code, _, _ := gus.post(invite+"/join", url.Values{"invite": {inviteCode}}); code != http.StatusSeeOther {
		t.Errorf("invite join with the code: %d", code)
	}
	owner.post(invite+"/invite", nil)
	if code, _, _ := newClient(t, srv).get(invite + "?invite=" + inviteCode); code != http.StatusNotFound {
		t.Errorf("an old invite link should stop working: %d", code)
	}
	if code, _, _ := gus.get(invite); code != http.StatusOK {
		t.Errorf("members keep access after a new link: %d", code)
	}

	for _, action := range []string{"/edit", "/delete", "/invite", "/members/1/remove"} {
		if code, _, _ := gus.post(invite+action, url.Values{"confirm": {"yes"}}); code != http.StatusNotFound {
			t.Errorf("non-owner %s: %d", action, code)
		}
	}
	if code, _, _ := gus.get(invite + "/edit"); code != http.StatusNotFound {
		t.Errorf("non-owner edit page: %d", code)
	}

	if code, body, _ := owner.post(open+"/leave", nil); code != http.StatusUnprocessableEntity || !strings.Contains(body, "can&#39;t leave") {
		t.Errorf("owner leave: %d", code)
	}
	if code, _, loc := gus.post(open+"/leave", nil); code != http.StatusSeeOther || loc != "/me/groups?ok=left" || members(open) != 1 {
		t.Errorf("leave: %d %q", code, loc)
	}
	if code, _, loc := gus.post(open+"/leave", nil); code != http.StatusSeeOther || loc != open {
		t.Errorf("leaving a group you're not in should just show it: %d %q", code, loc)
	}

	var gusID string
	if err := s.db.QueryRow(`SELECT Id FROM Users WHERE Username = 'gus'`).Scan(&gusID); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := owner.post(invite+"/members/"+gusID+"/remove", nil); code != http.StatusSeeOther {
		t.Errorf("remove member: %d", code)
	}
	if code, _, _ := gus.get(invite); code != http.StatusNotFound {
		t.Errorf("a removed member of an invite group loses access: %d", code)
	}

	if code, _, _ := owner.post(interest+"/delete", nil); code != http.StatusUnprocessableEntity || members(interest) != 2 {
		t.Errorf("delete without confirming: %d", code)
	}
	if code, _, loc := owner.post(interest+"/delete", url.Values{"confirm": {"yes"}}); code != http.StatusSeeOther || loc != "/me/groups?ok=deleted" {
		t.Errorf("delete: %d %q", code, loc)
	}
	if code, _, _ := owner.get(interest); code != http.StatusNotFound || members(interest) != 0 {
		t.Errorf("deleted group should be gone with its members: %d", code)
	}

	_, body, _ = owner.get("/me/groups")
	if !strings.Contains(body, ">Open group</a>") || !strings.Contains(body, ">Secret</a>") || strings.Contains(body, ">Graphs</a>") {
		t.Errorf("my groups: %s", body)
	}
	if code, _, loc := newClient(t, srv).get("/me/groups"); code != http.StatusSeeOther || !strings.HasPrefix(loc, "/signin") {
		t.Errorf("guest my groups: %d %q", code, loc)
	}
}

func TestGroupForm(t *testing.T) {
	srv, s := newTestServer(t)
	gus := newClient(t, srv)
	gus.signup("gus", "gus@gmail.com")
	if code, _, loc := newClient(t, srv).get("/groups/new"); code != http.StatusSeeOther || loc != "/signin?next=%2Fgroups%2Fnew" {
		t.Errorf("guest new group: %d %q", code, loc)
	}

	for _, tt := range []struct {
		field, value, want string
	}{
		{"name", "ab", "Group names are 3 to 80 characters."},
		{"short", " ", "Add a short description"},
		{"long", strings.Repeat("x", 2001), "Keep the long description"},
		{"place", strings.Repeat("x", 121), "Keep the meeting place"},
		{"mode", "NOPE", "Pick who can join."},
		{"mode", modeInterest, "Shared interest groups need at least one topic."},
		{"mode", modeSchool, "School groups need a .edu email"},
		{"topics", "a, b", "Topics are 2 to 40 characters each."},
		{"topics", "aa,bb,cc,dd,ee,ff,gg,hh,ii", "Use up to 8 topics."},
	} {
		form := url.Values{"name": {"Calc"}, "short": {"integrals"}, "mode": {modeOpen}}
		form.Set(tt.field, tt.value)
		code, body, _ := gus.post("/groups/new", form)
		if code != http.StatusUnprocessableEntity || !strings.Contains(body, tt.want) {
			t.Errorf("%s=%.20q: got %d, want %q", tt.field, tt.value, code, tt.want)
		}
	}

	path := gus.createGroup(url.Values{"name": {"  Calc   II "}, "topics": {" Linear   Algebra, linear algebra,SQL "}})
	_, body, _ := gus.get(path + "/edit")
	if !strings.Contains(body, `value="Calc II"`) || !strings.Contains(body, `value="Linear Algebra, SQL"`) {
		t.Errorf("edit form should hold the cleaned name and topics: %s", body)
	}
	code, _, loc := gus.post(path+"/edit", url.Values{
		"name": {"Calc III"}, "short": {"vectors"}, "mode": {modeAccount}, "topics": {"SQL"}, "place": {"Library"},
	})
	if code != http.StatusSeeOther || loc != path+"?ok=groupsaved" {
		t.Fatalf("edit: %d %q", code, loc)
	}
	_, body, _ = gus.get(path)
	if !strings.Contains(body, "Calc III") || !strings.Contains(body, "Signed-in only") || strings.Contains(body, "Linear Algebra") {
		t.Errorf("edited group page: %s", body)
	}

	sid := newClient(t, srv)
	sid.signup("sid", "sid@mail.sfsu.edu")
	sid.createGroup(url.Values{"name": {"SFSU only"}, "mode": {modeSchool}})
	var domain string
	if err := s.db.QueryRow(`SELECT SchoolDomain FROM StudyGroups WHERE Name = 'SFSU only'`).Scan(&domain); err != nil || domain != "sfsu.edu" {
		t.Errorf("school group domain: %q %v", domain, err)
	}
}

func TestHomeFilters(t *testing.T) {
	srv, s := newTestServer(t)
	olive := newClient(t, srv)
	olive.signup("olive", "olive@gmail.com")
	olive.createGroup(url.Values{"name": {"Calc Club"}, "topics": {"Calculus"}, "place": {"Library"}})
	olive.createGroup(url.Values{"name": {"SQL Club"}, "topics": {"Databases"}})
	gus := newClient(t, srv)
	gus.signup("gus", "gus@gmail.com")
	gus.createGroup(url.Values{"name": {"Art Club"}, "topics": {"Drawing"}})
	gus.post("/interests/", url.Values{"name": {"databases"}})

	var calcID string
	if err := s.db.QueryRow(`SELECT Id FROM Interests WHERE Name = 'calculus'`).Scan(&calcID); err != nil {
		t.Fatal(err)
	}
	for query, want := range map[string]string{
		"?query=library":        "Calc Club",
		"?query=DATABASES":      "SQL Club",
		"?topic=" + calcID:      "Calc Club",
		"?match=1":              "SQL Club",
		"?mine=1":               "Art Club",
		"?mine=1&query=nope":    "",
		"?topic=x&query=+draw+": "Art Club",
	} {
		_, body, _ := gus.get("/" + query)
		_, rows, _ := gus.do(htmxGet(t, srv.URL+"/"+query))
		for _, name := range []string{"Calc Club", "SQL Club", "Art Club"} {
			if strings.Contains(rows, ">"+name+"</a>") != (name == want) {
				t.Errorf("%s: %s listed = %v", query, name, !(name == want))
			}
		}
		if !strings.Contains(body, `name="topic" value="`+calcID+`"`) {
			t.Errorf("%s: the topic filter should list every topic", query)
		}
	}
	if _, body, _ := newClient(t, srv).get("/?mine=1"); !strings.Contains(body, ">Art Club</a>") || strings.Contains(body, `name="mine"`) {
		t.Errorf("guests get no personal filters")
	}
}

func htmxGet(t *testing.T, url string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("HX-Request", "true")
	return req
}

func TestSeed(t *testing.T) {
	_, s := newTestServer(t)
	for range 2 {
		if err := seed(context.Background(), s); err != nil {
			t.Fatal(err)
		}
	}
	var groups, users int
	s.db.QueryRow(`SELECT COUNT(*) FROM StudyGroups`).Scan(&groups)
	s.db.QueryRow(`SELECT COUNT(*) FROM Users`).Scan(&users)
	if groups != 8 || users != 5 {
		t.Errorf("seeding twice should add the demo data once: %d groups, %d users", groups, users)
	}
	all, err := s.groups(context.Background(), 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	modes := map[string]bool{}
	for _, g := range all {
		modes[g.Mode] = true
		if g.Members < 1 || len(g.Interests) == 0 {
			t.Errorf("%s: %d members, topics %v", g.Name, g.Members, g.Interests)
		}
	}
	if len(modes) != len(modeChoices) {
		t.Errorf("the demo should show every mode: %v", modes)
	}
}
