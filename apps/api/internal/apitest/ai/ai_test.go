package ai_test

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"testing"
	"time"

	"github.com/google/uuid"

	"arth/api/internal/config"
	"arth/api/internal/testutil"
)

// ---------------------------------------------------------------- helpers

type extraction struct {
	Type        *string    `json:"type"`
	AmountMinor *int64     `json:"amount_minor"`
	Currency    string     `json:"currency"`
	CategoryID  *uuid.UUID `json:"category_id"`
	CategoryKey *string    `json:"category_key"`
	Description *string    `json:"description"`
	OccurredOn  *string    `json:"occurred_on"`
	Transcript  *string    `json:"transcript"`
}

const path = "/v1/ai/receipt-extractions"

// jpeg returns n bytes that http.DetectContentType reports as image/jpeg.
func jpeg(n int) []byte {
	b := make([]byte, n)
	copy(b, []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0x00})
	return b
}

func png() []byte {
	b := make([]byte, 64)
	copy(b, "\x89PNG\x0D\x0A\x1A\x0A")
	return b
}

// heic returns an ISO-BMFF header with the "heic" brand.
func heic() []byte {
	b := make([]byte, 64)
	copy(b, "\x00\x00\x00\x18ftypheic\x00\x00\x00\x00mif1heic")
	return b
}

type part struct {
	name, filename, contentType string
	data                        []byte
}

