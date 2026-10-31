package money

// NOTE: these tests deliberately never use float64. Amounts are int64 minor units and decimal strings only.

import (
	"errors"
	"strings"
	"testing"
)

func TestExponentsAndAllowlist(t *testing.T) {
	want := map[string]int{
		"INR": 2, "USD": 2, "EUR": 2, "GBP": 2, "JPY": 0, "AUD": 2, "CAD": 2, "SGD": 2, "AED": 2, "CHF": 2,
		"CNY": 2, "KRW": 0, "HKD": 2, "NZD": 2, "SEK": 2, "NOK": 2, "DKK": 2, "ZAR": 2, "BRL": 2, "MXN": 2,
		"THB": 2, "MYR": 2, "IDR": 2, "PHP": 2, "SAR": 2, "QAR": 2, "KWD": 3, "BHD": 3, "OMR": 3, "LKR": 2,
		"NPR": 2, "BDT": 2, "PKR": 2,
	}
	if len(SupportedCodes) != len(want) {
		t.Fatalf("SupportedCodes has %d entries, want %d", len(SupportedCodes), len(want))
	}
	for code, exp := range want {
		if !IsSupported(code) {
			t.Errorf("IsSupported(%q) = false", code)
		}
		got, ok := Exponent(code)
		if !ok || got != exp {
			t.Errorf("Exponent(%q) = %d,%v want %d", code, got, ok, exp)
		}
	}
	for _, code := range []string{"", "inr", "Inr", "XXX", "BTC", "USDT", "VND", " INR"} {
		if IsSupported(code) {
			t.Errorf("IsSupported(%q) = true, want false", code)
		}
	}
	if _, ok := Exponent("XYZ"); ok {
		t.Error("Exponent(XYZ) ok = true")
	}
}

