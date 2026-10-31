// Package testutil gives integration tests a migrated REAL Postgres (TEST_DATABASE_URL), an httptest server
// running the full production handler, and helpers to create users with valid access tokens.
//
// Tests that use it must live in an external test package (e.g. package httpapi_test) to avoid import cycles.
// Every test creates its own users, so packages can run in parallel against the same database.
package testutil

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"arth/api/internal/auth"
	"arth/api/internal/config"
	"arth/api/internal/db"
	"arth/api/internal/httpapi"
	"arth/api/internal/migrate"
)

var (
	poolOnce sync.Once
	pool     *pgxpool.Pool
	poolErr  error
)

// apiDir is apps/api (resolved from this file's location).
func apiDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

// databaseURL returns TEST_DATABASE_URL from the environment or apps/api/.env. The value is never printed.
func databaseURL(t testing.TB) string {
	if v := os.Getenv("TEST_DATABASE_URL"); v != "" {
		return v
	}
	_ = config.LoadDotEnv(filepath.Join(apiDir(), ".env"))
	v := os.Getenv("TEST_DATABASE_URL")
	if v == "" {
		t.Skip("TEST_DATABASE_URL not set (see apps/api/.env.example); skipping DB integration test")
	}
	return v
}

// Pool returns a shared pool on the migrated test database (goose up runs once per test binary; goose's
// session lock serialises concurrent packages).
func Pool(t testing.TB) *pgxpool.Pool {
	t.Helper()
	url := databaseURL(t)
	poolOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		if poolErr = migrate.Up(ctx, url, io.Discard); poolErr != nil {
			return
		}
		pool, poolErr = pgxpool.New(context.Background(), url)
	})
	if poolErr != nil {
		t.Fatalf("test database: %v", redact(poolErr.Error(), url))
	}
	return pool
}

func redact(msg, url string) string {
	if url != "" {
		msg = strings.ReplaceAll(msg, url, "<TEST_DATABASE_URL>")
	}
	return msg
}

// Clock is a settable clock shared by tokens and rate limiters of a test server.
type Clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *Clock) Now() time.Time         { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *Clock) Advance(d time.Duration) { c.mu.Lock(); c.t = c.t.Add(d); c.mu.Unlock() }

// Server is a running test API.
type Server struct {
	*httptest.Server
	Deps  httpapi.Deps
	Clock *Clock
}

// Config returns a test config (random signing key, fake/log/memory drivers).
func Config() config.Config {
	key := make([]byte, 48)
	_, _ = rand.Read(key)
	return config.Config{
		AppEnv:                "test",
		Port:                  0,
		LogLevel:              slog.LevelWarn,
		AccessTokenSigningKey: key,
		AccessTokenTTL:        15 * time.Minute,
		RefreshTokenTTL:       30 * 24 * time.Hour,
		SessionMaxAge:         90 * 24 * time.Hour,
		CORSAllowedOrigins:    []string{"http://localhost:3000"},
		AppPublicURL:          "http://localhost:3000",
		AIDriver:              "fake",
		EmailDriver:           "none",
		StorageDriver:         "memory",
	}
}

// NewServer starts the full production handler on the test database. mutate (optional) can change the
// config before deps are built. The clock starts at the real current time and only moves when Advance is called.
func NewServer(t testing.TB, mutate ...func(*config.Config)) *Server {
	t.Helper()
	p := Pool(t)
	cfg := Config()
	for _, m := range mutate {
		m(&cfg)
	}
	clock := &Clock{t: time.Now().UTC()}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	deps, closeDeps, err := httpapi.BuildDeps(cfg, p, log, clock.Now)
	if err != nil {
		t.Fatalf("build deps: %v", err)
	}
	h, err := httpapi.NewHandler(deps)
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(func() { srv.Close(); closeDeps() })
	return &Server{Server: srv, Deps: deps, Clock: clock}
}

// Response is a captured HTTP response.
type Response struct {
	Status int
	Header http.Header
	Body   []byte
}

// JSON decodes the body into v (fails the test on error).
func (r *Response) JSON(t testing.TB, v any) {
	t.Helper()
	if err := json.Unmarshal(r.Body, v); err != nil {
		t.Fatalf("decode %d response %q: %v", r.Status, truncate(string(r.Body)), err)
	}
}

// Code returns the problem+json "code" field ("" if absent).
func (r *Response) Code() string {
	var p struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(r.Body, &p)
	return p.Code
}

func truncate(s string) string {
	if len(s) > 500 {
		return s[:500] + "…"
	}
	return s
}

// Do sends a request. body may be nil, a string/[]byte (sent as-is) or any value (JSON-encoded).
// token "" sends no Authorization header.
func (s *Server) Do(t testing.TB, method, path, token string, body any) *Response {
	t.Helper()
	return s.DoWith(t, method, path, token, body, nil)
}

