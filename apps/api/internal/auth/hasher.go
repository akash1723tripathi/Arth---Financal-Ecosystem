// Package auth holds Arth's own authentication building blocks (docs/AUTH.md): Argon2id password hashing,
// access-token (JWT) issue/verify, opaque refresh tokens, the password policy, in-memory rate limits and
// the request principal. Nothing here logs passwords, tokens, token hashes or full emails.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
	"golang.org/x/text/unicode/norm"
)

// normalize puts a password in Unicode NFC form before hashing (AUTH.md Q-1, decided: yes), so a composed
// "é" (U+00E9) and a decomposed "e"+U+0301 are the same password regardless of the client's keyboard/OS.
func normalize(password string) []byte { return []byte(norm.NFC.String(password)) }

// HashParams are the Argon2id parameters. Memory is in KiB.
type HashParams struct {
	Memory  uint32 // KiB
	Time    uint32 // iterations
	Threads uint8
	SaltLen uint32 // bytes
	KeyLen  uint32 // bytes
}

// DefaultHashParams is the OWASP baseline from docs/AUTH.md §2 (m=19 MiB, t=2, p=1, 16-byte salt, 32-byte key).
// Final values are Q-1 (tune on the production host).
func DefaultHashParams() HashParams {
	return HashParams{Memory: 19456, Time: 2, Threads: 1, SaltLen: 16, KeyLen: 32}
}

// Bounds applied to our own parameters and to parameters read back from a stored PHC string, so a corrupted
// or tampered hash cannot make a verify allocate gigabytes or spin for minutes.
const (
	minMemoryKiB = 8 * 1024
	maxMemoryKiB = 1024 * 1024 // 1 GiB
	minTime      = 1
	maxTime      = 16
	minSaltLen   = 8
	maxSaltLen   = 64
	minKeyLen    = 16
	maxKeyLen    = 64
	maxPHCLen    = 256
)

// ErrInvalidHash means a stored hash is not a well-formed Argon2id PHC string we accept.
var ErrInvalidHash = errors.New("auth: invalid password hash")

// Hasher hashes and verifies passwords with Argon2id, stored as PHC strings:
//
//	$argon2id$v=19$m=19456,t=2,p=1$<base64 salt>$<base64 hash>   (standard alphabet, no padding)
type Hasher struct {
	p     HashParams
	dummy string // PHC hash of a random secret; verified against for unknown emails
}

// NewHasher validates p and precomputes the dummy hash used by VerifyDummy.
func NewHasher(p HashParams) (*Hasher, error) {
	if err := validateParams(p); err != nil {
		return nil, err
	}
	h := &Hasher{p: p}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("auth: dummy secret: %w", err)
	}
	dummy, err := h.Hash(base64.RawStdEncoding.EncodeToString(secret))
	if err != nil {
		return nil, fmt.Errorf("auth: dummy hash: %w", err)
	}
	h.dummy = dummy
	return h, nil
}

// Params returns the parameters new hashes are created with.
func (h *Hasher) Params() HashParams { return h.p }

// Hash returns a PHC-encoded Argon2id hash of password with a fresh random salt.
func (h *Hasher) Hash(password string) (string, error) {
	salt := make([]byte, h.p.SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: salt: %w", err)
	}
	key := argon2.IDKey(normalize(password), salt, h.p.Time, h.p.Memory, h.p.Threads, h.p.KeyLen)
	return encodePHC(h.p, salt, key), nil
}

// Verify checks password against a stored PHC hash. ok is true on a match. needsRehash is true (only when ok)
// if the stored parameters differ from the Hasher's current ones, so the caller should store Hash(password).
// err is non-nil only for a malformed stored hash (wraps ErrInvalidHash); a wrong password is ok=false, err=nil.
func (h *Hasher) Verify(password, phc string) (ok bool, needsRehash bool, err error) {
	p, salt, want, err := decodePHC(phc)
	if err != nil {
		return false, false, err
	}
	got := argon2.IDKey(normalize(password), salt, p.Time, p.Memory, p.Threads, p.KeyLen)
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return false, false, nil
	}
	return true, p != h.p, nil
}

