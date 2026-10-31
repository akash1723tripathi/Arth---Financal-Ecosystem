package email

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
)

func TestNew(t *testing.T) {
	for _, d := range []string{"log", "none", ""} {
		if s, err := New(d, nil); err != nil || s == nil {
			t.Errorf("New(%q) = %v,%v", d, s, err)
		}
	}
	if _, err := New("resend", nil); err == nil {
		t.Error("New(resend) succeeded; provider not decided (D-OPEN-2)")
	}
}

func TestLogDriverRedacts(t *testing.T) {
	var buf bytes.Buffer
	s := NewLog(slog.New(slog.NewJSONHandler(&buf, nil)))
	msg := Message{
		To: "Jane.Doe@Example.com", Subject: "Your weekly tips",
		Text: "secret body text", HTML: "<p>secret html body</p>", Tags: map[string]string{"kind": "weekly_tips"},
	}
	id1, err := s.Send(context.Background(), msg)
	if err != nil {
		t.Fatal(err)
	}
	id2, _ := s.Send(context.Background(), msg)
	if !strings.HasPrefix(id1, "log-") || id1 == id2 {
		t.Fatalf("ids %q %q", id1, id2)
	}
	out := buf.String()
	for _, leak := range []string{"Jane.Doe", "jane.doe", "secret body", "secret html"} {
		if strings.Contains(out, leak) {
			t.Fatalf("log output leaks %q: %s", leak, out)
		}
	}
	for _, want := range []string{"Your weekly tips", "example.com", id1, "weekly_tips"} {
		if !strings.Contains(out, want) {
			t.Fatalf("log output missing %q: %s", want, out)
		}
	}
}

func TestValidation(t *testing.T) {
	senders := map[string]Sender{"log": NewLog(slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))), "memory": NewMemory()}
	bad := []Message{{To: "", Subject: "x"}, {To: "nobody", Subject: "x"}, {To: "a@b.c", Subject: "  "}}
	for name, s := range senders {
		for _, m := range bad {
			if _, err := s.Send(context.Background(), m); !errors.Is(err, ErrInvalidMessage) {
				t.Errorf("%s: Send(%+v) err = %v", name, m, err)
			}
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := s.Send(ctx, Message{To: "a@b.c", Subject: "x"}); !errors.Is(err, context.Canceled) {
			t.Errorf("%s: cancelled ctx err = %v", name, err)
		}
	}
}

func TestNoneDisabled(t *testing.T) {
	if _, err := (None{}).Send(context.Background(), Message{To: "a@b.c", Subject: "x"}); !errors.Is(err, ErrDisabled) {
		t.Fatalf("err = %v", err)
	}
}

func TestMemoryRecorder(t *testing.T) {
	m := NewMemory()
	tags := map[string]string{"kind": "budget_alert"}
	id, err := m.Send(context.Background(), Message{To: "a@b.c", Subject: "Budget 80%", Tags: tags})
	if err != nil || id != "mem-1" {
		t.Fatalf("Send = %q,%v", id, err)
	}
	tags["kind"] = "mutated"
	sent := m.Sent()
	if len(sent) != 1 || sent[0].Subject != "Budget 80%" || sent[0].Tags["kind"] != "budget_alert" {
		t.Fatalf("Sent = %+v", sent)
	}
	sent[0].Tags["kind"] = "mutated-again"
	if m.Sent()[0].Tags["kind"] != "budget_alert" {
		t.Fatal("Sent() exposes internal state")
	}

	m.Err = errors.New("boom")
	if _, err := m.Send(context.Background(), Message{To: "a@b.c", Subject: "x"}); err == nil || len(m.Sent()) != 1 {
		t.Fatal("Err not honoured")
	}
	m.Err = nil
	m.Reset()
	if len(m.Sent()) != 0 {
		t.Fatal("Reset did not clear")
	}

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = m.Send(context.Background(), Message{To: "a@b.c", Subject: "x"})
		}()
	}
	wg.Wait()
	if len(m.Sent()) != 50 {
		t.Fatalf("concurrent sends recorded %d", len(m.Sent()))
	}
}
