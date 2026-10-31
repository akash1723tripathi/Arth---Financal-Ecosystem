// Package ai defines the LLM-backed features the API needs (MVP: receipt extraction) as a small interface.
//
// No vendor is chosen: the LLM provider is OPEN (docs/DECISIONS.md D-OPEN-1). This package ships only the
// "fake" (deterministic, for dev and tests) and "none" (feature off) drivers. A real adapter goes in its own
// sub-package (e.g. provider/ai/<vendor>) only after D-OPEN-1 is decided; vendor SDKs are never imported here.
//
// Amounts come back as decimal strings and are parsed by the caller with money.ParseAmount. Every field of an
// Extraction is optional: the model may not find it, and the caller validates each one (type enum, category
// key against the user's categories, date format) before showing a pre-filled form.
package ai

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ErrUnavailable means the extractor is disabled or the provider failed (HTTP: 502 ai_unavailable).
var ErrUnavailable = errors.New("ai: extractor unavailable")

// ErrInvalidInput means the image was empty or of an unsupported content type.
var ErrInvalidInput = errors.New("ai: invalid input")

// Transaction types an extraction may report.
const (
	TypeIncome  = "INCOME"
	TypeExpense = "EXPENSE"
)

// Extraction is what a receipt scan proposes. nil means "not found".
type Extraction struct {
	Type        *string // "INCOME" | "EXPENSE"
	Amount      *string // decimal string in Currency, e.g. "249.00"; parse with money.ParseAmount
	Currency    *string // ISO-4217 if printed on the receipt
	CategoryKey *string // one of ExtractHint.CategoryKeys
	Description *string // short label, ideally the merchant name
	OccurredOn  *string // YYYY-MM-DD
}

// ExtractHint gives the model context: the user's default currency, the category keys it may choose from,
// and today's date (YYYY-MM-DD, in the user's timezone) for relative dates.
type ExtractHint struct {
	Currency     string
	CategoryKeys []string
	Today        string
}

// ReceiptExtractor turns a receipt image into a proposed transaction.
type ReceiptExtractor interface {
	ExtractReceipt(ctx context.Context, img []byte, contentType string, hint ExtractHint) (Extraction, error)
}

// Driver names accepted by New (AI_DRIVER).
const (
	DriverFake = "fake"
	DriverNone = "none"
)

// New returns the extractor for driver.
func New(driver string) (ReceiptExtractor, error) {
	switch driver {
	case DriverFake:
		return NewFake(), nil
	case DriverNone, "":
		return NewNone(), nil
	default:
		return nil, fmt.Errorf("ai: unknown driver %q", driver)
	}
}

// FakeDescription is the description the fake driver returns, so it is obvious in the UI.
const FakeDescription = "Fake receipt (AI_DRIVER=fake)"

// FakeAmount is the amount the fake driver returns.
const FakeAmount = "249.00"

type fake struct{ now func() time.Time }

// NewFake returns a deterministic extractor: EXPENSE, "249.00" in the hint currency, category "food" (when
// allowed by the hint), FakeDescription, dated hint.Today (or today in UTC). It reads nothing from the image.
func NewFake() ReceiptExtractor { return fake{now: time.Now} }

func (f fake) ExtractReceipt(ctx context.Context, img []byte, contentType string, hint ExtractHint) (Extraction, error) {
	if err := ctx.Err(); err != nil {
		return Extraction{}, fmt.Errorf("ai: %w", err)
	}
	if len(img) == 0 || !SupportedImageType(contentType) {
		return Extraction{}, ErrInvalidInput
	}
	ex := Extraction{
		Type:        ptr(TypeExpense),
		Amount:      ptr(FakeAmount),
		Description: ptr(FakeDescription),
	}
	if hint.Currency != "" {
		ex.Currency = ptr(hint.Currency)
	}
	if allowed(hint.CategoryKeys, "food") {
		ex.CategoryKey = ptr("food")
	}
	today := hint.Today
	if today == "" {
		today = f.now().UTC().Format(time.DateOnly)
	}
	ex.OccurredOn = ptr(today)
	return ex, nil
}

type none struct{}

// NewNone returns an extractor that always fails with ErrUnavailable (AI_DRIVER=none).
func NewNone() ReceiptExtractor { return none{} }

func (none) ExtractReceipt(context.Context, []byte, string, ExtractHint) (Extraction, error) {
	return Extraction{}, ErrUnavailable
}

// SupportedImageType reports whether contentType is an image type receipts may be uploaded as.
func SupportedImageType(contentType string) bool {
	switch contentType {
	case "image/jpeg", "image/png", "image/webp", "image/heic", "image/heif":
		return true
	}
	return false
}

func allowed(keys []string, k string) bool {
	if len(keys) == 0 {
		return true
	}
	for _, x := range keys {
		if x == k {
			return true
		}
	}
	return false
}

func ptr(s string) *string { return &s }