func TestParse(t *testing.T) {
	tests := []struct {
		in, cur string
		want    int64
		err     error
	}{
		{"0", "INR", 0, nil},
		{"123.45", "INR", 12345, nil},
		{"123.4", "INR", 12340, nil},
		{"123", "INR", 12300, nil},
		{"0.01", "USD", 1, nil},
		{"-0.05", "USD", -5, nil},
		{"-0", "USD", 0, nil},
		{"007.50", "INR", 750, nil},
		{"249.00", "INR", 24900, nil},
		{"249.000", "INR", 24900, nil}, // zero padding is lossless
		{"500", "JPY", 500, nil},
		{"500.00", "JPY", 500, nil},
		{"1500", "KRW", 1500, nil},
		{"1.234", "KWD", 1234, nil},
		{"1.2", "KWD", 1200, nil},
		{"0.001", "BHD", 1, nil},
		{"10000.50", "IDR", 1000050, nil},
		{"92233720368547758.07", "USD", 9223372036854775807, nil},   // MaxInt64
		{"-92233720368547758.07", "USD", -9223372036854775807, nil}, // -MaxInt64
		{"9223372036854775807", "JPY", 9223372036854775807, nil},

		// precision: never round
		{"123.456", "INR", 0, ErrPrecision},
		{"0.001", "USD", 0, ErrPrecision},
		{"500.5", "JPY", 0, ErrPrecision},
		{"1.2345", "KWD", 0, ErrPrecision},
		{"1.0000001", "INR", 0, ErrPrecision},

		// syntax
		{"", "INR", 0, ErrEmpty},
		{"-", "INR", 0, ErrSyntax},
		{".", "INR", 0, ErrSyntax},
		{".5", "INR", 0, ErrSyntax},
		{"5.", "INR", 0, ErrSyntax},
		{"+5", "INR", 0, ErrSyntax},
		{"--5", "INR", 0, ErrSyntax},
		{"5-", "INR", 0, ErrSyntax},
		{" 5", "INR", 0, ErrSyntax},
		{"5 ", "INR", 0, ErrSyntax},
		{"1,000", "INR", 0, ErrSyntax},
		{"1,5", "INR", 0, ErrSyntax},
		{"1e3", "INR", 0, ErrSyntax},
		{"1E3", "INR", 0, ErrSyntax},
		{"1.2.3", "INR", 0, ErrSyntax},
		{"0x10", "INR", 0, ErrSyntax},
		{"NaN", "INR", 0, ErrSyntax},
		{"Inf", "INR", 0, ErrSyntax},
		{"₹100", "INR", 0, ErrSyntax},
		{"१००", "INR", 0, ErrSyntax}, // Devanagari digits are not ASCII digits
		{"12_000", "INR", 0, ErrSyntax},

		// range
		{"92233720368547758.08", "USD", 0, ErrRange},
		{"9223372036854775808", "JPY", 0, ErrRange},
		{"99999999999999999999", "INR", 0, ErrRange},
		{strings.Repeat("1", 60), "INR", 0, ErrRange},

		// currency
		{"1.00", "inr", 0, ErrUnsupportedCurrency},
		{"1.00", "XXX", 0, ErrUnsupportedCurrency},
		{"1.00", "", 0, ErrUnsupportedCurrency},
		{"1.00", "VND", 0, ErrUnsupportedCurrency}, // known to ISO table, not on the allowlist
	}
	for _, tc := range tests {
		got, err := Parse(tc.in, tc.cur)
		if tc.err != nil {
			if !errors.Is(err, tc.err) {
				t.Errorf("Parse(%q,%q) err = %v, want %v", tc.in, tc.cur, err, tc.err)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("Parse(%q,%q) = %d,%v want %d", tc.in, tc.cur, got, err, tc.want)
		}
	}
}

func TestParseAmount(t *testing.T) {
	tests := []struct {
		in, cur string
		want    int64
		err     error
	}{
		{"0.01", "INR", 1, nil},
		{"249.00", "INR", 24900, nil},
		{"9999999999999.99", "INR", MaxAmountMinor, nil},
		{"999999999999999", "JPY", MaxAmountMinor, nil},
		{"999999999999.999", "KWD", MaxAmountMinor, nil},
		{"10000000000000.00", "INR", 0, ErrRange},
		{"1000000000000000", "JPY", 0, ErrRange},
		{"0", "INR", 0, ErrNotPositive},
		{"0.00", "INR", 0, ErrNotPositive},
		{"-1", "INR", 0, ErrNotPositive},
		{"-0.01", "INR", 0, ErrNotPositive},
		{"abc", "INR", 0, ErrSyntax},
		{"1.001", "INR", 0, ErrPrecision},
		{"1", "XXX", 0, ErrUnsupportedCurrency},
	}
	for _, tc := range tests {
		got, err := ParseAmount(tc.in, tc.cur)
		if tc.err != nil {
			if !errors.Is(err, tc.err) {
				t.Errorf("ParseAmount(%q,%q) err = %v, want %v", tc.in, tc.cur, err, tc.err)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("ParseAmount(%q,%q) = %d,%v want %d", tc.in, tc.cur, got, err, tc.want)
		}
	}
}

func TestFormat(t *testing.T) {
	tests := []struct {
		minor int64
		cur   string
		want  string
	}{
		{0, "INR", "0.00"},
		{1, "INR", "0.01"},
		{10, "INR", "0.10"},
		{123450, "INR", "1234.50"},
		{-5, "USD", "-0.05"},
		{-123450, "INR", "-1234.50"},
		{500, "JPY", "500"},
		{0, "JPY", "0"},
		{-500, "KRW", "-500"},
		{1234, "KWD", "1.234"},
		{5, "BHD", "0.005"},
		{-1, "OMR", "-0.001"},
		{1000050, "IDR", "10000.50"},
		{9223372036854775807, "USD", "92233720368547758.07"},
		{-9223372036854775808, "USD", "-92233720368547758.08"},
		{12345, "ZZZ", "123.45"}, // unknown → exponent 2
	}
	for _, tc := range tests {
		if got := Format(tc.minor, tc.cur); got != tc.want {
			t.Errorf("Format(%d,%q) = %q want %q", tc.minor, tc.cur, got, tc.want)
		}
	}
}

func TestRoundTrip(t *testing.T) {
	values := []int64{0, 1, -1, 99, 100, 12345, -98765, MaxAmountMinor, -MaxAmountMinor}
	for _, cur := range SupportedCodes {
		for _, v := range values {
			s := Format(v, cur)
			got, err := Parse(s, cur)
			if err != nil || got != v {
				t.Errorf("round trip %d %s via %q = %d,%v", v, cur, s, got, err)
			}
		}
	}
}

func TestMoneyString(t *testing.T) {
	if got := (Money{Minor: 12345, Currency: "INR"}).String(); got != "123.45 INR" {
		t.Errorf("String() = %q", got)
	}
}
