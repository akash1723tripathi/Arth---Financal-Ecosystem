package ai

import (
	"context"
	"errors"
	"testing"
	"time"
)

func deref(p *string) string {
	if p == nil {
		return "<nil>"
	}
	return *p
}

func TestNew(t *testing.T) {
	for _, d := range []string{"fake", "none", ""} {
		if x, err := New(d); err != nil || x == nil {
			t.Errorf("New(%q) = %v,%v", d, x, err)
		}
	}
	if _, err := New("gemini"); err == nil {
		t.Error("New(gemini) succeeded; real drivers are not decided (D-OPEN-1)")
	}
}

func TestFakeExtract(t *testing.T) {
	img := []byte{0xFF, 0xD8, 0xFF}
	tests := []struct {
		name string
		hint ExtractHint
		cat  string
		cur  string
		date string
	}{
		{"full hint", ExtractHint{Currency: "INR", CategoryKeys: []string{"food", "groceries"}, Today: "2026-10-31"}, "food", "INR", "2026-10-31"},
		{"food not allowed", ExtractHint{Currency: "USD", CategoryKeys: []string{"salary"}, Today: "2026-01-02"}, "<nil>", "USD", "2026-01-02"},
		{"empty hint", ExtractHint{}, "food", "<nil>", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ex, err := NewFake().ExtractReceipt(context.Background(), img, "image/jpeg", tc.hint)
			if err != nil {
				t.Fatal(err)
			}
			if deref(ex.Type) != TypeExpense || deref(ex.Amount) != "249.00" || deref(ex.Description) != FakeDescription {
				t.Fatalf("got type=%s amount=%s desc=%s", deref(ex.Type), deref(ex.Amount), deref(ex.Description))
			}
			if deref(ex.CategoryKey) != tc.cat || deref(ex.Currency) != tc.cur {
				t.Fatalf("category=%s currency=%s", deref(ex.CategoryKey), deref(ex.Currency))
			}
			if tc.date != "" && deref(ex.OccurredOn) != tc.date {
				t.Fatalf("date = %s", deref(ex.OccurredOn))
			}
			if _, err := time.Parse(time.DateOnly, deref(ex.OccurredOn)); err != nil {
				t.Fatalf("date %q not YYYY-MM-DD", deref(ex.OccurredOn))
			}
		})
	}
	// Deterministic.
	h := ExtractHint{Currency: "INR", Today: "2026-10-31"}
	a, _ := NewFake().ExtractReceipt(context.Background(), img, "image/png", h)
	b, _ := NewFake().ExtractReceipt(context.Background(), img, "image/png", h)
	if *a.Amount != *b.Amount || *a.OccurredOn != *b.OccurredOn {
		t.Fatal("fake is not deterministic")
	}
}

func TestFakeRejectsBadInput(t *testing.T) {
	f := NewFake()
	if _, err := f.ExtractReceipt(context.Background(), nil, "image/jpeg", ExtractHint{}); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("empty image err = %v", err)
	}
	if _, err := f.ExtractReceipt(context.Background(), []byte("x"), "application/pdf", ExtractHint{}); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("pdf err = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := f.ExtractReceipt(ctx, []byte("x"), "image/jpeg", ExtractHint{}); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled ctx err = %v", err)
	}
}

func TestNoneUnavailable(t *testing.T) {
	if _, err := NewNone().ExtractReceipt(context.Background(), []byte("x"), "image/jpeg", ExtractHint{}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v", err)
	}
}
