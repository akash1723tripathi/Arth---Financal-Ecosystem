package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"arth/api/internal/apierr"
)

var (
	testKey  = []byte("0123456789abcdef0123456789abcdef-test-key")
	otherKey = []byte("ffffffffffffffffffffffffffffffff-other-key")
	t0       = time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
)

type fakeClock struct{ t time.Time }

func (c *fakeClock) Now() time.Time          { return c.t }
func (c *fakeClock) Advance(d time.Duration) { c.t = c.t.Add(d) }

func newTestTokens(t *testing.T, cfg TokenConfig) (*Tokens, *fakeClock) {
	t.Helper()
	clk := &fakeClock{t: t0}
	if cfg.Key == nil {
		cfg.Key = testKey
	}
	cfg.Now = clk.Now
	tk, err := NewTokens(cfg)
	if err != nil {
		t.Fatalf("NewTokens: %v", err)
	}
	return tk, clk
}

// craft builds a token with arbitrary header/claims and an HMAC-SHA256 signature over key (or none).
func craft(t *testing.T, header, claims map[string]any, method jwt.SigningMethod, key any) string {
	t.Helper()
	enc := func(v any) string {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return base64.RawURLEncoding.EncodeToString(b)
	}
	signing := enc(header) + "." + enc(claims)
	if method == nil {
		return signing + "."
	}
	sig, err := method.Sign(signing, key)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return signing + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func validClaims(uid, sid uuid.UUID) map[string]any {
	return map[string]any{
		"iss": TokenIssuer, "aud": TokenAudience, "sub": uid.String(), "sid": sid.String(),
		"iat": t0.Unix(), "exp": t0.Add(15 * time.Minute).Unix(), "jti": uuid.NewString(),
	}
}

func with(m map[string]any, k string, v any) map[string]any {
	c := map[string]any{}
	for kk, vv := range m {
		c[kk] = vv
	}
	if v == nil {
		delete(c, k)
	} else {
		c[k] = v
	}
	return c
}

func assertUnauthorized(t *testing.T, err error) {
	t.Helper()
	var ae *apierr.Error
	if !errors.As(err, &ae) || ae.Code != "unauthorized" || ae.Status != http.StatusUnauthorized {
		t.Fatalf("err = %v, want apierr unauthorized", err)
	}
	if ae.Err == nil {
		t.Fatal("unauthorized error should wrap its cause")
	}
}

func TestTokensIssueVerify(t *testing.T) {
	tk, _ := newTestTokens(t, TokenConfig{})
	uid, sid := uuid.New(), uuid.New()
	tok, exp, err := tk.Issue(uid, sid)
	if err != nil {
		t.Fatal(err)
	}
	if exp != 15*time.Minute {
		t.Fatalf("expiresIn = %v", exp)
	}
	p, err := tk.Verify(tok)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if p.UserID != uid || p.SessionID != sid {
		t.Fatalf("principal = %+v", p)
	}

	// Header and claims carry what AUTH.md §2 says.
	parts := strings.Split(tok, ".")
	var hdr, cl map[string]any
	hb, _ := base64.RawURLEncoding.DecodeString(parts[0])
	cb, _ := base64.RawURLEncoding.DecodeString(parts[1])
	_ = json.Unmarshal(hb, &hdr)
	_ = json.Unmarshal(cb, &cl)
	if hdr["alg"] != "HS256" || hdr["kid"] != "k1" || hdr["typ"] != "JWT" {
		t.Fatalf("header = %v", hdr)
	}
	for _, k := range []string{"sub", "sid", "iss", "aud", "iat", "exp", "jti"} {
		if _, ok := cl[k]; !ok {
			t.Errorf("claim %s missing", k)
		}
	}
	if cl["iss"] != "arth-api" {
		t.Errorf("iss = %v", cl["iss"])
	}

	// jti is unique per token.
	tok2, _, _ := tk.Issue(uid, sid)
	if tok2 == tok {
		t.Fatal("two tokens are identical")
	}

	if _, _, err := tk.Issue(uuid.Nil, sid); err == nil {
		t.Fatal("Issue with nil user id succeeded")
	}
}

func TestTokensExpiryAndLeeway(t *testing.T) {
	tk, clk := newTestTokens(t, TokenConfig{})
	tok, _, _ := tk.Issue(uuid.New(), uuid.New())

	clk.Advance(15*time.Minute + 29*time.Second) // inside 30 s leeway
	if _, err := tk.Verify(tok); err != nil {
		t.Fatalf("within leeway: %v", err)
	}
	clk.Advance(2 * time.Second) // past exp + leeway
	_, err := tk.Verify(tok)
	assertUnauthorized(t, err)
	if !errors.Is(err, jwt.ErrTokenExpired) {
		t.Fatalf("want ErrTokenExpired in chain, got %v", err)
	}
}

func TestTokensRejections(t *testing.T) {
	tk, _ := newTestTokens(t, TokenConfig{})
	uid, sid := uuid.New(), uuid.New()
	base := validClaims(uid, sid)
	hs256 := map[string]any{"alg": "HS256", "typ": "JWT", "kid": "k1"}

	// Sanity: the crafting helper produces a token Verify accepts.
	if _, err := tk.Verify(craft(t, hs256, base, jwt.SigningMethodHS256, testKey)); err != nil {
		t.Fatalf("crafted valid token rejected: %v", err)
	}

	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	cases := map[string]string{
		"wrong signature key": craft(t, hs256, base, jwt.SigningMethodHS256, otherKey),
		"alg none":            craft(t, map[string]any{"alg": "none", "typ": "JWT", "kid": "k1"}, base, nil, nil),
		"alg none (unsafe lib sign)": craft(t, map[string]any{"alg": "none", "typ": "JWT", "kid": "k1"}, base,
			jwt.SigningMethodNone, jwt.UnsafeAllowNoneSignatureType),
		"alg HS512 same key": craft(t, map[string]any{"alg": "HS512", "typ": "JWT", "kid": "k1"}, base, jwt.SigningMethodHS512, testKey),
		"alg RS256":          craft(t, map[string]any{"alg": "RS256", "typ": "JWT", "kid": "k1"}, base, jwt.SigningMethodRS256, rsaKey),
		"HS256 sig, RS256 header": func() string {
			s := craft(t, hs256, base, jwt.SigningMethodHS256, testKey)
			p := strings.Split(s, ".")
			h, _ := json.Marshal(map[string]any{"alg": "RS256", "typ": "JWT", "kid": "k1"})
			return base64.RawURLEncoding.EncodeToString(h) + "." + p[1] + "." + p[2]
		}(),
		"wrong aud":         craft(t, hs256, with(base, "aud", "someone-else"), jwt.SigningMethodHS256, testKey),
		"missing aud":       craft(t, hs256, with(base, "aud", nil), jwt.SigningMethodHS256, testKey),
		"wrong iss":         craft(t, hs256, with(base, "iss", "evil"), jwt.SigningMethodHS256, testKey),
		"missing exp":       craft(t, hs256, with(base, "exp", nil), jwt.SigningMethodHS256, testKey),
		"expired":           craft(t, hs256, with(base, "exp", t0.Add(-time.Minute).Unix()), jwt.SigningMethodHS256, testKey),
		"iat in the future": craft(t, hs256, with(base, "iat", t0.Add(5*time.Minute).Unix()), jwt.SigningMethodHS256, testKey),
		"missing sub":       craft(t, hs256, with(base, "sub", nil), jwt.SigningMethodHS256, testKey),
		"non-uuid sub":      craft(t, hs256, with(base, "sub", "user_123"), jwt.SigningMethodHS256, testKey),
		"nil uuid sub":      craft(t, hs256, with(base, "sub", uuid.Nil.String()), jwt.SigningMethodHS256, testKey),
		"missing sid":       craft(t, hs256, with(base, "sid", nil), jwt.SigningMethodHS256, testKey),
		"missing jti":       craft(t, hs256, with(base, "jti", nil), jwt.SigningMethodHS256, testKey),
		"unknown kid":       craft(t, map[string]any{"alg": "HS256", "typ": "JWT", "kid": "k9"}, base, jwt.SigningMethodHS256, testKey),
		"missing kid":       craft(t, map[string]any{"alg": "HS256", "typ": "JWT"}, base, jwt.SigningMethodHS256, testKey),
		"empty":             "",
		"garbage":           "not.a.jwt",
	}
	for name, tok := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := tk.Verify(tok)
			assertUnauthorized(t, err)
		})
	}
}

