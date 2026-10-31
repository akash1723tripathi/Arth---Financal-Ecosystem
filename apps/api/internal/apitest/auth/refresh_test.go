package auth_test

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"arth/api/internal/auth"
	"arth/api/internal/config"
	"arth/api/internal/testutil"
)

func TestRefresh(t *testing.T) {
	t.Run("rotates: old token marked used and linked, new pair works", func(t *testing.T) {
		t.Parallel()
		srv := newSrv(t)
		a := register(t, srv)
		p := mustRefresh(t, srv, a.Pair.RefreshToken)
		if p.RefreshToken == a.Pair.RefreshToken || p.AccessToken == a.Pair.AccessToken || p.User.Id != a.UserID() {
			t.Fatal("refresh did not rotate")
		}
		var used bool
		var replacedBy, newID uuid.UUID
		ctx := context.Background()
		if err := srv.Deps.Pool.QueryRow(ctx, `SELECT used_at IS NOT NULL, replaced_by FROM refresh_tokens WHERE token_hash = $1`,
			auth.HashRefreshToken(a.Pair.RefreshToken)).Scan(&used, &replacedBy); err != nil {
			t.Fatal(err)
		}
		if err := srv.Deps.Pool.QueryRow(ctx, `SELECT id FROM refresh_tokens WHERE token_hash = $1`,
			auth.HashRefreshToken(p.RefreshToken)).Scan(&newID); err != nil {
			t.Fatal(err)
		}
		if !used || replacedBy != newID {
			t.Fatalf("old token used=%v replaced_by=%v, want used and linked to %v", used, replacedBy, newID)
		}
		if sessionOf(t, srv, p.RefreshToken) != sessionOf(t, srv, a.Pair.RefreshToken) {
			t.Fatal("successor belongs to a different session")
		}
		wantAuthenticated(t, srv, p.AccessToken)
		p2 := mustRefresh(t, srv, p.RefreshToken)
		wantAuthenticated(t, srv, p2.AccessToken)
	})

	t.Run("reusing a used token → 401 and the whole session is revoked (successor dies too)", func(t *testing.T) {
		t.Parallel()
		srv := newSrv(t)
		a := register(t, srv)
		sid := sessionOf(t, srv, a.Pair.RefreshToken)
		p := mustRefresh(t, srv, a.Pair.RefreshToken)
		wantStatus(t, refresh(t, srv, a.Pair.RefreshToken), http.StatusUnauthorized, "refresh_invalid")
		wantSessionRevoked(t, srv, sid, "REFRESH_REUSE")
		wantStatus(t, refresh(t, srv, p.RefreshToken), http.StatusUnauthorized, "refresh_invalid")
		wantRejected(t, srv, p.AccessToken)
		wantRejected(t, srv, a.Pair.AccessToken)
	})

	t.Run("concurrent refresh with one token: no grace window (Q-8), one wins and the session is revoked", func(t *testing.T) {
		t.Parallel()
		srv := newSrv(t)
		a := register(t, srv)
		sid := sessionOf(t, srv, a.Pair.RefreshToken)
		var wg sync.WaitGroup
		statuses := make([]int, 2)
		for i := range statuses {
			wg.Add(1)
			go func() {
				defer wg.Done()
				statuses[i] = refresh(t, srv, a.Pair.RefreshToken).Status
			}()
		}
		wg.Wait()
		ok, unauthorized := 0, 0
		for _, s := range statuses {
			switch s {
			case http.StatusOK:
				ok++
			case http.StatusUnauthorized:
				unauthorized++
			}
		}
		if ok != 1 || unauthorized != 1 {
			t.Fatalf("statuses = %v, want one 200 and one 401", statuses)
		}
		wantSessionRevoked(t, srv, sid, "REFRESH_REUSE")
	})

	t.Run("malformed or unknown token → 401 refresh_invalid", func(t *testing.T) {
		t.Parallel()
		srv := newSrv(t)
		raw, _, err := auth.NewRefreshToken()
		if err != nil {
			t.Fatal(err)
		}
		for name, tok := range map[string]string{
			"short":        "abc",
			"bad alphabet": strings.Repeat("!", auth.RefreshTokenLen),
			"too long":     raw + "A",
			"unknown":      raw,
		} {
			r := refresh(t, srv, tok)
			if r.Status != http.StatusUnauthorized || r.Code() != "refresh_invalid" {
				t.Fatalf("%s: got %d %q", name, r.Status, r.Code())
			}
		}
		wantStatus(t, srv.Do(t, http.MethodPost, "/v1/auth/refresh", "", map[string]any{"refresh_token": ""}), http.StatusUnprocessableEntity, "validation_failed")
	})

	t.Run("revoked session → 401", func(t *testing.T) {
		t.Parallel()
		srv := newSrv(t)
		a := register(t, srv)
		wantStatus(t, srv.Do(t, http.MethodPost, "/v1/auth/logout-all", a.Pair.AccessToken, nil), http.StatusNoContent, "")
		wantStatus(t, refresh(t, srv, a.Pair.RefreshToken), http.StatusUnauthorized, "refresh_invalid")
	})

	t.Run("session past its absolute lifetime → refresh fails (expires_at set in the past via SQL)", func(t *testing.T) {
		t.Parallel()
		srv := newSrv(t)
		a := register(t, srv)
		sid := sessionOf(t, srv, a.Pair.RefreshToken)
		if _, err := srv.Deps.Pool.Exec(context.Background(), `UPDATE sessions SET expires_at = $1 WHERE id = $2`,
			srv.Clock.Now().Add(-time.Minute), sid); err != nil {
			t.Fatal(err)
		}
		wantStatus(t, refresh(t, srv, a.Pair.RefreshToken), http.StatusUnauthorized, "refresh_invalid")
		wantRejected(t, srv, a.Pair.AccessToken) // the per-request session check sees the expiry too
	})

	t.Run("session past its absolute lifetime → refresh fails (clock), refresh expiry capped at session expiry", func(t *testing.T) {
		t.Parallel()
		srv := testutil.NewServer(t, func(c *config.Config) { c.SessionMaxAge = time.Hour })
		a := register(t, srv)
		sid := sessionOf(t, srv, a.Pair.RefreshToken)
		var sessExp time.Time
		if err := srv.Deps.Pool.QueryRow(context.Background(), `SELECT expires_at FROM sessions WHERE id = $1`, sid).Scan(&sessExp); err != nil {
			t.Fatal(err)
		}
		if !a.Pair.RefreshExpiresAt.Equal(sessExp) {
			t.Fatalf("refresh_expires_at %v, want capped at session expiry %v", a.Pair.RefreshExpiresAt, sessExp)
		}
		srv.Clock.Advance(30 * time.Minute)
		p := mustRefresh(t, srv, a.Pair.RefreshToken)
		if !p.RefreshExpiresAt.Equal(sessExp) {
			t.Fatalf("rotated refresh_expires_at %v, want capped at %v", p.RefreshExpiresAt, sessExp)
		}
		srv.Clock.Advance(31 * time.Minute)
		wantStatus(t, refresh(t, srv, p.RefreshToken), http.StatusUnauthorized, "refresh_invalid")
	})

	t.Run("idle expiry is sliding (renewed on rotation) and an idle token expires", func(t *testing.T) {
		t.Parallel()
		srv := newSrv(t)
		a := register(t, srv)
		srv.Clock.Advance(20 * 24 * time.Hour)
		p := mustRefresh(t, srv, a.Pair.RefreshToken)
		if !near(p.RefreshExpiresAt, srv.Clock.Now().Add(30*24*time.Hour), time.Minute) {
			t.Fatalf("refresh_expires_at %v, want now+30d (sliding)", p.RefreshExpiresAt)
		}
		srv.Clock.Advance(30*24*time.Hour + time.Minute)
		wantStatus(t, refresh(t, srv, p.RefreshToken), http.StatusUnauthorized, "refresh_invalid")
	})

	t.Run("rate limit 60/h per session → 429 with Retry-After, token not consumed, allowed after the window", func(t *testing.T) {
		t.Parallel()
		srv := newSrv(t)
		a := register(t, srv)
		rt := a.Pair.RefreshToken
		for i := 0; i < auth.RuleRefresh.Limit; i++ {
			rt = mustRefresh(t, srv, rt).RefreshToken
		}
		r := refresh(t, srv, rt)
		wantStatus(t, r, http.StatusTooManyRequests, "rate_limited")
		if r.Header.Get("Retry-After") == "" {
			t.Fatal("429 without Retry-After")
		}
		srv.Clock.Advance(auth.RuleRefresh.Window)
		// The same token still works: the 429 rolled back without marking it used (so it is not a "reuse").
		p := mustRefresh(t, srv, rt)
		wantAuthenticated(t, srv, p.AccessToken)
	})
}
