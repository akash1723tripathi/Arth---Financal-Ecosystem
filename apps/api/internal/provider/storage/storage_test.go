package storage

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
)

func TestNew(t *testing.T) {
	for _, d := range []string{"none", "memory", ""} {
		if s, err := New(d); err != nil || s == nil {
			t.Errorf("New(%q) = %v,%v", d, s, err)
		}
	}
	if _, err := New("s3"); err == nil {
		t.Error("New(s3) succeeded; provider not decided (D-OPEN-7)")
	}
}

func TestValidKey(t *testing.T) {
	tests := map[string]bool{
		"receipts/u1/abc.jpg":            true,
		"a":                              true,
		"":                               false,
		"/abs":                           false,
		"a//b":                           false,
		"a/":                             false,
		"a/../b":                         false,
		"./a":                            false,
		`a\b`:                            false,
		"a\x00b":                         false,
		"a\nb":                           false,
		strings.Repeat("a", MaxKeyLen):   true,
		strings.Repeat("a", MaxKeyLen+1): false,
	}
	for k, want := range tests {
		if got := ValidKey(k); got != want {
			t.Errorf("ValidKey(%q) = %v want %v", k, got, want)
		}
	}
}

func TestNoneDisabled(t *testing.T) {
	ctx := context.Background()
	var s None
	if err := s.Put(ctx, "k", strings.NewReader("x"), 1, "text/plain"); !errors.Is(err, ErrDisabled) {
		t.Error(err)
	}
	if _, err := s.Get(ctx, "k"); !errors.Is(err, ErrDisabled) {
		t.Error(err)
	}
	if err := s.Delete(ctx, "k"); !errors.Is(err, ErrDisabled) {
		t.Error(err)
	}
}

func TestMemoryRoundTrip(t *testing.T) {
	ctx := context.Background()
	m := NewMemory()
	if err := m.Put(ctx, "receipts/u1/r1", strings.NewReader("hello"), 5, "image/jpeg"); err != nil {
		t.Fatal(err)
	}
	rc, err := m.Get(ctx, "receipts/u1/r1")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(rc)
	_ = rc.Close()
	if string(b) != "hello" {
		t.Fatalf("Get = %q", b)
	}
	if ct, ok := m.ContentType("receipts/u1/r1"); !ok || ct != "image/jpeg" {
		t.Fatalf("content type = %q,%v", ct, ok)
	}
	// Overwrite.
	if err := m.Put(ctx, "receipts/u1/r1", strings.NewReader("bye"), 3, "image/png"); err != nil {
		t.Fatal(err)
	}
	rc, _ = m.Get(ctx, "receipts/u1/r1")
	b, _ = io.ReadAll(rc)
	if string(b) != "bye" || m.Len() != 1 {
		t.Fatalf("after overwrite: %q len=%d", b, m.Len())
	}
	if err := m.Delete(ctx, "receipts/u1/r1"); err != nil {
		t.Fatal(err)
	}
	if err := m.Delete(ctx, "receipts/u1/r1"); err != nil {
		t.Fatalf("second delete: %v", err)
	}
	if _, err := m.Get(ctx, "receipts/u1/r1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after delete err = %v", err)
	}
}

func TestMemoryErrors(t *testing.T) {
	ctx := context.Background()
	m := NewMemory()
	cases := []struct {
		name string
		key  string
		body string
		size int64
		want error
	}{
		{"short reader", "k", "abc", 5, ErrSizeMismatch},
		{"long reader", "k", "abcdef", 3, ErrSizeMismatch},
		{"negative size", "k", "", -1, ErrSizeMismatch},
		{"bad key", "../etc/passwd", "x", 1, ErrInvalidKey},
		{"empty key", "", "x", 1, ErrInvalidKey},
	}
	for _, tc := range cases {
		if err := m.Put(ctx, tc.key, strings.NewReader(tc.body), tc.size, "text/plain"); !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v want %v", tc.name, err, tc.want)
		}
	}
	if m.Len() != 0 {
		t.Fatal("failed puts stored data")
	}
	if err := m.Put(ctx, "empty", strings.NewReader(""), 0, "text/plain"); err != nil {
		t.Fatalf("zero-byte put: %v", err)
	}
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if err := m.Put(cctx, "k", strings.NewReader("x"), 1, ""); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled put err = %v", err)
	}
}

func TestMemoryGetReturnsCopy(t *testing.T) {
	ctx := context.Background()
	m := NewMemory()
	_ = m.Put(ctx, "k", strings.NewReader("abc"), 3, "")
	rc, _ := m.Get(ctx, "k")
	b, _ := io.ReadAll(rc)
	b[0] = 'X'
	rc2, _ := m.Get(ctx, "k")
	b2, _ := io.ReadAll(rc2)
	if string(b2) != "abc" {
		t.Fatalf("stored data mutated via Get: %q", b2)
	}
}

func TestMemoryConcurrent(t *testing.T) {
	ctx := context.Background()
	m := NewMemory()
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := "k/" + string(rune('a'+i%26))
			_ = m.Put(ctx, key, strings.NewReader("data"), 4, "")
			if rc, err := m.Get(ctx, key); err == nil {
				_, _ = io.ReadAll(rc)
			}
			_ = m.Delete(ctx, key)
		}(i)
	}
	wg.Wait()
}
