package auth_test

import (
	"context"
	"crypto/sha256"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"arth/api/internal/auth"
	"arth/api/internal/db"
	"arth/api/internal/httpapi/gen"
	"arth/api/internal/testutil"
)

var phcRe = regexp.MustCompile(`^\$argon2id\$v=19\$m=19456,t=2,p=1\$[A-Za-z0-9+/]{22}\$[A-Za-z0-9+/]{43}$`)

func TestRegister(t *testing.T) {
	t.Run("201 token pair with Location and needs_onboarding", func(t *testing.T) {
		t.Parallel()
		srv := newSrv(t)
		email := "Mixed.Case-" + strings.TrimPrefix(randomLocal(), "user-") + "@Example.test" // stored as entered
		r := srv.Do(t, http.MethodPost, "/v1/auth/register", "", registerBody(email, strongPassword))
		wantStatus(t, r, http.StatusCreated, "")
		if loc := r.Header.Get("Location"); loc != "/v1/me" {
			t.Fatalf("Location = %q, want /v1/me", loc)
		}
		var p gen.TokenPair
		r.JSON(t, &p)
		if p.AccessToken == "" || p.AccessExpiresIn != 900 {
			t.Fatalf("access token %q expires_in %d", p.AccessToken, p.AccessExpiresIn)
		}
		if !auth.ValidRefreshTokenFormat(p.RefreshToken) {
			t.Fatalf("refresh token has the wrong format (len %d)", len(p.RefreshToken))
		}
		if !near(p.RefreshExpiresAt, srv.Clock.Now().Add(30*24*time.Hour), time.Minute) {
			t.Fatalf("refresh_expires_at = %v, want now+30d", p.RefreshExpiresAt)
		}
		u := p.User
		if u.Email != email {
			t.Fatalf("email = %q, want %q", u.Email, email)
		}
		if !u.NeedsOnboarding || u.Currency != nil || u.Timezone != "UTC" || u.EmailVerified || u.Name == nil || *u.Name != "Asha" {
			t.Fatalf("unexpected Me: %+v", u)
		}
		wantAuthenticated(t, srv, p.AccessToken)
	})

	t.Run("creates the default Cash account", func(t *testing.T) {
		t.Parallel()
		srv := newSrv(t)
		a := register(t, srv)
		rows, err := srv.Deps.Q.ListAccounts(context.Background(), a.UserID())
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 {
			t.Fatalf("%d accounts, want 1", len(rows))
		}
		acc := rows[0]
		if acc.Name != "Cash" || acc.Type != "CASH" || acc.Currency != "INR" || !acc.IsDefault || acc.BalanceMinor != 0 || acc.OpeningBalanceMinor != 0 {
			t.Fatalf("default account = %+v", acc)
		}
	})

	t.Run("creates a session and stores only the refresh token hash", func(t *testing.T) {
		t.Parallel()
		srv := newSrv(t)
		a := register(t, srv)
		sid := sessionOf(t, srv, a.Pair.RefreshToken)
		s, err := srv.Deps.Q.GetSession(context.Background(), db.GetSessionParams{ID: sid, UserID: a.UserID()})
		if err != nil {
			t.Fatal(err)
		}
		if s.Client != "MOBILE" || s.RevokedAt != nil || !near(s.ExpiresAt, srv.Clock.Now().Add(90*24*time.Hour), time.Minute) {
			t.Fatalf("session = %+v", s)
		}
		if s.Ip == nil || !s.Ip.IsLoopback() {
			t.Fatalf("session ip = %v, want loopback", s.Ip)
		}
		sum := sha256.Sum256([]byte(a.Pair.RefreshToken))
		if n := count(t, srv, `SELECT count(*) FROM refresh_tokens WHERE session_id = $1 AND token_hash = $2`, sid, sum[:]); n != 1 {
			t.Fatalf("refresh token rows with sha256 hash = %d, want 1", n)
		}
		if n := count(t, srv, `SELECT count(*) FROM refresh_tokens rt WHERE session_id = $1 AND position($2 in row_to_json(rt)::text) > 0`, sid, a.Pair.RefreshToken); n != 0 {
			t.Fatal("raw refresh token stored in refresh_tokens")
		}
	})

	t.Run("duplicate email in any case → 409 email_taken", func(t *testing.T) {
		t.Parallel()
		srv := newSrv(t)
		local := randomLocal()
		r := srv.Do(t, http.MethodPost, "/v1/auth/register", "", registerBody(strings.ToUpper(local)+"@X.example.test", strongPassword))
		wantStatus(t, r, http.StatusCreated, "")
		for _, dup := range []string{local + "@x.example.test", strings.ToUpper(local) + "@X.EXAMPLE.TEST", local + "@X.Example.Test"} {
			r = srv.Do(t, http.MethodPost, "/v1/auth/register", "", registerBody(dup, strongPassword))
			wantStatus(t, r, http.StatusConflict, "email_taken")
		}
		if n := count(t, srv, `SELECT count(*) FROM users WHERE lower(email) = lower($1)`, local+"@x.example.test"); n != 1 {
			t.Fatalf("%d users with that email, want 1", n)
		}
	})

	t.Run("weak, short or too-long password → 422 weak_password", func(t *testing.T) {
		t.Parallel()
		srv := newSrv(t)
		for name, pw := range map[string]string{
			"7 chars":     "zq8#Lm2",
			"129 chars":   strings.Repeat("a", 128) + "b",
			"common":      "password",
			"common case": "Password123",
			"only spaces": "          ",
		} {
			email := testutil.RandomEmail()
			r := srv.Do(t, http.MethodPost, "/v1/auth/register", "", registerBody(email, pw))
			if r.Status != http.StatusUnprocessableEntity || r.Code() != "weak_password" {
				t.Fatalf("%s: got %d %q, want 422 weak_password", name, r.Status, r.Code())
			}
			if n := count(t, srv, `SELECT count(*) FROM users WHERE email = $1`, email); n != 0 {
				t.Fatalf("%s: user created despite weak password", name)
			}
		}
	})

	t.Run("client enum and required fields → 422 validation_failed, unknown field → 400", func(t *testing.T) {
		t.Parallel()
		srv := newSrv(t)
		bad := registerBody(testutil.RandomEmail(), strongPassword)
		bad["client"] = "DESKTOP"
		wantStatus(t, srv.Do(t, http.MethodPost, "/v1/auth/register", "", bad), http.StatusUnprocessableEntity, "validation_failed")
		missing := registerBody(testutil.RandomEmail(), strongPassword)
		delete(missing, "client")
		wantStatus(t, srv.Do(t, http.MethodPost, "/v1/auth/register", "", missing), http.StatusUnprocessableEntity, "validation_failed")
		extra := registerBody(testutil.RandomEmail(), strongPassword)
		extra["user_id"] = "00000000-0000-0000-0000-000000000001"
		wantStatus(t, srv.Do(t, http.MethodPost, "/v1/auth/register", "", extra), http.StatusBadRequest, "bad_request")
		lb := loginBody(testutil.RandomEmail(), strongPassword)
		lb["client"] = "TV"
		wantStatus(t, srv.Do(t, http.MethodPost, "/v1/auth/login", "", lb), http.StatusUnprocessableEntity, "validation_failed")
	})

	t.Run("stored hash is a PHC Argon2id string and the plaintext is nowhere in the users row", func(t *testing.T) {
		t.Parallel()
		srv := newSrv(t)
		a := register(t, srv)
		u, err := srv.Deps.Q.GetUserByID(context.Background(), a.UserID())
		if err != nil {
			t.Fatal(err)
		}
		if !phcRe.MatchString(u.PasswordHash) {
			t.Fatalf("password_hash %q is not a PHC argon2id string", u.PasswordHash)
		}
		if n := count(t, srv, `SELECT count(*) FROM users u WHERE id = $1 AND position($2 in row_to_json(u)::text) > 0`, a.UserID(), a.Password); n != 0 {
			t.Fatal("plaintext password found in the users row")
		}
	})
}

