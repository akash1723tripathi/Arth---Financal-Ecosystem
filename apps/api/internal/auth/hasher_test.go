package auth

import (
	"encoding/base64"
	"errors"
	"regexp"
	"strings"
	"testing"
)

var phcRe = regexp.MustCompile(`^\$argon2id\$v=19\$m=19456,t=2,p=1\$[A-Za-z0-9+/]{22}\$[A-Za-z0-9+/]{43}$`)

func newTestHasher(t *testing.T, p HashParams) *Hasher {
	t.Helper()
	h, err := NewHasher(p)
	if err != nil {
		t.Fatalf("NewHasher: %v", err)
	}
	return h
}

func TestHasherRoundTrip(t *testing.T) {
	h := newTestHasher(t, DefaultHashParams())
	for _, pw := range []string{"correct horse battery staple", "pässwörd-ünïcode-🔑", strings.Repeat("x", 128), " leading and trailing "} {
		phc, err := h.Hash(pw)
		if err != nil {
			t.Fatalf("Hash: %v", err)
		}
		if !phcRe.MatchString(phc) {
			t.Fatalf("PHC %q does not match the expected format", phc)
		}
		if strings.Contains(phc, pw) {
			t.Fatal("PHC contains the plaintext")
		}
		ok, rehash, err := h.Verify(pw, phc)
		if err != nil || !ok || rehash {
			t.Fatalf("Verify(correct) = %v,%v,%v", ok, rehash, err)
		}
		ok, _, err = h.Verify(pw+"x", phc)
		if err != nil || ok {
			t.Fatalf("Verify(wrong) = %v,%v", ok, err)
		}
	}
}

func TestHasherSaltIsRandom(t *testing.T) {
	h := newTestHasher(t, DefaultHashParams())
	a, _ := h.Hash("same password here")
	b, _ := h.Hash("same password here")
	if a == b {
		t.Fatal("two hashes of the same password are identical (salt not random)")
	}
}

func TestHasherNeedsRehash(t *testing.T) {
	old := HashParams{Memory: 12288, Time: 3, Threads: 1, SaltLen: 16, KeyLen: 32}
	hOld := newTestHasher(t, old)
	hNew := newTestHasher(t, DefaultHashParams())
	phc, err := hOld.Hash("a decent passphrase")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(phc, "$argon2id$v=19$m=12288,t=3,p=1$") {
		t.Fatalf("unexpected PHC prefix: %s", phc)
	}
	ok, rehash, err := hNew.Verify("a decent passphrase", phc)
	if err != nil || !ok || !rehash {
		t.Fatalf("Verify old-param hash with new hasher = %v,%v,%v; want true,true,nil", ok, rehash, err)
	}
	ok, rehash, err = hNew.Verify("wrong passphrase!!", phc)
	if err != nil || ok || rehash {
		t.Fatalf("wrong password must not report rehash: %v,%v,%v", ok, rehash, err)
	}
}

func TestHasherTamperedAndMalformed(t *testing.T) {
	h := newTestHasher(t, DefaultHashParams())
	pw := "tamper test password"
	phc, err := h.Hash(pw)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(phc, "$")
	key, _ := base64.RawStdEncoding.DecodeString(parts[5])
	key[0] ^= 0x01
	flippedKey := strings.Join(append(parts[:5:5], base64.RawStdEncoding.EncodeToString(key)), "$")
	salt, _ := base64.RawStdEncoding.DecodeString(parts[4])
	salt[0] ^= 0x01
	flippedSalt := strings.Join([]string{"", parts[1], parts[2], parts[3], base64.RawStdEncoding.EncodeToString(salt), parts[5]}, "$")
	lowerT := strings.Replace(phc, "t=2", "t=1", 1)

	// Well-formed but not matching: ok=false, err=nil.
	for name, s := range map[string]string{"flipped key": flippedKey, "flipped salt": flippedSalt, "changed t": lowerT} {
		ok, _, err := h.Verify(pw, s)
		if err != nil || ok {
			t.Errorf("%s: Verify = %v,%v; want false,nil", name, ok, err)
		}
	}

	malformed := map[string]string{
		"empty":           "",
		"argon2i":         strings.Replace(phc, "$argon2id$", "$argon2i$", 1),
		"bcrypt":          "$2a$10$abcdefghijklmnopqrstuuOe0bHq3Sx1lBnKLdM3W5P1n6pBO7ERW",
		"version 16":      strings.Replace(phc, "v=19", "v=16", 1),
		"param order":     strings.Replace(phc, "m=19456,t=2,p=1", "t=2,m=19456,p=1", 1),
		"leading zero":    strings.Replace(phc, "t=2", "t=02", 1),
		"negative":        strings.Replace(phc, "t=2", "t=-2", 1),
		"missing p":       strings.Replace(phc, ",p=1", "", 1),
		"extra param":     strings.Replace(phc, "p=1", "p=1,x=1", 1),
		"huge memory":     strings.Replace(phc, "m=19456", "m=4194304", 1),
		"zero time":       strings.Replace(phc, "t=2", "t=0", 1),
		"padded salt":     strings.Replace(phc, parts[4], parts[4]+"==", 1),
		"bad base64 hash": strings.Replace(phc, parts[5], parts[5][:42]+"!", 1),
		"url alphabet":    strings.Join([]string{"", parts[1], parts[2], parts[3], "-_" + parts[4][2:], parts[5]}, "$"),
		"short hash":      strings.Replace(phc, parts[5], parts[5][:8], 1),
		"trailing field":  phc + "$x",
		"too long":        phc + strings.Repeat("A", 300),
		"no leading $":    phc[1:],
	}
	for name, s := range malformed {
		ok, rehash, err := h.Verify(pw, s)
		if !errors.Is(err, ErrInvalidHash) || ok || rehash {
			t.Errorf("%s: Verify = %v,%v,%v; want ErrInvalidHash", name, ok, rehash, err)
		}
	}
}

func TestHasherVerifyDummy(t *testing.T) {
	h := newTestHasher(t, DefaultHashParams())
	if !phcRe.MatchString(h.dummy) {
		t.Fatalf("dummy hash %q is not a valid PHC string", h.dummy)
	}
	if h.VerifyDummy("anything at all") {
		t.Fatal("VerifyDummy returned true")
	}
}

func TestNewHasherRejectsBadParams(t *testing.T) {
	bad := []HashParams{
		{},
		{Memory: 1024, Time: 2, Threads: 1, SaltLen: 16, KeyLen: 32},
		{Memory: 19456, Time: 0, Threads: 1, SaltLen: 16, KeyLen: 32},
		{Memory: 19456, Time: 2, Threads: 0, SaltLen: 16, KeyLen: 32},
		{Memory: 19456, Time: 2, Threads: 1, SaltLen: 4, KeyLen: 32},
		{Memory: 19456, Time: 2, Threads: 1, SaltLen: 16, KeyLen: 8},
	}
	for _, p := range bad {
		if _, err := NewHasher(p); err == nil {
			t.Errorf("NewHasher(%+v) succeeded", p)
		}
	}
}