// VerifyDummy runs a full verify against a fixed random hash and always returns false. Call it when the email
// is unknown so the response time does not reveal whether the account exists (docs/AUTH.md §3 Login).
func (h *Hasher) VerifyDummy(password string) bool {
	_, _, _ = h.Verify(password, h.dummy) // the work is the point; the result is ignored
	return false
}

func validateParams(p HashParams) error {
	switch {
	case p.Threads < 1:
		return fmt.Errorf("auth: argon2 threads must be >= 1")
	case p.Memory < minMemoryKiB || p.Memory > maxMemoryKiB || p.Memory < 8*uint32(p.Threads):
		return fmt.Errorf("auth: argon2 memory %d KiB out of range [%d, %d]", p.Memory, minMemoryKiB, maxMemoryKiB)
	case p.Time < minTime || p.Time > maxTime:
		return fmt.Errorf("auth: argon2 time %d out of range [%d, %d]", p.Time, minTime, maxTime)
	case p.SaltLen < minSaltLen || p.SaltLen > maxSaltLen:
		return fmt.Errorf("auth: argon2 salt length %d out of range [%d, %d]", p.SaltLen, minSaltLen, maxSaltLen)
	case p.KeyLen < minKeyLen || p.KeyLen > maxKeyLen:
		return fmt.Errorf("auth: argon2 key length %d out of range [%d, %d]", p.KeyLen, minKeyLen, maxKeyLen)
	}
	return nil
}

var b64 = base64.RawStdEncoding.Strict()

func encodePHC(p HashParams, salt, key []byte) string {
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, p.Memory, p.Time, p.Threads, b64.EncodeToString(salt), b64.EncodeToString(key))
}

// decodePHC parses exactly "$argon2id$v=19$m=<n>,t=<n>,p=<n>$<salt>$<hash>" (fixed field order, canonical
// decimal numbers, unpadded standard base64). Anything else is ErrInvalidHash.
func decodePHC(phc string) (HashParams, []byte, []byte, error) {
	var zero HashParams
	if len(phc) > maxPHCLen {
		return zero, nil, nil, ErrInvalidHash
	}
	parts := strings.Split(phc, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" || parts[2] != "v="+strconv.Itoa(argon2.Version) {
		return zero, nil, nil, ErrInvalidHash
	}
	kv := strings.Split(parts[3], ",")
	if len(kv) != 3 {
		return zero, nil, nil, ErrInvalidHash
	}
	m, err1 := phcUint(kv[0], "m=", 32)
	t, err2 := phcUint(kv[1], "t=", 32)
	th, err3 := phcUint(kv[2], "p=", 8)
	if err := errors.Join(err1, err2, err3); err != nil {
		return zero, nil, nil, ErrInvalidHash
	}
	salt, err := b64.DecodeString(parts[4])
	if err != nil {
		return zero, nil, nil, ErrInvalidHash
	}
	key, err := b64.DecodeString(parts[5])
	if err != nil {
		return zero, nil, nil, ErrInvalidHash
	}
	p := HashParams{Memory: uint32(m), Time: uint32(t), Threads: uint8(th), SaltLen: uint32(len(salt)), KeyLen: uint32(len(key))}
	if err := validateParams(p); err != nil {
		return zero, nil, nil, fmt.Errorf("%w: %v", ErrInvalidHash, err)
	}
	return p, salt, key, nil
}

func phcUint(s, prefix string, bits int) (uint64, error) {
	v, ok := strings.CutPrefix(s, prefix)
	if !ok || v == "" || (len(v) > 1 && v[0] == '0') {
		return 0, ErrInvalidHash
	}
	for i := 0; i < len(v); i++ {
		if v[i] < '0' || v[i] > '9' {
			return 0, ErrInvalidHash
		}
	}
	n, err := strconv.ParseUint(v, 10, bits)
	if err != nil {
		return 0, ErrInvalidHash
	}
	return n, nil
}
