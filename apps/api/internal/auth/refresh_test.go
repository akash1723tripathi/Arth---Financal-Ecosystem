package auth

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"
)

var refreshRe = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

func TestNewRefreshToken(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		raw, hash, err := NewRefreshToken()
		if err != nil {
			t.Fatal(err)
		}
		if !refreshRe.MatchString(raw) {
			t.Fatalf("token %q is not 43 base64url chars", raw)
		}
		want := sha256.Sum256([]byte(raw))
		if !bytes.Equal(hash, want[:]) || !bytes.Equal(HashRefreshToken(raw), hash) {
			t.Fatal("hash is not sha256(raw)")
		}
		if seen[raw] {
			t.Fatal("duplicate refresh token")
		}
		seen[raw] = true
		if h, err := ParseRefreshToken(raw); err != nil || !bytes.Equal(h, hash) {
			t.Fatalf("ParseRefreshToken(valid) = %v", err)
		}
	}
}

func TestParseRefreshTokenRejectsMalformed(t *testing.T) {
	raw, _, _ := NewRefreshToken()
	bad := []string{
		"",
		raw[:42],
		raw + "A",
		raw[:42] + "=",
		raw[:42] + "+",
		raw[:42] + "/",
		strings.Repeat("!", 43),
		strings.Repeat("A", 10000),
		raw[:41] + "é",
	}
	for _, s := range bad {
		if _, err := ParseRefreshToken(s); !errors.Is(err, ErrMalformedRefreshToken) {
			t.Errorf("ParseRefreshToken(%q...) err = %v", truncate(s), err)
		}
	}
}

func truncate(s string) string {
	if len(s) > 20 {
		return s[:20]
	}
	return s
}

func TestPrincipalContext(t *testing.T) {
	if _, ok := PrincipalFrom(context.Background()); ok {
		t.Fatal("empty context has a principal")
	}
	p := Principal{UserID: uuid.New(), SessionID: uuid.New()}
	got, ok := PrincipalFrom(WithPrincipal(context.Background(), p))
	if !ok || got != p {
		t.Fatalf("PrincipalFrom = %+v,%v", got, ok)
	}
	if _, ok := PrincipalFrom(WithPrincipal(context.Background(), Principal{})); ok {
		t.Fatal("nil user id accepted as principal")
	}
}