func TestLogin(t *testing.T) {
	t.Run("correct password → 200 with a new session", func(t *testing.T) {
		t.Parallel()
		srv := newSrv(t)
		a := register(t, srv)
		p := login(t, srv, strings.ToUpper(a.Email), a.Password) // lookup is case-insensitive
		if p.User.Id != a.UserID() || p.RefreshToken == a.Pair.RefreshToken {
			t.Fatalf("login pair = %+v", p)
		}
		if s1, s2 := sessionOf(t, srv, a.Pair.RefreshToken), sessionOf(t, srv, p.RefreshToken); s1 == s2 {
			t.Fatal("login reused the register session")
		}
		wantAuthenticated(t, srv, p.AccessToken)
		wantAuthenticated(t, srv, a.Pair.AccessToken)
	})

	t.Run("wrong password and unknown email → identical 401 body", func(t *testing.T) {
		t.Parallel()
		srv := newSrv(t)
		a := register(t, srv)
		wrong := srv.Do(t, http.MethodPost, "/v1/auth/login", "", loginBody(a.Email, a.Password+"x"))
		unknown := srv.Do(t, http.MethodPost, "/v1/auth/login", "", loginBody(testutil.RandomEmail(), a.Password))
		wantStatus(t, wrong, http.StatusUnauthorized, "invalid_credentials")
		wantStatus(t, unknown, http.StatusUnauthorized, "invalid_credentials")
		if w, u := problemWithoutRequestID(t, wrong.Body), problemWithoutRequestID(t, unknown.Body); w != u {
			t.Fatalf("bodies differ:\nwrong   %s\nunknown %s", w, u)
		}
		if w, u := wrong.Header.Get("Content-Type"), unknown.Header.Get("Content-Type"); w != u {
			t.Fatalf("content types differ: %q vs %q", w, u)
		}
	})

	t.Run("timing of unknown email vs wrong password is roughly equal (smoke)", func(t *testing.T) {
		// Deliberately not t.Parallel: the parallel siblings above stay paused until this function returns, so
		// the measurement runs alone in this package. Median of 3 cuts noise; the bounds are loose on purpose.
		srv := newSrv(t)
		a := register(t, srv)
		measure := func(email, pw string) time.Duration {
			var ds []time.Duration
			for i := 0; i < 3; i++ {
				start := time.Now()
				r := srv.Do(t, http.MethodPost, "/v1/auth/login", "", loginBody(email, pw))
				ds = append(ds, time.Since(start))
				wantStatus(t, r, http.StatusUnauthorized, "invalid_credentials")
			}
			return median(ds)
		}
		unknown := measure(testutil.RandomEmail(), "some wrong password")
		wrong := measure(a.Email, "some wrong password")
		t.Logf("median unknown-email %v, wrong-password %v", unknown, wrong)
		for name, d := range map[string]time.Duration{"unknown email": unknown, "wrong password": wrong} {
			if d < 5*time.Millisecond {
				t.Fatalf("%s took %v; want > 5ms (Argon2id must run)", name, d)
			}
		}
		if unknown > 10*wrong || wrong > 10*unknown {
			t.Fatalf("timings differ by more than 10x: unknown=%v wrong=%v", unknown, wrong)
		}
	})

	t.Run("rehash on login when the stored parameters are older", func(t *testing.T) {
		t.Parallel()
		srv := newSrv(t)
		a := register(t, srv)
		old, err := auth.NewHasher(auth.HashParams{Memory: 12288, Time: 3, Threads: 1, SaltLen: 16, KeyLen: 32})
		if err != nil {
			t.Fatal(err)
		}
		oldHash, err := old.Hash(a.Password)
		if err != nil {
			t.Fatal(err)
		}
		var changedBefore time.Time
		if err := srv.Deps.Pool.QueryRow(context.Background(),
			`UPDATE users SET password_hash = $1 WHERE id = $2 RETURNING password_changed_at`, oldHash, a.UserID()).Scan(&changedBefore); err != nil {
			t.Fatal(err)
		}
		login(t, srv, a.Email, a.Password)
		u, err := srv.Deps.Q.GetUserByID(context.Background(), a.UserID())
		if err != nil {
			t.Fatal(err)
		}
		if !phcRe.MatchString(u.PasswordHash) {
			t.Fatalf("hash not upgraded: %q", u.PasswordHash)
		}
		if !u.PasswordChangedAt.Equal(changedBefore) {
			t.Fatal("transparent rehash must not bump password_changed_at")
		}
		login(t, srv, a.Email, a.Password) // still the same password
	})
}

func median(ds []time.Duration) time.Duration {
	s := append([]time.Duration(nil), ds...)
	for i := range s {
		for j := i + 1; j < len(s); j++ {
			if s[j] < s[i] {
				s[i], s[j] = s[j], s[i]
			}
		}
	}
	return s[len(s)/2]
}
