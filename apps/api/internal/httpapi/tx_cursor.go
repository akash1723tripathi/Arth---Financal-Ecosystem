package httpapi

// Keyset cursor for GET /v1/transactions (docs/SCHEMA.md §6): the last row's (occurred_on, created_at, id),
// as base64url (no padding) of a small JSON object. It also carries a fingerprint of the filters it was
// issued for, so a cursor replayed with different filters is rejected (api/openapi.yaml: 400 bad_request).

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"

	"arth/api/internal/apierr"
	"arth/api/internal/httpapi/gen"
)

// maxTxCursorLen bounds the work done on hostile input (a real cursor is ~150 characters).
const maxTxCursorLen = 512

type txCursor struct {
	OccurredOn time.Time
	CreatedAt  time.Time
	ID         uuid.UUID
}

type txCursorWire struct {
	D string `json:"d"` // occurred_on, YYYY-MM-DD
	C string `json:"c"` // created_at, RFC 3339 with nanoseconds, UTC
	I string `json:"i"` // id
	F string `json:"f"` // filter fingerprint
}

func encodeTxCursor(c txCursor, filters string) string {
	w := txCursorWire{
		D: c.OccurredOn.Format(time.DateOnly),
		C: c.CreatedAt.UTC().Format(time.RFC3339Nano),
		I: c.ID.String(),
		F: filters,
	}
	b, _ := json.Marshal(w) // cannot fail: only strings
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeTxCursor(s, filters string) (txCursor, error) {
	bad := apierr.BadRequest("Invalid cursor.")
	if s == "" || len(s) > maxTxCursorLen {
		return txCursor{}, bad
	}
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return txCursor{}, bad
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var w txCursorWire
	if err := dec.Decode(&w); err != nil {
		return txCursor{}, bad
	}
	d, err := time.Parse(time.DateOnly, w.D)
	if err != nil {
		return txCursor{}, bad
	}
	c, err := time.Parse(time.RFC3339Nano, w.C)
	if err != nil {
		return txCursor{}, bad
	}
	id, err := uuid.Parse(w.I)
	if err != nil {
		return txCursor{}, bad
	}
	if w.F != filters {
		return txCursor{}, apierr.BadRequest("The cursor was issued for different filters.")
	}
	return txCursor{OccurredOn: d, CreatedAt: c.UTC(), ID: id}, nil
}

// txFilterFingerprint is a short stable hash of the list filters (not the limit: changing the page size
// mid-scroll is fine).
func txFilterFingerprint(p gen.ListTransactionsParams) string {
	var sb strings.Builder
	if p.Type != nil {
		sb.WriteString(string(*p.Type))
	}
	sb.WriteByte(0)
	if p.AccountId != nil {
		sb.WriteString(p.AccountId.String())
	}
	sb.WriteByte(0)
	if p.CategoryId != nil {
		sb.WriteString(p.CategoryId.String())
	}
	sb.WriteByte(0)
	if p.From != nil {
		sb.WriteString(p.From.Format(time.DateOnly))
	}
	sb.WriteByte(0)
	if p.To != nil {
		sb.WriteString(p.To.Format(time.DateOnly))
	}
	sb.WriteByte(0)
	if p.Q != nil {
		sb.WriteString(*p.Q)
	}
	sum := sha256.Sum256([]byte(sb.String()))
	return hex.EncodeToString(sum[:8])
}
