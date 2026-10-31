package auth_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"arth/api/internal/auth"
)

func TestLogout(t *testing.T) {
	t.Run("revokes only that session; another session of the same user keeps working", func(t *testing.T) {
		t.Parallel()
		srv := newSrv(t)
		a := register(t, srv)
		second := login(t, srv, a.Email, a.Password)
		s1, s2 := sessionOf(t, srv, a.Pair.RefreshToken), sessionOf(t, srv, second.RefreshToken)

		wantStatus(t, srv.Do(t, http.MethodPost, "/v1/auth/logout", a.Pair.AccessToken, map[string]any{"refresh_token": a.Pair.RefreshToken}), http.StatusNoContent, "")
		wantSessionRevoked(t, srv, s1, "LOGOUT")
		wantRejected(t, srv, a.Pair.AccessToken) // per-request session check (Q-5): no 15-min tail
		wantStatus(t, refresh(t, srv, a.Pair.RefreshToken), http.StatusUnauthorized, "refresh_invalid")

		wantSessionActive(t, srv, s2)
		wantAuthenticated(t, srv, second.AccessToken)
		mustRefresh(t, srv, second.RefreshToken)
	})

	t.Run("logging out session 2 using session 1's access token works (same user)", func(t *testing.T) {
		t.Parallel()
		srv := newSrv(t)
		a := register(t, srv)
		second := login(t, srv, a.Email, a.Password)
		wantStatus(t, srv.Do(t, http.MethodPost, "/v1/auth/logout", a.Pair.AccessToken, map[string]any{"refresh_token": second.RefreshToken}), http.StatusNoContent, "")
		wantSessionRevoked(t, srv, sessionOf(t, srv, second.RefreshToken), "LOGOUT")
		wantAuthenticated(t, srv, a.Pair.AccessToken)
	})

	t.Run("unknown, malformed or already-used/revoked token → 204 and nothing changes", func(t *testing.T) {
		t.Parallel()
		srv := newSrv(t)
		a := register(t, srv)
		sid := sessionOf(t, srv, a.Pair.RefreshToken)
		raw, _, _ := auth.NewRefreshToken()
		for _, tok := range []string{raw, "definitely-not-a-token"} {
			wantStatus(t, srv.Do(t, http.MethodPost, "/v1/auth/logout", a.Pair.AccessToken, map[string]any{"refresh_token": tok}), http.StatusNoContent, "")
		}
		wantSessionActive(t, srv, sid)
		// Logging out twice is fine.
		wantStatus(t, srv.Do(t, http.MethodPost, "/v1/auth/logout", a.Pair.AccessToken, map[string]any{"refresh_token": a.Pair.RefreshToken}), http.StatusNoContent, "")
		second := login(t, srv, a.Email, a.Password)
		wantStatus(t, srv.Do(t, http.MethodPost, "/v1/auth/logout", second.AccessToken, map[string]any{"refresh_token": a.Pair.RefreshToken}), http.StatusNoContent, "")
		wantSessionRevoked(t, srv, sid, "LOGOUT")
	})

	t.Run("user A cannot revoke user B's session with B's refresh token", func(t *testing.T) {
		t.Parallel()
		srv := newSrv(t)
		a, b := register(t, srv), register(t, srv)
		bSID := sessionOf(t, srv, b.Pair.RefreshToken)
		wantStatus(t, srv.Do(t, http.MethodPost, "/v1/auth/logout", a.Pair.AccessToken, map[string]any{"refresh_token": b.Pair.RefreshToken}), http.StatusNoContent, "")
		wantSessionActive(t, srv, bSID)
		wantAuthenticated(t, srv, b.Pair.AccessToken)
		p := mustRefresh(t, srv, b.Pair.RefreshToken) // B's token is untouched (not marked used either)
		wantAuthenticated(t, srv, p.AccessToken)
		wantAuthenticated(t, srv, a.Pair.AccessToken)
	})

	t.Run("logout-all revokes every session of the user and no one else's", func(t *testing.T) {
		t.Parallel()
		srv := newSrv(t)
		a := register(t, srv)
		pairs := []struct{ access, refresh string }{{a.Pair.AccessToken, a.Pair.RefreshToken}}
		for i := 0; i < 2; i++ {
			p := login(t, srv, a.Email, a.Password)
			pairs = append(pairs, struct{ access, refresh string }{p.AccessToken, p.RefreshToken})
		}
		b := register(t, srv)

		wantStatus(t, srv.Do(t, http.MethodPost, "/v1/auth/logout-all", pairs[1].access, nil), http.StatusNoContent, "")
		for _, p := range pairs {
			wantRejected(t, srv, p.access)
			wantStatus(t, refresh(t, srv, p.refresh), http.StatusUnauthorized, "refresh_invalid")
			wantSessionRevoked(t, srv, sessionOf(t, srv, p.refresh), "LOGOUT_ALL")
		}
		if n := count(t, srv, `SELECT count(*) FROM sessions WHERE user_id = $1 AND revoked_at IS NULL`, a.UserID()); n != 0 {
			t.Fatalf("%d active sessions left, want 0", n)
		}
		wantAuthenticated(t, srv, b.Pair.AccessToken)
		// A can sign in again afterwards.
		wantAuthenticated(t, srv, login(t, srv, a.Email, a.Password).AccessToken)
	})
}

