package main

import (
	"crypto/subtle"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

func groupPath(id int64, invite string) string {
	p := "/groups/" + strconv.FormatInt(id, 10)
	if invite != "" {
		p += "?invite=" + url.QueryEscape(invite)
	}
	return p
}

// visibleGroups is every group the signed-in user, or a guest, can see.
func (a *app) visibleGroups(r *http.Request) ([]StudyGroup, error) {
	user := currentUser(r)
	var userID int64
	if user != nil {
		userID = user.ID
	}
	groups, err := a.store.groups(r.Context(), userID, 0)
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(groups, func(g StudyGroup) bool {
		return !canView(g, access{user: user})
	}), nil
}

func (a *app) home(w http.ResponseWriter, r *http.Request) {
	groups, err := a.visibleGroups(r)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	user := currentUser(r)
	q := r.URL.Query()

	type homeView struct {
		view
		Query       string
		Topics      []Interest     // every topic on a group in the list, for the filter
		Picked      map[int64]bool // topics ticked in the filter
		Match, Mine bool
		Groups      []StudyGroup
	}
	v := homeView{
		view:   newView(r, ""),
		Query:  q.Get("query"),
		Picked: map[int64]bool{},
		Match:  user != nil && q.Get("match") == "1",
		Mine:   user != nil && q.Get("mine") == "1",
	}
	v.Map = true
	for _, id := range q["topic"] {
		if n, err := strconv.ParseInt(id, 10, 64); err == nil {
			v.Picked[n] = true
		}
	}
	myInterests := map[int64]bool{}
	if v.Match {
		interests, err := a.store.userInterests(r.Context(), user.ID)
		if err != nil {
			a.serverError(w, r, err)
			return
		}
		for _, i := range interests {
			myInterests[i.ID] = true
		}
	}

	query := strings.ToLower(strings.TrimSpace(v.Query))
	for _, g := range groups {
		for _, t := range g.Interests {
			if !slices.ContainsFunc(v.Topics, func(i Interest) bool { return i.ID == t.ID }) {
				v.Topics = append(v.Topics, t)
			}
		}
		if matchesQuery(g, query) && (len(v.Picked) == 0 || hasTopic(g, v.Picked)) &&
			(!v.Match || hasTopic(g, myInterests)) && (!v.Mine || g.Role != "") {
			v.Groups = append(v.Groups, g)
		}
	}
	slices.SortFunc(v.Topics, func(x, y Interest) int {
		return strings.Compare(strings.ToLower(x.Name), strings.ToLower(y.Name))
	})

	// send only the rows when htmx asks, and never cache one as the other
	name := "base"
	if isHTMXRequest(r) {
		name = "rows"
	}
	w.Header().Add("Vary", "HX-Request")
	a.render(w, http.StatusOK, "home.html", name, v)
}

// matchesQuery looks for lowercase query in everything a group shows.
func matchesQuery(g StudyGroup, query string) bool {
	text := []string{g.Name, g.ShortDescription, g.LongDescription, g.MeetingPlace, g.MeetingTime}
	for _, t := range g.Interests {
		text = append(text, t.Name)
	}
	return slices.ContainsFunc(text, func(s string) bool { return strings.Contains(strings.ToLower(s), query) })
}

func hasTopic(g StudyGroup, ids map[int64]bool) bool {
	return slices.ContainsFunc(g.Interests, func(i Interest) bool { return ids[i.ID] })
}

// loadGroup finds the group in the URL and what the viewer brings to it. If
// the viewer can't see the group, it answers the request and returns false.
func (a *app) loadGroup(w http.ResponseWriter, r *http.Request, invite string) (StudyGroup, access, bool) {
	acc := access{user: currentUser(r)}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		a.notFound(w, r)
		return StudyGroup{}, acc, false
	}
	var userID int64
	if acc.user != nil {
		userID = acc.user.ID
	}
	groups, err := a.store.groups(r.Context(), userID, id)
	if err != nil {
		a.serverError(w, r, err)
		return StudyGroup{}, acc, false
	}
	if len(groups) == 0 {
		a.notFound(w, r)
		return StudyGroup{}, acc, false
	}

	g := groups[0]
	acc.inviteOK = invite != "" && subtle.ConstantTimeCompare([]byte(invite), []byte(g.inviteCode)) == 1
	if acc.user != nil {
		mine, err := a.store.userInterests(r.Context(), userID)
		if err != nil {
			a.serverError(w, r, err)
			return StudyGroup{}, acc, false
		}
		for i, t := range g.Interests {
			g.Interests[i].Mine = slices.ContainsFunc(mine, func(m Interest) bool { return m.ID == t.ID })
			acc.sharesTopic = acc.sharesTopic || g.Interests[i].Mine
		}
	}
	if !canView(g, acc) {
		// signing in could open these two, so say so instead of hiding them
		if acc.user == nil && (g.Mode == modeAccount || g.Mode == modeSchool) {
			http.Redirect(w, r, "/signin?next="+url.QueryEscape(groupPath(g.ID, "")), http.StatusSeeOther)
		} else {
			a.notFound(w, r)
		}
		return StudyGroup{}, acc, false
	}
	return g, acc, true
}

