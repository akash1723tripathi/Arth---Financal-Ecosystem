package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
)

// Refresh tokens are opaque: 32 random bytes, base64url without padding (43 characters). Only the SHA-256 of
// the raw string is stored (refresh_tokens.token_hash). docs/AUTH.md §2.
const (
	refreshTokenBytes = 32
	// RefreshTokenLen is the length of an encoded refresh token.
	RefreshTokenLen = 43 // base64.RawURLEncoding.EncodedLen(32)
)

// ErrMalformedRefreshToken means the presented string cannot be a token we issued.
var ErrMalformedRefreshToken = errors.New("auth: malformed refresh token")

// NewRefreshToken returns a fresh raw token (give it to the client once) and its SHA-256 hash (store this).
func NewRefreshToken() (raw string, hash []byte, err error) {
	b := make([]byte, refreshTokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", nil, fmt.Errorf("auth: refresh token: %w", err)
	}
	raw = base64.RawURLEncoding.EncodeToString(b)
	return raw, HashRefreshToken(raw), nil
}

// HashRefreshToken is SHA-256 over the raw token string's bytes. It does not validate the format; use
// ParseRefreshToken on client input.
func HashRefreshToken(raw string) []byte {
	sum := sha256.Sum256([]byte(raw))
	return sum[:]
}

// ValidRefreshTokenFormat cheaply checks the shape (length first, then the base64url alphabet).
func ValidRefreshTokenFormat(raw string) bool {
	if len(raw) != RefreshTokenLen {
		return false
	}
	_, err := base64.RawURLEncoding.Strict().DecodeString(raw)
	return err == nil
}

// ParseRefreshToken validates client input and returns its hash for the DB lookup. A malformed token returns
// ErrMalformedRefreshToken (callers answer 401 refresh_invalid, same as an unknown token).
func ParseRefreshToken(raw string) ([]byte, error) {
	if !ValidRefreshTokenFormat(raw) {
		return nil, ErrMalformedRefreshToken
	}
	return HashRefreshToken(raw), nil
}
