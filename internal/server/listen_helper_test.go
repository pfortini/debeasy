package server

import (
	"net"
	"testing"
)

// listenEphemeral binds a TCP listener on a free port on 127.0.0.1 and returns
// it + its address. Shared by run_test.go.
func listenEphemeral(t *testing.T) (ln net.Listener, addr string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	return ln, ln.Addr().String()
}
