package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"arth/api/internal/apierr"
)

// Access-token constants (docs/AUTH.md §2, §7).
const (
	TokenIssuer      = "arth-api"
	TokenAudience    = "arth"
	DefaultKeyID     = "k1"
	DefaultAccessTTL = 15 * time.Minute
	ClockSkew        = 30 * time.Second
	MinSigningKeyLen = 32
)

// The signing algorithm is chosen in exactly one place. HS256 vs EdDSA is AUTH.md Q-3: switching means
// changing signingMethod and the key types in TokenConfig/NewTokens (crypto/ed25519 private key to sign,
// public keys to verify); Issue/Verify do not change.
var signingMethod jwt.SigningMethod = jwt.SigningMethodHS256

// TokenConfig configures the access-token issuer/verifier.
type TokenConfig struct {
	Key       []byte // current signing key, ≥ 32 bytes (AUTH_ACCESS_TOKEN_SIGNING_KEY)
	KeyID     string // kid for Key; default "k1"
	PrevKey   []byte // optional previous key, still accepted for verification during rotation
	PrevKeyID string // kid for PrevKey; required when PrevKey is set, must differ from KeyID
	TTL       time.Duration
	Now       func() time.Time // injectable clock; default time.Now
}

// Tokens issues and verifies short-lived access JWTs.
type Tokens struct {
	signKID string
	signKey []byte
	keys    map[string][]byte // kid → verification key
	ttl     time.Duration
	now     func() time.Time
	parser  *jwt.Parser
}

type accessClaims struct {
	jwt.RegisteredClaims
	SID string `json:"sid"`
}

// NewTokens validates cfg. Keys are copied.
func NewTokens(cfg TokenConfig) (*Tokens, error) {
	if len(cfg.Key) < MinSigningKeyLen {
		return nil, fmt.Errorf("auth: signing key must be at least %d bytes", MinSigningKeyLen)
	}
	kid := cfg.KeyID
	if kid == "" {
		kid = DefaultKeyID
	}
	ttl := cfg.TTL
	if ttl == 0 {
		ttl = DefaultAccessTTL
	}
	if ttl < 0 {
		return nil, errors.New("auth: access token TTL must be positive")
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	t := &Tokens{
		signKID: kid,
		signKey: append([]byte(nil), cfg.Key...),
		keys:    map[string][]byte{},
		ttl:     ttl,
		now:     now,
	}
	t.keys[kid] = t.signKey
	if len(cfg.PrevKey) > 0 {
		if len(cfg.PrevKey) < MinSigningKeyLen {
			return nil, fmt.Errorf("auth: previous signing key must be at least %d bytes", MinSigningKeyLen)
		}
		if cfg.PrevKeyID == "" || cfg.PrevKeyID == kid {
			return nil, errors.New("auth: previous key needs its own kid, different from the current kid")
		}
		t.keys[cfg.PrevKeyID] = append([]byte(nil), cfg.PrevKey...)
	}
	t.parser = jwt.NewParser(
		jwt.WithValidMethods([]string{signingMethod.Alg()}),
		jwt.WithIssuer(TokenIssuer),
		jwt.WithAudience(TokenAudience),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithLeeway(ClockSkew),
		jwt.WithTimeFunc(func() time.Time { return t.now() }),
	)
	return t, nil
}

// TTL is the access-token lifetime (returned to clients as expires_in).
func (t *Tokens) TTL() time.Duration { return t.ttl }

// Issue signs an access token for the user's session.
func (t *Tokens) Issue(userID, sessionID uuid.UUID) (token string, expiresIn time.Duration, err error) {
	if userID == uuid.Nil || sessionID == uuid.Nil {
		return "", 0, errors.New("auth: issue token: nil user or session id")
	}
	jti, err := uuid.NewRandom()
	if err != nil {
		return "", 0, fmt.Errorf("auth: issue token: jti: %w", err)
	}
	now := t.now().UTC().Truncate(time.Second)
	claims := accessClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    TokenIssuer,
			Subject:   userID.String(),
			Audience:  jwt.ClaimStrings{TokenAudience},
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(t.ttl)),
			ID:        jti.String(),
		},
		SID: sessionID.String(),
	}
	tok := jwt.NewWithClaims(signingMethod, claims)
	tok.Header["kid"] = t.signKID
	s, err := tok.SignedString(t.signKey)
	if err != nil {
		return "", 0, fmt.Errorf("auth: sign token: %w", err)
	}
	return s, t.ttl, nil
}

// Verify checks the signature (fixed algorithm allow-list, key chosen by kid), iss, aud, exp (required),
// iat, with 30 s leeway, and returns the principal. Every failure is apierr.Unauthorized() wrapping the cause.
func (t *Tokens) Verify(token string) (Principal, error) {
	var c accessClaims
	if _, err := t.parser.ParseWithClaims(token, &c, t.keyFunc); err != nil {
		return Principal{}, unauthorized(err)
	}
	uid, err := uuid.Parse(c.Subject)
	if err != nil || uid == uuid.Nil {
		return Principal{}, unauthorized(errors.New("invalid sub claim"))
	}
	sid, err := uuid.Parse(c.SID)
	if err != nil || sid == uuid.Nil {
		return Principal{}, unauthorized(errors.New("invalid sid claim"))
	}
	if c.ID == "" {
		return Principal{}, unauthorized(errors.New("missing jti claim"))
	}
	return Principal{UserID: uid, SessionID: sid}, nil
}

func (t *Tokens) keyFunc(tok *jwt.Token) (any, error) {
	// The parser already enforced the allow-list; re-check the concrete type as defence in depth.
	if tok.Method != signingMethod {
		return nil, fmt.Errorf("unexpected signing method %v", tok.Header["alg"])
	}
	kid, ok := tok.Header["kid"].(string)
	if !ok || kid == "" {
		return nil, errors.New("missing kid")
	}
	key, ok := t.keys[kid]
	if !ok {
		return nil, errors.New("unknown kid")
	}
	return key, nil
}

func unauthorized(cause error) error {
	e := apierr.Unauthorized()
	e.Err = fmt.Errorf("verify access token: %w", cause)
	return e
}
