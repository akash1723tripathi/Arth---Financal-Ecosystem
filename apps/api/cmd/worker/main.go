// Command worker runs background jobs. It is a stub until the job queue is chosen.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	_ "time/tzdata" // IANA zones for per-user schedules (weekly tips in the user's timezone)

	"arth/api/internal/config"
)

// TODO(D-OPEN-9): job queue library not chosen yet; budget alerts, weekly tips, balance reconcile run here.
// When it is decided (River or an equivalent Postgres-backed queue, docs/DECISIONS.md), this binary opens the
// pgx pool, registers the job workers from internal/jobs and runs the periodic schedules. No job library is
// imported until then.

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	if err := config.LoadDotEnv(".env"); err != nil {
		return fmt.Errorf("load .env: %w", err)
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	opts := &slog.HandlerOptions{Level: cfg.LogLevel}
	var h slog.Handler = slog.NewJSONHandler(os.Stdout, opts)
	if cfg.IsLocal() {
		h = slog.NewTextHandler(os.Stdout, opts)
	}
	log := slog.New(h).With("service", "worker")
	slog.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Info("worker started (stub)", "env", cfg.AppEnv)
	<-ctx.Done()
	log.Info("worker stopped")
	return nil
}
