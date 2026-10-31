package auth

import (
	_ "embed"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"

	"arth/api/internal/apierr"
)

// Password length limits, counted in Unicode code points (docs/AUTH.md §4).
const (
	MinPasswordRunes = 8
	MaxPasswordRunes = 128
)

//go:embed common_passwords.txt
var commonPasswordsFile string

// commonPasswords is the lower-cased set from common_passwords.txt (one per line; '#' lines are comments).
var commonPasswords = func() map[string]struct{} {
	set := make(map[string]struct{}, 512)
	for _, line := range strings.Split(commonPasswordsFile, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		set[strings.ToLower(line)] = struct{}{}
	}
	return set
}()

// CheckPassword applies the NIST SP 800-63B style policy: 8–128 characters, not all whitespace, not on the
// embedded common-passwords list. No composition rules. It returns an *apierr.Error (422 weak_password) or nil.
//
// Length is counted on the NFC form, the same form the Hasher hashes (Q-1 decided: NFC).
func CheckPassword(pw string) error {
	if !utf8.ValidString(pw) {
		return apierr.WeakPassword("Password must be valid UTF-8 text.")
	}
	pw = norm.NFC.String(pw)
	n := utf8.RuneCountInString(pw)
	if n < MinPasswordRunes {
		return apierr.WeakPassword("Password must be at least 8 characters.")
	}
	if n > MaxPasswordRunes {
		return apierr.WeakPassword("Password must be at most 128 characters.")
	}
	if strings.TrimFunc(pw, unicode.IsSpace) == "" {
		return apierr.WeakPassword("Password must not be only spaces.")
	}
	if _, bad := commonPasswords[strings.ToLower(pw)]; bad {
		return apierr.WeakPassword("This password is too common. Choose a less predictable one.")
	}
	return nil
}
