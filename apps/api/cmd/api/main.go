// Command api runs the Arth HTTP API.
//
//	api [serve]                 start the HTTP server (default)
//	api migrate up|down|status  run the embedded goose migrations (MIGRATIONS_DATABASE_URL, else DATABASE_URL)
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata" // IANA zones for user timezones even on distroless/scratch images

	"github.com/jackc/pgx/v5/pgxpool"

	"arth/api/internal/apierr"
	"arth/api/internal/config"
	"arth/api/internal/httpapi"
	"arth/api/internal/migrate"
)

const usage = `usage: api [serve] | api migrate up|down|status`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if err := config.LoadDotEnv(".env"); err != nil {
		return fmt.Errorf("load .env: %w", err)
	}
	cmd := "serve"
	if len(args) > 0 {
		cmd = args[0]
	}
	switch cmd {
	case "serve":
		if len(args) > 1 {
			return errors.New(usage)
		}
		return serve()
	case "migrate":
		if len(args) != 2 {
			return errors.New(usage)
		}
		return runMigrate(args[1])
	case "-h", "--help", "help":
		fmt.Println(usage)
		return nil
	default:
		return fmt.Errorf("unknown command %q\n%s", cmd, usage)
	}
}

// newLogger returns a JSON logger (text when APP_ENV=local) and installs it as the default and apierr logger.
func newLogger(cfg config.Config, w io.Writer) *slog.Logger {
	opts := &slog.HandlerOptions{Level: cfg.LogLevel}
	var h slog.Handler = slog.NewJSONHandler(w, opts)
	if cfg.IsLocal() {
		h = slog.NewTextHandler(w, opts)
	}
	log := slog.New(h).With("service", "api")
	slog.SetDefault(log)
	apierr.Logger = log
	return log
}

// runMigrate needs only the migrations URL, so it does not require the full API config (e.g. signing keys).
func runMigrate(sub string) error {
	url := os.Getenv("MIGRATIONS_DATABASE_URL")
	if url == "" {
		url = os.Getenv("DATABASE_URL")
	}
	if url == "" {
		return errors.New("MIGRATIONS_DATABASE_URL (or DATABASE_URL) is required")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	switch sub {
	case "up":
		return migrate.Up(ctx, url, os.Stdout)
	case "down":
		return migrate.Down(ctx, url, os.Stdout)
	case "status":
		return migrate.Status(ctx, url, os.Stdout)
	default:
		return fmt.Errorf("unknown migrate command %q\n%s", sub, usage)
	}
}

func newPool(ctx context.Context, cfg config.Config) (*pgxpool.Pool, error) {
	pc, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return nil, errors.New("parse DATABASE_URL: invalid connection string") // never echo the URL
	}
	pc.MaxConns = 10
	pc.MinConns = 1
	pc.MaxConnLifetime = time.Hour
	pc.MaxConnLifetimeJitter = 5 * time.Minute
	pc.MaxConnIdleTime = 15 * time.Minute
	pc.HealthCheckPeriod = time.Minute
	pc.ConnConfig.ConnectTimeout = 5 * time.Second
	if pc.ConnConfig.RuntimeParams == nil {
		pc.ConnConfig.RuntimeParams = map[string]string{}
	}
	pc.ConnConfig.RuntimeParams["application_name"] = "arth-api"
	pc.ConnConfig.RuntimeParams["timezone"] = "UTC"

	pool, err := pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		return nil, fmt.Errorf("create db pool: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("connect to database: %w", err)
	}
	return pool, nil
}

func serve() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := newLogger(cfg, os.Stdout)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := newPool(ctx, cfg)
	if err != nil {
		return err
	}
	defer pool.Close() // runs after the server has drained

	deps, closeDeps, err := httpapi.BuildDeps(cfg, pool, log, time.Now)
	if err != nil {
		return err
	}
	defer closeDeps()
	handler, err := httpapi.NewHandler(deps)
	if err != nil {
		return err
	}

	srv := &http.Server{
		Addr:              cfg.Addr(),
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second, // receipt uploads (≤ 6 MiB) on slow mobile links
		WriteTimeout:      60 * time.Second, // CSV export / AI extraction
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    32 << 10,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}

	errc := make(chan error, 1)
	go func() {
		log.Info("api listening", "addr", srv.Addr, "env", cfg.AppEnv)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- err
		}
		close(errc)
	}()

	select {
	case err := <-errc:
		if err != nil {
			return fmt.Errorf("http server: %w", err)
		}
		return nil
	case <-ctx.Done():
	}
	stop() // a second signal kills the process immediately
	log.Info("shutting down", "timeout", cfg.ShutdownTimeout.String())
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	log.Info("server stopped")
	return nil
}
