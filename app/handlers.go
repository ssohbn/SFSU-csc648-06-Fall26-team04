package main

import (
	"bytes"
	"cmp"
	"context"
	"database/sql"
	"embed"
	"errors"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

//go:embed templates static
var files embed.FS

type app struct {
	store *store
	pages map[string]*template.Template
}

func newApp(db *sql.DB) (*app, error) {
	pages, err := parsePages()
	if err != nil {
		return nil, err
	}
	return &app{store: &store{db: db}, pages: pages}, nil
}

// parsePages pairs every page template with its own copy of the base layout,
// so each page can define "content" without clashing with the others.
func parsePages() (map[string]*template.Template, error) {
	names, err := fs.Glob(files, "templates/*.html")
	if err != nil {
		return nil, err
	}
	pages := map[string]*template.Template{}
	for _, name := range names {
		if name == "templates/base.html" {
			continue
		}
		t, err := template.ParseFS(files, "templates/base.html", name)
		if err != nil {
			return nil, err
		}
		pages[path.Base(name)] = t
	}
	return pages, nil
}

func (a *app) routes() http.Handler {
	static, err := fs.Sub(files, "static")
	if err != nil {
		panic(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", a.home)
	mux.HandleFunc("GET /about/", a.about)
	mux.HandleFunc("GET /interests/", a.interests)
	mux.HandleFunc("POST /interests/", a.createInterest)
	mux.HandleFunc("POST /interests/{id}/add", a.changeInterest(a.store.addUserInterest))
	mux.HandleFunc("POST /interests/{id}/remove", a.changeInterest(a.store.removeUserInterest))
	mux.HandleFunc("GET /signup", a.signup)
	mux.HandleFunc("POST /signup", a.signup)
	mux.HandleFunc("GET /signin", a.signin)
	mux.HandleFunc("POST /signin", a.signin)
	mux.HandleFunc("POST /signout", a.signout)
	mux.HandleFunc("GET /me", a.profile)
	mux.HandleFunc("POST /me", a.profile)
	mux.HandleFunc("GET /me/groups", a.myGroups)
	mux.HandleFunc("GET /groups/new", a.newGroup)
	mux.HandleFunc("POST /groups/new", a.newGroup)
	mux.HandleFunc("GET /groups/{id}", a.group)
	mux.HandleFunc("GET /groups/{id}/edit", a.editGroup)
	mux.HandleFunc("POST /groups/{id}/edit", a.editGroup)
	mux.HandleFunc("POST /groups/{id}/delete", a.deleteGroup)
	mux.HandleFunc("POST /groups/{id}/join", a.joinGroup)
	mux.HandleFunc("POST /groups/{id}/leave", a.leaveGroup)
	mux.HandleFunc("POST /groups/{id}/invite", a.resetInvite)
	mux.HandleFunc("POST /groups/{id}/members/{user}/remove", a.removeMember)
	mux.HandleFunc("GET /up", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok\n"))
	})
	staticFiles := http.StripPrefix("/static/", http.FileServerFS(static))
	mux.Handle("GET /static/", staticFiles)
	mux.HandleFunc("GET /static/vendor/", func(w http.ResponseWriter, r *http.Request) {
		// vendored files have their version in the path, so browsers can keep them
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		staticFiles.ServeHTTP(w, r)
	})
	mux.HandleFunc("/", a.notFound)

	// browsers mark cross-site form posts, so this blocks csrf without tokens
	return a.withUser(http.NewCrossOriginProtection().Handler(mux))
}

// view is what the shared layout needs; every page's data embeds it.
type view struct {
	Title  string
	Path   string
	User   *User
	Notice string
	Error  string
	Map    bool // the page draws a map, so load maplibre
}

// notices are picked by ?ok= from a fixed list, so a link can't put its own
// text on the page.
var notices = map[string]string{
	"welcome":    "Your account is ready. Add a few interests so groups can find you.",
	"signedout":  "You're signed out.",
	"saved":      "Profile saved.",
	"interest":   "Added to your interests.",
	"created":    "Your group is ready. Share the page so people can join.",
	"groupsaved": "Group saved.",
	"joined":     "You joined the group.",
	"left":       "You left the group.",
	"removed":    "Member removed.",
	"deleted":    "Group deleted.",
	"invite":     "New invite link made. The old one no longer works.",
}

func newView(r *http.Request, title string) view {
	return view{Title: title, Path: r.URL.Path, User: currentUser(r), Notice: notices[r.URL.Query().Get("ok")]}
}

func isHTMXRequest(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true"
}

// render executes into a buffer first, so a template error becomes a clean 500
// instead of half a page.
func (a *app) render(w http.ResponseWriter, status int, page, name string, data any) {
	var buf bytes.Buffer
	if err := a.pages[page].ExecuteTemplate(&buf, name, data); err != nil {
		log.Printf("render %s: %v", page, err)
		http.Error(w, "something went wrong, please try again", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	buf.WriteTo(w)
}

func (a *app) errorPage(w http.ResponseWriter, r *http.Request, status int, title, message string) {
	type errorView struct {
		view
		Message string
	}
	a.render(w, status, "error.html", "base", errorView{view: newView(r, title), Message: message})
}

func (a *app) notFound(w http.ResponseWriter, r *http.Request) {
	a.errorPage(w, r, http.StatusNotFound, "Page not found", "We couldn't find that page.")
}

func (a *app) serverError(w http.ResponseWriter, r *http.Request, err error) {
	log.Printf("%s %s: %v", r.Method, r.URL.Path, err)
	a.errorPage(w, r, http.StatusInternalServerError, "Something went wrong", "Please try again in a moment.")
}

func (a *app) about(w http.ResponseWriter, r *http.Request) {
	a.render(w, http.StatusOK, "about.html", "base", newView(r, "About us"))
}

func (a *app) interests(w http.ResponseWriter, r *http.Request) {
	a.renderInterests(w, r, http.StatusOK, r.FormValue("query"), "")
}

func (a *app) renderInterests(w http.ResponseWriter, r *http.Request, status int, query, errMsg string) {
	var userID int64
	if user := currentUser(r); user != nil {
		userID = user.ID
	}
	interests, err := a.store.searchInterests(r.Context(), query, userID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}

	type interestsView struct {
		view
		Query     string
		Interests []Interest
		Back      string // where add and remove return to
		NewName   string // offered as a new interest when nothing matches exactly
	}
	v := interestsView{view: newView(r, "Interests"), Query: query, Interests: interests, Back: "/interests/"}
	v.Error = errMsg
	if query != "" {
		v.Back += "?query=" + url.QueryEscape(query)
	}
	name := cleanName(query)
	if userID != 0 && validInterestName(name) && !slices.ContainsFunc(interests, func(i Interest) bool { return strings.EqualFold(i.Name, name) }) {
		v.NewName = name
	}

	page := "base"
	if isHTMXRequest(r) {
		page = "interest-rows"
	}
	w.Header().Add("Vary", "HX-Request")
	a.render(w, status, "interests.html", page, v)
}

// cleanName trims and collapses spaces, so "  Linear   Algebra " and
// "Linear Algebra" are the same interest.
func cleanName(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func validInterestName(name string) bool {
	n := utf8.RuneCountInString(name)
	return n >= 2 && n <= 40
}

func (a *app) createInterest(w http.ResponseWriter, r *http.Request) {
	user, ok := requireUser(w, r, "/interests/")
	if !ok {
		return
	}
	name := cleanName(r.PostFormValue("name"))
	if !validInterestName(name) {
		a.renderInterests(w, r, http.StatusUnprocessableEntity, name, "Interest names are 2 to 40 characters.")
		return
	}
	if err := a.store.createUserInterest(r.Context(), user.ID, name); err != nil {
		a.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/interests/?ok=interest&query="+url.QueryEscape(name), http.StatusSeeOther)
}

// changeInterest adds or removes one of the signed-in user's interests, then
// goes back to the page the button was on.
func (a *app) changeInterest(change func(ctx context.Context, userID, interestID int64) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		next := cmp.Or(safeNext(r.PostFormValue("next")), "/interests/")
		user, ok := requireUser(w, r, next)
		if !ok {
			return
		}
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			a.notFound(w, r)
			return
		}
		if err := change(r.Context(), user.ID, id); err != nil {
			a.serverError(w, r, err)
			return
		}
		http.Redirect(w, r, next, http.StatusSeeOther)
	}
}

func (a *app) profile(w http.ResponseWriter, r *http.Request) {
	user, ok := requireUser(w, r, "/me")
	if !ok {
		return
	}
	type profileView struct {
		view
		Email, Bio string
		School     string
		Interests  []Interest
	}
	v := profileView{view: newView(r, "Your profile"), Email: user.Email, Bio: user.Bio, School: schoolDomain(user.Email)}
	status := http.StatusOK

	if r.Method == http.MethodPost {
		v.Email, v.Bio = strings.TrimSpace(r.PostFormValue("email")), strings.TrimSpace(r.PostFormValue("bio"))
		email, emailOK := cleanEmail(v.Email)
		switch {
		case !emailOK:
			v.Error = "Enter a valid email address."
		case utf8.RuneCountInString(v.Bio) > 500:
			v.Error = "Keep your bio to 500 characters."
		default:
			err := a.store.updateProfile(r.Context(), user.ID, email, v.Bio)
			if errors.Is(err, errEmailTaken) {
				v.Error = "Another account already uses that email."
				break
			}
			if err != nil {
				a.serverError(w, r, err)
				return
			}
			http.Redirect(w, r, "/me?ok=saved", http.StatusSeeOther)
			return
		}
		status = http.StatusUnprocessableEntity
	}

	interests, err := a.store.userInterests(r.Context(), user.ID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	v.Interests = interests
	a.render(w, status, "me.html", "base", v)
}
