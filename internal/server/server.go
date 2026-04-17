package server

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/securecookie"

	"github.com/pfortini/debeasy/internal/config"
	"github.com/pfortini/debeasy/internal/crypto"
	"github.com/pfortini/debeasy/internal/dbx"
	"github.com/pfortini/debeasy/internal/store"
	"github.com/pfortini/debeasy/internal/updates"
	"github.com/pfortini/debeasy/internal/version"
	"github.com/pfortini/debeasy/web"
)

type Server struct {
	cfg     *config.Config
	logger  *slog.Logger
	store   *store.Store
	pool    *dbx.Pool
	keyring *crypto.Keyring
	rend    *Renderer
	sc      *securecookie.SecureCookie
	rl      *loginRateLimiter
	srv     *http.Server
	updates *updates.Checker // nil when DEBEASY_UPDATE_CHECK=0
}

func New(cfg *config.Config, logger *slog.Logger) (*Server, error) {
	st, err := store.Open(cfg.StorePath())
	if err != nil {
		return nil, err
	}
	kr := crypto.NewKeyring(cfg.AppSecret)
	st.Connections.WithKeyring(kr)
	pool := dbx.NewPool(st.Connections)
	rend, err := NewRenderer()
	if err != nil {
		return nil, err
	}
	hashKey, blockKey := crypto.CookieKeys(cfg.AppSecret)
	sc := newCookieCodec(hashKey, blockKey)
	s := &Server{
		cfg: cfg, logger: logger, store: st, pool: pool, keyring: kr,
		rend: rend, sc: sc, rl: newLoginRateLimiter(),
	}
	if cfg.UpdateCheckEnabled && cfg.UpdateRepo != "" {
		s.updates = updates.New(cfg.UpdateRepo, version.Version, cfg.DataDir, nil)
		if err := s.updates.Load(); err != nil {
			logger.Debug("updates cache load", "err", err)
		}
	}
	s.srv = &http.Server{
		Addr:              cfg.Addr,
		Handler:           s.routes(),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	return s, nil
}

// Run serves HTTP until ctx is cancelled or the listener errors out. On cancel,
// it gracefully stops the server and tears down the pool + store. Delegates to
// serveOn, which tests can call with a pre-bound listener.
func (s *Server) Run(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.srv.Addr)
	if err != nil {
		return err
	}
	return s.serveOn(ctx, ln)
}

// serveOn drives the serve + janitor + graceful-shutdown loop on a supplied
// listener. Split from Run so tests can bind their own ephemeral port.
func (s *Server) serveOn(ctx context.Context, ln net.Listener) error {
	go s.runJanitor(ctx)
	if s.updates != nil {
		go s.runUpdateChecker(ctx)
	}

	errCh := make(chan error, 1)
	go func() {
		s.logger.Info("listening", "addr", ln.Addr().String())
		if err := s.srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		s.shutdown() //nolint:contextcheck // shutdown owns its own detached deadline; caller's ctx is already done
		return nil
	case err := <-errCh:
		return err
	}
}

// runJanitor purges expired sessions on a timer until ctx is cancelled. Extracted
// from Run so it's testable in isolation (tests poke tick via a short-lived ctx).
func (s *Server) runJanitor(ctx context.Context) {
	const every = 15 * time.Minute
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// detached context — this janitor runs independently of any request ctx
			_ = s.store.Sessions.PurgeExpired(context.Background()) //nolint:contextcheck // janitor is parent-ctx-independent by design
		}
	}
}

// runUpdateChecker polls GitHub for newer releases at cfg.UpdateCheckInterval.
// Errors are logged at Debug because the unauthenticated GitHub API rate-limits
// aggressively and noisy warnings would train operators to ignore real ones.
// An initial short delay keeps startup quick.
func (s *Server) runUpdateChecker(ctx context.Context) {
	const initialDelay = 30 * time.Second
	select {
	case <-ctx.Done():
		return
	case <-time.After(initialDelay):
	}
	s.tickUpdateCheck(ctx)

	ticker := time.NewTicker(s.cfg.UpdateCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.tickUpdateCheck(ctx)
		}
	}
}

func (s *Server) tickUpdateCheck(ctx context.Context) {
	if _, err := s.updates.Check(ctx); err != nil {
		s.logger.Debug("update check", "err", err)
	}
}

