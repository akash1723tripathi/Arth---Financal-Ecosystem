package httpapi

// ai slice: POST /v1/ai/receipt-extractions (docs/API.md "AI").
//
// The upload is one multipart `file` part (≤ 5 MiB; the router caps the whole body at UploadBodyLimit).
// The image goes to the ReceiptExtractor behind the provider interface (vendor OPEN, D-OPEN-1) and the
// result is validated field by field into a draft. Nothing is persisted, and neither the image nor the
// extracted text is ever logged.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"arth/api/internal/apierr"
	"arth/api/internal/db"
	"arth/api/internal/httpapi/gen"
	"arth/api/internal/money"
	"arth/api/internal/provider/ai"
)

const (
	// maxReceiptBytes is the largest receipt image accepted (docs/API.md: ≤ 5 MB).
	maxReceiptBytes = 5 << 20
	// aiTimeout bounds one provider call.
	aiTimeout = 30 * time.Second
	// maxDescriptionRunes matches transactions.description (CHECK length ≤ 200).
	maxDescriptionRunes = 200
)

func (s *Server) ExtractReceipt(ctx context.Context, request gen.ExtractReceiptRequestObject) (gen.ExtractReceiptResponseObject, error) {
	c, err := s.loadCaller(ctx)
	if err != nil {
		return nil, err
	}
	if request.Body == nil {
		return nil, apierr.BadRequest("missing multipart body")
	}
	img, declared, err := readReceiptPart(request)
	if err != nil {
		return nil, err
	}
	contentType, err := receiptContentType(declared, img)
	if err != nil {
		return nil, err
	}

	keys, err := s.systemCategoryKeys(ctx, c)
	if err != nil {
		return nil, err
	}
	hint := ai.ExtractHint{Currency: c.Currency, CategoryKeys: keys, Today: c.today(s.d.Now()).Format(time.DateOnly)}

	callCtx, cancel := context.WithTimeout(ctx, aiTimeout)
	defer cancel()
	ex, err := s.d.AI.ExtractReceipt(callCtx, img, contentType, hint)
	if err != nil {
		if errors.Is(err, ai.ErrInvalidInput) {
			return nil, apierr.UnsupportedMediaType()
		}
		// ErrUnavailable, timeouts and any other provider failure. Only a classification is kept as the
		// logged cause: a provider error string could echo extracted text.
		e := apierr.AIUnavailable().WithDetail("the receipt could not be read right now; enter it manually")
		e.Err = aiFailureCause(err)
		return nil, e
	}
	out, err := s.toExtraction(ctx, ex, c.Currency)
	if err != nil {
		return nil, err
	}
	return gen.ExtractReceipt200JSONResponse(out), nil
}

// readReceiptPart reads the single `file` part. Other form fields are ignored (drained).
func readReceiptPart(request gen.ExtractReceiptRequestObject) (img []byte, contentType string, err error) {
	found := false
	for {
		part, err := request.Body.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, "", uploadReadError(err)
		}
		if part.FormName() != "file" {
			if _, err := io.Copy(io.Discard, part); err != nil {
				return nil, "", uploadReadError(err)
			}
			continue
		}
		if found {
			return nil, "", apierr.Validation(apierr.FieldError{Field: "file", Message: "exactly one file part is allowed"})
		}
		found = true
		data, err := io.ReadAll(io.LimitReader(part, maxReceiptBytes+1))
		if err != nil {
			return nil, "", uploadReadError(err)
		}
		if len(data) > maxReceiptBytes {
			return nil, "", apierr.PayloadTooLarge().WithDetail("the image must be at most 5 MB")
		}
		img, contentType = data, part.Header.Get("Content-Type")
	}
	if !found {
		return nil, "", apierr.Validation(apierr.FieldError{Field: "file", Message: "is required"})
	}
	if len(img) == 0 {
		return nil, "", apierr.Validation(apierr.FieldError{Field: "file", Message: "must not be empty"})
	}
	return img, contentType, nil
}

// uploadReadError maps a multipart read failure: over the router's body cap → 413, else 400.
func uploadReadError(err error) error {
	var mbe *http.MaxBytesError
	if errors.As(err, &mbe) {
		return apierr.PayloadTooLarge().WithDetail("the image must be at most 5 MB")
	}
	return apierr.BadRequest("malformed multipart body")
}

