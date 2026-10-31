// Package apierr defines domain errors with stable codes and the single writer that turns them into
// RFC 9457 problem+json responses (docs/API.md §2).
package apierr

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"
)

// FieldError is one entry of the problem's "errors" array.
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// Error is a domain error that maps to an HTTP problem response.
type Error struct {
	Status     int
	Code       string
	Title      string
	Detail     string
	Fields     []FieldError
	Extra      map[string]any
	RetryAfter time.Duration
	Err        error // wrapped cause (logged, never sent to the client)
}

func (e *Error) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Detail, e.Err)
	}
	if e.Detail != "" {
		return e.Code + ": " + e.Detail
	}
	return e.Code
}

func (e *Error) Unwrap() error { return e.Err }

// Is lets errors.Is match on the code: errors.Is(err, apierr.NotFound()).
func (e *Error) Is(target error) bool {
	var t *Error
	if errors.As(target, &t) {
		return t.Code == e.Code
	}
	return false
}

// WithDetail returns a copy with a human-readable detail.
func (e *Error) WithDetail(detail string) *Error { c := *e; c.Detail = detail; return &c }

// WithExtra returns a copy with an extra top-level member (e.g. transaction_count).
func (e *Error) WithExtra(key string, v any) *Error {
	c := *e
	c.Extra = map[string]any{}
	for k, val := range e.Extra {
		c.Extra[k] = val
	}
	c.Extra[key] = v
	return &c
}

func newErr(status int, code, title string) *Error {
	return &Error{Status: status, Code: code, Title: title}
}

func NotFound() *Error { return newErr(http.StatusNotFound, "not_found", "Resource not found") }
func Unauthorized() *Error {
	return newErr(http.StatusUnauthorized, "unauthorized", "Authentication required")
}
func InvalidCredentials() *Error {
	return newErr(http.StatusUnauthorized, "invalid_credentials", "Invalid email or password")
}
func RefreshInvalid() *Error {
	return newErr(http.StatusUnauthorized, "refresh_invalid", "Refresh token is invalid or expired")
}
func EmailTaken() *Error {
	return newErr(http.StatusConflict, "email_taken", "An account with this email already exists")
}
func WeakPassword(detail string) *Error {
	e := newErr(http.StatusUnprocessableEntity, "weak_password", "Password does not meet the policy")
	e.Detail = detail
	return e
}
func Validation(fields ...FieldError) *Error {
	e := newErr(http.StatusUnprocessableEntity, "validation_failed", "Request validation failed")
	e.Fields = fields
	if len(fields) > 0 {
		e.Detail = fields[0].Field + " " + fields[0].Message
	}
	return e
}
func Conflict(code, detail string) *Error {
	e := newErr(http.StatusConflict, code, "Conflict")
	e.Detail = detail
	return e
}
func BadRequest(detail string) *Error {
	e := newErr(http.StatusBadRequest, "bad_request", "Bad request")
	e.Detail = detail
	return e
}
func RateLimited(retryAfter time.Duration) *Error {
	e := newErr(http.StatusTooManyRequests, "rate_limited", "Too many requests")
	e.RetryAfter = retryAfter
	return e
}
func PayloadTooLarge() *Error {
	return newErr(http.StatusRequestEntityTooLarge, "payload_too_large", "Payload too large")
}
func UnsupportedMediaType() *Error {
	return newErr(http.StatusUnsupportedMediaType, "unsupported_media_type", "Unsupported media type")
}
func AIUnavailable() *Error {
	return newErr(http.StatusBadGateway, "ai_unavailable", "The AI provider is unavailable")
}
func Internal(err error) *Error {
	e := newErr(http.StatusInternalServerError, "internal", "Internal server error")
	e.Err = err
	return e
}

// RequestIDFunc returns the request id for a request (set by the router's middleware).
// It is a variable so this package does not import the router.
var RequestIDFunc = func(r *http.Request) string { return r.Header.Get("X-Request-Id") }

// Logger used for 5xx errors. Replaced at boot.
var Logger = slog.Default()

// Write writes err as application/problem+json. Errors that are not *Error become 500 internal.
func Write(w http.ResponseWriter, r *http.Request, err error) {
	var e *Error
	if !errors.As(err, &e) {
		e = Internal(err)
	}
	reqID := ""
	if r != nil {
		reqID = RequestIDFunc(r)
	}
	if e.Status >= 500 {
		Logger.Error("request failed", "request_id", reqID, "code", e.Code, "err", err)
	}
	body := map[string]any{
		"type":   "/problems/" + e.Code,
		"title":  e.Title,
		"status": e.Status,
		"code":   e.Code,
	}
	if e.Detail != "" && e.Status < 500 {
		body["detail"] = e.Detail
	}
	if len(e.Fields) > 0 {
		body["errors"] = e.Fields
	}
	if reqID != "" {
		body["request_id"] = reqID
	}
	for k, v := range e.Extra {
		if _, taken := body[k]; !taken {
			body[k] = v
		}
	}
	if e.RetryAfter > 0 {
		secs := int(e.RetryAfter.Round(time.Second) / time.Second)
		if secs < 1 {
			secs = 1
		}
		w.Header().Set("Retry-After", strconv.Itoa(secs))
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(e.Status)
	_ = json.NewEncoder(w).Encode(body)
}
