package auth

import (
	"strings"
	"testing"
)

// Code points are built with rune() so the test does not depend on how an editor stores accented text.
var (
	eAcuteComposed = string(rune(0x00E9))       // é as one code point
	eAcuteDecomp   = "e" + string(rune(0x0301)) // e + COMBINING ACUTE ACCENT
	eGraveComposed = string(rune(0x00E8))       // è
	eGraveDecomp   = "e" + string(rune(0x0300)) // e + COMBINING GRAVE ACCENT
	uCircComposed  = string(rune(0x00FB))       // û
	uCircDecomp    = "u" + string(rune(0x0302)) // u + COMBINING CIRCUMFLEX ACCENT
)

func TestHasherNFC(t *testing.T) {
	h := newTestHasher(t, DefaultHashParams())
	composed := "caf" + eAcuteComposed + "-cr" + eGraveComposed + "me-br" + uCircComposed + "l" + eAcuteComposed + "e"
	decomposed := "caf" + eAcuteDecomp + "-cr" + eGraveDecomp + "me-br" + uCircDecomp + "l" + eAcuteDecomp + "e"
	if composed == decomposed {
		t.Fatal("test strings must differ byte-wise")
	}
	phc, err := h.Hash(composed)
	if err != nil {
		t.Fatal(err)
	}
	if ok, _, err := h.Verify(decomposed, phc); err != nil || !ok {
		t.Fatalf("decomposed form must verify against composed hash: %v %v", ok, err)
	}
	phc2, err := h.Hash(decomposed)
	if err != nil {
		t.Fatal(err)
	}
	if ok, _, err := h.Verify(composed, phc2); err != nil || !ok {
		t.Fatalf("composed form must verify against decomposed hash: %v %v", ok, err)
	}
	if ok, _, _ := h.Verify("cafe-creme-brulee", phc); ok {
		t.Fatal("unaccented password must not verify")
	}
}

func TestCheckPasswordCountsNFC(t *testing.T) {
	// 4 × (e + U+0301) is 8 code points decomposed but 4 after NFC → too short.
	if err := CheckPassword(strings.Repeat(eAcuteDecomp, 4)); err == nil {
		t.Fatal("decomposed 4-letter password accepted")
	}
	// 128 × (e + U+0301) is 256 code points decomposed but 128 after NFC → allowed.
	if err := CheckPassword(strings.Repeat(eAcuteDecomp, 128)); err != nil {
		t.Fatalf("128 NFC runes rejected: %v", err)
	}
	// 129 after NFC → too long.
	if err := CheckPassword(strings.Repeat(eAcuteDecomp, 129)); err == nil {
		t.Fatal("129 NFC runes accepted")
	}
}
