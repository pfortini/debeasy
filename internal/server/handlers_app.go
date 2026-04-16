package server

import (
	"net/http"

	"github.com/pfortini/debeasy/internal/dbx"
	"github.com/pfortini/debeasy/internal/store"
)

type HomeData struct {
	Connections []store.Connection
}

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	conns, _ := s.store.Connections.List(r.Context())
	data := s.layout(r, "debeasy", &HomeData{Connections: conns})
	data.ActiveNav = NavHome
	s.rend.Render(w, http.StatusOK, "home.html", data)
}

type ConnAppData struct {
	Conn      *store.Connection
	Databases []dbx.DB
	Schemas   []dbx.Schema
}

type ConnErrorData struct {
	Conn *store.Connection
	Err  string
}

// handleConnectionApp renders the main shell for a connection. Unlike other handlers,
// a pool-open failure here renders a friendlier "connection error" page rather than
// returning a bare 502 — the user might want to click "edit" and fix the credentials.
func (s *Server) handleConnectionApp(w http.ResponseWriter, r *http.Request) {
	id := intParam(r, "id")
	c, err := s.store.Connections.Get(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	d, err := s.pool.Get(r.Context(), id)
	if err != nil {
		data := s.layout(r, c.Name, &ConnErrorData{Conn: c, Err: err.Error()})
		data.ActiveNav = NavConnections
		s.rend.Render(w, http.StatusOK, "conn_error.html", data)
		return
	}
	dbs, _ := d.ListDatabases(r.Context())
	schemas, _ := d.ListSchemas(r.Context(), c.Database)
	data := s.layout(r, c.Name, &ConnAppData{Conn: c, Databases: dbs, Schemas: schemas})
	data.ActiveNav = NavConnections
	s.rend.Render(w, http.StatusOK, "conn_app.html", data)
}

type TreeData struct {
	Conn    *store.Connection
	Schema  string
	Objects dbx.ObjectTree
}

func (s *Server) handleTree(w http.ResponseWriter, r *http.Request) {
	c, d := s.resolveConn(w, r)
	if c == nil {
		return
	}
	schema := r.URL.Query().Get("schema")
	if schema == "" {
		schemas, err := d.ListSchemas(r.Context(), c.Database)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		if len(schemas) > 0 {
			schema = schemas[0].Name
		}
	}
	tree, err := d.ListObjects(r.Context(), c.Database, schema)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	s.rend.Render(w, http.StatusOK, "partials/tree.html",
		&TreeData{Conn: c, Schema: schema, Objects: tree})
}

type HistoryData struct {
	Entries []store.HistoryEntry
}

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	entries, err := s.store.History.Recent(r.Context(), CurrentUser(r).ID, 50)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.rend.Render(w, http.StatusOK, "partials/history.html", &HistoryData{Entries: entries})
}
