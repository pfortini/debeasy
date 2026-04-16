package server

import (
	"context"
	"html"
	"net/http"
	"strconv"
	"time"

	"github.com/pfortini/debeasy/internal/store"
)

type ConnectionsPageData struct {
	Connections []store.Connection
}

func (s *Server) handleConnectionsList(w http.ResponseWriter, r *http.Request) {
	conns, err := s.store.Connections.List(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	data := s.layout(r, "Connections", &ConnectionsPageData{Connections: conns})
	data.ActiveNav = NavConnections
	s.rend.Render(w, http.StatusOK, "connections.html", data)
}

type ConnectionFormData struct {
	Conn       *store.Connection
	IsEdit     bool
	Err        string
	Action     string
	TestResult string
}

func (s *Server) handleConnectionForm(w http.ResponseWriter, r *http.Request) {
	data := s.layout(r, "New connection", &ConnectionFormData{
		Conn:   &store.Connection{Kind: "postgres"},
		Action: "/connections",
	})
	data.ActiveNav = NavConnections
	s.rend.Render(w, http.StatusOK, "connection_form.html", data)
}

func (s *Server) handleConnectionEditForm(w http.ResponseWriter, r *http.Request) {
	id := intParam(r, "id")
	c, err := s.store.Connections.Get(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	data := s.layout(r, "Edit connection", &ConnectionFormData{
		Conn:   c,
		IsEdit: true,
		Action: "/connections/" + strconv.FormatInt(id, 10),
	})
	data.ActiveNav = NavConnections
	s.rend.Render(w, http.StatusOK, "connection_form.html", data)
}

func (s *Server) handleConnectionCreate(w http.ResponseWriter, r *http.Request) {
	c := connectionFromForm(r)
	created, err := s.store.Connections.Create(r.Context(), c, CurrentUser(r).ID)
	if err != nil {
		data := s.layout(r, "New connection", &ConnectionFormData{
			Conn: c, Action: "/connections", Err: err.Error(),
		})
		s.rend.Render(w, http.StatusBadRequest, "connection_form.html", data)
		return
	}
	http.Redirect(w, r, "/conn/"+strconv.FormatInt(created.ID, 10), http.StatusSeeOther)
}

func (s *Server) handleConnectionUpdate(w http.ResponseWriter, r *http.Request) {
	id := intParam(r, "id")
	c := connectionFromForm(r)
	c.ID = id
	updatePassword := r.FormValue("password") != "" || r.FormValue("password_clear") == "on"
	if r.FormValue("password_clear") == "on" {
		c.Password = ""
	}
	if err := s.store.Connections.Update(r.Context(), c, updatePassword); err != nil {
		data := s.layout(r, "Edit connection", &ConnectionFormData{
			Conn: c, IsEdit: true,
			Action: "/connections/" + strconv.FormatInt(id, 10),
			Err:    err.Error(),
		})
		s.rend.Render(w, http.StatusBadRequest, "connection_form.html", data)
		return
	}
	s.pool.Evict(id)
	http.Redirect(w, r, "/conn/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

func (s *Server) handleConnectionDelete(w http.ResponseWriter, r *http.Request) {
	id := intParam(r, "id")
	s.pool.Evict(id)
	if err := s.store.Connections.Delete(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if IsHTMX(r) {
		w.Header().Set("HX-Redirect", "/connections")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, "/connections", http.StatusSeeOther)
}

func (s *Server) handleConnectionTest(w http.ResponseWriter, r *http.Request) {
	c := connectionFromForm(r)
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.pool.Test(ctx, c); err != nil {
		_, _ = w.Write([]byte(`<div class="test-result test-fail">✘ ` + html.EscapeString(err.Error()) + `</div>`))
		return
	}
	_, _ = w.Write([]byte(`<div class="test-result test-ok">✓ connection successful</div>`))
}

func connectionFromForm(r *http.Request) *store.Connection {
	return &store.Connection{
		Name:     r.FormValue("name"),
		Kind:     r.FormValue("kind"),
		Host:     r.FormValue("host"),
		Port:     formInt(r, "port", 0),
		Username: r.FormValue("username"),
		Password: r.FormValue("password"),
		Database: r.FormValue("database"),
		SSLMode:  r.FormValue("sslmode"),
		Params:   r.FormValue("params"),
	}
}
