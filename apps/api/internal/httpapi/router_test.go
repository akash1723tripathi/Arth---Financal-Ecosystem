package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"arth/api/internal/apierr"
	"arth/api/internal/config"
)

func newTestRouter(t *testing.T, api http.Handler) (http.Handler, *bytes.Buffer) {
	t.Helper()
	var logBuf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	prev := apierr.Logger
	apierr.Logger = log
	t.Cleanup(func() { apierr.Logger = prev })
	cfg := config.Config{CORSAllowedOrigins: []string{"https://app.example.com", "http://localhost:3000"}}
	return NewRouter(Deps{Cfg: cfg, Log: log}, api), &logBuf
}

func do(h http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decodeProblem(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Fatalf("Content-Type = %q, want application/problem+json (body %s)", ct, rec.Body)
	}
	var p map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	return p
}

func TestHealthz(t *testing.T) {
	h, _ := newTestRouter(t, nil)
	rec := do(h, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != `{"status":"ok"}` {
		t.Errorf("body = %s", got)
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("missing nosniff")
	}
	if rec.Header().Get(HeaderRequestID) == "" {
		t.Errorf("missing request id header")
	}
}

func TestReadyzWithoutPool(t *testing.T) {
	h, _ := newTestRouter(t, nil)
	rec := do(h, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", rec.Code)
	}
	p := decodeProblem(t, rec)
	if p["code"] != "not_ready" || p["reason"] != "database_unavailable" || p["type"] != "/problems/not_ready" {
		t.Errorf("problem = %v", p)
	}
}

func TestNotFoundAndMethodNotAllowed(t *testing.T) {
	h, _ := newTestRouter(t, nil)
	rec := do(h, httptest.NewRequest(http.MethodGet, "/nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("404 status = %d", rec.Code)
	}
	p := decodeProblem(t, rec)
	if p["code"] != "not_found" || p["request_id"] != rec.Header().Get(HeaderRequestID) || p["request_id"] == "" {
		t.Errorf("404 problem = %v (header id %q)", p, rec.Header().Get(HeaderRequestID))
	}

	rec = do(h, httptest.NewRequest(http.MethodPost, "/healthz", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("405 status = %d", rec.Code)
	}
	if p := decodeProblem(t, rec); p["code"] != "method_not_allowed" {
		t.Errorf("405 problem = %v", p)
	}
}

func TestRequestID(t *testing.T) {
	var seen string
	api := chi.NewRouter()
	api.Get("/v1/ping", func(w http.ResponseWriter, r *http.Request) { seen = RequestID(r.Context()) })
	h, _ := newTestRouter(t, api)

	tests := []struct {
		name, in string
		echo     bool
	}{
		{"sane client id echoed", "abc-DEF_123", true},
		{"max length echoed", strings.Repeat("a", 64), true},
		{"too long replaced", strings.Repeat("a", 65), false},
		{"bad chars replaced", "abc def<script>", false},
		{"empty generated", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/v1/ping", nil)
			if tt.in != "" {
				req.Header.Set(HeaderRequestID, tt.in)
			}
			rec := do(h, req)
			got := rec.Header().Get(HeaderRequestID)
			if got != seen {
				t.Errorf("header %q != context %q", got, seen)
			}
			if tt.echo && got != tt.in {
				t.Errorf("got %q, want echo %q", got, tt.in)
			}
			if !tt.echo && (got == tt.in || len(got) != 32 || !validRequestID(got)) {
				t.Errorf("got %q, want a fresh 32-char id", got)
			}
		})
	}
}

func TestCORS(t *testing.T) {
	api := chi.NewRouter()
	api.Get("/v1/x", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	h, _ := newTestRouter(t, api)

	t.Run("preflight allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodOptions, "/v1/x", nil)
		req.Header.Set("Origin", "https://app.example.com")
		req.Header.Set("Access-Control-Request-Method", "POST")
		rec := do(h, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("status = %d", rec.Code)
		}
		hd := rec.Header()
		if hd.Get("Access-Control-Allow-Origin") != "https://app.example.com" {
			t.Errorf("allow-origin = %q", hd.Get("Access-Control-Allow-Origin"))
		}
		if hd.Get("Access-Control-Allow-Headers") != "Authorization, Content-Type, Idempotency-Key, X-Request-Id" {
			t.Errorf("allow-headers = %q", hd.Get("Access-Control-Allow-Headers"))
		}
		if !strings.Contains(hd.Get("Access-Control-Allow-Methods"), "PATCH") {
			t.Errorf("allow-methods = %q", hd.Get("Access-Control-Allow-Methods"))
		}
		if hd.Get("Access-Control-Allow-Credentials") != "" {
			t.Errorf("credentials must not be allowed")
		}
	})
	t.Run("preflight disallowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodOptions, "/v1/x", nil)
		req.Header.Set("Origin", "https://evil.example.com")
		req.Header.Set("Access-Control-Request-Method", "POST")
		rec := do(h, req)
		if rec.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Errorf("disallowed origin got CORS headers")
		}
	})
	t.Run("simple allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/x", nil)
		req.Header.Set("Origin", "http://localhost:3000")
		rec := do(h, req)
		if rec.Code != http.StatusOK || rec.Header().Get("Access-Control-Allow-Origin") != "http://localhost:3000" {
			t.Errorf("status %d allow-origin %q", rec.Code, rec.Header().Get("Access-Control-Allow-Origin"))
		}
		if rec.Header().Get("Access-Control-Expose-Headers") != "X-Request-Id, Retry-After, Location, Content-Disposition" {
			t.Errorf("expose = %q", rec.Header().Get("Access-Control-Expose-Headers"))
		}
		if rec.Header().Get("Vary") != "Origin" {
			t.Errorf("vary = %q", rec.Header().Values("Vary"))
		}
	})
	t.Run("simple disallowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/x", nil)
		req.Header.Set("Origin", "https://evil.example.com")
		rec := do(h, req)
		if rec.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Errorf("disallowed origin got allow-origin")
		}
	})
}