// shutdown gracefully stops the HTTP server, pool, and store. Safe to call once;
// callers should follow up with a fresh Server if they need to restart.
func (s *Server) shutdown() {
	// detached ctx — caller's ctx is already done
	shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second) //nolint:contextcheck // shutdown needs its own deadline
	defer cancel()
	_ = s.srv.Shutdown(shutCtx) //nolint:contextcheck // see above
	s.pool.Stop()
	_ = s.store.Close()
}

func (s *Server) routes() http.Handler {
	r := chi.NewRouter()
	r.Use(loggingMiddleware(s.logger))
	r.Use(recoverMiddleware(s.logger))
	r.Use(s.sessionMiddleware)
	r.Use(s.csrfMiddleware)

	// static + health
	staticFS, _ := fs.Sub(web.Static, "static")
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte("ok"))
	})

	// public
	r.Get("/setup", s.handleSetupForm)
	r.Post("/setup", s.handleSetupSubmit)
	r.Get("/login", s.handleLoginForm)
	r.Post("/login", s.handleLoginSubmit)
	r.Post("/logout", s.handleLogout)

	// authenticated
	r.Group(func(r chi.Router) {
		r.Use(s.requireAuth)
		r.Get("/", s.handleHome)
		r.Get("/connections", s.handleConnectionsList)
		r.Get("/connections/new", s.handleConnectionForm)
		r.Post("/connections", s.handleConnectionCreate)
		r.Get("/connections/{id}/edit", s.handleConnectionEditForm)
		r.Post("/connections/{id}", s.handleConnectionUpdate)
		r.Post("/connections/{id}/delete", s.handleConnectionDelete)
		r.Post("/connections/test", s.handleConnectionTest)

		r.Get("/conn/{id}", s.handleConnectionApp)
		r.Get("/conn/{id}/tree", s.handleTree)
		r.Get("/conn/{id}/object/{schema}/{name}", s.handleObjectDetail)
		r.Get("/conn/{id}/object/{schema}/{name}/data", s.handleObjectData)
		r.Post("/conn/{id}/query", s.handleQuery)
		r.Get("/conn/{id}/history", s.handleHistory)

		// create wizards
		r.Get("/conn/{id}/create/database", s.handleCreateDBForm)
		r.Post("/conn/{id}/create/database", s.handleCreateDB)
		r.Get("/conn/{id}/create/table", s.handleCreateTableForm)
		r.Post("/conn/{id}/create/table", s.handleCreateTable)
		r.Get("/conn/{id}/create/view", s.handleCreateViewForm)
		r.Post("/conn/{id}/create/view", s.handleCreateView)
		r.Get("/conn/{id}/create/index", s.handleCreateIndexForm)
		r.Post("/conn/{id}/create/index", s.handleCreateIndex)

		// row crud
		r.Get("/conn/{id}/object/{schema}/{name}/row/new", s.handleRowForm)
		r.Post("/conn/{id}/object/{schema}/{name}/row", s.handleRowInsert)
		r.Get("/conn/{id}/object/{schema}/{name}/row/edit", s.handleRowEditForm)
		r.Post("/conn/{id}/object/{schema}/{name}/row/update", s.handleRowUpdate)
		r.Post("/conn/{id}/object/{schema}/{name}/row/delete", s.handleRowDelete)

		// admin
		r.Group(func(r chi.Router) {
			r.Use(s.requireAdmin)
			r.Get("/users", s.handleUsersList)
			r.Post("/users", s.handleUserCreate)
			r.Post("/users/{id}/disable", s.handleUserSetDisabled(true))
			r.Post("/users/{id}/enable", s.handleUserSetDisabled(false))
			r.Post("/users/{id}/reset", s.handleUserReset)
		})
	})

	// global redirect: any URL while no users exist → /setup
	return s.firstRunRedirect(r)
}

// firstRunRedirect: when zero users exist, push every browser to /setup.
func (s *Server) firstRunRedirect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/setup" || r.URL.Path == "/healthz" || hasPrefix(r.URL.Path, "/static/") {
			next.ServeHTTP(w, r)
			return
		}
		n, err := s.store.Users.Count(r.Context())
		if err == nil && n == 0 {
			http.Redirect(w, r, "/setup", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func hasPrefix(s, p string) bool { return len(s) >= len(p) && s[:len(p)] == p }
