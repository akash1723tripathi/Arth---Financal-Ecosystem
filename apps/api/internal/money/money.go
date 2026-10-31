// Package money handles amounts as integer minor units plus an ISO-4217 currency code.
//
// Rule: float64 (or any binary floating point) is never used for amounts in this package or its callers.
// Amounts enter the system as decimal strings (request bodies, LLM output) and are parsed exactly with
// Parse/ParseAmount; they leave it as decimal strings via Format (CSV, emails). JSON carries amount_minor.
package money

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// MaxAmountMinor is the largest single amount accepted by ParseAmount. It matches the
// transactions.amount_minor CHECK constraint (docs/SCHEMA.md).
const MaxAmountMinor int64 = 999_999_999_999_999

// maxInputLen bounds the work Parse does on hostile input. 19 integer digits + '.' + fraction + sign fit easily.
const maxInputLen = 48

// Errors returned by Parse/ParseAmount. Match them with errors.Is.
var (
	ErrUnsupportedCurrency = errors.New("money: unsupported currency")
	ErrEmpty               = errors.New("money: empty amount")
	ErrSyntax              = errors.New("money: invalid amount syntax")
	ErrPrecision           = errors.New("money: too many decimal places for currency")
	ErrRange               = errors.New("money: amount out of range")
	ErrNotPositive         = errors.New("money: amount must be greater than zero")
)

// Money is an amount in minor units of Currency (e.g. 12345 INR = ₹123.45).
type Money struct {
	Minor    int64
	Currency string
}

// String renders "123.45 INR". Intended for debugging, not for user-facing formatting.
func (m Money) String() string { return Format(m.Minor, m.Currency) + " " + m.Currency }

// isoExponents is the ISO-4217 minor-unit exponent for each code we know about. It is broader than the
// supported allowlist so Format stays correct for any code that might appear later.
var isoExponents = map[string]int{
	// exponent 0
	"JPY": 0, "KRW": 0, "VND": 0, "CLP": 0, "ISK": 0, "UGX": 0, "XAF": 0, "XOF": 0, "PYG": 0, "RWF": 0,
	// exponent 3
	"KWD": 3, "BHD": 3, "OMR": 3, "JOD": 3, "IQD": 3, "LYD": 3, "TND": 3,
	// exponent 2
	"INR": 2, "USD": 2, "EUR": 2, "GBP": 2, "AUD": 2, "CAD": 2, "SGD": 2, "AED": 2, "CHF": 2, "CNY": 2,
	"HKD": 2, "NZD": 2, "SEK": 2, "NOK": 2, "DKK": 2, "ZAR": 2, "BRL": 2, "MXN": 2, "THB": 2, "MYR": 2,
	"IDR": 2, "PHP": 2, "SAR": 2, "QAR": 2, "LKR": 2, "NPR": 2, "BDT": 2, "PKR": 2, "TRY": 2, "PLN": 2,
	"CZK": 2, "HUF": 2, "ILS": 2, "EGP": 2, "NGN": 2, "KES": 2, "TWD": 2, "RUB": 2, "ARS": 2, "COP": 2,
}

// supported is the explicit allowlist of currencies users can pick (accounts, onboarding, budgets).
var supported = map[string]struct{}{}

// SupportedCodes lists the supported currencies in a stable order (for docs/tests/UI lists).
var SupportedCodes = []string{
	"INR", "USD", "EUR", "GBP", "JPY", "AUD", "CAD", "SGD", "AED", "CHF", "CNY", "KRW", "HKD", "NZD",
	"SEK", "NOK", "DKK", "ZAR", "BRL", "MXN", "THB", "MYR", "IDR", "PHP", "SAR", "QAR", "KWD", "BHD",
	"OMR", "LKR", "NPR", "BDT", "PKR",
}

func init() {
	for _, c := range SupportedCodes {
		supported[c] = struct{}{}
	}
}

// IsSupported reports whether code (exact upper-case ISO-4217) is on the allowlist.
func IsSupported(code string) bool {
	_, ok := supported[code]
	return ok
}

