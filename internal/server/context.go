package server

import (
	"context"
	"net/http"

	"github.com/pfortini/debeasy/internal/store"
)

type ctxKey int

const (
	ctxKeyUser ctxKey = iota
	ctxKeySession
	ctxKeyCSRF
)

func withUser(r *http.Request, u *store.User) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), ctxKeyUser, u))
}

func withSession(r *http.Request, s *store.Session) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), ctxKeySession, s))
}

// CurrentUser returns the authenticated user, or nil if anonymous.
func CurrentUser(r *http.Request) *store.User {
	v, _ := r.Context().Value(ctxKeyUser).(*store.User)
	return v
}

// CurrentSession returns the current session, or nil if anonymous.
func CurrentSession(r *http.Request) *store.Session {
	v, _ := r.Context().Value(ctxKeySession).(*store.Session)
	return v
}

func CSRFToken(r *http.Request) string {
	if s := CurrentSession(r); s != nil {
		return s.CSRFToken
	}
	return ""
}
