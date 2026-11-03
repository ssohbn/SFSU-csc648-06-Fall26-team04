package main

import (
	"cmp"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"net/mail"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

const (
	sessionCookie = "session"
	sessionTTL    = 30 * 24 * time.Hour
)

type userKey struct{}

// withUser loads the signed-in user, if any, for currentUser.
func (a *app) withUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie(sessionCookie); err == nil {
			user, err := a.store.userBySession(r.Context(), hashToken(c.Value), time.Now())
			switch {
			case err == nil:
				r = r.WithContext(context.WithValue(r.Context(), userKey{}, &user))
			case !errors.Is(err, sql.ErrNoRows):
				log.Printf("session lookup: %v", err)
			}
		}
		next.ServeHTTP(w, r)
	})
}

func currentUser(r *http.Request) *User {
	user, _ := r.Context().Value(userKey{}).(*User)
	return user
}

// requireUser sends guests to sign in first, then on to next.
func requireUser(w http.ResponseWriter, r *http.Request, next string) (*User, bool) {
	if user := currentUser(r); user != nil {
		return user, true
	}
	http.Redirect(w, r, "/signin?next="+url.QueryEscape(next), http.StatusSeeOther)
	return nil, false
}

func hashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

func (a *app) startSession(w http.ResponseWriter, r *http.Request, userID int64) error {
	token := rand.Text()
	expires := time.Now().Add(sessionTTL)
	if err := a.store.createSession(r.Context(), hashToken(token), userID, expires); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: token, Path: "/", Expires: expires,
		HttpOnly: true, Secure: isHTTPS(r), SameSite: http.SameSiteLaxMode,
	})
	return nil
}

func isHTTPS(r *http.Request) bool {
	return r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
}

// safeNext keeps redirects after sign in on this site. Browsers read "//x" and
// "/\x" as another host, and url.Parse rejects control characters.
func safeNext(next string) string {
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.Contains(next, `\`) {
		return ""
	}
	if u, err := url.Parse(next); err != nil || u.Host != "" {
		return ""
	}
	return next
}

var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{3,30}$`)

func cleanEmail(s string) (string, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	addr, err := mail.ParseAddress(s)
	if err != nil || addr.Address != s || len(s) > 254 {
		return "", false
	}
	_, domain, _ := strings.Cut(s, "@")
	return s, strings.Contains(domain, ".")
}

// dummyHash hashes a random password nobody knows. checking it makes a sign
// in for a missing account cost as much as a wrong password, so response
// times don't reveal which usernames exist.
const dummyHash = "$2a$10$cHvw9lX.yV98L3HMTTakBOHZrPXJehNbFaYIFRtAk0JGnyeM4bQHi"

func (a *app) signup(w http.ResponseWriter, r *http.Request) {
	type signupView struct {
		view
		Next, Username, Email string
	}
	v := signupView{
		view:     newView(r, "Sign up"),
		Next:     safeNext(r.FormValue("next")),
		Username: strings.TrimSpace(r.FormValue("username")),
		Email:    strings.TrimSpace(r.FormValue("email")),
	}
	if r.Method != http.MethodPost {
		a.render(w, http.StatusOK, "signup.html", "base", v)
		return
	}

	email, emailOK := cleanEmail(v.Email)
	password := r.PostFormValue("password")
	switch {
	case !usernamePattern.MatchString(v.Username):
		v.Error = "Usernames are 3 to 30 letters, numbers, dots, dashes or underscores."
	case !emailOK:
		v.Error = "Enter a valid email address."
	case utf8.RuneCountInString(password) < 8:
		v.Error = "Passwords need at least 8 characters."
	case len(password) > 72: // bcrypt ignores anything past 72 bytes
		v.Error = "That password is too long."
	case password != r.PostFormValue("confirm"):
		v.Error = "The passwords don't match."
	}
	if v.Error == "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			a.serverError(w, r, err)
			return
		}
		id, err := a.store.createUser(r.Context(), v.Username, email, string(hash))
		switch {
		case errors.Is(err, errUsernameTaken):
			v.Error = "That username is taken."
		case errors.Is(err, errEmailTaken):
			v.Error = "An account with that email already exists. Try signing in."
		case err != nil:
			a.serverError(w, r, err)
			return
		default:
			if err := a.startSession(w, r, id); err != nil {
				a.serverError(w, r, err)
				return
			}
			http.Redirect(w, r, cmp.Or(v.Next, "/interests/?ok=welcome"), http.StatusSeeOther)
			return
		}
	}
	a.render(w, http.StatusUnprocessableEntity, "signup.html", "base", v)
}

func (a *app) signin(w http.ResponseWriter, r *http.Request) {
	type signinView struct {
		view
		Next, Login string
	}
	v := signinView{
		view:  newView(r, "Sign in"),
		Next:  safeNext(r.FormValue("next")),
		Login: strings.TrimSpace(r.FormValue("login")),
	}
	if r.Method != http.MethodPost {
		a.render(w, http.StatusOK, "signin.html", "base", v)
		return
	}

	id, hash, err := a.store.passwordHash(r.Context(), v.Login)
	if errors.Is(err, sql.ErrNoRows) {
		hash = dummyHash
	} else if err != nil {
		a.serverError(w, r, err)
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(r.PostFormValue("password"))) != nil || id == 0 {
		v.Error = "Wrong username, email or password."
		a.render(w, http.StatusUnprocessableEntity, "signin.html", "base", v)
		return
	}
	if err := a.startSession(w, r, id); err != nil {
		a.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, cmp.Or(v.Next, "/"), http.StatusSeeOther)
}

func (a *app) signout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		if err := a.store.deleteSession(r.Context(), hashToken(c.Value)); err != nil {
			a.serverError(w, r, err)
			return
		}
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: isHTTPS(r), SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/?ok=signedout", http.StatusSeeOther)
}
