package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"arth/api/internal/apierr"
)

// Body size limits. The receipt upload route needs the larger one (see bodyLimitOverrides in router.go).
const (
	DefaultBodyLimit int64 = 1 << 20 // 1 MiB
	UploadBodyLimit  int64 = 6 << 20 // 6 MiB
)

// HeaderRequestID is the request id header (accepted from clients when sane, always set on responses).
const HeaderRequestID = "X-Request-Id"

const maxClientRequestIDLen = 64

type ctxKey int

const (
	requestIDKey ctxKey = iota
	logAttrsKey
)

func init() {
	// apierr reads the request id from the request context set by RequestIDMiddleware.
	apierr.RequestIDFunc = func(r *http.Request) string { return RequestID(r.Context()) }
}

// RequestID returns the request id stored by RequestIDMiddleware, or "".
func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

// RequestIDMiddleware echoes a sane client X-Request-Id (≤ 64 chars of [A-Za-z0-9-_]) or generates one,
// stores it in the context and sets it on the response.
func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(HeaderRequestID)
		if !validRequestID(id) {
			id = newRequestID()
		}
		w.Header().Set(HeaderRequestID, id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey, id)))
	})
}

func validRequestID(id string) bool {
	if id == "" || len(id) > maxClientRequestIDLen {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

func newRequestID() string {
	var b [16]byte
	_, _ = rand.Read(b[:]) // crypto/rand.Read never returns an error (Go 1.24+)
	return hex.EncodeToString(b[:])
}

// logAttrs collects extra attributes for the access log line (e.g. user_id from the auth middleware).
type logAttrs struct {
	mu    sync.Mutex
	attrs []slog.Attr
}

// AddLogAttrs adds attributes to this request's access-log line. Never pass secrets, emails, amounts or bodies.
func AddLogAttrs(ctx context.Context, attrs ...slog.Attr) {
	if la, ok := ctx.Value(logAttrsKey).(*logAttrs); ok {
		la.mu.Lock()
		la.attrs = append(la.attrs, attrs...)
		la.mu.Unlock()
	}
}

// AccessLog logs one line per request: method, route pattern, status, duration_ms, bytes, request_id.
// It never logs the raw path, query string, headers or bodies.
func AccessLog(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			la := &logAttrs{}
			r = r.WithContext(context.WithValue(r.Context(), logAttrsKey, la))
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			defer func() {
				status := ww.Status()
				if status == 0 {
					status = http.StatusOK
				}
				route := ""
				if rc := chi.RouteContext(r.Context()); rc != nil {
					route = rc.RoutePattern()
				}
				if route == "" {
					route = "unmatched"
				}
				attrs := []slog.Attr{
					slog.String("method", r.Method),
					slog.String("route", route),
					slog.Int("status", status),
					slog.Int64("duration_ms", time.Since(start).Milliseconds()),
					slog.Int("bytes", ww.BytesWritten()),
					slog.String("request_id", RequestID(r.Context())),
				}
				la.mu.Lock()
				attrs = append(attrs, la.attrs...)
				la.mu.Unlock()
				level := slog.LevelInfo
				if status >= 500 {
					level = slog.LevelWarn
				}
				log.LogAttrs(r.Context(), level, "http request", attrs...)
			}()
			next.ServeHTTP(ww, r)
		})
	}
}

// Recoverer turns a panic into a 500 internal problem. The panic value and stack go to the log only.
func Recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			rec := recover()
			if rec == nil {
				return
			}
			if rec == http.ErrAbortHandler {
				panic(rec) // let net/http abort the connection as intended
			}
			apierr.Write(w, r, apierr.Internal(fmt.Errorf("panic: %v\n%s", rec, debug.Stack())))
		}()
		next.ServeHTTP(w, r)
	})
}

// CORS allowlist. Exposed/allowed headers follow docs/API.md. Credentials are not allowed (bearer tokens only;
// web cookie auth is OPEN, D-OPEN-6). "*" in the list allows any origin (rejected by config outside local/test).
var (
	corsAllowMethods  = "GET, POST, PUT, PATCH, DELETE, OPTIONS"
	corsAllowHeaders  = "Authorization, Content-Type, Idempotency-Key, X-Request-Id"
	corsExposeHeaders = "X-Request-Id, Retry-After, Location, Content-Disposition"
	corsMaxAge        = strconv.Itoa(int((10 * time.Minute).Seconds()))
)

func CORS(allowed []string) func(http.Handler) http.Handler {
	allowAll := slices.Contains(allowed, "*")
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin == "" {
				next.ServeHTTP(w, r)
				return
			}
			h := w.Header()
			h.Add("Vary", "Origin")
			ok := allowAll || slices.Contains(allowed, strings.TrimRight(origin, "/"))
			preflight := r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != ""
			if preflight {
				h.Add("Vary", "Access-Control-Request-Method")
				h.Add("Vary", "Access-Control-Request-Headers")
				if ok {
					h.Set("Access-Control-Allow-Origin", origin)
					h.Set("Access-Control-Allow-Methods", corsAllowMethods)
					h.Set("Access-Control-Allow-Headers", corsAllowHeaders)
					h.Set("Access-Control-Max-Age", corsMaxAge)
				}
				// A disallowed origin gets no CORS headers; the browser blocks the request.
				w.WriteHeader(http.StatusNoContent)
				return
			}
			if ok {
				h.Set("Access-Control-Allow-Origin", origin)
				h.Set("Access-Control-Expose-Headers", corsExposeHeaders)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// MaxBodySize caps request bodies at limit bytes, or at overrides[r.URL.Path] for listed paths.
// A declared Content-Length above the limit is rejected with 413 up front; otherwise reads past the limit
// fail with *http.MaxBytesError, which handlers/decoders must map to apierr.PayloadTooLarge().
func MaxBodySize(limit int64, overrides map[string]int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			n := limit
			if o, ok := overrides[r.URL.Path]; ok {
				n = o
			}
			if r.ContentLength > n {
				apierr.Write(w, r, apierr.PayloadTooLarge())
				return
			}
			if r.Body != nil && r.Body != http.NoBody {
				r.Body = http.MaxBytesReader(w, r.Body, n)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// SecurityHeaders sets conservative defaults for a JSON API. Handlers may override Cache-Control.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}