func TestChangePassword(t *testing.T) {
	const newPassword = "a brand new passphrase"

	t.Run("revokes other sessions but not the current one", func(t *testing.T) {
		t.Parallel()
		srv := newSrv(t)
		a := register(t, srv)
		other := login(t, srv, a.Email, a.Password)
		s1, s2 := sessionOf(t, srv, a.Pair.RefreshToken), sessionOf(t, srv, other.RefreshToken)
		var changedBefore time.Time
		if err := srv.Deps.Pool.QueryRow(context.Background(), `SELECT password_changed_at FROM users WHERE id = $1`, a.UserID()).Scan(&changedBefore); err != nil {
			t.Fatal(err)
		}

		r := srv.Do(t, http.MethodPost, "/v1/auth/password", a.Pair.AccessToken, map[string]any{"current_password": a.Password, "new_password": newPassword})
		wantStatus(t, r, http.StatusNoContent, "")

		wantSessionActive(t, srv, s1)
		wantAuthenticated(t, srv, a.Pair.AccessToken)
		mustRefresh(t, srv, a.Pair.RefreshToken)
		wantSessionRevoked(t, srv, s2, "PASSWORD_CHANGED")
		wantRejected(t, srv, other.AccessToken)
		wantStatus(t, refresh(t, srv, other.RefreshToken), http.StatusUnauthorized, "refresh_invalid")

		wantStatus(t, srv.Do(t, http.MethodPost, "/v1/auth/login", "", loginBody(a.Email, a.Password)), http.StatusUnauthorized, "invalid_credentials")
		login(t, srv, a.Email, newPassword)
		var changedAfter time.Time
		if err := srv.Deps.Pool.QueryRow(context.Background(), `SELECT password_changed_at FROM users WHERE id = $1`, a.UserID()).Scan(&changedAfter); err != nil {
			t.Fatal(err)
		}
		if !changedAfter.After(changedBefore) {
			t.Fatal("password_changed_at not bumped")
		}
	})

	t.Run("wrong current password → 401 invalid_credentials, nothing changes", func(t *testing.T) {
		t.Parallel()
		srv := newSrv(t)
		a := register(t, srv)
		other := login(t, srv, a.Email, a.Password)
		r := srv.Do(t, http.MethodPost, "/v1/auth/password", a.Pair.AccessToken, map[string]any{"current_password": "not my password", "new_password": newPassword})
		wantStatus(t, r, http.StatusUnauthorized, "invalid_credentials")
		wantAuthenticated(t, srv, a.Pair.AccessToken) // invalid_credentials is not a session failure
		wantAuthenticated(t, srv, other.AccessToken)
		login(t, srv, a.Email, a.Password)
	})

	t.Run("weak new password → 422 weak_password, nothing changes", func(t *testing.T) {
		t.Parallel()
		srv := newSrv(t)
		a := register(t, srv)
		other := login(t, srv, a.Email, a.Password)
		for _, pw := range []string{"short", "password1"} {
			r := srv.Do(t, http.MethodPost, "/v1/auth/password", a.Pair.AccessToken, map[string]any{"current_password": a.Password, "new_password": pw})
			wantStatus(t, r, http.StatusUnprocessableEntity, "weak_password")
		}
		wantAuthenticated(t, srv, other.AccessToken)
		login(t, srv, a.Email, a.Password)
	})

	t.Run("repeated wrong current passwords are rate-limited like logins", func(t *testing.T) {
		t.Parallel()
		srv := newSrv(t)
		a := register(t, srv)
		for i := 0; i < auth.RuleLoginEmail.Limit; i++ {
			r := srv.Do(t, http.MethodPost, "/v1/auth/password", a.Pair.AccessToken, map[string]any{"current_password": "wrong guess", "new_password": newPassword})
			wantStatus(t, r, http.StatusUnauthorized, "invalid_credentials")
		}
		r := srv.Do(t, http.MethodPost, "/v1/auth/password", a.Pair.AccessToken, map[string]any{"current_password": a.Password, "new_password": newPassword})
		wantStatus(t, r, http.StatusTooManyRequests, "rate_limited")
		if r.Header.Get("Retry-After") == "" {
			t.Fatal("429 without Retry-After")
		}
	})
}

