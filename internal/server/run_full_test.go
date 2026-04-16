package server

import (
	"context"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/pfortini/debeasy/internal/config"
)

// TestRun_FullPath exercises the Run() entrypoint (which itself calls net.Listen
// and delegates to serveOn). We bind a free port, hand the returned address back
// into s.srv.Addr, and immediately cancel — proving Run returns cleanly.
func TestRun_FullPath(t *testing.T) {
	// Grab a port, then release it so Run can re-bind there. Short window is fine;
	// nothing else on the test host is going to claim 127.0.0.1:$PORT in <1ms.
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := probe.Addr().String()
	_ = probe.Close()

	dir := t.TempDir()
	secret := make([]byte, 32)
	s, err := New(&config.Config{Addr: addr, DataDir: dir, AppSecret: secret},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()

	// Give Run a moment to bind then cancel.
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run returned err: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run didn't return within 5s of cancel")
	}
}

func TestRun_BindError(t *testing.T) {
	// An invalid address should make Run error out immediately without starting.
	dir := t.TempDir()
	secret := make([]byte, 32)
	s, err := New(&config.Config{Addr: "not-a-real-addr", DataDir: dir, AppSecret: secret},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.shutdown)
	if err := s.Run(context.Background()); err == nil {
		t.Error("expected bind error")
	}
}
