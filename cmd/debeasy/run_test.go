package main

import (
	"context"
	"flag"
	"io"
	"net"
	"os"
	"testing"
	"time"
)

// TestRunServerCtx_CleanShutdown drives runServerCtx in-process with a fresh data
// dir. Coverage counts because this runs within the test binary (unlike the
// subprocess-based main tests). We bind to an ephemeral port, pulse the context,
// and assert a clean 0-exit.
func TestRunServerCtx_CleanShutdown(t *testing.T) {
	// Grab a port, close it, then hand the address off — small race window is fine.
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := probe.Addr().String()
	_ = probe.Close()

	dir := t.TempDir()
	resetFlags(t, []string{"debeasy", "--addr", addr, "--data-dir", dir})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()

	if rc := runServerCtx(ctx, io.Discard); rc != 0 {
		t.Errorf("exit = %d; want 0", rc)
	}
}

func TestRunServerCtx_BindError(t *testing.T) {
	resetFlags(t, []string{"debeasy", "--addr", "not-a-real-addr", "--data-dir", t.TempDir()})

	if rc := runServerCtx(context.Background(), io.Discard); rc != 1 {
		t.Errorf("exit = %d; want 1 (bind error)", rc)
	}
}

func TestRunServerCtx_InitError(t *testing.T) {
	// Point DataDir at a file (not a directory) so store.Open can't create the
	// sqlite file — forces the server.New error branch.
	bad := t.TempDir() + "/not-a-dir"
	if err := os.WriteFile(bad, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	resetFlags(t, []string{"debeasy", "--data-dir", bad})

	if rc := runServerCtx(context.Background(), io.Discard); rc != 1 {
		t.Errorf("exit = %d; want 1 (init error)", rc)
	}
}

// resetFlags isolates runServerCtx's call to config.Load from ambient flag state.
func resetFlags(t *testing.T, args []string) {
	t.Helper()
	oldArgs := os.Args
	oldFlags := flag.CommandLine
	os.Args = args
	flag.CommandLine = flag.NewFlagSet(args[0], flag.ExitOnError)
	t.Cleanup(func() {
		os.Args = oldArgs
		flag.CommandLine = oldFlags
	})
}
