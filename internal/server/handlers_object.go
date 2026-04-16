package server

import (
	"net/http"
	"strconv"

	"github.com/pfortini/debeasy/internal/dbx"
	"github.com/pfortini/debeasy/internal/store"
)

type ObjectDetailData struct {
	Conn   *store.Connection
	Ref    dbx.ObjectRef
	Def    dbx.TableDef
	Schema string
}

func (s *Server) handleObjectDetail(w http.ResponseWriter, r *http.Request) {
	c, _, ref, def := s.resolveTable(w, r)
	if c == nil {
		return
	}
	body := &ObjectDetailData{Conn: c, Ref: ref, Def: *def, Schema: ref.Schema}
	s.rend.Render(w, http.StatusOK, "partials/object_detail.html", body)
}

type ObjectDataPage struct {
	Conn    *store.Connection
	Ref     dbx.ObjectRef
	Def     dbx.TableDef
	Rows    dbx.RowSet
	Page    dbx.Page
	HasMore bool
	HasPK   bool
	PKCols  []string
	CSRF    string
}

func (s *Server) handleObjectData(w http.ResponseWriter, r *http.Request) {
	c, d, ref, def := s.resolveTable(w, r)
	if c == nil {
		return
	}
	page := dbx.Page{
		Offset:  formInt(r, "offset", 0),
		Limit:   formInt(r, "limit", 50),
		OrderBy: r.URL.Query().Get("order_by"),
		Desc:    r.URL.Query().Get("desc") == "1",
	}
	rows, err := d.SampleRows(r.Context(), ref, page)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	pkCols := []string{}
	for _, col := range def.Columns {
		if col.IsPK {
			pkCols = append(pkCols, col.Name)
		}
	}
	body := &ObjectDataPage{
		Conn: c, Ref: ref, Def: *def, Rows: rows, Page: page,
		HasMore: len(rows.Rows) >= page.Limit,
		HasPK:   len(pkCols) > 0, PKCols: pkCols,
		CSRF: CSRFToken(r),
	}
	s.rend.Render(w, http.StatusOK, "partials/object_data.html", body)
}

type RowFormData struct {
	Conn      *store.Connection
	Ref       dbx.ObjectRef
	Def       dbx.TableDef
	IsEdit    bool
	Values    map[string]string
	PKValues  map[string]string
	Err       string
	ActionURL string
}

func (s *Server) handleRowForm(w http.ResponseWriter, r *http.Request) {
	c, _, ref, def := s.resolveTable(w, r)
	if c == nil {
		return
	}
	body := &RowFormData{
		Conn: c, Ref: ref, Def: *def,
		Values:    map[string]string{},
		ActionURL: rowURL(c.ID, ref, ""),
	}
	s.rend.Render(w, http.StatusOK, "partials/row_form.html", body)
}