func TestMaxBodySize(t *testing.T) {
	read := func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.ReadAll(r.Body); err != nil {
			var mbe *http.MaxBytesError
			if errors.As(err, &mbe) {
				apierr.Write(w, r, apierr.PayloadTooLarge())
				return
			}
			apierr.Write(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
	h := MaxBodySize(10, map[string]int64{"/big": 20})(http.HandlerFunc(read))

	tests := []struct {
		name, path string
		size       int
		unknownLen bool
		want       int
	}{
		{"within limit", "/x", 10, false, http.StatusNoContent},
		{"declared too large", "/x", 11, false, http.StatusRequestEntityTooLarge},
		{"streamed too large", "/x", 11, true, http.StatusRequestEntityTooLarge},
		{"override allows more", "/big", 20, false, http.StatusNoContent},
		{"override still capped", "/big", 21, true, http.StatusRequestEntityTooLarge},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var body io.Reader = bytes.NewReader(bytes.Repeat([]byte("a"), tt.size))
			if tt.unknownLen {
				body = io.MultiReader(body) // hides the length from NewRequest
			}
			req := httptest.NewRequest(http.MethodPost, tt.path, body)
			if tt.unknownLen {
				req.ContentLength = -1
			}
			if rec := do(h, req); rec.Code != tt.want {
				t.Errorf("status = %d, want %d", rec.Code, tt.want)
			}
		})
	}
}

func TestRouterBodyLimitDefault(t *testing.T) {
	api := chi.NewRouter()
	api.Post("/v1/x", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	h, _ := newTestRouter(t, api)
	req := httptest.NewRequest(http.MethodPost, "/v1/x", bytes.NewReader(make([]byte, DefaultBodyLimit+1)))
	rec := do(h, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d", rec.Code)
	}
	if p := decodeProblem(t, rec); p["code"] != "payload_too_large" {
		t.Errorf("problem = %v", p)
	}
}

func TestRecovererAndAccessLog(t *testing.T) {
	api := chi.NewRouter()
	api.Get("/v1/boom/{id}", func(w http.ResponseWriter, r *http.Request) { panic("kaboom") })
	api.Get("/v1/items/{id}", func(w http.ResponseWriter, r *http.Request) {
		AddLogAttrs(r.Context(), slog.String("user_id", "u-1"))
		_, _ = w.Write([]byte("hello"))
	})
	h, logBuf := newTestRouter(t, api)

	rec := do(h, httptest.NewRequest(http.MethodGet, "/v1/boom/42?token=secret-query-value", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", rec.Code)
	}
	p := decodeProblem(t, rec)
	if p["code"] != "internal" || strings.Contains(rec.Body.String(), "kaboom") {
		t.Errorf("problem leaks or wrong: %s", rec.Body)
	}

	rec = do(h, httptest.NewRequest(http.MethodGet, "/v1/items/7?email=someone%40example.com", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}

	logs := logBuf.String()
	if strings.Contains(logs, "secret-query-value") || strings.Contains(logs, "someone") {
		t.Errorf("logs contain query string data:\n%s", logs)
	}
	if !strings.Contains(logs, "kaboom") {
		t.Errorf("panic not logged:\n%s", logs)
	}
	var found bool
	for _, line := range strings.Split(strings.TrimSpace(logs), "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("bad log line %q: %v", line, err)
		}
		if m["msg"] != "http request" || m["route"] != "/v1/items/{id}" {
			continue
		}
		found = true
		if m["method"] != "GET" || m["status"] != float64(200) || m["bytes"] != float64(5) || m["user_id"] != "u-1" {
			t.Errorf("access log = %v", m)
		}
		if _, ok := m["duration_ms"]; !ok || m["request_id"] == "" {
			t.Errorf("access log missing duration/request_id: %v", m)
		}
	}
	if !found {
		t.Errorf("no access log line for /v1/items/{id}:\n%s", logs)
	}
}