// receiptContentType decides the image type from the bytes (sniffed) and the part's declared
// Content-Type. The sniffed type must be a supported image; a declared type, when it is specific, must
// agree with it (HEIC and HEIF count as one family). A missing or application/octet-stream declaration
// falls back to the sniffed type.
func receiptContentType(declared string, img []byte) (string, error) {
	sniffed := sniffImage(img)
	if !ai.SupportedImageType(sniffed) {
		return "", apierr.UnsupportedMediaType().WithDetail("upload a JPEG, PNG, WebP or HEIC image")
	}
	d := ""
	if declared != "" {
		mt, _, err := mime.ParseMediaType(declared)
		if err != nil {
			return "", apierr.UnsupportedMediaType().WithDetail("invalid Content-Type of the file part")
		}
		d = strings.ToLower(mt)
	}
	if d == "" || d == "application/octet-stream" {
		return sniffed, nil
	}
	if !ai.SupportedImageType(d) || imageFamily(d) != imageFamily(sniffed) {
		return "", apierr.UnsupportedMediaType().WithDetail("the file content does not match its Content-Type")
	}
	return d, nil
}

// sniffImage is http.DetectContentType plus HEIC/HEIF detection (ISO-BMFF `ftyp` box with an HEIF brand),
// which the standard library does not know.
func sniffImage(b []byte) string {
	if len(b) >= 12 && string(b[4:8]) == "ftyp" {
		switch string(b[8:12]) {
		case "heic", "heix", "hevc", "hevx", "heim", "heis":
			return "image/heic"
		case "mif1", "msf1", "heif":
			return "image/heif"
		}
	}
	mt, _, _ := mime.ParseMediaType(http.DetectContentType(b))
	return mt
}

func imageFamily(ct string) string {
	if ct == "image/heif" {
		return "image/heic"
	}
	return ct
}

// systemCategoryKeys lists the keys the model may choose from: every system category (both kinds).
func (s *Server) systemCategoryKeys(ctx context.Context, c caller) ([]string, error) {
	cats, err := s.d.Q.ListCategories(ctx, db.ListCategoriesParams{UserID: c.ID, Kind: nil})
	if err != nil {
		return nil, apierr.Internal(fmt.Errorf("list categories: %w", err))
	}
	keys := make([]string, 0, len(cats))
	for _, cat := range cats {
		if cat.UserID == nil && cat.Key != nil {
			keys = append(keys, *cat.Key)
		}
	}
	return keys, nil
}

// toExtraction validates every field of the provider's answer; anything invalid becomes null.
func (s *Server) toExtraction(ctx context.Context, ex ai.Extraction, userCurrency string) (gen.Extraction, error) {
	out := gen.Extraction{Currency: userCurrency}
	if ex.Currency != nil {
		if cur := strings.ToUpper(strings.TrimSpace(*ex.Currency)); money.IsSupported(cur) {
			out.Currency = cur
		}
	}
	if ex.Type != nil && (*ex.Type == ai.TypeExpense || *ex.Type == ai.TypeIncome) {
		t := gen.EntryType(*ex.Type)
		out.Type = &t
	}
	if ex.Amount != nil {
		if v, err := money.ParseAmount(strings.TrimSpace(*ex.Amount), out.Currency); err == nil {
			out.AmountMinor = &v
		}
	}
	if ex.CategoryKey != nil {
		key := strings.TrimSpace(*ex.CategoryKey)
		cat, err := s.d.Q.GetSystemCategoryByKey(ctx, key)
		switch {
		case errors.Is(err, pgx.ErrNoRows): // unknown key → null
		case err != nil:
			return gen.Extraction{}, apierr.Internal(fmt.Errorf("get system category: %w", err))
		case out.Type != nil && string(*out.Type) != cat.Kind: // contradicts the type → drop the category
		default:
			id := cat.ID
			out.CategoryId, out.CategoryKey = &id, &key
		}
	}
	if ex.Description != nil {
		if d := truncateRunes(strings.TrimSpace(*ex.Description), maxDescriptionRunes); d != "" {
			out.Description = &d
		}
	}
	if ex.OccurredOn != nil {
		if t, err := time.Parse(time.DateOnly, strings.TrimSpace(*ex.OccurredOn)); err == nil {
			d := openapi_types.Date{Time: t}
			out.OccurredOn = &d
		}
	}
	return out, nil // Transcript stays null (voice is post-MVP)
}

// aiFailureCause reduces a provider error to a loggable classification.
func aiFailureCause(err error) error {
	switch {
	case errors.Is(err, ai.ErrUnavailable):
		return fmt.Errorf("receipt extraction: %w", ai.ErrUnavailable)
	case errors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf("receipt extraction: %w", context.DeadlineExceeded)
	case errors.Is(err, context.Canceled):
		return fmt.Errorf("receipt extraction: %w", context.Canceled)
	default:
		return errors.New("receipt extraction: provider error")
	}
}

// truncateRunes drops invalid UTF-8 and keeps at most n runes.
func truncateRunes(s string, n int) string {
	s = strings.ToValidUTF8(s, "")
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}
