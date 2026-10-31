package auth

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"arth/api/internal/apierr"
)

func TestCheckPassword(t *testing.T) {
	tests := []struct {
		name string
		pw   string
		ok   bool
	}{
		{"8 ascii", "zq8#Lm2v", true},
		{"passphrase with spaces", "correct horse battery staple", true},
		{"unicode 8 runes (more bytes)", "ñañañaña", true},
		{"emoji 8 runes", "🔑🔒🗝️🔓🔐🔏x", true},
		{"128 runes", strings.Repeat("ab", 64), true},
		{"128 multibyte runes", strings.Repeat("é", 128), true},
		{"7 chars", "zq8#Lm2", false},
		{"7 runes but 14 bytes", "ééééééé", false},
		{"empty", "", false},
		{"129 runes", strings.Repeat("a", 128) + "b", false},
		{"all spaces", "          ", false},
		{"all whitespace mixed", " \t\n      ", false},
		{"common", "password", false},
		{"common upper", "PASSWORD", false},
		{"common mixed case", "PassWord123", false},
		{"common digits", "12345678", false},
		{"common qwerty", "qwertyuiop", false},
		{"common iloveyou", "iloveyou", false},
		{"invalid utf8", "abc\xff\xfe\xfddefgh", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckPassword(tc.pw)
			if tc.ok {
				if err != nil {
					t.Fatalf("CheckPassword = %v, want nil", err)
				}
				return
			}
			var ae *apierr.Error
			if !errors.As(err, &ae) {
				t.Fatalf("CheckPassword = %v, want *apierr.Error", err)
			}
			if ae.Code != "weak_password" || ae.Status != http.StatusUnprocessableEntity || ae.Detail == "" {
				t.Fatalf("got code=%q status=%d detail=%q", ae.Code, ae.Status, ae.Detail)
			}
		})
	}
}

func TestCommonPasswordListLoaded(t *testing.T) {
	if n := len(commonPasswords); n < 300 {
		t.Fatalf("common password list has %d entries, want >= 300", n)
	}
	for pw := range commonPasswords {
		if pw != strings.ToLower(pw) || strings.TrimSpace(pw) != pw || strings.HasPrefix(pw, "#") {
			t.Errorf("bad list entry %q", pw)
		}
	}
}
