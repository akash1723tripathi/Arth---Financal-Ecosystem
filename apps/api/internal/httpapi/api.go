package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"arth/api/internal/apierr"
	"arth/api/internal/auth"
	"arth/api/internal/config"
	"arth/api/internal/db"
	"arth/api/internal/httpapi/gen"
	"arth/api/internal/provider/ai"
	"arth/api/internal/provider/email"
	"arth/api/internal/provider/storage"
)

// Server implements gen.StrictServerInterface. Its methods live in one file per domain
// (server_auth.go, server_me.go, server_accounts.go, server_transactions.go, server_budgets.go, server_ai.go).
type Server struct {
	d Deps
}

var _ gen.StrictServerInterface = (*Server)(nil)

// BuildDeps constructs every dependency from config. The caller owns pool. Call the returned close func
// on shutdown (it stops the rate-limit janitor).
func BuildDeps(cfg config.Config, pool *pgxpool.Pool, log *slog.Logger, now func() time.Time) (Deps, func(), error) {
	if now == nil {
		now = time.Now
	}
	tokens, err := auth.NewTokens(auth.TokenConfig{Key: cfg.AccessTokenSigningKey, TTL: cfg.AccessTokenTTL, Now: now})
	if err != nil {
		return Deps{}, nil, fmt.Errorf("tokens: %w", err)
	}
	hasher, err := auth.NewHasher(auth.DefaultHashParams())
	if err != nil {
		return Deps{}, nil, fmt.Errorf("hasher: %w", err)
	}
	aiX, err := ai.New(cfg.AIDriver)
	if err != nil {
		return Deps{}, nil, fmt.Errorf("ai driver: %w", err)
	}
	mail, err := email.New(cfg.EmailDriver, log)
	if err != nil {
		return Deps{}, nil, fmt.Errorf("email driver: %w", err)
	}
	store, err := storage.New(cfg.StorageDriver)
	if err != nil {
		return Deps{}, nil, fmt.Errorf("storage driver: %w", err)
	}
	limits := auth.NewLimits(auth.LimitsConfig{Now: now})
	d := Deps{
		Pool: pool, Cfg: cfg, Log: log, Now: now,
		Q: db.New(pool), Tokens: tokens, Hasher: hasher, Limits: limits,
		AI: aiX, Email: mail, Storage: store,
	}
	return d, limits.Close, nil
}

// NewHandler returns the full HTTP handler: base middleware + /healthz + /readyz (router.go) and the
// generated /v1 API behind auth and request validation.
func NewHandler(d Deps) (http.Handler, error) {
	api, err := NewAPI(d)
	if err != nil {
		return nil, err
	}
	return NewRouter(d, api), nil
}

// NewAPI wires the generated strict server: auth middleware → OpenAPI request validation → handlers.
func NewAPI(d Deps) (http.Handler, error) {
	validator, err := NewValidator()
	if err != nil {
		return nil, err
	}
	strict := gen.NewStrictHandlerWithOptions(&Server{d: d}, nil, gen.StrictHTTPServerOptions{
		RequestErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			apierr.Write(w, r, requestError(err))
		},
		ResponseErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			apierr.Write(w, r, err)
		},
	})
	r := chi.NewRouter()
	r.Use(WithRequestMeta, Authenticate(d), validator)
	r.NotFound(func(w http.ResponseWriter, r *http.Request) { apierr.Write(w, r, apierr.NotFound()) })
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) { apierr.Write(w, r, methodNotAllowed()) })
	return gen.HandlerWithOptions(strict, gen.ChiServerOptions{
		BaseRouter: r,
		ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			apierr.Write(w, r, apierr.BadRequest(err.Error()))
		},
	}), nil
}

// requestError maps a decode error from the strict handler (the validator normally catches these first).
func requestError(err error) error {
	var mbe *http.MaxBytesError
	if errors.As(err, &mbe) {
		return apierr.PayloadTooLarge()
	}
	return apierr.BadRequest("malformed request body")
}

// publicPaths need no access token (docs/API.md §1, .claude/rules/api-contract.md).
var publicPaths = map[string]bool{
	"/healthz":          true,
	"/readyz":           true,
	"/v1/auth/register": true,
	"/v1/auth/login":    true,
	"/v1/auth/refresh":  true,
}

func isPublic(path string) bool {
	return publicPaths[path] || strings.HasPrefix(path, "/v1/public/")
}

// Authenticate verifies the bearer access token, checks that its session is still active (AUTH.md Q-5,
// decided: per-request check) and stores the Principal in the context. The user id comes only from here.
func Authenticate(d Deps) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isPublic(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}
			h := r.Header.Get("Authorization")
			const prefix = "Bearer "
			if len(h) <= len(prefix) || !strings.EqualFold(h[:len(prefix)], prefix) {
				apierr.Write(w, r, apierr.Unauthorized())
				return
			}
			p, err := d.Tokens.Verify(strings.TrimSpace(h[len(prefix):]))
			if err != nil {
				apierr.Write(w, r, apierr.Unauthorized())
				return
			}
			active, err := d.Q.IsSessionActive(r.Context(), db.IsSessionActiveParams{ID: p.SessionID, UserID: p.UserID})
			if err != nil {
				apierr.Write(w, r, apierr.Internal(fmt.Errorf("session check: %w", err)))
				return
			}
			if !active {
				apierr.Write(w, r, apierr.Unauthorized())
				return
			}
			AddLogAttrs(r.Context(), slog.String("user_id", p.UserID.String()), slog.String("session_id", p.SessionID.String()))
			next.ServeHTTP(w, r.WithContext(auth.WithPrincipal(r.Context(), p)))
		})
	}
}

// principal returns the authenticated caller from a strict-handler ctx. Handlers behind Authenticate always
// have one; a missing principal is a wiring bug and yields 401, never another user's data.
func principal(ctx context.Context) (auth.Principal, error) {
	p, ok := auth.PrincipalFrom(ctx)
	if !ok {
		return auth.Principal{}, apierr.Unauthorized()
	}
	return p, nil
}

// RequestMeta is what handlers may need from the raw request (strict handlers only get a ctx).
type RequestMeta struct {
	IP        string // remote address without port; X-Forwarded-For is NOT trusted until a proxy is configured (hosting OPEN, AUTH.md §5)
	UserAgent string // truncated to 512 bytes
}

type requestMetaKey struct{}

// WithRequestMeta stores RequestMeta in the context.
func WithRequestMeta(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			ip = r.RemoteAddr
		}
		ua := r.UserAgent()
		if len(ua) > 512 {
			ua = ua[:512]
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestMetaKey{}, RequestMeta{IP: ip, UserAgent: ua})))
	})
}

// requestMeta returns the RequestMeta stored by WithRequestMeta (zero value if absent).
func requestMeta(ctx context.Context) RequestMeta {
	m, _ := ctx.Value(requestMetaKey{}).(RequestMeta)
	return m
}

// notImplemented is returned by stub handlers until their slice is built.
func notImplemented() *apierr.Error {
	return &apierr.Error{Status: http.StatusNotImplemented, Code: "not_implemented", Title: "Not implemented"}
}
