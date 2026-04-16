package server

import (
	"errors"
	"net/http"
	"time"

	"github.com/pfortini/debeasy/internal/store"
)

func (s *Server) handleSetupForm(w http.ResponseWriter, r *http.Request) {
	n, err := s.store.Users.Count(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if n > 0 {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	s.rend.Render(w, 200, "setup.html", &LayoutData{Title: "Set up debeasy"})
}

func (s *Server) handleSetupSubmit(w http.ResponseWriter, r *http.Request) {
	n, err := s.store.Users.Count(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if n > 0 {
		http.Error(w, "setup already completed", http.StatusForbidden)
		return
	}
	username := r.FormValue("username")
	password := r.FormValue("password")
	confirm := r.FormValue("confirm")
	if password != confirm {
		s.rend.Render(w, 400, "setup.html", &LayoutData{Title: "Set up debeasy", Err: "passwords don't match"})
		return
	}
	u, err := s.store.Users.Create(r.Context(), username, password, "admin")
	if err != nil {
		s.rend.Render(w, 400, "setup.html", &LayoutData{Title: "Set up debeasy", Err: err.Error()})
		return
	}
	if err := s.startSession(w, r, u); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) handleLoginForm(w http.ResponseWriter, r *http.Request) {
	if u := CurrentUser(r); u != nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	s.rend.Render(w, 200, "login.html", &LayoutData{Title: "Sign in"})
}

func (s *Server) handleLoginSubmit(w http.ResponseWriter, r *http.Request) {
	if !s.rl.Allow(clientIP(r)) {
		http.Error(w, "too many attempts — try again in a minute", http.StatusTooManyRequests)
		return
	}
	u, err := s.store.Users.Verify(r.Context(), r.FormValue("username"), r.FormValue("password"))
	if err != nil {
		msg := "invalid credentials"
		if errors.Is(err, store.ErrDisabled) {
			msg = "your account is disabled"
		}
		s.rend.Render(w, 401, "login.html", &LayoutData{Title: "Sign in", Err: msg})
		return
	}
	if err := s.startSession(w, r, u); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	next := r.FormValue("next")
	if next == "" || next[0] != '/' {
		next = "/"
	}
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if sess := CurrentSession(r); sess != nil {
		_ = s.store.Sessions.Delete(r.Context(), sess.ID)
	}
	clearSessionCookie(w, s.cookieSecure(r))
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (s *Server) startSession(w http.ResponseWriter, r *http.Request, u *store.User) error {
	sess, err := s.store.Sessions.Create(r.Context(), u.ID, 30*24*time.Hour)
	if err != nil {
		return err
	}
	return setSessionCookie(w, s.sc, sess.ID, s.cookieSecure(r))
}

// cookieSecure returns true if request was over TLS or X-Forwarded-Proto says so.
func (s *Server) cookieSecure(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	if p := r.Header.Get("X-Forwarded-Proto"); p == "https" {
		return true
	}
	return false
}
