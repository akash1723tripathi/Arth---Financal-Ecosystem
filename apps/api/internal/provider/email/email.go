// Package email defines outbound email delivery as a small interface.
//
// No vendor is chosen: email delivery is OPEN (docs/DECISIONS.md D-OPEN-2). This package ships only the
// "log" (dev: logs metadata, sends nothing), "none" (disabled) and in-memory (tests) drivers. A real adapter
// goes in its own sub-package (e.g. provider/email/<vendor>) only after D-OPEN-2 is decided.
package email

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
)

// ErrDisabled means email delivery is turned off (EMAIL_DRIVER=none).
var ErrDisabled = errors.New("email: delivery disabled")

// ErrInvalidMessage means the message has no recipient or subject.
var ErrInvalidMessage = errors.New("email: invalid message")

// Message is one email. Tags are provider metadata (e.g. {"kind":"budget_alert"}); never put PII in them.
type Message struct {
	To      string
	Subject string
	Text    string
	HTML    string
	Tags    map[string]string
}

// Sender delivers a message and returns the provider's message id.
type Sender interface {
	Send(ctx context.Context, m Message) (providerID string, err error)
}

// Driver names accepted by New (EMAIL_DRIVER).
const (
	DriverLog  = "log"
	DriverNone = "none"
)

// New returns the sender for driver. logger is used by the log driver (nil = slog.Default()).
func New(driver string, logger *slog.Logger) (Sender, error) {
	switch driver {
	case DriverLog:
		return NewLog(logger), nil
	case DriverNone, "":
		return None{}, nil
	default:
		return nil, fmt.Errorf("email: unknown driver %q", driver)
	}
}

func validate(m Message) error {
	if !strings.Contains(m.To, "@") || strings.TrimSpace(m.Subject) == "" {
		return ErrInvalidMessage
	}
	return nil
}

// recipientDomain is the only part of an address that may be logged.
func recipientDomain(addr string) string {
	if i := strings.LastIndexByte(addr, '@'); i >= 0 {
		return strings.ToLower(addr[i+1:])
	}
	return ""
}

// Log is the dev driver: it logs the subject and the recipient's domain only (never the full address or the
// body) and returns a random id.
type Log struct{ log *slog.Logger }

// NewLog returns a Log sender.
func NewLog(logger *slog.Logger) *Log {
	if logger == nil {
		logger = slog.Default()
	}
	return &Log{log: logger}
}

func (l *Log) Send(ctx context.Context, m Message) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("email: %w", err)
	}
	if err := validate(m); err != nil {
		return "", err
	}
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("email: id: %w", err)
	}
	id := "log-" + hex.EncodeToString(b)
	l.log.InfoContext(ctx, "email (log driver, not sent)",
		"provider_id", id, "subject", m.Subject, "to_domain", recipientDomain(m.To), "kind", m.Tags["kind"])
	return id, nil
}

// None is the disabled driver: every Send returns ErrDisabled.
type None struct{}

func (None) Send(context.Context, Message) (string, error) { return "", ErrDisabled }

// Memory records messages for tests. Safe for concurrent use.
type Memory struct {
	mu   sync.Mutex
	sent []Message
	// Err, when set, is returned by Send (the message is not recorded).
	Err error
}

// NewMemory returns an empty recorder.
func NewMemory() *Memory { return &Memory{} }

func (m *Memory) Send(ctx context.Context, msg Message) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("email: %w", err)
	}
	if err := validate(msg); err != nil {
		return "", err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return "", m.Err
	}
	m.sent = append(m.sent, cloneMessage(msg))
	return "mem-" + strconv.Itoa(len(m.sent)), nil
}

// Sent returns a copy of the recorded messages in send order.
func (m *Memory) Sent() []Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Message, len(m.sent))
	for i, msg := range m.sent {
		out[i] = cloneMessage(msg)
	}
	return out
}

// Reset clears the recorded messages.
func (m *Memory) Reset() {
	m.mu.Lock()
	m.sent = nil
	m.mu.Unlock()
}

func cloneMessage(m Message) Message {
	if m.Tags != nil {
		tags := make(map[string]string, len(m.Tags))
		for k, v := range m.Tags {
			tags[k] = v
		}
		m.Tags = tags
	}
	return m
}
