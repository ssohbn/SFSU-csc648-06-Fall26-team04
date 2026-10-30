package main

import (
	"bytes"
	"database/sql"
	"embed"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"path"
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
	mux.HandleFunc("GET /up", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok\n"))
	})
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(static)))
	mux.HandleFunc("/", a.notFound)
	return mux
}

// view is what the shared layout needs; every page's data embeds it.
type view struct {
	Title string
	Path  string
}

func newView(r *http.Request, title string) view {
	return view{Title: title, Path: r.URL.Path}
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

func (a *app) home(w http.ResponseWriter, r *http.Request) {
	query := r.FormValue("query")
	groups, err := a.store.searchGroups(r.Context(), query)
	if err != nil {
		a.serverError(w, r, err)
		return
	}

	type homeView struct {
		view
		Query  string
		Groups []StudyGroup
	}

	// send only the rows when htmx asks, and never cache one as the other
	name := "base"
	if isHTMXRequest(r) {
		name = "rows"
	}
	w.Header().Add("Vary", "HX-Request")
	a.render(w, http.StatusOK, "home.html", name, homeView{view: newView(r, ""), Query: query, Groups: groups})
}

func (a *app) about(w http.ResponseWriter, r *http.Request) {
	a.render(w, http.StatusOK, "about.html", "base", newView(r, "About us"))
}

func (a *app) interests(w http.ResponseWriter, r *http.Request) {
	query := r.FormValue("query")
	interests, err := a.store.searchInterests(r.Context(), query)
	if err != nil {
		a.serverError(w, r, err)
		return
	}

	type interestsView struct {
		view
		Query     string
		Interests []Interest
	}

	name := "base"
	if isHTMXRequest(r) {
		name = "interest-rows"
	}
	w.Header().Add("Vary", "HX-Request")
	a.render(w, http.StatusOK, "interests.html", name, interestsView{view: newView(r, "Interests"), Query: query, Interests: interests})
}