// Exponent returns the ISO-4217 minor-unit exponent for code (exact upper-case). ok is false for unknown codes.
func Exponent(code string) (exp int, ok bool) {
	exp, ok = isoExponents[code]
	return exp, ok
}

// Parse converts an exact decimal string into minor units of a supported currency.
//
// Accepted grammar: an optional leading '-', one or more ASCII digits, optionally followed by '.' and one or
// more ASCII digits. Rejected: empty input, '+', spaces, commas, exponents ("1e3"), a bare/leading/trailing
// '.', non-ASCII digits. Fractional digits beyond the currency exponent are accepted only when they are all
// zero ("249.000" INR); any non-zero excess digit is ErrPrecision (never rounded). Values that do not fit
// in int64 are ErrRange.
func Parse(s, currency string) (int64, error) {
	exp, ok := supportedExponent(currency)
	if !ok {
		return 0, fmt.Errorf("%w: %q", ErrUnsupportedCurrency, currency)
	}
	if s == "" {
		return 0, ErrEmpty
	}
	if len(s) > maxInputLen {
		return 0, ErrRange
	}

	neg := false
	rest := s
	if rest[0] == '-' {
		neg = true
		rest = rest[1:]
	}
	intPart, fracPart, hasDot := strings.Cut(rest, ".")
	if intPart == "" || !allDigits(intPart) {
		return 0, ErrSyntax
	}
	if hasDot && (fracPart == "" || !allDigits(fracPart)) {
		return 0, ErrSyntax
	}
	if len(fracPart) > exp {
		for i := exp; i < len(fracPart); i++ {
			if fracPart[i] != '0' {
				return 0, ErrPrecision
			}
		}
		fracPart = fracPart[:exp]
	}

	// Accumulate as a non-negative magnitude with overflow checks (magnitude ≤ MaxInt64, so -magnitude fits).
	var v int64
	push := func(d byte) error {
		n := int64(d - '0')
		if v > (maxInt64-n)/10 {
			return ErrRange
		}
		v = v*10 + n
		return nil
	}
	for i := 0; i < len(intPart); i++ {
		if err := push(intPart[i]); err != nil {
			return 0, err
		}
	}
	for i := 0; i < exp; i++ {
		d := byte('0')
		if i < len(fracPart) {
			d = fracPart[i]
		}
		if err := push(d); err != nil {
			return 0, err
		}
	}
	if neg {
		v = -v
	}
	return v, nil
}

// ParseAmount is Parse for a transaction/budget amount: the result must be > 0 and ≤ MaxAmountMinor.
func ParseAmount(s, currency string) (int64, error) {
	v, err := Parse(s, currency)
	if err != nil {
		return 0, err
	}
	if v <= 0 {
		return 0, ErrNotPositive
	}
	if v > MaxAmountMinor {
		return 0, ErrRange
	}
	return v, nil
}

// Format renders minor units as a plain decimal string with exactly Exponent(currency) fraction digits:
// 123450 INR → "1234.50", -5 USD → "-0.05", 500 JPY → "500", 1234 KWD → "1.234". No symbols or grouping
// (used for CSV). An unknown currency code falls back to exponent 2.
func Format(minor int64, currency string) string {
	exp, ok := isoExponents[currency]
	if !ok {
		exp = 2
	}
	neg := minor < 0
	// Work on the unsigned magnitude so MinInt64 is handled without overflow.
	var mag uint64
	if neg {
		mag = uint64(-(minor + 1)) + 1
	} else {
		mag = uint64(minor)
	}
	digits := strconv.FormatUint(mag, 10)
	if exp > 0 {
		if len(digits) <= exp {
			digits = strings.Repeat("0", exp-len(digits)+1) + digits
		}
		digits = digits[:len(digits)-exp] + "." + digits[len(digits)-exp:]
	}
	if neg {
		return "-" + digits
	}
	return digits
}

const maxInt64 = int64(^uint64(0) >> 1)

func supportedExponent(code string) (int, bool) {
	if !IsSupported(code) {
		return 0, false
	}
	return Exponent(code)
}

func allDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
