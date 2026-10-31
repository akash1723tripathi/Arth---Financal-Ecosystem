package httpapi_test

import (
	"net/http"
	"testing"

	"arth/api/internal/testutil"
)

// TestWiring proves the full handler chain: public routes, bearer auth, the per-request session check
// (AUTH.md Q-5) and the validator in front of the generated handlers.
func TestWiring(t *testing.T) {
	srv := testutil.NewServer(t)

	if r := srv.Do(t, http.MethodGet, "/healthz", "", nil); r.Status != http.StatusOK {
		t.Fatalf("healthz: %d", r.Status)
	}
	if r := srv.Do(t, http.MethodGet, "/readyz", "", nil); r.Status != http.StatusOK {
		t.Fatalf("readyz: %d %s", r.Status, r.Body)
	}
	if r := srv.Do(t, http.MethodGet, "/v1/me", "", nil); r.Status != http.StatusUnauthorized || r.Code() != "unauthorized" {
		t.Fatalf("no token: %d %s", r.Status, r.Body)
	}
	if r := srv.Do(t, http.MethodGet, "/v1/me", "not-a-jwt", nil); r.Status != http.StatusUnauthorized {
		t.Fatalf("bad token: %d", r.Status)
	}
	u := srv.CreateUser(t)
	if r := srv.Do(t, http.MethodGet, "/v1/nope", u.AccessToken, nil); r.Status != http.StatusNotFound || r.Code() != "not_found" {
		t.Fatalf("unknown route: %d %s", r.Status, r.Body)
	}
	// Validator runs after auth: unknown field → 400.
	if r := srv.Do(t, http.MethodPost, "/v1/accounts", u.AccessToken, map[string]any{"name": "x", "type": "CASH", "bogus": 1}); r.Status != http.StatusBadRequest {
		t.Fatalf("unknown field: %d %s", r.Status, r.Body)
	}
	// Revoked session → the still-unexpired token stops working immediately.
	if _, err := srv.Deps.Pool.Exec(t.Context(), `UPDATE sessions SET revoked_at = now(), revoke_reason = 'LOGOUT' WHERE id = $1`, u.SessionID); err != nil {
		t.Fatal(err)
	}
	if r := srv.Do(t, http.MethodGet, "/v1/accounts", u.AccessToken, nil); r.Status != http.StatusUnauthorized {
		t.Fatalf("revoked session: %d %s", r.Status, r.Body)
	}
}