// multipartBody builds a multipart/form-data body and returns it with its Content-Type.
func multipartBody(t *testing.T, parts ...part) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for _, p := range parts {
		h := textproto.MIMEHeader{}
		disp := `form-data; name="` + p.name + `"`
		if p.filename != "" {
			disp += `; filename="` + p.filename + `"`
		}
		h.Set("Content-Disposition", disp)
		if p.contentType != "" {
			h.Set("Content-Type", p.contentType)
		}
		pw, err := w.CreatePart(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pw.Write(p.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes(), w.FormDataContentType()
}

func upload(t *testing.T, srv *testutil.Server, token string, parts ...part) *testutil.Response {
	t.Helper()
	body, ct := multipartBody(t, parts...)
	return srv.DoWith(t, http.MethodPost, path, token, body, map[string]string{"Content-Type": ct})
}

func file(ct string, data []byte) part {
	return part{name: "file", filename: "receipt", contentType: ct, data: data}
}

func wantStatus(t *testing.T, r *testutil.Response, status int, code string) {
	t.Helper()
	if r.Status != status || (code != "" && r.Code() != code) {
		t.Fatalf("got %d %q, want %d %q: %s", r.Status, r.Code(), status, code, r.Body)
	}
}

func sysCategory(t *testing.T, srv *testutil.Server, key string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := srv.Deps.Pool.QueryRow(context.Background(),
		`SELECT id FROM categories WHERE key = $1 AND user_id IS NULL`, key).Scan(&id); err != nil {
		t.Fatalf("system category %q: %v", key, err)
	}
	return id
}

func txCount(t *testing.T, srv *testutil.Server, userID uuid.UUID) int {
	t.Helper()
	var n int
	if err := srv.Deps.Pool.QueryRow(context.Background(), `SELECT count(*) FROM transactions WHERE user_id = $1`, userID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// ---------------------------------------------------------------- tests

func TestReceiptExtractionFakeDriver(t *testing.T) {
	srv := testutil.NewServer(t)
	u := srv.CreateUser(t, testutil.UserOpts{Onboarded: true}) // INR, Asia/Kolkata
	food := sysCategory(t, srv, "food")

	r := upload(t, srv, u.AccessToken, part{name: "note", data: []byte("ignored")}, file("image/jpeg", jpeg(2048)))
	wantStatus(t, r, http.StatusOK, "")
	var ex extraction
	r.JSON(t, &ex)
	today := srv.Clock.Now().In(mustLoc(t, "Asia/Kolkata")).Format(time.DateOnly)
	if ex.AmountMinor == nil || *ex.AmountMinor != 24900 || ex.Currency != "INR" {
		t.Fatalf("amount: %+v", ex)
	}
	if ex.Type == nil || *ex.Type != "EXPENSE" {
		t.Fatalf("type: %+v", ex.Type)
	}
	if ex.CategoryID == nil || *ex.CategoryID != food || ex.CategoryKey == nil || *ex.CategoryKey != "food" {
		t.Fatalf("category: id=%v key=%v want %s", ex.CategoryID, ex.CategoryKey, food)
	}
	if ex.OccurredOn == nil || *ex.OccurredOn != today {
		t.Fatalf("occurred_on = %v, want user-local today %s", ex.OccurredOn, today)
	}
	if ex.Description == nil || *ex.Description == "" || ex.Transcript != nil {
		t.Fatalf("description/transcript: %+v", ex)
	}
	if n := txCount(t, srv, u.ID); n != 0 {
		t.Fatalf("extraction persisted %d transactions", n)
	}
	srv.AssertBalance(t, u.DefaultAccountID, 0)
}

func TestReceiptExtractionCurrencies(t *testing.T) {
	srv := testutil.NewServer(t)
	for _, tc := range []struct {
		name string
		opts testutil.UserOpts
		cur  string
		want int64
	}{
		{"before onboarding falls back to INR", testutil.UserOpts{}, "INR", 24900},
		{"USD", testutil.UserOpts{Onboarded: true, Currency: "USD"}, "USD", 24900},
		{"JPY has no minor unit", testutil.UserOpts{Onboarded: true, Currency: "JPY"}, "JPY", 249},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u := srv.CreateUser(t, tc.opts)
			r := upload(t, srv, u.AccessToken, file("image/png", png()))
			wantStatus(t, r, 200, "")
			var ex extraction
			r.JSON(t, &ex)
			if ex.Currency != tc.cur || ex.AmountMinor == nil || *ex.AmountMinor != tc.want {
				t.Fatalf("got currency=%s amount=%v, want %s %d", ex.Currency, ex.AmountMinor, tc.cur, tc.want)
			}
		})
	}
}

func TestReceiptImageTypes(t *testing.T) {
	srv := testutil.NewServer(t)
	u := srv.CreateUser(t, testutil.UserOpts{Onboarded: true})
	for _, tc := range []struct {
		name   string
		p      part
		status int
		code   string
	}{
		{"jpeg", file("image/jpeg", jpeg(100)), 200, ""},
		{"png", file("image/png", png()), 200, ""},
		{"heic", file("image/heic", heic()), 200, ""},
		{"heic declared as heif", file("image/heif", heic()), 200, ""},
		{"no declared type: sniffed", file("", jpeg(100)), 200, ""},
		{"octet-stream: sniffed", file("application/octet-stream", png()), 200, ""},
		{"declared type with params", file("image/jpeg; charset=binary", jpeg(100)), 200, ""},
		{"text/plain", file("text/plain", []byte("hello, this is not an image")), 415, "unsupported_media_type"},
		{"text declared as jpeg", file("image/jpeg", []byte("hello, this is not an image")), 415, "unsupported_media_type"},
		{"png declared as jpeg", file("image/jpeg", png()), 415, "unsupported_media_type"},
		{"gif", file("image/gif", []byte("GIF89a\x01\x00\x01\x00\x00\x00\x00")), 415, "unsupported_media_type"},
		{"pdf", file("application/pdf", []byte("%PDF-1.7\n%...")), 415, "unsupported_media_type"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wantStatus(t, upload(t, srv, u.AccessToken, tc.p), tc.status, tc.code)
		})
	}
}

func TestReceiptUploadErrors(t *testing.T) {
	srv := testutil.NewServer(t)
	u := srv.CreateUser(t, testutil.UserOpts{Onboarded: true})

	t.Run("over 5 MiB", func(t *testing.T) {
		wantStatus(t, upload(t, srv, u.AccessToken, file("image/jpeg", jpeg(5<<20+1))), 413, "payload_too_large")
	})
	t.Run("exactly 5 MiB", func(t *testing.T) {
		wantStatus(t, upload(t, srv, u.AccessToken, file("image/jpeg", jpeg(5<<20))), 200, "")
	})
	t.Run("body over the 6 MiB router cap", func(t *testing.T) {
		wantStatus(t, upload(t, srv, u.AccessToken, part{name: "padding", data: make([]byte, 7<<20)}, file("image/jpeg", jpeg(100))), 413, "payload_too_large")
	})
	t.Run("missing file part", func(t *testing.T) {
		r := upload(t, srv, u.AccessToken, part{name: "note", data: []byte("x")})
		wantStatus(t, r, 422, "validation_failed")
		var p struct {
			Errors []struct{ Field string } `json:"errors"`
		}
		r.JSON(t, &p)
		if len(p.Errors) != 1 || p.Errors[0].Field != "file" {
			t.Fatalf("errors: %s", r.Body)
		}
	})
	t.Run("no parts at all", func(t *testing.T) {
		wantStatus(t, upload(t, srv, u.AccessToken), 422, "validation_failed")
	})
	t.Run("empty file", func(t *testing.T) {
		wantStatus(t, upload(t, srv, u.AccessToken, file("image/jpeg", nil)), 422, "validation_failed")
	})
	t.Run("two file parts", func(t *testing.T) {
		wantStatus(t, upload(t, srv, u.AccessToken, file("image/jpeg", jpeg(10)), file("image/jpeg", jpeg(10))), 422, "validation_failed")
	})
	t.Run("json body", func(t *testing.T) {
		wantStatus(t, srv.Do(t, http.MethodPost, path, u.AccessToken, map[string]any{"file": "abc"}), 400, "bad_request") // generated multipart decoder (api.go requestError)
	})
	if n := txCount(t, srv, u.ID); n != 0 {
		t.Fatalf("persisted %d transactions", n)
	}
}

func TestReceiptNoneDriver(t *testing.T) {
	srv := testutil.NewServer(t, func(c *config.Config) { c.AIDriver = "none" })
	u := srv.CreateUser(t, testutil.UserOpts{Onboarded: true})
	r := upload(t, srv, u.AccessToken, file("image/jpeg", jpeg(100)))
	wantStatus(t, r, http.StatusBadGateway, "ai_unavailable")
	// Input errors are still reported before the provider is called.
	wantStatus(t, upload(t, srv, u.AccessToken, file("text/plain", []byte("plain text"))), 415, "unsupported_media_type")
}

func TestReceiptRequiresToken(t *testing.T) {
	srv := testutil.NewServer(t)
	wantStatus(t, upload(t, srv, "", file("image/jpeg", jpeg(100))), 401, "unauthorized")
	wantStatus(t, upload(t, srv, "bogus", file("image/jpeg", jpeg(100))), 401, "unauthorized")
}

func mustLoc(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}
