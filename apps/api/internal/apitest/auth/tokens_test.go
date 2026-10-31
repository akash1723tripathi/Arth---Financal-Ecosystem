package auth_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"arth/api/internal/auth"
	"arth/api/internal/testutil"
)

type forgedClaims struct {
	jwt.RegisteredClaims
	SID string `json:"sid"`
}

func claimsFor(srv *testutil.Server, userID, sessionID uuid.UUID) forgedClaims {
	now := srv.Clock.Now()
	return forgedClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    auth.TokenIssuer,
			Subject:   userID.String(),
			Audience:  jwt.ClaimStrings{auth.TokenAudience},
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(15 * time.Minute)),
			ID:        uuid.NewString(),
		},
		SID: sessionID.String(),
	}
}

func sign(t *testing.T, method jwt.SigningMethod, key any, c forgedClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(method, c)
	tok.Header["kid"] = auth.DefaultKeyID
	s, err := tok.SignedString(key)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return s
}

// Access tokens are checked at GET /v1/me (any protected route goes through the same middleware).
func TestAccessToken(t *testing.T) {
	srv := newSrv(t)
	a := register(t, srv)
	sid := sessionOf(t, srv, a.Pair.RefreshToken)
	key := srv.Deps.Cfg.AccessTokenSigningKey

	t.Run("valid token is accepted (control)", func(t *testing.T) {
		wantAuthenticated(t, srv, a.Pair.AccessToken)
		wantAuthenticated(t, srv, sign(t, jwt.SigningMethodHS256, key, claimsFor(srv, a.UserID(), sid)))
	})

	t.Run("no token → 401", func(t *testing.T) {
		wantStatus(t, srv.Do(t, http.MethodGet, "/v1/me", "", nil), http.StatusUnauthorized, "unauthorized")
		wantStatus(t, srv.Do(t, http.MethodPost, "/v1/auth/logout-all", "", nil), http.StatusUnauthorized, "unauthorized")
		wantStatus(t, srv.Do(t, http.MethodPost, "/v1/auth/logout", "", map[string]any{"refresh_token": a.Pair.RefreshToken}), http.StatusUnauthorized, "unauthorized")
		wantStatus(t, srv.Do(t, http.MethodPost, "/v1/auth/password", "", map[string]any{"current_password": a.Password, "new_password": "another good passphrase"}), http.StatusUnauthorized, "unauthorized")
		wantStatus(t, srv.DoWith(t, http.MethodGet, "/v1/me", "", nil, map[string]string{"Authorization": "Basic Zm9vOmJhcg=="}), http.StatusUnauthorized, "unauthorized")
	})

	t.Run("garbage token → 401", func(t *testing.T) {
		wantRejected(t, srv, "not.a.jwt")
		wantRejected(t, srv, a.Pair.RefreshToken) // a refresh token is not an access token
	})

	t.Run("wrong signature → 401", func(t *testing.T) {
		other, err := auth.NewTokens(auth.TokenConfig{Key: []byte("a-completely-different-signing-key-0123456789"), Now: srv.Clock.Now})
		if err != nil {
			t.Fatal(err)
		}
		tok, _, err := other.Issue(a.UserID(), sid)
		if err != nil {
			t.Fatal(err)
		}
		wantRejected(t, srv, tok)
		// Tamper with the payload of a genuine token (signature no longer matches).
		good := a.Pair.AccessToken
		parts := splitJWT(t, good)
		forged := sign(t, jwt.SigningMethodHS256, []byte("x-other-key-x-other-key-x-other-key"), claimsFor(srv, a.UserID(), sid))
		wantRejected(t, srv, parts[0]+"."+splitJWT(t, forged)[1]+"."+parts[2])
	})

	t.Run("alg none → 401", func(t *testing.T) {
		tok := sign(t, jwt.SigningMethodNone, jwt.UnsafeAllowNoneSignatureType, claimsFor(srv, a.UserID(), sid))
		wantRejected(t, srv, tok)
	})

	t.Run("wrong aud / wrong iss / missing exp → 401", func(t *testing.T) {
		c := claimsFor(srv, a.UserID(), sid)
		c.Audience = jwt.ClaimStrings{"someone-else"}
		wantRejected(t, srv, sign(t, jwt.SigningMethodHS256, key, c))
		c = claimsFor(srv, a.UserID(), sid)
		c.Issuer = "evil"
		wantRejected(t, srv, sign(t, jwt.SigningMethodHS256, key, c))
		c = claimsFor(srv, a.UserID(), sid)
		c.ExpiresAt = nil
		wantRejected(t, srv, sign(t, jwt.SigningMethodHS256, key, c))
	})

	t.Run("token mixing user A with user B's session → 401", func(t *testing.T) {
		b := register(t, srv)
		bSID := sessionOf(t, srv, b.Pair.RefreshToken)
		wantRejected(t, srv, sign(t, jwt.SigningMethodHS256, key, claimsFor(srv, a.UserID(), bSID)))
		wantRejected(t, srv, sign(t, jwt.SigningMethodHS256, key, claimsFor(srv, uuid.New(), sid)))
	})

	// Last: moves the shared clock.
	t.Run("expired → 401", func(t *testing.T) {
		wantAuthenticated(t, srv, a.Pair.AccessToken)
		srv.Clock.Advance(16 * time.Minute) // TTL 15 min + 30 s leeway
		wantRejected(t, srv, a.Pair.AccessToken)
		// The session itself is fine: a refresh yields a working access token again.
		p := mustRefresh(t, srv, a.Pair.RefreshToken)
		wantAuthenticated(t, srv, p.AccessToken)
	})
}

func splitJWT(t *testing.T, tok string) [3]string {
	t.Helper()
	var out [3]string
	start, n := 0, 0
	for i := 0; i <= len(tok); i++ {
		if i == len(tok) || tok[i] == '.' {
			if n > 2 {
				t.Fatalf("token has more than 3 parts")
			}
			out[n] = tok[start:i]
			n++
			start = i + 1
		}
	}
	if n != 3 {
		t.Fatalf("token has %d parts", n)
	}
	return out
}
