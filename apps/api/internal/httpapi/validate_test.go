package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"

	"arth/api/internal/httpapi/gen"
)

const (
	valAccountID  = "3f2c1a4e-8b7d-4c1e-9a2b-5d6e7f8a9b0c"
	valCategoryID = "0192f5c8-7a3b-7c4d-8e5f-6a7b8c9d0e1f" // UUIDv7: must be accepted too
)

// valHarness wraps a recording "next" handler with the validator.
type valHarness struct {
	h        http.Handler
	called   bool
	lastBody []byte
}

func newValHarness(t *testing.T) *valHarness {
	t.Helper()
	mw, err := NewValidator()
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}
	vh := &valHarness{}
	vh.h = mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		vh.called = true
		vh.lastBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	return vh
}

func (vh *valHarness) send(method, target, contentType, body string) *httptest.ResponseRecorder {
	vh.called, vh.lastBody = false, nil
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, rd)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	vh.h.ServeHTTP(rec, req)
	return rec
}

type valProblem struct {
	Status int    `json:"status"`
	Code   string `json:"code"`
	Detail string `json:"detail"`
	Errors []struct {
		Field   string `json:"field"`
		Message string `json:"message"`
	} `json:"errors"`
}

func readValProblem(t *testing.T, rec *httptest.ResponseRecorder) valProblem {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Fatalf("Content-Type = %q, want application/problem+json (body %s)", ct, rec.Body.String())
	}
	var p valProblem
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatalf("decode problem: %v (%s)", err, rec.Body.String())
	}
	return p
}

func expectValProblem(t *testing.T, vh *valHarness, rec *httptest.ResponseRecorder, status int, code string) valProblem {
	t.Helper()
	if vh.called {
		t.Fatalf("next handler was called; want %d %s", status, code)
	}
	if rec.Code != status {
		t.Fatalf("status = %d, want %d (body %s)", rec.Code, status, rec.Body.String())
	}
	p := readValProblem(t, rec)
	if p.Code != code {
		t.Fatalf("code = %q, want %q (body %s)", p.Code, code, rec.Body.String())
	}
	return p
}

func expectValPass(t *testing.T, vh *valHarness, rec *httptest.ResponseRecorder) {
	t.Helper()
	if !vh.called || rec.Code != http.StatusOK {
		t.Fatalf("want pass-through, got status %d called=%v body %s", rec.Code, vh.called, rec.Body.String())
	}
}

func validTxBody(amount string) string {
	return `{"account_id":"` + valAccountID + `","category_id":"` + valCategoryID +
		`","type":"EXPENSE","amount_minor":` + amount + `,"occurred_on":"2026-10-31","description":"Lunch"}`
}

func TestValidatorUnknownFieldIs400(t *testing.T) {
	vh := newValHarness(t)
	rec := vh.send(http.MethodPost, "/v1/accounts", "application/json", `{"name":"Wallet","type":"CASH","balance":5}`)
	p := expectValProblem(t, vh, rec, http.StatusBadRequest, "bad_request")
	if !strings.Contains(p.Detail, `"balance"`) {
		t.Errorf("detail = %q, want it to name the unknown field", p.Detail)
	}
}

func TestValidatorUnknownFieldWinsOverSchemaErrors(t *testing.T) {
	vh := newValHarness(t)
	rec := vh.send(http.MethodPost, "/v1/accounts", "application/json", `{"name":"","type":"CASH","user_id":"x"}`)
	expectValProblem(t, vh, rec, http.StatusBadRequest, "bad_request")
}

func TestValidatorMalformedJSONIs400(t *testing.T) {
	vh := newValHarness(t)
	rec := vh.send(http.MethodPost, "/v1/transactions", "application/json", `{"amount_minor": 12`)
	expectValProblem(t, vh, rec, http.StatusBadRequest, "bad_request")
}