// ownGroup is loadGroup for actions only the group's admin can take.
func (a *app) ownGroup(w http.ResponseWriter, r *http.Request) (StudyGroup, bool) {
	if _, ok := requireUser(w, r, "/groups/"+r.PathValue("id")); !ok {
		return StudyGroup{}, false
	}
	g, _, ok := a.loadGroup(w, r, "")
	if ok && g.Role != "ADMIN" {
		a.notFound(w, r)
		return StudyGroup{}, false
	}
	return g, ok
}

func (a *app) group(w http.ResponseWriter, r *http.Request) {
	invite := r.URL.Query().Get("invite")
	if g, acc, ok := a.loadGroup(w, r, invite); ok {
		a.renderGroup(w, r, http.StatusOK, g, acc, invite, "")
	}
}

func (a *app) renderGroup(w http.ResponseWriter, r *http.Request, status int, g StudyGroup, acc access, invite, problem string) {
	type groupView struct {
		view
		Group      StudyGroup
		Mode       modeChoice
		Invite     string // a valid invite code, kept on join and sign in links
		Next       string // where signing in comes back to
		CanJoin    bool
		NeedTopic  bool // signed in, but missing the topic an interest group asks for
		Members    []Member
		InviteLink string
	}
	v := groupView{view: newView(r, g.Name), Group: g}
	v.Error = problem
	v.Map = g.Pin != nil
	v.Mode, _ = modeInfo(g.Mode)
	if acc.inviteOK {
		v.Invite = invite
	}
	v.Next = groupPath(g.ID, v.Invite)
	if acc.user != nil && g.Role == "" {
		v.CanJoin = canJoin(g, acc)
		v.NeedTopic = !v.CanJoin
	}
	if g.Role != "" {
		members, err := a.store.members(r.Context(), g.ID)
		if err != nil {
			a.serverError(w, r, err)
			return
		}
		v.Members = members
	}
	if g.Role == "ADMIN" && g.Mode == modeInvite {
		scheme := "http"
		if isHTTPS(r) {
			scheme = "https"
		}
		v.InviteLink = scheme + "://" + r.Host + groupPath(g.ID, g.inviteCode)
	}
	a.render(w, status, "group.html", "base", v)
}

func (a *app) joinGroup(w http.ResponseWriter, r *http.Request) {
	invite := r.PostFormValue("invite")
	g, acc, ok := a.loadGroup(w, r, invite)
	if !ok {
		return
	}
	if _, ok := requireUser(w, r, groupPath(g.ID, invite)); !ok {
		return
	}
	if g.Role != "" {
		http.Redirect(w, r, groupPath(g.ID, ""), http.StatusSeeOther)
		return
	}
	if !canJoin(g, acc) {
		a.renderGroup(w, r, http.StatusUnprocessableEntity, g, acc, invite, "Add one of this group's topics to your profile to join.")
		return
	}
	if err := a.store.joinGroup(r.Context(), acc.user.ID, g.ID); err != nil {
		a.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, groupPath(g.ID, "")+"?ok=joined", http.StatusSeeOther)
}

