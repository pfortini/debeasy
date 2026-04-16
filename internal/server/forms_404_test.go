package server

import (
	"net/http"
	"testing"
)

// Every *Form endpoint reads the connection row before rendering its template.
// These tests hit all of them with an unknown id to cover the 404 branch and
// the "unknown conn" response path.
func TestForms_UnknownConn_404(t *testing.T) {
	env := newTestEnv(t)
	paths := []string{
		"/connections/9999/edit",
		"/conn/9999/create/database",
		"/conn/9999/create/table",
		"/conn/9999/create/view",
		"/conn/9999/create/index",
	}
	for _, p := range paths {
		st, _ := env.get(p)
		if st != http.StatusNotFound {
			t.Errorf("GET %s → %d; want 404", p, st)
		}
	}
}