func TestValidatorMissingBodyIs400(t *testing.T) {
	vh := newValHarness(t)
	rec := vh.send(http.MethodPost, "/v1/auth/login", "application/json", "")
	expectValProblem(t, vh, rec, http.StatusBadRequest, "bad_request")
}

func TestValidatorWrongContentTypeIs415(t *testing.T) {
	vh := newValHarness(t)
	rec := vh.send(http.MethodPost, "/v1/transactions", "text/plain", validTxBody("100"))
	expectValProblem(t, vh, rec, http.StatusUnsupportedMediaType, "unsupported_media_type")
}

func TestValidatorAmountZeroIs422(t *testing.T) {
	vh := newValHarness(t)
	rec := vh.send(http.MethodPost, "/v1/transactions", "application/json", validTxBody("0"))
	p := expectValProblem(t, vh, rec, http.StatusUnprocessableEntity, "validation_failed")
	if len(p.Errors) == 0 || p.Errors[0].Field != "amount_minor" {
		t.Fatalf("errors = %+v, want errors[0].field == amount_minor", p.Errors)
	}
	if p.Errors[0].Message != "must be at least 1" {
		t.Errorf("message = %q", p.Errors[0].Message)
	}
}

func TestValidatorAmountTooLargeIs422(t *testing.T) {
	vh := newValHarness(t)
	rec := vh.send(http.MethodPost, "/v1/transactions", "application/json", validTxBody("1000000000000000"))
	p := expectValProblem(t, vh, rec, http.StatusUnprocessableEntity, "validation_failed")
	if p.Errors[0].Field != "amount_minor" || p.Errors[0].Message != "must be at most 999999999999999" {
		t.Fatalf("errors = %+v", p.Errors)
	}
}

func TestValidatorCollectsSeveralFieldErrors(t *testing.T) {
	vh := newValHarness(t)
	body := `{"account_id":"nope","category_id":"` + valCategoryID + `","type":"GIFT","amount_minor":1.5,"occurred_on":"31-10-2026"}`
	rec := vh.send(http.MethodPost, "/v1/transactions", "application/json", body)
	p := expectValProblem(t, vh, rec, http.StatusUnprocessableEntity, "validation_failed")
	got := map[string]string{}
	for _, e := range p.Errors {
		got[e.Field] = e.Message
	}
	for _, f := range []string{"account_id", "type", "amount_minor", "occurred_on"} {
		if _, ok := got[f]; !ok {
			t.Errorf("missing error for %s; got %+v", f, p.Errors)
		}
	}
}

func TestValidatorMissingRequiredFieldIs422(t *testing.T) {
	vh := newValHarness(t)
	rec := vh.send(http.MethodPost, "/v1/auth/register", "application/json",
		`{"email":"a@example.com","password":"correct horse","name":"A"}`)
	p := expectValProblem(t, vh, rec, http.StatusUnprocessableEntity, "validation_failed")
	if len(p.Errors) != 1 || p.Errors[0].Field != "client" || p.Errors[0].Message != "is required" {
		t.Fatalf("errors = %+v", p.Errors)
	}
}

func TestValidatorEmailFormatIs422(t *testing.T) {
	vh := newValHarness(t)
	rec := vh.send(http.MethodPost, "/v1/auth/login", "application/json",
		`{"email":"not-an-email","password":"x","client":"MOBILE"}`)
	p := expectValProblem(t, vh, rec, http.StatusUnprocessableEntity, "validation_failed")
	if p.Errors[0].Field != "email" {
		t.Fatalf("errors = %+v", p.Errors)
	}
}

func TestValidatorNullInPatchIs422(t *testing.T) {
	vh := newValHarness(t)
	rec := vh.send(http.MethodPatch, "/v1/transactions/"+valAccountID, "application/json", `{"description":null}`)
	p := expectValProblem(t, vh, rec, http.StatusUnprocessableEntity, "validation_failed")
	if p.Errors[0].Field != "description" || p.Errors[0].Message != "must not be null" {
		t.Fatalf("errors = %+v", p.Errors)
	}
}

