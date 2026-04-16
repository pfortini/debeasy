package server

import (
	"strconv"
	"strings"
	"testing"
)

func TestHome_ListsConnections(t *testing.T) {
	env := newTestEnv(t)
	env.mustCreateConnection("c1")
	env.mustCreateConnection("c2")

	st, body := env.get("/")
	if st != 200 {
		t.Fatalf("status = %d", st)
	}
	for _, want := range []string{"c1", "c2", "recent connections"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in home", want)
		}
	}
}

func TestConnApp_RendersSidebar(t *testing.T) {
	env := newTestEnv(t)
	c := env.mustCreateConnection("c1")

	st, body := env.get("/conn/" + strconv.FormatInt(c.ID, 10))
	if st != 200 {
		t.Fatalf("status = %d", st)
	}
	for _, want := range []string{"chip-sqlite", "c1", "SQL editor", "sidebar"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
}

func TestConnApp_UnknownConn_404(t *testing.T) {
	env := newTestEnv(t)
	st, _ := env.get("/conn/9999")
	if st != 404 {
		t.Errorf("status = %d", st)
	}
}

func TestTree_ReturnsPartial(t *testing.T) {
	env := newTestEnv(t)
	c := env.mustCreateConnection("c1")
	d, err := env.s.pool.Get(t.Context(), c.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = d.Exec(t.Context(), "CREATE TABLE foo(id INTEGER PRIMARY KEY);", 10)
	if err != nil {
		t.Fatal(err)
	}

	st, body := env.get("/conn/" + strconv.FormatInt(c.ID, 10) + "/tree?schema=main")
	if st != 200 {
		t.Fatalf("status = %d", st)
	}
	if !strings.Contains(body, "foo") {
		t.Errorf("expected table 'foo' in tree: %s", body)
	}
}

func TestHistory_Empty(t *testing.T) {
	env := newTestEnv(t)
	st, body := env.get("/conn/1/history") // any id works; history is per-user
	if st != 200 {
		t.Fatalf("status = %d", st)
	}
	if !strings.Contains(body, "no queries yet") {
		t.Errorf("expected empty-state message")
	}
}
