// Package httpapi wires the HTTP router, middleware and (later) the generated API handlers.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"arth/api/internal/apierr"
	"arth/api/internal/auth"
	"arth/api/internal/config"
	"arth/api/internal/db"
	"arth/api/internal/migrate"
	"arth/api/internal/provider/ai"
	"arth/api/internal/provider/email"
	"arth/api/internal/provider/storage"
)

// Deps are the shared dependencies of the HTTP layer.
type Deps struct {
	Pool *pgxpool.Pool
	Cfg  config.Config
	Log  *slog.Logger
	Now  func() time.Time

	Q       *db.Queries
	Tokens  *auth.Tokens
	Hasher  *auth.Hasher
	Limits  *auth.Limits
	AI      ai.ReceiptExtractor
	Email   email.Sender
	Storage storage.BlobStore
}

// bodyLimitOverrides maps exact request paths to a body limit other than DefaultBodyLimit.
// The receipt upload route belongs here with UploadBodyLimit once its path is in api/openapi.yaml.
var bodyLimitOverrides = map[string]int64{
	"/v1/ai/receipt-extractions": UploadBodyLimit,
}

const readyTimeout = 2 * time.Second

// NewRouter builds the root handler: middleware, /healthz, /readyz, problem+json 404/405,
// and api (the generated /v1 handler) mounted at "/" for everything else. api may be nil.
func NewRouter(d Deps, api http.Handler) http.Handler {
	if d.Log == nil {
		d.Log = slog.Default()
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	r := chi.NewRouter()
	r.Use(
		RequestIDMiddleware,
		AccessLog(d.Log),
		Recoverer,
		SecurityHeaders,
		CORS(d.Cfg.CORSAllowedOrigins),
		MaxBodySize(DefaultBodyLimit, bodyLimitOverrides),
	)
	r.NotFound(func(w http.ResponseWriter, r *http.Request) { apierr.Write(w, r, apierr.NotFound()) })
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) { apierr.Write(w, r, methodNotAllowed()) })

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	r.Get("/readyz", readyz(d))

	if api != nil {
		r.Mount("/", api)
	}
	return r
}

func methodNotAllowed() *apierr.Error {
	return &apierr.Error{Status: http.StatusMethodNotAllowed, Code: "method_not_allowed", Title: "Method not allowed"}
}

func notReady(reason string, err error) *apierr.Error {
	e := &apierr.Error{Status: http.StatusServiceUnavailable, Code: "not_ready", Title: "Service not ready", Err: err}
	return e.WithExtra("reason", reason)
}

// readyz: 200 when the DB answers and the schema is at the latest embedded migration, else 503 problem.
func readyz(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.Pool == nil {
			apierr.Write(w, r, notReady("database_unavailable", errors.New("readyz: no database pool")))
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), readyTimeout)
		defer cancel()
		if err := d.Pool.Ping(ctx); err != nil {
			apierr.Write(w, r, notReady("database_unavailable", fmt.Errorf("readyz: ping: %w", err)))
			return
		}
		current, latest, err := migrate.Pending(ctx, d.Pool)
		if err != nil {
			apierr.Write(w, r, notReady("database_unavailable", fmt.Errorf("readyz: schema version: %w", err)))
			return
		}
		if current != latest {
			apierr.Write(w, r, notReady("migrations_pending",
				fmt.Errorf("readyz: schema version %d, binary expects %d", current, latest)))
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