func TestValidatorBadUUIDPathParamIs422(t *testing.T) {
	vh := newValHarness(t)
	rec := vh.send(http.MethodGet, "/v1/accounts/not-a-uuid", "", "")
	p := expectValProblem(t, vh, rec, http.StatusUnprocessableEntity, "validation_failed")
	if len(p.Errors) != 1 || p.Errors[0].Field != "path.id" || p.Errors[0].Message != "must be a UUID" {
		t.Fatalf("errors = %+v", p.Errors)
	}
}

func TestValidatorLimitOutOfRangeIs422(t *testing.T) {
	vh := newValHarness(t)
	rec := vh.send(http.MethodGet, "/v1/transactions?limit=500", "", "")
	p := expectValProblem(t, vh, rec, http.StatusUnprocessableEntity, "validation_failed")
	if len(p.Errors) != 1 || p.Errors[0].Field != "query.limit" || p.Errors[0].Message != "must be at most 200" {
		t.Fatalf("errors = %+v", p.Errors)
	}

	rec = vh.send(http.MethodGet, "/v1/transactions?limit=abc", "", "")
	p = expectValProblem(t, vh, rec, http.StatusUnprocessableEntity, "validation_failed")
	if p.Errors[0].Field != "query.limit" || p.Errors[0].Message != "must be an integer" {
		t.Fatalf("errors = %+v", p.Errors)
	}
}

func TestValidatorMonthPatternIs422(t *testing.T) {
	vh := newValHarness(t)
	rec := vh.send(http.MethodGet, "/v1/budgets?month=2026-13", "", "")
	p := expectValProblem(t, vh, rec, http.StatusUnprocessableEntity, "validation_failed")
	if p.Errors[0].Field != "query.month" {
		t.Fatalf("errors = %+v", p.Errors)
	}
	expectValPass(t, vh, vh.send(http.MethodGet, "/v1/budgets?month=2026-10", "", ""))
	expectValPass(t, vh, vh.send(http.MethodGet, "/v1/budgets", "", ""))
}

func TestValidatorGoodBodyPassesAndBodyIsReadable(t *testing.T) {
	vh := newValHarness(t)
	body := validTxBody("12345")
	rec := vh.send(http.MethodPost, "/v1/transactions", "application/json", body)
	expectValPass(t, vh, rec)
	if string(vh.lastBody) != body {
		t.Fatalf("next handler saw body %q, want the original", vh.lastBody)
	}
}

func TestValidatorGoodRequestsPass(t *testing.T) {
	vh := newValHarness(t)
	cases := []struct{ method, target, ct, body string }{
		{http.MethodGet, "/healthz", "", ""},
		{http.MethodGet, "/v1/me", "", ""},
		{http.MethodPatch, "/v1/me", "application/json", `{"timezone":"Asia/Kolkata","email_weekly_tips":false}`},
		{http.MethodPatch, "/v1/transactions/" + valAccountID, "application/json", `{"description":""}`},
		{http.MethodGet, "/v1/transactions?type=EXPENSE&limit=200&from=2026-10-01&to=2026-10-31", "", ""},
		{http.MethodGet, "/v1/accounts/" + strings.ToUpper(valAccountID), "", ""},
		{http.MethodDelete, "/v1/accounts/" + valAccountID + "?cascade=true", "", ""},
		{http.MethodPost, "/v1/auth/logout-all", "", ""},
		{http.MethodPut, "/v1/budgets/categories/" + valCategoryID, "application/json", `{"amount_minor":500000}`},
		{http.MethodPost, "/v1/me/onboarding", "application/json", `{"currency":"INR","timezone":"Asia/Kolkata","opening_balance_minor":-2500}`},
	}
	for _, c := range cases {
		rec := vh.send(c.method, c.target, c.ct, c.body)
		if !vh.called || rec.Code != http.StatusOK {
			t.Errorf("%s %s: status %d called=%v body %s", c.method, c.target, rec.Code, vh.called, rec.Body.String())
		}
	}
}

