package server

import (
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/securecookie"
)

const sessionCookie = "debeasy_sess"

func (s *Server) sessionMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var sid string
		if c, err := r.Cookie(sessionCookie); err == nil {
			_ = s.sc.Decode(sessionCookie, c.Value, &sid)
		}
		if sid != "" {
			sess, err := s.store.Sessions.Get(r.Context(), sid)
			if err == nil {
				u, err := s.store.Users.Get(r.Context(), sess.UserID)
				if err == nil && !u.Disabled {
					r = withSession(r, sess)
					r = withUser(r, u)
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

// requireAuth returns a 302 to /login when no user is in the context.
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if CurrentUser(r) == nil {
			// for HTMX, return a header redirect so the browser navigates
			if IsHTMX(r) {
				w.Header().Set("HX-Redirect", "/login")
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := CurrentUser(r)
		if u == nil || !u.IsAdmin() {
			http.Error(w, "admin required", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// CSRF: double-submit token. Required on every non-GET/HEAD/OPTIONS request when a session exists.
func (s *Server) csrfMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		sess := CurrentSession(r)
		if sess == nil {
			// allow unauthenticated POSTs (login, setup) — those handlers do their own checks
			next.ServeHTTP(w, r)
			return
		}
		token := r.Header.Get("X-CSRF-Token")
		if token == "" {
			_ = r.ParseForm()
			token = r.FormValue("csrf_token")
		}
		if token == "" || token != sess.CSRFToken {
			http.Error(w, "invalid CSRF token", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// statusRecorder lets the logging middleware know the response status code.
type statusRecorder struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (s *statusRecorder) WriteHeader(code int) {
	if s.wrote {
		return
	}
	s.status = code
	s.wrote = true
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if !s.wrote {
		s.status = 200
		s.wrote = true
	}
	return s.ResponseWriter.Write(b)
}

func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func loggingMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w, status: 200}
			next.ServeHTTP(rec, r)
			logger.Info("http",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.status,
				"dur_ms", time.Since(start).Milliseconds(),
			)
		})
	}
}

func recoverMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rv := recover(); rv != nil {
					logger.Error("panic", "err", rv, "stack", string(debug.Stack()))
					http.Error(w, "internal error", http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// loginRateLimiter — simple in-memory token bucket per IP, used only on /login POST.
type loginRateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*ipBucket
}

type ipBucket struct {
	tokens   int
	lastFill time.Time
}

func newLoginRateLimiter() *loginRateLimiter {
	return &loginRateLimiter{buckets: map[string]*ipBucket{}}
}

func (l *loginRateLimiter) Allow(ip string) bool {
	const maxTokens = 10
	const refillPerSec = 0.1 // 6/min
	l.mu.Lock()
	defer l.mu.Unlock()
	b, ok := l.buckets[ip]
	now := time.Now()
	if !ok {
		b = &ipBucket{tokens: maxTokens, lastFill: now}
		l.buckets[ip] = b
	}
	elapsed := now.Sub(b.lastFill).Seconds()
	add := int(elapsed * refillPerSec)
	if add > 0 {
		b.tokens += add
		if b.tokens > maxTokens {
			b.tokens = maxTokens
		}
		b.lastFill = now
	}
	if b.tokens <= 0 {
		return false
	}
	b.tokens--
	return true
}

func clientIP(r *http.Request) string {
	if xf := r.Header.Get("X-Forwarded-For"); xf != "" {
		if i := strings.IndexByte(xf, ','); i > 0 {
			return strings.TrimSpace(xf[:i])
		}
		return strings.TrimSpace(xf)
	}
	host := r.RemoteAddr
	if i := strings.LastIndex(host, ":"); i > 0 {
		return host[:i]
	}
	return host
}

// configures cookie codec from app secret
func newCookieCodec(hashKey, blockKey []byte) *securecookie.SecureCookie {
	sc := securecookie.New(hashKey, blockKey)
	sc.MaxAge(int((30 * 24 * time.Hour).Seconds()))
	return sc
}

// helper to set the session cookie on the response
func setSessionCookie(w http.ResponseWriter, sc *securecookie.SecureCookie, sessID string, secure bool) error {
	enc, err := sc.Encode(sessionCookie, sessID)
	if err != nil {
		return err
	}
	cookie := &http.Cookie{
		Name:     sessionCookie,
		Value:    enc,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int((30 * 24 * time.Hour).Seconds()),
	}
	http.SetCookie(w, cookie)
	return nil
}

// session cleared on logout
func clearSessionCookie(w http.ResponseWriter, secure bool) {
	cookie := &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	}
	http.SetCookie(w, cookie)
}
