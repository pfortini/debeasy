package server

import (
	"net/http"
	"net/url"
	"strconv"
	"testing"

	"github.com/pfortini/debeasy/internal/store"
)

// deadPgConnection returns a stored connection pointing at a closed TCP port, so
// every pool.Get against it will fail — exactly what we need to exercise the
// 502 branch of the resolveConn/resolveTable helpers.
func deadPgConnection(t *testing.T, env *testEnv, name string) *store.Connection {
	t.Helper()
	c, err := env.s.store.Connections.Create(t.Context(), &store.Connection{
		Name: name, Kind: "postgres",
		Host: "127.0.0.1", Port: 1, // port 1 is guaranteed-closed for unprivileged binds
		Username: "u", Database: "d", SSLMode: "disable",
	}, env.admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestResolveConn_NotFound(t *testing.T) {
	env := newTestEnv(t)
	if st, _ := env.get("/conn/9999/tree"); st != http.StatusNotFound {
		t.Errorf("unknown conn → status %d; want 404", st)
	}
}

func TestResolveConn_PoolFailure_GET(t *testing.T) {
	env := newTestEnv(t)
	c := deadPgConnection(t, env, "dead-pg-get")
	id := strconv.FormatInt(c.ID, 10)

	// Every endpoint that calls resolveConn/resolveTable must return 502 here.
	paths := []string{
		"/conn/" + id + "/tree",
		"/conn/" + id + "/object/public/widgets",
		"/conn/" + id + "/object/public/widgets/data",
		"/conn/" + id + "/object/public/widgets/row/new",
		"/conn/" + id + "/object/public/widgets/row/edit?pk_id=1",
	}
	for _, p := range paths {
		st, _ := env.get(p)
		if st != http.StatusBadGateway {
			t.Errorf("%s → %d; want 502", p, st)
		}
	}
}

func TestResolveConn_PoolFailure_POST(t *testing.T) {
	env := newTestEnv(t)
	c := deadPgConnection(t, env, "dead-pg-post")
	id := strconv.FormatInt(c.ID, 10)

	posts := []struct {
		path string
		form url.Values
	}{
		{"/conn/" + id + "/query", url.Values{"sql": {"SELECT 1"}}},
		{"/conn/" + id + "/create/database", url.Values{"name": {"x"}}},
		{"/conn/" + id + "/create/table", url.Values{"schema": {"s"}, "name": {"t"}, "col_name": {"id"}, "col_type": {"INT"}}},
		{"/conn/" + id + "/create/view", url.Values{"schema": {"s"}, "name": {"v"}, "sql": {"SELECT 1"}}},
		{"/conn/" + id + "/create/index", url.Values{"name": {"i"}, "schema": {"s"}, "table": {"t"}, "columns": {"a"}}},
		{"/conn/" + id + "/object/s/t/row", url.Values{"c_id": {"1"}}},
		{"/conn/" + id + "/object/s/t/row/update", url.Values{"pk_id": {"1"}, "c_id": {"1"}}},
		{"/conn/" + id + "/object/s/t/row/delete", url.Values{"pk_id": {"1"}}},
	}
	for _, p := range posts {
		st, _ := env.post(p.path, p.form)
		if st != http.StatusBadGateway {
			t.Errorf("POST %s → %d; want 502", p.path, st)
		}
	}
}