func TestValidatorExportRouteIsNotTreatedAsID(t *testing.T) {
	vh := newValHarness(t)
	expectValPass(t, vh, vh.send(http.MethodGet, "/v1/transactions/export?from=2026-01-01&to=2026-10-31", "", ""))
	rec := vh.send(http.MethodGet, "/v1/transactions/export?from=2026-01-01", "", "")
	p := expectValProblem(t, vh, rec, http.StatusUnprocessableEntity, "validation_failed")
	if p.Errors[0].Field != "query.to" || p.Errors[0].Message != "is required" {
		t.Fatalf("errors = %+v", p.Errors)
	}
}

func TestValidatorUnknownRoutePassesThrough(t *testing.T) {
	vh := newValHarness(t)
	expectValPass(t, vh, vh.send(http.MethodGet, "/v1/nope", "", ""))
	expectValPass(t, vh, vh.send(http.MethodPut, "/v1/me", "application/json", `{}`)) // method not in spec
}

func TestValidatorMultipartUploadPasses(t *testing.T) {
	vh := newValHarness(t)
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, err := mw.CreateFormFile("file", "receipt.jpg")
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("\xff\xd8\xff\xe0fake-jpeg")
	if _, err := part.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	sent := buf.String()
	rec := vh.send(http.MethodPost, "/v1/ai/receipt-extractions", mw.FormDataContentType(), sent)
	expectValPass(t, vh, rec)
	if string(vh.lastBody) != sent {
		t.Fatal("multipart body was consumed or altered before the handler")
	}
}

func TestValidatorBodyOverLimitIs413(t *testing.T) {
	mw, err := NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	req := httptest.NewRequest(http.MethodPost, "/v1/transactions", strings.NewReader(validTxBody("100")))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	req.Body = http.MaxBytesReader(rec, req.Body, 10)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413 (body %s)", rec.Code, rec.Body.String())
	}
}

// TestOpenAPISpecIsValid lints api/openapi.yaml and checks the copy embedded in the generated code.
func TestOpenAPISpecIsValid(t *testing.T) {
	ctx := context.Background()
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromFile("../../../../api/openapi.yaml")
	if err != nil {
		t.Fatalf("load api/openapi.yaml: %v", err)
	}
	if !doc.IsOpenAPI31OrLater() {
		t.Errorf("openapi = %q, want 3.1.x", doc.OpenAPI)
	}
	if err := doc.Validate(ctx); err != nil {
		t.Fatalf("api/openapi.yaml is invalid: %v", err)
	}

	embedded, err := gen.GetSwagger()
	if err != nil {
		t.Fatalf("embedded spec: %v", err)
	}
	if err := embedded.Validate(ctx); err != nil {
		t.Fatalf("embedded spec is invalid: %v", err)
	}
	if got, want := len(embedded.Paths.Map()), len(doc.Paths.Map()); got != want {
		t.Errorf("embedded spec has %d paths, file has %d: run go generate ./internal/httpapi/gen/", got, want)
	}

	// Every operation has an operationId, and every request body object forbids unknown fields.
	for path, item := range doc.Paths.Map() {
		for method, op := range item.Operations() {
			if op.OperationID == "" {
				t.Errorf("%s %s has no operationId", method, path)
			}
			if op.RequestBody == nil {
				continue
			}
			if mt := op.RequestBody.Value.Content.Get("application/json"); mt != nil {
				s := mt.Schema.Value
				if s.AdditionalProperties.Has == nil || *s.AdditionalProperties.Has {
					t.Errorf("%s %s: request body must set additionalProperties: false", method, path)
				}
			}
		}
	}
}
