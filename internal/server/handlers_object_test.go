package server

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/pfortini/debeasy/internal/dbx"
)

func seedTable(t *testing.T, env *testEnv) (connID int64, ref dbx.ObjectRef) {
	t.Helper()
	c := env.mustCreateConnection("c1")
	d, err := env.s.pool.Get(t.Context(), c.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = d.Exec(t.Context(), `
        CREATE TABLE widgets(id INTEGER PRIMARY KEY, label TEXT NOT NULL);
        INSERT INTO widgets(id, label) VALUES (1,'alpha'),(2,'beta'),(3,'gamma');
    `, 100)
	if err != nil {
		t.Fatal(err)
	}
	return c.ID, dbx.ObjectRef{Schema: "main", Name: "widgets"}
}

func TestObjectDetail(t *testing.T) {
	env := newTestEnv(t)
	connID, _ := seedTable(t, env)
	st, body := env.get("/conn/" + strconv.FormatInt(connID, 10) + "/object/main/widgets")
	if st != 200 {
		t.Fatalf("status = %d", st)
	}
	for _, want := range []string{"widgets", "PK", "columns", "CREATE TABLE"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in detail", want)
		}
	}
}

func TestObjectData_Paginated(t *testing.T) {
	env := newTestEnv(t)
	connID, _ := seedTable(t, env)
	st, body := env.get("/conn/" + strconv.FormatInt(connID, 10) + "/object/main/widgets/data?limit=2&offset=0")
	if st != 200 {
		t.Fatalf("status = %d", st)
	}
	if !strings.Contains(body, "alpha") || !strings.Contains(body, "beta") || strings.Contains(body, "gamma") {
		t.Errorf("pagination not applied (limit=2): %s", body)
	}
}

func TestRow_Insert(t *testing.T) {
	env := newTestEnv(t)
	connID, _ := seedTable(t, env)
	form := form(
		"c_id", "4",
		"c_label", "delta",
	)
	resp := env.do(http.MethodPost,
		"/conn/"+strconv.FormatInt(connID, 10)+"/object/main/widgets/row",
		form)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	// Verify via data endpoint
	_, body := env.get("/conn/" + strconv.FormatInt(connID, 10) + "/object/main/widgets/data?limit=10")
	if !strings.Contains(body, "delta") {
		t.Errorf("new row not visible")
	}
}

func TestRow_Update(t *testing.T) {
	env := newTestEnv(t)
	connID, _ := seedTable(t, env)
	resp := env.do(http.MethodPost,
		"/conn/"+strconv.FormatInt(connID, 10)+"/object/main/widgets/row/update",
		form("pk_id", "1", "c_id", "1", "c_label", "ALPHA"))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	_, body := env.get("/conn/" + strconv.FormatInt(connID, 10) + "/object/main/widgets/data")
	if !strings.Contains(body, "ALPHA") {
		t.Errorf("update not visible")
	}
}

func TestRow_Delete(t *testing.T) {
	env := newTestEnv(t)
	connID, _ := seedTable(t, env)
	resp := env.do(http.MethodPost,
		"/conn/"+strconv.FormatInt(connID, 10)+"/object/main/widgets/row/delete",
		form("pk_id", "2"))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	_, body := env.get("/conn/" + strconv.FormatInt(connID, 10) + "/object/main/widgets/data")
	if strings.Contains(body, "beta") {
		t.Errorf("deleted row still visible")
	}
}

func TestRow_NewForm(t *testing.T) {
	env := newTestEnv(t)
	connID, _ := seedTable(t, env)
	st, body := env.get("/conn/" + strconv.FormatInt(connID, 10) + "/object/main/widgets/row/new")
	if st != 200 {
		t.Fatalf("status = %d", st)
	}
	for _, want := range []string{"c_id", "c_label", "new row"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
}

func TestRow_EditForm(t *testing.T) {
	env := newTestEnv(t)
	connID, _ := seedTable(t, env)
	st, body := env.get("/conn/" + strconv.FormatInt(connID, 10) + "/object/main/widgets/row/edit?pk_id=1")
	if st != 200 {
		t.Fatalf("status = %d", st)
	}
	if !strings.Contains(body, "alpha") {
		t.Errorf("existing value not pre-filled")
	}
}
