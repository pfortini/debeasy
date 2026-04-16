package server

import (
	"net/http"
	"strconv"
	"strings"
	"testing"
)

func TestUsers_AdminGuard(t *testing.T) {
	env := newTestEnv(t)
	// Downgrade admin to plain user — should lose access
	_, err := env.s.store.DB.ExecContext(t.Context(), `UPDATE users SET role='user' WHERE id=?`, env.admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	st, _ := env.get("/users")
	if st != http.StatusForbidden {
		t.Errorf("non-admin /users = %d; want 403", st)
	}
}

func TestUsers_CreateDisableEnableReset(t *testing.T) {
	env := newTestEnv(t)

	// GET /users renders with the admin row
	st, body := env.get("/users")
	if st != 200 {
		t.Fatalf("list status = %d", st)
	}
	if !strings.Contains(body, "alice") {
		t.Errorf("admin not listed")
	}

	// Create a new regular user
	resp := env.do(http.MethodPost, "/users", form(
		"username", "bob", "password", "hunter2x", "role", "user",
	))
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("create status = %d", resp.StatusCode)
	}

	// Look up Bob's id
	bob, err := env.s.store.Users.FindByUsername(t.Context(), "bob")
	if err != nil {
		t.Fatalf("bob not created: %v", err)
	}
	bobID := strconv.FormatInt(bob.ID, 10)

	// Disable
	resp = env.do(http.MethodPost, "/users/"+bobID+"/disable", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("disable status = %d", resp.StatusCode)
	}
	got, _ := env.s.store.Users.Get(t.Context(), bob.ID)
	if !got.Disabled {
		t.Errorf("bob not disabled")
	}

	// Re-enable
	resp = env.do(http.MethodPost, "/users/"+bobID+"/enable", nil)
	resp.Body.Close()
	got, _ = env.s.store.Users.Get(t.Context(), bob.ID)
	if got.Disabled {
		t.Errorf("bob still disabled after enable")
	}

	// Reset password
	resp = env.do(http.MethodPost, "/users/"+bobID+"/reset", form("password", "newpassword1"))
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("reset status = %d", resp.StatusCode)
	}
	if _, err := env.s.store.Users.Verify(t.Context(), "bob", "newpassword1"); err != nil {
		t.Errorf("new password not in effect: %v", err)
	}
}