func TestTokensKeyRotation(t *testing.T) {
	// Old deployment signs with k1 = testKey.
	oldTk, _ := newTestTokens(t, TokenConfig{Key: testKey, KeyID: "k1"})
	uid, sid := uuid.New(), uuid.New()
	oldTok, _, _ := oldTk.Issue(uid, sid)

	// New deployment: current k2 = otherKey, previous k1 = testKey.
	newTk, _ := newTestTokens(t, TokenConfig{Key: otherKey, KeyID: "k2", PrevKey: testKey, PrevKeyID: "k1"})
	if p, err := newTk.Verify(oldTok); err != nil || p.UserID != uid {
		t.Fatalf("token signed with previous key rejected: %v", err)
	}
	newTok, _, _ := newTk.Issue(uid, sid)
	hb, _ := base64.RawURLEncoding.DecodeString(strings.Split(newTok, ".")[0])
	if !strings.Contains(string(hb), `"kid":"k2"`) {
		t.Fatalf("new token header = %s, want kid k2", hb)
	}
	if _, err := newTk.Verify(newTok); err != nil {
		t.Fatalf("new token: %v", err)
	}

	// After the previous key is dropped, old tokens die; old servers never accept k2 tokens.
	newest, _ := newTestTokens(t, TokenConfig{Key: otherKey, KeyID: "k2"})
	_, err := newest.Verify(oldTok)
	assertUnauthorized(t, err)
	_, err = oldTk.Verify(newTok)
	assertUnauthorized(t, err)

	// A token with kid k1 but signed by the k2 key must not verify (key chosen strictly by kid).
	forged := craft(t, map[string]any{"alg": "HS256", "typ": "JWT", "kid": "k1"}, validClaims(uid, sid), jwt.SigningMethodHS256, otherKey)
	_, err = newTk.Verify(forged)
	assertUnauthorized(t, err)
}

func TestNewTokensConfigValidation(t *testing.T) {
	bad := []TokenConfig{
		{Key: []byte("short")},
		{Key: testKey, PrevKey: []byte("short"), PrevKeyID: "k0"},
		{Key: testKey, PrevKey: otherKey},                  // missing prev kid
		{Key: testKey, PrevKey: otherKey, PrevKeyID: "k1"}, // same as default kid
		{Key: testKey, TTL: -time.Minute},
	}
	for i, c := range bad {
		if _, err := NewTokens(c); err == nil {
			t.Errorf("case %d: NewTokens succeeded", i)
		}
	}
	tk, err := NewTokens(TokenConfig{Key: testKey, TTL: 5 * time.Minute})
	if err != nil || tk.TTL() != 5*time.Minute {
		t.Fatalf("TTL config: %v %v", err, tk)
	}
}
