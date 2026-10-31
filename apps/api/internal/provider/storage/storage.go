// Package storage defines object storage (receipt images, avatars) as a small interface.
//
// No vendor is chosen: object storage is OPEN (docs/DECISIONS.md D-OPEN-7; "store nothing in MVP" is one of
// the options). This package ships only the "none" (disabled) and "memory" (dev/tests) drivers. A real
// adapter goes in its own sub-package (e.g. provider/storage/<vendor>) only after D-OPEN-7 is decided.
package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
)

var (
	// ErrDisabled means storage is turned off (STORAGE_DRIVER=none).
	ErrDisabled = errors.New("storage: disabled")
	// ErrNotFound means no object exists under the key.
	ErrNotFound = errors.New("storage: object not found")
	// ErrInvalidKey means the key is empty, too long, or not a clean relative path.
	ErrInvalidKey = errors.New("storage: invalid key")
	// ErrSizeMismatch means the reader did not yield exactly size bytes.
	ErrSizeMismatch = errors.New("storage: size mismatch")
)

// MaxKeyLen bounds object keys.
const MaxKeyLen = 512

// BlobStore stores opaque objects by key. Keys are server-generated (e.g. "receipts/<user_id>/<uuid>"),
// never taken from clients.
type BlobStore interface {
	Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
}

// Driver names accepted by New (STORAGE_DRIVER).
const (
	DriverNone   = "none"
	DriverMemory = "memory"
)

// New returns the store for driver.
func New(driver string) (BlobStore, error) {
	switch driver {
	case DriverNone, "":
		return None{}, nil
	case DriverMemory:
		return NewMemory(), nil
	default:
		return nil, fmt.Errorf("storage: unknown driver %q", driver)
	}
}

// ValidKey reports whether key is a clean relative path: non-empty, ≤ MaxKeyLen, no leading '/', no empty,
// "." or ".." segments, no backslashes or control characters.
func ValidKey(key string) bool {
	if key == "" || len(key) > MaxKeyLen || strings.ContainsRune(key, '\\') {
		return false
	}
	for _, r := range key {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	for _, seg := range strings.Split(key, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	return true
}

// None is the disabled driver.
type None struct{}

func (None) Put(context.Context, string, io.Reader, int64, string) error { return ErrDisabled }
func (None) Get(context.Context, string) (io.ReadCloser, error)          { return nil, ErrDisabled }
func (None) Delete(context.Context, string) error                        { return ErrDisabled }

// Memory keeps objects in a map. For dev and tests only (lost on restart). Safe for concurrent use.
type Memory struct {
	mu      sync.RWMutex
	objects map[string]object
}

type object struct {
	data        []byte
	contentType string
}

// NewMemory returns an empty in-memory store.
func NewMemory() *Memory { return &Memory{objects: map[string]object{}} }

// Put reads exactly size bytes from r (size must be ≥ 0) and stores them under key, replacing any object.
func (m *Memory) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("storage: %w", err)
	}
	if !ValidKey(key) {
		return ErrInvalidKey
	}
	if size < 0 {
		return ErrSizeMismatch
	}
	// Read one byte past size to detect a reader that is longer than declared.
	data, err := io.ReadAll(io.LimitReader(r, size+1))
	if err != nil {
		return fmt.Errorf("storage: read: %w", err)
	}
	if int64(len(data)) != size {
		return ErrSizeMismatch
	}
	m.mu.Lock()
	m.objects[key] = object{data: data, contentType: contentType}
	m.mu.Unlock()
	return nil
}

// Get returns a reader over a copy of the object.
func (m *Memory) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("storage: %w", err)
	}
	if !ValidKey(key) {
		return nil, ErrInvalidKey
	}
	m.mu.RLock()
	o, ok := m.objects[key]
	m.mu.RUnlock()
	if !ok {
		return nil, ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(bytes.Clone(o.data))), nil
}

// Delete removes the object. Deleting a missing key is not an error (idempotent, like S3).
func (m *Memory) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("storage: %w", err)
	}
	if !ValidKey(key) {
		return ErrInvalidKey
	}
	m.mu.Lock()
	delete(m.objects, key)
	m.mu.Unlock()
	return nil
}

// ContentType returns the stored content type (tests/dev only).
func (m *Memory) ContentType(key string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	o, ok := m.objects[key]
	return o.contentType, ok
}

// Len is the number of stored objects.
func (m *Memory) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.objects)
}
