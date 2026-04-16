package server

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/pfortini/debeasy/internal/dbx"
	"github.com/pfortini/debeasy/internal/store"
)

// ActiveNav identifiers used by the top bar to highlight the current section.
const (
	NavHome        = "home"
	NavConnections = "connections"
	NavUsers       = "users"
)

// LayoutData carries the standard fields every page needs.
type LayoutData struct {
	Title       string
	User        *store.User
	CSRF        string
	ActiveNav   string
	Connections []store.Connection
	Flash       string
	Err         string
	Body        any
	Req         *http.Request
}

func (s *Server) layout(r *http.Request, title string, body any) *LayoutData {
	conns, _ := s.store.Connections.List(r.Context())
	return &LayoutData{
		Title:       title,
		User:        CurrentUser(r),
		CSRF:        CSRFToken(r),
		Connections: conns,
		Body:        body,
		Req:         r,
	}
}

//nolint:unparam // name kept for symmetry with strParam; future handlers may vary
func intParam(r *http.Request, name string) int64 {
	v, _ := strconv.ParseInt(chi.URLParam(r, name), 10, 64)
	return v
}

func strParam(r *http.Request, name string) string { return chi.URLParam(r, name) }

func formInt(r *http.Request, name string, def int) int {
	if v, err := strconv.Atoi(r.FormValue(name)); err == nil {
		return v
	}
	return def
}

func formBool(r *http.Request, name string) bool {
	v := r.FormValue(name)
	return v == "on" || v == "true" || v == "1"
}

// resolveConn loads the saved connection at URL param "id" and the corresponding
// open driver from the pool. On any error it writes the HTTP response and returns
// (nil, nil); callers should `return` in that case.
//
// This consolidates the six-line prelude that every connection-scoped handler used
// to inline — fewer statements and a single place to test the error paths.
func (s *Server) resolveConn(w http.ResponseWriter, r *http.Request) (*store.Connection, dbx.Driver) {
	id := intParam(r, "id")
	c, err := s.store.Connections.Get(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return nil, nil
	}
	d, err := s.pool.Get(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return nil, nil
	}
	return c, d
}

// resolveTable extends resolveConn by also pulling {schema,name} URL params and
// describing the table. Returns (nil, nil, ref, nil) on error (after writing the
// response). Callers should `return` when c == nil.
func (s *Server) resolveTable(w http.ResponseWriter, r *http.Request) (*store.Connection, dbx.Driver, dbx.ObjectRef, *dbx.TableDef) {
	c, d := s.resolveConn(w, r)
	if c == nil {
		return nil, nil, dbx.ObjectRef{}, nil
	}
	ref := dbx.ObjectRef{Schema: strParam(r, "schema"), Name: strParam(r, "name")}
	def, err := d.DescribeTable(r.Context(), ref)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return nil, nil, ref, nil
	}
	return c, d, ref, &def
}
