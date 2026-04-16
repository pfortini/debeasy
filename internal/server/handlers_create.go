package server

import (
	"net/http"
	"strings"

	"github.com/pfortini/debeasy/internal/dbx"
	"github.com/pfortini/debeasy/internal/store"
)

// ---------- create database ----------

type CreateDBData struct {
	Conn *store.Connection
	Kind string
	Err  string
}

func (s *Server) handleCreateDBForm(w http.ResponseWriter, r *http.Request) {
	id := intParam(r, "id")
	c, err := s.store.Connections.Get(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	s.rend.Render(w, http.StatusOK, "partials/create_db.html",
		&CreateDBData{Conn: c, Kind: c.Kind})
}

func (s *Server) handleCreateDB(w http.ResponseWriter, r *http.Request) {
	c, d := s.resolveConn(w, r)
	if c == nil {
		return
	}
	err := d.CreateDatabase(r.Context(), r.FormValue("name"), dbx.DBOpts{
		Encoding:  r.FormValue("encoding"),
		Collation: r.FormValue("collation"),
	})
	if err != nil {
		s.rend.Render(w, http.StatusBadRequest, "partials/create_db.html",
			&CreateDBData{Conn: c, Kind: c.Kind, Err: err.Error()})
		return
	}
	// Schema dropdown is rendered server-side; a tree-refresh alone wouldn't show
	// the new DB. Force a full page reload so the dropdown re-populates.
	w.Header().Set("HX-Refresh", "true")
	w.WriteHeader(http.StatusNoContent)
}

// ---------- create table ----------

type CreateTableData struct {
	Conn   *store.Connection
	Schema string
	Kind   string
	Err    string
}

func (s *Server) handleCreateTableForm(w http.ResponseWriter, r *http.Request) {
	id := intParam(r, "id")
	c, err := s.store.Connections.Get(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	s.rend.Render(w, http.StatusOK, "partials/create_table.html",
		&CreateTableData{Conn: c, Schema: r.URL.Query().Get("schema"), Kind: c.Kind})
}

func (s *Server) handleCreateTable(w http.ResponseWriter, r *http.Request) {
	c, d := s.resolveConn(w, r)
	if c == nil {
		return
	}
	def := parseCreateTableForm(r)
	if err := d.CreateTable(r.Context(), def); err != nil {
		s.rend.Render(w, http.StatusBadRequest, "partials/create_table.html",
			&CreateTableData{Conn: c, Schema: def.Schema, Kind: c.Kind, Err: err.Error()})
		return
	}
	w.Header().Set("HX-Trigger", "tree-refresh")
	w.WriteHeader(http.StatusNoContent)
}

// parseCreateTableForm reads the per-column repeating form fields into a CreateTableDef.
func parseCreateTableForm(r *http.Request) dbx.CreateTableDef {
	_ = r.ParseForm() // idempotent; populates r.Form so col_* arrays are readable
	def := dbx.CreateTableDef{
		Schema: r.FormValue("schema"),
		Name:   r.FormValue("name"),
	}
	names := r.Form["col_name"]
	types := r.Form["col_type"]
	nullables := r.Form["col_nullable"]
	pks := r.Form["col_pk"]
	defaults := r.Form["col_default"]
	for i, n := range names {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		col := dbx.ColumnDef{Name: n}
		if i < len(types) {
			col.DataType = types[i]
		}
		if i < len(nullables) {
			col.Nullable = nullables[i] == "1"
		}
		if i < len(pks) {
			col.IsPK = pks[i] == "1"
		}
		if i < len(defaults) {
			col.Default = defaults[i]
		}
		def.Columns = append(def.Columns, col)
	}
	return def
}

// ---------- create view ----------

type CreateViewData struct {
	Conn   *store.Connection
	Schema string
	Err    string
}

func (s *Server) handleCreateViewForm(w http.ResponseWriter, r *http.Request) {
	id := intParam(r, "id")
	c, err := s.store.Connections.Get(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	s.rend.Render(w, http.StatusOK, "partials/create_view.html",
		&CreateViewData{Conn: c, Schema: r.URL.Query().Get("schema")})
}

func (s *Server) handleCreateView(w http.ResponseWriter, r *http.Request) {
	c, d := s.resolveConn(w, r)
	if c == nil {
		return
	}
	schema := r.FormValue("schema")
	if err := d.CreateView(r.Context(), schema, r.FormValue("name"), r.FormValue("sql")); err != nil {
		s.rend.Render(w, http.StatusBadRequest, "partials/create_view.html",
			&CreateViewData{Conn: c, Schema: schema, Err: err.Error()})
		return
	}
	w.Header().Set("HX-Trigger", "tree-refresh")
	w.WriteHeader(http.StatusNoContent)
}

// ---------- create index ----------

type CreateIndexData struct {
	Conn   *store.Connection
	Schema string
	Table  string
	Err    string
}

func (s *Server) handleCreateIndexForm(w http.ResponseWriter, r *http.Request) {
	id := intParam(r, "id")
	c, err := s.store.Connections.Get(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	s.rend.Render(w, http.StatusOK, "partials/create_index.html",
		&CreateIndexData{
			Conn:   c,
			Schema: r.URL.Query().Get("schema"),
			Table:  r.URL.Query().Get("table"),
		})
}

func (s *Server) handleCreateIndex(w http.ResponseWriter, r *http.Request) {
	c, d := s.resolveConn(w, r)
	if c == nil {
		return
	}
	def := dbx.IndexDef{
		Name:    r.FormValue("name"),
		Schema:  r.FormValue("schema"),
		Table:   r.FormValue("table"),
		Columns: splitCSV(r.FormValue("columns")),
		Unique:  formBool(r, "unique"),
		Method:  r.FormValue("method"),
	}
	if err := d.CreateIndex(r.Context(), def); err != nil {
		s.rend.Render(w, http.StatusBadRequest, "partials/create_index.html",
			&CreateIndexData{Conn: c, Schema: def.Schema, Table: def.Table, Err: err.Error()})
		return
	}
	w.Header().Set("HX-Trigger", "tree-refresh")
	w.WriteHeader(http.StatusNoContent)
}

// splitCSV returns non-empty, trimmed segments of a comma-separated string.
func splitCSV(s string) []string {
	out := []string{}
	for _, v := range strings.Split(s, ",") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}