func (s *Server) handleRowInsert(w http.ResponseWriter, r *http.Request) {
	c, d, ref, def := s.resolveTable(w, r)
	if c == nil {
		return
	}
	values := map[string]string{}
	for _, col := range def.Columns {
		v := r.FormValue("c_" + col.Name)
		if v == "" && r.FormValue("null_"+col.Name) == "on" {
			continue
		}
		values[col.Name] = v
	}
	if err := d.InsertRow(r.Context(), ref, values); err != nil {
		body := &RowFormData{Conn: c, Ref: ref, Def: *def, Values: values, Err: err.Error(), ActionURL: rowURL(c.ID, ref, "")}
		s.rend.Render(w, http.StatusBadRequest, "partials/row_form.html", body)
		return
	}
	w.Header().Set("HX-Trigger", "row-saved")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRowEditForm(w http.ResponseWriter, r *http.Request) {
	c, d, ref, def := s.resolveTable(w, r)
	if c == nil {
		return
	}
	pkVals := parsePrefixedQuery(r, "pk_")
	current, err := fetchRowByPK(r, d, *def, pkVals)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	body := &RowFormData{
		Conn: c, Ref: ref, Def: *def, IsEdit: true,
		Values: current, PKValues: pkVals,
		ActionURL: rowURL(c.ID, ref, "/update"),
	}
	s.rend.Render(w, http.StatusOK, "partials/row_form.html", body)
}

func (s *Server) handleRowUpdate(w http.ResponseWriter, r *http.Request) {
	c, d, ref, def := s.resolveTable(w, r)
	if c == nil {
		return
	}
	values := map[string]string{}
	pk := map[string]string{}
	for _, col := range def.Columns {
		values[col.Name] = r.FormValue("c_" + col.Name)
		if col.IsPK {
			pk[col.Name] = r.FormValue("pk_" + col.Name)
		}
	}
	if err := d.UpdateRow(r.Context(), ref, pk, values); err != nil {
		body := &RowFormData{Conn: c, Ref: ref, Def: *def, IsEdit: true, Values: values, PKValues: pk, Err: err.Error(), ActionURL: rowURL(c.ID, ref, "/update")}
		s.rend.Render(w, http.StatusBadRequest, "partials/row_form.html", body)
		return
	}
	w.Header().Set("HX-Trigger", "row-saved")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRowDelete(w http.ResponseWriter, r *http.Request) {
	c, d := s.resolveConn(w, r)
	if c == nil {
		return
	}
	_ = r.ParseForm() // needed because parsePrefixedForm reads r.PostForm directly
	ref := dbx.ObjectRef{Schema: strParam(r, "schema"), Name: strParam(r, "name")}
	pk := parsePrefixedForm(r, "pk_")
	if err := d.DeleteRow(r.Context(), ref, pk); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("HX-Trigger", "row-saved")
	w.WriteHeader(http.StatusNoContent)
}

// rowURL builds the endpoint for row operations on a given table.
func rowURL(id int64, ref dbx.ObjectRef, suffix string) string {
	return "/conn/" + strconv.FormatInt(id, 10) + "/object/" + ref.Schema + "/" + ref.Name + "/row" + suffix
}

// parsePrefixedQuery pulls URL query pairs whose key starts with prefix and returns
// a stripped-key map (e.g. "pk_id=1" → {"id": "1"} when prefix="pk_").
func parsePrefixedQuery(r *http.Request, prefix string) map[string]string {
	out := map[string]string{}
	for k, v := range r.URL.Query() {
		if len(v) > 0 && len(k) > len(prefix) && k[:len(prefix)] == prefix {
			out[k[len(prefix):]] = v[0]
		}
	}
	return out
}

// parsePrefixedForm does the same for PostForm values.
func parsePrefixedForm(r *http.Request, prefix string) map[string]string {
	out := map[string]string{}
	for k, v := range r.PostForm {
		if len(v) > 0 && len(k) > len(prefix) && k[:len(prefix)] == prefix {
			out[k[len(prefix):]] = v[0]
		}
	}
	return out
}

// fetchRowByPK pulls the current value of a row identified by its primary-key
// columns. Scans up to a small sample of rows — acceptable because row-edit is
// only used interactively on admin-scale tables; large tables use the SQL editor.
func fetchRowByPK(r *http.Request, d dbx.Driver, def dbx.TableDef, pk map[string]string) (map[string]string, error) {
	rs, err := d.SampleRows(r.Context(), def.Ref, dbx.Page{Limit: 1000})
	if err != nil {
		return nil, err
	}
	for _, row := range rs.Rows {
		if rowMatches(rs.Columns, row, pk) {
			out := make(map[string]string, len(rs.Columns))
			for i, c := range rs.Columns {
				out[c] = stringify(row[i])
			}
			return out, nil
		}
	}
	// Not found in the sample — seed pk values so the form can still render.
	out := map[string]string{}
	for _, c := range def.Columns {
		if v, ok := pk[c.Name]; ok {
			out[c.Name] = v
		}
	}
	return out, nil
}

func rowMatches(cols []string, row []any, pk map[string]string) bool {
	for k, want := range pk {
		idx := -1
		for i, c := range cols {
			if c == k {
				idx = i
				break
			}
		}
		if idx < 0 || stringify(row[idx]) != want {
			return false
		}
	}
	return true
}

func stringify(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return formatAny(v)
}

func formatAny(v any) string {
	switch x := v.(type) {
	case bool:
		if x {
			return "true"
		}
		return "false"
	case int64:
		return strconv.FormatInt(x, 10)
	case int:
		return strconv.Itoa(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case []byte:
		return string(x)
	}
	return ""
}