// DoWith is Do with extra headers.
func (s *Server) DoWith(t testing.TB, method, path, token string, body any, headers map[string]string) *Response {
	t.Helper()
	var rdr io.Reader
	switch b := body.(type) {
	case nil:
	case string:
		rdr = strings.NewReader(b)
	case []byte:
		rdr = bytes.NewReader(b)
	default:
		buf, err := json.Marshal(b)
		if err != nil {
			t.Fatalf("encode body: %v", err)
		}
		rdr = bytes.NewReader(buf)
	}
	req, err := http.NewRequest(method, s.URL+path, rdr)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if rdr != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := s.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return &Response{Status: res.StatusCode, Header: res.Header, Body: b}
}

// User is a test user created directly in the database (not through /v1/auth/register).
type User struct {
	ID               uuid.UUID
	Email            string
	Password         string
	SessionID        uuid.UUID
	AccessToken      string
	DefaultAccountID uuid.UUID
}

// UserOpts customises CreateUser.
type UserOpts struct {
	Onboarded bool   // complete onboarding (currency + timezone) — default false
	Currency  string // default "INR"
	Timezone  string // default "Asia/Kolkata" when onboarded
}

// RandomEmail returns a unique address on a reserved test domain.
func RandomEmail() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return "user-" + hex.EncodeToString(b) + "@example.test"
}

// CreateUser inserts a user (Argon2id hash of a random password), the default "Cash" account (like
// register does), a session, and mints an access token for that session.
func (s *Server) CreateUser(t testing.TB, opts ...UserOpts) User {
	t.Helper()
	o := UserOpts{}
	if len(opts) > 0 {
		o = opts[0]
	}
	if o.Currency == "" {
		o.Currency = "INR"
	}
	if o.Timezone == "" {
		o.Timezone = "Asia/Kolkata"
	}
	ctx := context.Background()
	pwb := make([]byte, 12)
	_, _ = rand.Read(pwb)
	password := "pw-" + hex.EncodeToString(pwb)
	hash, err := s.Deps.Hasher.Hash(password)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	name := "Test User"
	var u User
	err = db.InTx(ctx, s.Deps.Pool, func(q *db.Queries) error {
		row, err := q.CreateUser(ctx, db.CreateUserParams{Email: RandomEmail(), PasswordHash: hash, Name: &name})
		if err != nil {
			return err
		}
		acct, err := q.CreateAccount(ctx, db.CreateAccountParams{UserID: row.ID, Name: "Cash", Type: "CASH", Currency: "INR", IsDefault: true})
		if err != nil {
			return err
		}
		if o.Onboarded {
			if _, err := q.CompleteOnboarding(ctx, db.CompleteOnboardingParams{Currency: o.Currency, Timezone: o.Timezone, ID: row.ID}); err != nil {
				return err
			}
			if _, err := q.SetDefaultAccountOpening(ctx, db.SetDefaultAccountOpeningParams{Currency: o.Currency, OpeningBalanceMinor: 0, UserID: row.ID}); err != nil {
				return err
			}
		}
		sess, err := q.CreateSession(ctx, db.CreateSessionParams{UserID: row.ID, Client: "MOBILE", ExpiresAt: s.Clock.Now().Add(90 * 24 * time.Hour)})
		if err != nil {
			return err
		}
		u = User{ID: row.ID, Email: row.Email, Password: password, SessionID: sess.ID, DefaultAccountID: acct.ID}
		return nil
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	u.AccessToken = s.Token(t, u.ID, u.SessionID)
	return u
}

// Token mints an access token for (user, session) with the server's key and clock.
func (s *Server) Token(t testing.TB, userID, sessionID uuid.UUID) string {
	t.Helper()
	tok, _, err := s.Deps.Tokens.Issue(userID, sessionID)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	return tok
}

// Principal is re-exported for tests that need to build contexts.
type Principal = auth.Principal

// Balance reads an account's cached balance and recomputes the truth (opening + Σ delta). Tests assert both
// are equal and match the expected value (the balance invariant, docs/SCHEMA.md §5).
func (s *Server) Balance(t testing.TB, accountID uuid.UUID) (cached, truth int64) {
	t.Helper()
	err := s.Deps.Pool.QueryRow(context.Background(), `
		SELECT a.balance_minor,
		       a.opening_balance_minor + COALESCE((SELECT SUM(delta_minor) FROM transactions WHERE account_id = a.id), 0)::bigint
		FROM accounts a WHERE a.id = $1`, accountID).Scan(&cached, &truth)
	if err != nil {
		t.Fatalf("balance: %v", err)
	}
	return cached, truth
}

// AssertBalance fails unless the cached balance equals the recomputed one and want.
func (s *Server) AssertBalance(t testing.TB, accountID uuid.UUID, want int64) {
	t.Helper()
	cached, truth := s.Balance(t, accountID)
	if cached != truth || cached != want {
		t.Fatalf("balance invariant: cached=%d truth=%d want=%d", cached, truth, want)
	}
}
