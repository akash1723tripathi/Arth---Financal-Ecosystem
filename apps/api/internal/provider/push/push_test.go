package push

import (
	"context"
	"errors"
	"testing"
)

func TestPush(t *testing.T) {
	for _, d := range []string{"none", ""} {
		s, err := New(d)
		if err != nil {
			t.Fatalf("New(%q): %v", d, err)
		}
		if err := s.Send(context.Background(), "tok", "t", "b"); !errors.Is(err, ErrDisabled) {
			t.Fatalf("Send err = %v", err)
		}
	}
	if _, err := New("fcm"); err == nil {
		t.Fatal("New(fcm) succeeded; provider not decided (D-OPEN-8)")
	}
}
