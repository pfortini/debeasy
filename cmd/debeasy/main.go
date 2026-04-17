package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/pfortini/debeasy/internal/config"
	"github.com/pfortini/debeasy/internal/server"
	"github.com/pfortini/debeasy/internal/version"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "admin":
			if err := runAdmin(os.Args[2:]); err != nil {
				fmt.Fprintln(os.Stderr, "error:", err)
				os.Exit(1)
			}
			return
		case "update":
			if err := runUpdate(os.Args[2:]); err != nil {
				fmt.Fprintln(os.Stderr, "error:", err)
				os.Exit(1)
			}
			return
		case "version", "--version", "-v":
			fmt.Println(version.Version)
			return
		}
	}
	os.Exit(runServer())
}

// runServer wires up the default signal-aware context and delegates to runServerCtx.
// Tests exercise runServerCtx directly so the signal-handling path doesn't pollute
// the test process.
func runServer() int {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	return runServerCtx(ctx, os.Stderr)
}

// runServerCtx loads config, builds the Server, and runs it until ctx is cancelled.
// The logger writer is a parameter so tests can silence it.
func runServerCtx(ctx context.Context, logOut io.Writer) int {
	logger := slog.New(slog.NewTextHandler(logOut, &slog.HandlerOptions{Level: slog.LevelInfo}))
	cfg, err := config.Load()
	if err != nil {
		logger.Error("config", "err", err)
		return 1
	}
	srv, err := server.New(cfg, logger)
	if err != nil {
		logger.Error("init", "err", err)
		return 1
	}
	if err := srv.Run(ctx); err != nil {
		logger.Error("run", "err", err)
		return 1
	}
	logger.Info("shutdown complete")
	return 0
}
