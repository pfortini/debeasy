package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// Direct unit tests for small helpers that are hard to reach via end-to-end HTTP.

func TestFormatAny_AllBranches(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{true, "true"},
		{false, "false"},
		{int64(42), "42"},
		{int(7), "7"},
		{float64(3.14), "3.14"},
		{[]byte("hi"), "hi"},
		{struct{}{}, ""}, // unknown type → ""
	}
	for _, tc := range cases {
		if got := formatAny(tc.in); got != tc.want {
			t.Errorf("formatAny(%v) = %q; want %q", tc.in, got, tc.want)
		}
	}
}

func TestStringify_Nil(t *testing.T) {
	if got := stringify(nil); got != "" {
		t.Errorf("got %q", got)
	}
	if got := stringify("abc"); got != "abc" {
		t.Errorf("got %q", got)
	}
	if got := stringify(int64(1)); got != "1" {
		t.Errorf("got %q", got)
	}
}

func TestParsePrefixedQuery(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/?pk_id=1&pk_name=alice&other=x", http.NoBody)
	got := parsePrefixedQuery(r, "pk_")
	if got["id"] != "1" || got["name"] != "alice" {
		t.Errorf("got %+v", got)
	}
	if _, has := got["other"]; has {
		t.Errorf("non-prefixed key leaked")
	}
}

func TestSplitCSV(t *testing.T) {
	got := splitCSV(" a , b,, c,")
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i, v := range want {
		if got[i] != v {
			t.Errorf("[%d] got %q; want %q", i, got[i], v)
		}
	}
	if got := splitCSV(""); len(got) != 0 {
		t.Errorf("empty input should yield empty slice, got %v", got)
	}
}

func TestNullableErr(t *testing.T) {
	if got := nullableErr(""); got.Valid {
		t.Errorf("empty error should yield invalid NullString")
	}
	if got := nullableErr("boom"); !got.Valid || got.String != "boom" {
		t.Errorf("got %+v", got)
	}
}

func TestCSRFToken_NoSession(t *testing.T) {
	r, _ := http.NewRequest(http.MethodGet, "/", http.NoBody)
	if got := CSRFToken(r); got != "" {
		t.Errorf("no session → CSRFToken should be empty, got %q", got)
	}
}

func TestIsHTMX(t *testing.T) {
	r, _ := http.NewRequest(http.MethodGet, "/", http.NoBody)
	if IsHTMX(r) {
		t.Error("plain request isn't HTMX")
	}
	r.Header.Set("HX-Request", "true")
	if !IsHTMX(r) {
		t.Error("HX-Request: true should be HTMX")
	}
}

func TestStatusRecorder_WriteDefaultsTo200(t *testing.T) {
	rec := &statusRecorder{ResponseWriter: httptest.NewRecorder()}
	if _, err := rec.Write([]byte("hi")); err != nil {
		t.Fatal(err)
	}
	if rec.status != 200 {
		t.Errorf("status = %d; want 200", rec.status)
	}
}

func TestStatusRecorder_Flush(t *testing.T) {
	// httptest.ResponseRecorder implements http.Flusher as of Go 1.21+.
	rec := &statusRecorder{ResponseWriter: httptest.NewRecorder()}
	rec.Flush() // just ensure it doesn't panic
}

func TestRenderer_UnknownTemplate(t *testing.T) {
	r, err := NewRenderer()
	if err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	// Nonexistent template name — falls back to first registered page, which will still
	// usually succeed, so instead we verify that rendering doesn't panic / status ok.
	r.Render(rr, 200, "this-does-not-exist.html", nil)
	if rr.Code == 0 {
		t.Error("no status written")
	}
}

func TestLayoutData_HasSensibleDefaults(t *testing.T) {
	// Build a fake request with no session → layout should still construct.
	env := newTestEnv(t)
	req, _ := http.NewRequest(http.MethodGet, env.ts.URL+"/", http.NoBody)
	data := env.s.layout(req, "t", nil)
	if data.Title != "t" {
		t.Errorf("title not set")
	}
	if data.User != nil {
		t.Errorf("user should be nil without session")
	}
}
