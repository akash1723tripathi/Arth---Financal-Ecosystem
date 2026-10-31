package auth_test

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"arth/api/internal/auth"
	"arth/api/internal/httpapi"
	"arth/api/internal/testutil"
)

func wantRetryAfter(t *testing.T, r *testutil.Response) {
	t.Helper()
	wantStatus(t, r, http.StatusTooManyRequests, "rate_limited")
	secs, err := strconv.Atoi(r.Header.Get("Retry-After"))
	if err != nil || secs < 1 {
		t.Fatalf("Retry-After = %q, want a positive number of seconds", r.Header.Get("Retry-After"))
	}
}

func TestRateLimits(t *testing.T) {
	t.Run("login: 5 failures per email → 429 with Retry-After (even with the right password), reset after the window", func(t *testing.T) {
		t.Parallel()
		srv := newSrv(t)
		a := register(t, srv)
		for i := 0; i < auth.RuleLoginEmail.Limit; i++ {
			wantStatus(t, srv.Do(t, http.MethodPost, "/v1/auth/login", "", loginBody(a.Email, "wrong password!")), http.StatusUnauthorized, "invalid_credentials")
		}
		wantRetryAfter(t, srv.Do(t, http.MethodPost, "/v1/auth/login", "", loginBody(strings.ToUpper(a.Email), a.Password)))
		srv.Clock.Advance(auth.RuleLoginEmail.Window)
		login(t, srv, a.Email, a.Password)
	})

	t.Run("login: failures for unknown emails count too", func(t *testing.T) {
		t.Parallel()
		srv := newSrv(t)
		email := testutil.RandomEmail()
		for i := 0; i < auth.RuleLoginEmail.Limit; i++ {
			wantStatus(t, srv.Do(t, http.MethodPost, "/v1/auth/login", "", loginBody(email, "whatever pass")), http.StatusUnauthorized, "invalid_credentials")
		}
		wantRetryAfter(t, srv.Do(t, http.MethodPost, "/v1/auth/login", "", loginBody(email, "whatever pass")))
	})

	t.Run("login: 20 attempts per IP → 429, reset after the window", func(t *testing.T) {
		t.Parallel()
		srv := newSrv(t)
		a := register(t, srv)
		for i := 0; i < auth.RuleLoginIP.Limit; i++ { // distinct emails so only the IP bucket fills
			wantStatus(t, srv.Do(t, http.MethodPost, "/v1/auth/login", "", loginBody(testutil.RandomEmail(), "whatever pass")), http.StatusUnauthorized, "invalid_credentials")
		}
		wantRetryAfter(t, srv.Do(t, http.MethodPost, "/v1/auth/login", "", loginBody(a.Email, a.Password)))
		srv.Clock.Advance(auth.RuleLoginIP.Window)
		login(t, srv, a.Email, a.Password)
	})

	t.Run("register: 5 per hour per IP → 429, reset after the window", func(t *testing.T) {
		t.Parallel()
		srv := newSrv(t)
		for i := 0; i < auth.RuleRegisterIP.Limit; i++ {
			register(t, srv)
		}
		email := testutil.RandomEmail()
		wantRetryAfter(t, srv.Do(t, http.MethodPost, "/v1/auth/register", "", registerBody(email, strongPassword)))
		if n := count(t, srv, `SELECT count(*) FROM users WHERE email = $1`, email); n != 0 {
			t.Fatal("user created despite 429")
		}
		srv.Clock.Advance(auth.RuleRegisterIP.Window)
		wantStatus(t, srv.Do(t, http.MethodPost, "/v1/auth/register", "", registerBody(email, strongPassword)), http.StatusCreated, "")
	})
	// refresh: TestRefresh/"rate limit 60/h per session ..."
}

// TestNoSecretsInLogs runs every auth flow against a server whose logger is captured at DEBUG and checks that
// no password, email, refresh token or access token was written.
func TestNoSecretsInLogs(t *testing.T) {
	var buf syncBuffer
	log := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	deps, closeDeps, err := httpapi.BuildDeps(testutil.Config(), testutil.Pool(t), log, nil)
	if err != nil {
		t.Fatal(err)
	}
	h, err := httpapi.NewHandler(deps)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(h)
	t.Cleanup(func() { ts.Close(); closeDeps() })
	srv := &testutil.Server{Server: ts, Deps: deps, Clock: &testutil.Clock{}}

	a := register(t, srv)
	wantStatus(t, srv.Do(t, http.MethodPost, "/v1/auth/login", "", loginBody(a.Email, "wrong password!")), http.StatusUnauthorized, "invalid_credentials")
	wantStatus(t, srv.Do(t, http.MethodPost, "/v1/auth/login", "", loginBody("Unknown-"+a.Email, a.Password)), http.StatusUnauthorized, "invalid_credentials")
	second := login(t, srv, a.Email, a.Password)
	rotated := mustRefresh(t, srv, second.RefreshToken)
	wantStatus(t, refresh(t, srv, second.RefreshToken), http.StatusUnauthorized, "refresh_invalid") // reuse → security event
	const newPassword = "another long passphrase"
	wantStatus(t, srv.Do(t, http.MethodPost, "/v1/auth/password", a.Pair.AccessToken, map[string]any{"current_password": a.Password, "new_password": newPassword}), http.StatusNoContent, "")
	wantStatus(t, srv.Do(t, http.MethodPost, "/v1/auth/logout", a.Pair.AccessToken, map[string]any{"refresh_token": a.Pair.RefreshToken}), http.StatusNoContent, "")
	third := login(t, srv, a.Email, newPassword)
	wantStatus(t, srv.Do(t, http.MethodDelete, "/v1/me", third.AccessToken, map[string]any{"password": newPassword}), http.StatusNoContent, "")

	out := buf.String()
	if !strings.Contains(out, "security: refresh token reuse") {
		t.Fatalf("expected the reuse security event in the logs; got:\n%s", out)
	}
	email := strings.ToLower(a.Email)
	local := email[:strings.IndexByte(email, '@')]
	secrets := map[string]string{
		"password":          a.Password,
		"new password":      newPassword,
		"wrong password":    "wrong password!",
		"email local part":  local,
		"refresh token (1)": a.Pair.RefreshToken,
		"refresh token (2)": second.RefreshToken,
		"refresh token (3)": rotated.RefreshToken,
		"access token (1)":  a.Pair.AccessToken,
		"access token (2)":  third.AccessToken,
	}
	lower := strings.ToLower(out)
	for name, s := range secrets {
		if strings.Contains(lower, strings.ToLower(s)) {
			t.Errorf("%s appears in the logs", name)
		}
	}
	// Argon2id PHC prefix: no hash may be logged either.
	if strings.Contains(out, "$argon2id$") {
		t.Error("a password hash appears in the logs")
	}
	t.Logf("captured %d log lines, none contain secrets", strings.Count(out, "\n"))
}