func TestDeleteMe(t *testing.T) {
	t.Run("wrong password → 401 invalid_credentials and nothing is deleted", func(t *testing.T) {
		t.Parallel()
		srv := newSrv(t)
		a := register(t, srv)
		r := srv.Do(t, http.MethodDelete, "/v1/me", a.Pair.AccessToken, map[string]any{"password": "not my password"})
		wantStatus(t, r, http.StatusUnauthorized, "invalid_credentials")
		if n := count(t, srv, `SELECT count(*) FROM users WHERE id = $1`, a.UserID()); n != 1 {
			t.Fatal("user deleted with a wrong password")
		}
		wantAuthenticated(t, srv, a.Pair.AccessToken)
	})

	t.Run("right password → 204, all user rows gone, tokens dead", func(t *testing.T) {
		t.Parallel()
		srv := newSrv(t)
		a := register(t, srv)
		other := login(t, srv, a.Email, a.Password)
		b := register(t, srv)
		sessions := []uuid.UUID{sessionOf(t, srv, a.Pair.RefreshToken), sessionOf(t, srv, other.RefreshToken)}

		wantStatus(t, srv.Do(t, http.MethodDelete, "/v1/me", a.Pair.AccessToken, map[string]any{"password": a.Password}), http.StatusNoContent, "")

		for table, sql := range map[string]string{
			"users":    `SELECT count(*) FROM users WHERE id = $1`,
			"accounts": `SELECT count(*) FROM accounts WHERE user_id = $1`,
			"sessions": `SELECT count(*) FROM sessions WHERE user_id = $1`,
		} {
			if n := count(t, srv, sql, a.UserID()); n != 0 {
				t.Fatalf("%d %s rows left", n, table)
			}
		}
		if n := count(t, srv, `SELECT count(*) FROM refresh_tokens WHERE session_id = ANY($1)`, sessions); n != 0 {
			t.Fatalf("%d refresh_tokens rows left", n)
		}
		wantRejected(t, srv, a.Pair.AccessToken)
		wantRejected(t, srv, other.AccessToken)
		wantStatus(t, refresh(t, srv, other.RefreshToken), http.StatusUnauthorized, "refresh_invalid")
		wantStatus(t, srv.Do(t, http.MethodPost, "/v1/auth/login", "", loginBody(a.Email, a.Password)), http.StatusUnauthorized, "invalid_credentials")

		// User B is untouched.
		if n := count(t, srv, `SELECT count(*) FROM accounts WHERE user_id = $1`, b.UserID()); n != 1 {
			t.Fatalf("user B has %d accounts, want 1", n)
		}
		wantAuthenticated(t, srv, b.Pair.AccessToken)

		// The email can be registered again.
		r := srv.Do(t, http.MethodPost, "/v1/auth/register", "", registerBody(a.Email, strongPassword))
		wantStatus(t, r, http.StatusCreated, "")
	})

	t.Run("no token → 401; missing password → 422", func(t *testing.T) {
		t.Parallel()
		srv := newSrv(t)
		a := register(t, srv)
		wantStatus(t, srv.Do(t, http.MethodDelete, "/v1/me", "", map[string]any{"password": a.Password}), http.StatusUnauthorized, "unauthorized")
		wantStatus(t, srv.Do(t, http.MethodDelete, "/v1/me", a.Pair.AccessToken, map[string]any{}), http.StatusUnprocessableEntity, "validation_failed")
		if n := count(t, srv, `SELECT count(*) FROM users WHERE id = $1`, a.UserID()); n != 1 {
			t.Fatal("user deleted")
		}
	})
}