func (a *app) leaveGroup(w http.ResponseWriter, r *http.Request) {
	g, acc, ok := a.loadGroup(w, r, "")
	if !ok {
		return
	}
	if _, ok := requireUser(w, r, groupPath(g.ID, "")); !ok {
		return
	}
	if g.Role == "" {
		http.Redirect(w, r, groupPath(g.ID, ""), http.StatusSeeOther)
		return
	}
	if g.Role == "ADMIN" {
		a.renderGroup(w, r, http.StatusUnprocessableEntity, g, acc, "", "You run this group, so you can't leave it. You can delete it from the edit page.")
		return
	}
	if err := a.store.removeMember(r.Context(), acc.user.ID, g.ID); err != nil {
		a.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/me/groups?ok=left", http.StatusSeeOther)
}

func (a *app) removeMember(w http.ResponseWriter, r *http.Request) {
	g, ok := a.ownGroup(w, r)
	if !ok {
		return
	}
	userID, err := strconv.ParseInt(r.PathValue("user"), 10, 64)
	if err != nil {
		a.notFound(w, r)
		return
	}
	if err := a.store.removeMember(r.Context(), userID, g.ID); err != nil {
		a.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, groupPath(g.ID, "")+"?ok=removed", http.StatusSeeOther)
}

func (a *app) resetInvite(w http.ResponseWriter, r *http.Request) {
	g, ok := a.ownGroup(w, r)
	if !ok {
		return
	}
	if err := a.store.resetInvite(r.Context(), g.ID); err != nil {
		a.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, groupPath(g.ID, "")+"?ok=invite", http.StatusSeeOther)
}

func (a *app) deleteGroup(w http.ResponseWriter, r *http.Request) {
	g, ok := a.ownGroup(w, r)
	if !ok {
		return
	}
	if r.PostFormValue("confirm") != "yes" {
		a.renderGroupForm(w, r, http.StatusUnprocessableEntity, g, topicList(g), "Tick the box to confirm you want to delete this group.")
		return
	}
	if err := a.store.deleteGroup(r.Context(), g.ID); err != nil {
		a.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/me/groups?ok=deleted", http.StatusSeeOther)
}

func (a *app) newGroup(w http.ResponseWriter, r *http.Request) {
	user, ok := requireUser(w, r, "/groups/new")
	if !ok {
		return
	}
	if r.Method != http.MethodPost {
		a.renderGroupForm(w, r, http.StatusOK, StudyGroup{Mode: modeOpen}, "", "")
		return
	}
	g, topics, problem := readGroupForm(r, user, StudyGroup{})
	if problem != "" {
		a.renderGroupForm(w, r, http.StatusUnprocessableEntity, g, r.PostFormValue("topics"), problem)
		return
	}
	id, err := a.store.createGroup(r.Context(), user.ID, g, topics)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, groupPath(id, "")+"?ok=created", http.StatusSeeOther)
}

func (a *app) editGroup(w http.ResponseWriter, r *http.Request) {
	g, ok := a.ownGroup(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodPost {
		a.renderGroupForm(w, r, http.StatusOK, g, topicList(g), "")
		return
	}
	g, topics, problem := readGroupForm(r, currentUser(r), g)
	if problem != "" {
		a.renderGroupForm(w, r, http.StatusUnprocessableEntity, g, r.PostFormValue("topics"), problem)
		return
	}
	if err := a.store.updateGroup(r.Context(), g, topics); err != nil {
		a.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, groupPath(g.ID, "")+"?ok=groupsaved", http.StatusSeeOther)
}

func (a *app) renderGroupForm(w http.ResponseWriter, r *http.Request, status int, g StudyGroup, topics, problem string) {
	type groupFormView struct {
		view
		Group  StudyGroup
		Topics string
		Modes  []modeChoice
		School string // the user's school, "" without an .edu email
	}
	title := "Start a group"
	if g.ID != 0 {
		title = "Edit group"
	}
	v := groupFormView{view: newView(r, title), Group: g, Topics: topics, Modes: modeChoices, School: schoolDomain(currentUser(r).Email)}
	v.Error = problem
	v.Map = true
	a.render(w, status, "groupform.html", "base", v)
}

func topicList(g StudyGroup) string {
	names := make([]string, len(g.Interests))
	for i, t := range g.Interests {
		names[i] = t.Name
	}
	return strings.Join(names, ", ")
}

// readGroupForm returns the group as submitted, its cleaned topics, and what's
// wrong with it, if anything.
func readGroupForm(r *http.Request, user *User, g StudyGroup) (StudyGroup, []string, string) {
	g.Name = cleanName(r.PostFormValue("name"))
	g.ShortDescription = strings.TrimSpace(r.PostFormValue("short"))
	g.LongDescription = strings.TrimSpace(r.PostFormValue("long"))
	g.MeetingPlace = strings.TrimSpace(r.PostFormValue("place"))
	g.MeetingTime = strings.TrimSpace(r.PostFormValue("time"))
	g.Mode = r.PostFormValue("mode")
	g.SchoolDomain = ""
	if g.Mode == modeSchool {
		g.SchoolDomain = schoolDomain(user.Email)
	}

	// the map fills these in, both or neither
	g.Pin = nil
	pinOK := true
	if lat, lng := r.PostFormValue("lat"), r.PostFormValue("lng"); lat != "" || lng != "" {
		la, errLat := strconv.ParseFloat(lat, 64)
		ln, errLng := strconv.ParseFloat(lng, 64)
		// the comparisons are false for NaN, so it's rejected too
		pinOK = errLat == nil && errLng == nil && math.Abs(la) <= 90 && math.Abs(ln) <= 180
		if pinOK {
			g.Pin = &Pin{la, ln}
		}
	}

	var topics []string
	for _, t := range strings.Split(r.PostFormValue("topics"), ",") {
		t = cleanName(t)
		if t != "" && !slices.ContainsFunc(topics, func(s string) bool { return strings.EqualFold(s, t) }) {
			topics = append(topics, t)
		}
	}

	n := utf8.RuneCountInString
	_, modeOK := modeInfo(g.Mode)
	problem := ""
	switch {
	case n(g.Name) < 3 || n(g.Name) > 80:
		problem = "Group names are 3 to 80 characters."
	case g.ShortDescription == "" || n(g.ShortDescription) > 140:
		problem = "Add a short description of up to 140 characters."
	case n(g.LongDescription) > 2000:
		problem = "Keep the long description to 2000 characters."
	case n(g.MeetingPlace) > 120:
		problem = "Keep the meeting place to 120 characters."
	case n(g.MeetingTime) > 80:
		problem = "Keep the meeting time to 80 characters."
	case !pinOK:
		problem = "Drop the pin on the map again."
	case len(topics) > 8:
		problem = "Use up to 8 topics."
	case slices.ContainsFunc(topics, func(t string) bool { return !validInterestName(t) }):
		problem = "Topics are 2 to 40 characters each."
	case !modeOK:
		problem = "Pick who can join."
	case g.Mode == modeInterest && len(topics) == 0:
		problem = "Shared interest groups need at least one topic."
	case g.Mode == modeSchool && g.SchoolDomain == "":
		problem = "School groups need a .edu email on your profile."
	}
	return g, topics, problem
}

func (a *app) myGroups(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUser(w, r, "/me/groups"); !ok {
		return
	}
	groups, err := a.visibleGroups(r)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	type myGroupsView struct {
		view
		Groups []StudyGroup
	}
	v := myGroupsView{view: newView(r, "My groups")}
	v.Groups = slices.DeleteFunc(groups, func(g StudyGroup) bool { return g.Role == "" })
	a.render(w, http.StatusOK, "mygroups.html", "base", v)
}
