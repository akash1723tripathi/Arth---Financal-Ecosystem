package httpapi

// GET /v1/transactions/export: the caller's transactions in [from, to] as a CSV attachment.
// Amounts are decimal strings from money.Format (never floats), always positive; the type column carries
// the direction. Text cells that a spreadsheet would treat as a formula are neutralised (CSV injection).

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"arth/api/internal/apierr"
	"arth/api/internal/db"
	"arth/api/internal/httpapi/gen"
	"arth/api/internal/money"
)

// maxExportDays is the longest allowed export range, counting both ends (docs/API.md).
const maxExportDays = 366

var txCSVHeader = []string{"date", "type", "amount", "currency", "category", "account", "description", "input_method"}

func (s *Server) ExportTransactions(ctx context.Context, request gen.ExportTransactionsRequestObject) (gen.ExportTransactionsResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	params := request.Params
	if params.Format != nil && *params.Format != "csv" {
		return nil, apierr.Validation(apierr.FieldError{Field: "query.format", Message: "must be csv"})
	}
	from, to := fromDate(params.From), fromDate(params.To)
	if from.After(to) {
		return nil, apierr.Validation(apierr.FieldError{Field: "query.to", Message: "must be on or after from"})
	}
	if days := int(to.Sub(from)/(24*time.Hour)) + 1; days > maxExportDays {
		return nil, apierr.Validation(apierr.FieldError{Field: "query.to", Message: "the range may span at most 366 days"})
	}
	if params.AccountId != nil {
		if _, err := s.d.Q.GetAccount(ctx, db.GetAccountParams{ID: *params.AccountId, UserID: p.UserID}); err != nil {
			return nil, txNotFoundOr(err, "get account")
		}
	}

	rows, err := s.d.Q.ExportTransactions(ctx, db.ExportTransactionsParams{
		UserID: p.UserID, FromDate: from, ToDate: to, AccountID: params.AccountId,
	})
	if err != nil {
		return nil, fmt.Errorf("export transactions: %w", err)
	}

	var buf bytes.Buffer
	cw := csv.NewWriter(&buf)
	if err := cw.Write(txCSVHeader); err != nil {
		return nil, fmt.Errorf("write csv: %w", err)
	}
	// The query returns oldest first; the spec (api/openapi.yaml) promises newest first, like the list.
	for i := len(rows) - 1; i >= 0; i-- {
		r := rows[i]
		category := ""
		if r.CategoryName != nil {
			category = *r.CategoryName
		}
		description := ""
		if r.Description != nil {
			description = *r.Description
		}
		rec := []string{
			r.OccurredOn.Format(time.DateOnly),
			r.Type,
			money.Format(r.AmountMinor, r.Currency),
			r.Currency,
			txCSVSafe(category),
			txCSVSafe(r.AccountName),
			txCSVSafe(description),
			r.InputMethod,
		}
		if err := cw.Write(rec); err != nil {
			return nil, fmt.Errorf("write csv: %w", err)
		}
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		return nil, fmt.Errorf("write csv: %w", err)
	}

	name := fmt.Sprintf("arth-transactions-%s-%s.csv", from.Format(time.DateOnly), to.Format(time.DateOnly))
	return txCSVResponse{body: buf.Bytes(), filename: name}, nil
}

// txCSVSafe prefixes a cell that a spreadsheet would evaluate as a formula (OWASP "CSV injection") with a
// single quote, so it is shown as text.
func txCSVSafe(s string) string {
	if s == "" {
		return s
	}
	switch s[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + s
	}
	return s
}

// txCSVResponse writes the CSV with an explicit charset (the generated response type sets a bare
// "text/csv").
type txCSVResponse struct {
	body     []byte
	filename string
}

func (r txCSVResponse) VisitExportTransactionsResponse(w http.ResponseWriter) error {
	h := w.Header()
	h.Set("Content-Type", "text/csv; charset=utf-8")
	h.Set("Content-Disposition", `attachment; filename="`+r.filename+`"`)
	h.Set("Content-Length", strconv.Itoa(len(r.body)))
	h.Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, err := w.Write(r.body)
	return err
}
