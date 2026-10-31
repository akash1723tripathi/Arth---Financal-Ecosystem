// Package auth_test is the auth slice's integration suite (docs/AUTH.md §9) against REAL Postgres through the
// full production handler.
//
// "Token works" assertions use GET /v1/me and accept any status other than 401 (the per-request session check
// passed). GET /v1/me belongs to the me slice and may still be the 501 stub; 501 ≠ 401 means "authenticated".
//
// Rate limits are in-memory per server instance and every request comes from 127.0.0.1, so each subtest that
// registers or logs in several times builds its own server (newSrv) to stay under the per-IP buckets.
package auth_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"arth/api/internal/auth"
	"arth/api/internal/httpapi/gen"
	"arth/api/internal/testutil"
)

const strongPassword = "correct horse battery staple"

func newSrv(t *testing.T) *testutil.Server {
	t.Helper()
	return testutil.NewServer(t)
}

func registerBody(email, password string) map[string]any {
	return map[string]any{"email": email, "password": password, "name": "Asha", "client": "MOBILE"}
}

func loginBody(email, password string) map[string]any {
	return map[string]any{"email": email, "password": password, "client": "MOBILE"}
}

// account is a user registered through the API with one session.
type account struct {
	Email    string
	Password string
	Pair     gen.TokenPair
}

func (a account) UserID() uuid.UUID { return a.Pair.User.Id }

func register(t *testing.T, srv *testutil.Server) account {
	t.Helper()
	email := testutil.RandomEmail()
	r := srv.Do(t, http.MethodPost, "/v1/auth/register", "", registerBody(email, strongPassword))
	if r.Status != http.StatusCreated {
		t.Fatalf("register = %d %s", r.Status, r.Body)
	}
	var p gen.TokenPair
	r.JSON(t, &p)
	return account{Email: email, Password: strongPassword, Pair: p}
}

func login(t *testing.T, srv *testutil.Server, email, password string) gen.TokenPair {
	t.Helper()
	r := srv.Do(t, http.MethodPost, "/v1/auth/login", "", loginBody(email, password))
	if r.Status != http.StatusOK {
		t.Fatalf("login = %d %s", r.Status, r.Body)
	}
	var p gen.TokenPair
	r.JSON(t, &p)
	return p
}

func refresh(t *testing.T, srv *testutil.Server, rt string) *testutil.Response {
	t.Helper()
	return srv.Do(t, http.MethodPost, "/v1/auth/refresh", "", map[string]any{"refresh_token": rt})
}

func mustRefresh(t *testing.T, srv *testutil.Server, rt string) gen.TokenPair {
	t.Helper()
	r := refresh(t, srv, rt)
	if r.Status != http.StatusOK {
		t.Fatalf("refresh = %d %s", r.Status, r.Body)
	}
	var p gen.TokenPair
	r.JSON(t, &p)
	return p
}

func wantStatus(t *testing.T, r *testutil.Response, status int, code string) {
	t.Helper()
	if r.Status != status || (code != "" && r.Code() != code) {
		t.Fatalf("got %d %q, want %d %q (body %s)", r.Status, r.Code(), status, code, r.Body)
	}
}

// authenticated reports whether token passes the auth middleware (GET /v1/me ≠ 401; 501 stub counts as passed).
func authenticated(t *testing.T, srv *testutil.Server, token string) bool {
	t.Helper()
	r := srv.Do(t, http.MethodGet, "/v1/me", token, nil)
	if r.Status == http.StatusUnauthorized {
		if r.Code() != "unauthorized" {
			t.Fatalf("401 with code %q, want unauthorized", r.Code())
		}
		return false
	}
	return true
}

func wantAuthenticated(t *testing.T, srv *testutil.Server, token string) {
	t.Helper()
	if !authenticated(t, srv, token) {
		t.Fatal("access token rejected (401), want accepted")
	}
}

func wantRejected(t *testing.T, srv *testutil.Server, token string) {
	t.Helper()
	if authenticated(t, srv, token) {
		t.Fatal("access token accepted, want 401")
	}
}

// sessionOf returns the session id that owns a raw refresh token.
func sessionOf(t *testing.T, srv *testutil.Server, rt string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := srv.Deps.Pool.QueryRow(context.Background(),
		`SELECT session_id FROM refresh_tokens WHERE token_hash = $1`, auth.HashRefreshToken(rt)).Scan(&id); err != nil {
		t.Fatalf("session of refresh token: %v", err)
	}
	return id
}

// sessionState returns (revoked, revoke_reason) of a session.
func sessionState(t *testing.T, srv *testutil.Server, id uuid.UUID) (bool, string) {
	t.Helper()
	var revoked bool
	var reason *string
	if err := srv.Deps.Pool.QueryRow(context.Background(),
		`SELECT revoked_at IS NOT NULL, revoke_reason FROM sessions WHERE id = $1`, id).Scan(&revoked, &reason); err != nil {
		t.Fatalf("session state: %v", err)
	}
	if reason == nil {
		return revoked, ""
	}
	return revoked, *reason
}

func wantSessionRevoked(t *testing.T, srv *testutil.Server, id uuid.UUID, reason string) {
	t.Helper()
	revoked, got := sessionState(t, srv, id)
	if !revoked || got != reason {
		t.Fatalf("session %s revoked=%v reason=%q, want revoked with %q", id, revoked, got, reason)
	}
}

func wantSessionActive(t *testing.T, srv *testutil.Server, id uuid.UUID) {
	t.Helper()
	if revoked, reason := sessionState(t, srv, id); revoked {
		t.Fatalf("session %s revoked (%q), want active", id, reason)
	}
}

func count(t *testing.T, srv *testutil.Server, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := srv.Deps.Pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatalf("count %q: %v", sql, err)
	}
	return n
}

// problemWithoutRequestID strips the per-request id so two problem bodies can be compared byte for byte.
func problemWithoutRequestID(t *testing.T, b []byte) string {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("decode problem %q: %v", b, err)
	}
	delete(m, "request_id")
	out, err := json.Marshal(m) // map keys are sorted, so equal problems give equal strings
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func near(got, want time.Time, tol time.Duration) bool {
	d := got.Sub(want)
	return d >= -tol && d <= tol
}

// syncBuffer collects log output from concurrent handlers.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// randomLocal is a unique email local part.
func randomLocal() string {
	e := testutil.RandomEmail()
	return e[:strings.IndexByte(e, '@')]
}
