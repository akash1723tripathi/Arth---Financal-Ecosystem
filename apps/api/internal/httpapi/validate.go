package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers/legacy"

	"arth/api/internal/apierr"
	"arth/api/internal/httpapi/gen"
)

// uuidPattern accepts any RFC 9562 textual UUID (all versions, including v7), lower or upper case.
const uuidPattern = `^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`

var registerFormatsOnce sync.Once

// registerFormats turns on the string formats kin-openapi does not check by default. They are global
// (parameter validation ignores per-call format options). It also stops kin-openapi from embedding
// request values in SchemaError.Error() strings, so a logged validation error can never carry a password.
func registerFormats() {
	openapi3.DefineStringFormatValidator("uuid", openapi3.NewRegexpFormatValidator(uuidPattern))
	openapi3.DefineStringFormatValidator("email", openapi3.NewRegexpFormatValidator(openapi3.FormatOfStringForEmail))
	openapi3.SchemaErrorDetailsDisabled = true
}

// NewValidator returns middleware that validates every request against api/openapi.yaml (the spec embedded
// in the generated code) before it reaches the generated chi/strict handlers.
//
// Mapping (docs/API.md §2):
//   - malformed JSON, missing body, unknown property (additionalProperties: false) → 400 bad_request
//   - Content-Type not declared for the operation → 415 unsupported_media_type
//   - body over a http.MaxBytesReader limit set by an earlier middleware → 413 payload_too_large
//   - any other schema or parameter violation (type, range, length, pattern, enum, format, required,
//     invalid path UUID) → 422 validation_failed with errors[{field, message}]; body fields are reported by
//     their path (`amount_minor`, `items.0.name`), parameters as `<in>.<name>` (`query.limit`, `path.id`).
//
// Requests that match no operation pass through untouched (the router answers 404/405). Security is not
// checked here (the auth middleware does that) and responses are not validated. multipart/form-data
// bodies are not read here: the upload handler streams them and enforces size and type itself.
//
// The validator reads JSON bodies fully into memory, so a body-size limit (http.MaxBytesReader) must run
// before it.
func NewValidator() (func(http.Handler) http.Handler, error) {
	registerFormatsOnce.Do(registerFormats)

	doc, err := gen.GetSwagger()
	if err != nil {
		return nil, fmt.Errorf("load embedded openapi spec: %w", err)
	}
	// Match on the path only; the spec's servers list (http://localhost:8080) must not constrain the host.
	doc.Servers = nil

	router, err := legacy.NewRouter(doc) // also validates the document (as OpenAPI 3.1)
	if err != nil {
		return nil, fmt.Errorf("build openapi router: %w", err)
	}
	// For 3.1 documents openapi3filter switches to its JSON Schema 2020-12 adapter, which recompiles the
	// schema on every request, ignores our format validators and reports errors only as strings. Our request
	// schemas use nothing the built-in validator cannot handle (type arrays included), so after the 3.1
	// structural validation above we mark this private copy as 3.0 to keep the fast built-in validator and
	// its structured errors (field paths, failing keyword).
	doc.OpenAPI = "3.0.3"

	multipartOps := map[*openapi3.Operation]bool{}
	for _, item := range doc.Paths.Map() {
		for _, op := range item.Operations() {
			if op.RequestBody != nil && op.RequestBody.Value != nil &&
				op.RequestBody.Value.Content.Get("multipart/form-data") != nil {
				multipartOps[op] = true
			}
		}
	}

	base := openapi3filter.Options{
		MultiError:         true,
		AuthenticationFunc: openapi3filter.NoopAuthenticationFunc,
	}
	noBody := base
	noBody.ExcludeRequestBody = true

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			route, pathParams, err := router.FindRoute(r)
			if err != nil {
				// Unknown path or method: not ours to judge.
				next.ServeHTTP(w, r)
				return
			}
			opts := &base
			if multipartOps[route.Operation] {
				opts = &noBody
			}
			input := &openapi3filter.RequestValidationInput{
				Request:    r,
				PathParams: pathParams,
				Route:      route,
				Options:    opts,
			}
			if err := openapi3filter.ValidateRequest(r.Context(), input); err != nil {
				apierr.Write(w, r, mapValidationError(err))
				return
			}
			next.ServeHTTP(w, r)
		})
	}, nil
}

var unsupportedPropRe = regexp.MustCompile(`^property "(.*)" is unsupported$`)

// mapValidationError converts openapi3filter errors into an *apierr.Error.
// Precedence: 413, 415, 400, then 422 with every collected field error.
func mapValidationError(err error) *apierr.Error {
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		return apierr.PayloadTooLarge()
	}

	var (
		fields      []apierr.FieldError
		badRequest  string
		unsupported bool
	)
	for _, e := range flatten(err) {
		var re *openapi3filter.RequestError
		if !errors.As(e, &re) {
			// Not a request error (should not happen: security is skipped). Be conservative.
			if badRequest == "" {
				badRequest = "request does not match the API contract"
			}
			continue
		}
		switch {
		case re.Parameter != nil:
			fields = append(fields, parameterErrors(re)...)
		case re.RequestBody != nil:
			switch {
			case errors.Is(re.Err, openapi3filter.ErrInvalidRequired):
				badRequest = "request body is required"
			case strings.HasPrefix(re.Reason, "header Content-Type has unexpected value"):
				unsupported = true
			case isParseError(re.Err) || re.Reason == "reading failed":
				badRequest = "request body is not valid JSON"
			default:
				fs, unknown := bodySchemaErrors(re.Err)
				if unknown != "" && badRequest == "" {
					badRequest = unknown
				}
				fields = append(fields, fs...)
			}
		default:
			if badRequest == "" {
				badRequest = "request does not match the API contract"
			}
		}
	}

	switch {
	case unsupported:
		return apierr.UnsupportedMediaType().WithDetail("Content-Type must be application/json")
	case badRequest != "":
		return apierr.BadRequest(badRequest)
	case len(fields) > 0:
		return apierr.Validation(fields...)
	default:
		return apierr.BadRequest("request does not match the API contract")
	}
}

// flatten expands (nested) openapi3.MultiError values into their leaf errors.
func flatten(err error) []error {
	var me openapi3.MultiError
	if errors.As(err, &me) {
		// errors.As on MultiError may match a nested one; only expand when err itself is a MultiError.
		if direct, ok := err.(openapi3.MultiError); ok {
			var out []error
			for _, e := range direct {
				out = append(out, flatten(e)...)
			}
			return out
		}
	}
	return []error{err}
}

func isParseError(err error) bool {
	var pe *openapi3filter.ParseError
	return errors.As(err, &pe)
}

// parameterErrors maps one parameter's failure to field errors named "<in>.<name>".
func parameterErrors(re *openapi3filter.RequestError) []apierr.FieldError {
	field := re.Parameter.In + "." + re.Parameter.Name
	switch {
	case errors.Is(re.Err, openapi3filter.ErrInvalidRequired):
		return []apierr.FieldError{{Field: field, Message: "is required"}}
	case errors.Is(re.Err, openapi3filter.ErrInvalidEmptyValue):
		return []apierr.FieldError{{Field: field, Message: "must not be empty"}}
	case isParseError(re.Err):
		msg := "has an invalid value"
		if re.Parameter.Schema != nil && re.Parameter.Schema.Value != nil {
			msg = "must be " + typeDescription(re.Parameter.Schema.Value)
		}
		return []apierr.FieldError{{Field: field, Message: msg}}
	}
	var out []apierr.FieldError
	for _, se := range schemaErrors(re.Err) {
		out = append(out, apierr.FieldError{Field: field, Message: schemaMessage(se)})
	}
	if len(out) == 0 {
		out = append(out, apierr.FieldError{Field: field, Message: "has an invalid value"})
	}
	return out
}

// bodySchemaErrors maps body schema errors to field errors. An unknown property is reported separately
// (returned as a bad-request detail) because docs/API.md makes unknown fields a 400, not a 422.
func bodySchemaErrors(err error) (fields []apierr.FieldError, unknown string) {
	for _, se := range schemaErrors(err) {
		path := se.JSONPointer()
		if se.SchemaField == "properties" {
			if m := unsupportedPropRe.FindStringSubmatch(se.Reason); m != nil {
				if unknown == "" {
					name := m[1]
					if len(name) > 64 {
						name = name[:64]
					}
					unknown = fmt.Sprintf("unknown field %q", joinPath(append(path, name)))
				}
				continue
			}
		}
		field := joinPath(path)
		if field == "" {
			field = "body"
		}
		fields = append(fields, apierr.FieldError{Field: field, Message: schemaMessage(se)})
	}
	if len(fields) == 0 && unknown == "" {
		fields = append(fields, apierr.FieldError{Field: "body", Message: "does not match the schema"})
	}
	return fields, unknown
}

// schemaErrors collects the *openapi3.SchemaError leaves of err.
func schemaErrors(err error) []*openapi3.SchemaError {
	var out []*openapi3.SchemaError
	for _, e := range flatten(err) {
		var se *openapi3.SchemaError
		if errors.As(e, &se) {
			out = append(out, se)
		}
	}
	return out
}

func joinPath(p []string) string { return strings.Join(p, ".") }

// schemaMessage returns a short, value-free message for one failed JSON Schema keyword.
func schemaMessage(se *openapi3.SchemaError) string {
	s := se.Schema
	if s == nil {
		return "is invalid"
	}
	switch se.SchemaField {
	case "required":
		return "is required"
	case "nullable":
		return "must not be null"
	case "type":
		return "must be " + typeDescription(s)
	case "minimum":
		if s.Min != nil {
			return "must be at least " + num(*s.Min)
		}
	case "maximum":
		if s.Max != nil {
			return "must be at most " + num(*s.Max)
		}
	case "exclusiveMinimum":
		if s.Min != nil {
			return "must be greater than " + num(*s.Min)
		}
	case "exclusiveMaximum":
		if s.Max != nil {
			return "must be less than " + num(*s.Max)
		}
	case "minLength":
		if s.MinLength == 1 {
			return "must not be empty"
		}
		return fmt.Sprintf("must be at least %d characters", s.MinLength)
	case "maxLength":
		if s.MaxLength != nil {
			return fmt.Sprintf("must be at most %d characters", *s.MaxLength)
		}
	case "minItems":
		return fmt.Sprintf("must have at least %d items", s.MinItems)
	case "maxItems":
		if s.MaxItems != nil {
			return fmt.Sprintf("must have at most %d items", *s.MaxItems)
		}
	case "pattern":
		return "has an invalid format"
	case "format":
		switch s.Format {
		case "date":
			return "must be a date (YYYY-MM-DD)"
		case "date-time":
			return "must be an RFC 3339 timestamp"
		case "uuid":
			return "must be a UUID"
		case "email":
			return "must be an email address"
		case "int32", "int64":
			return "is out of range"
		default:
			return "must be a valid " + s.Format
		}
	case "enum":
		vals := make([]string, 0, len(s.Enum))
		for _, v := range s.Enum {
			vals = append(vals, fmt.Sprint(v))
		}
		return "must be one of: " + strings.Join(vals, ", ")
	}
	if se.Reason != "" {
		return se.Reason // kin-openapi reasons never include the input value
	}
	return "is invalid"
}

func typeDescription(s *openapi3.Schema) string {
	if s.Type == nil || len(*s.Type) == 0 {
		return "a valid value"
	}
	var parts []string
	for _, t := range *s.Type {
		switch t {
		case openapi3.TypeInteger:
			parts = append(parts, "an integer")
		case openapi3.TypeNumber:
			parts = append(parts, "a number")
		case openapi3.TypeBoolean:
			parts = append(parts, "a boolean")
		case openapi3.TypeString:
			parts = append(parts, "a string")
		case openapi3.TypeArray:
			parts = append(parts, "an array")
		case openapi3.TypeObject:
			parts = append(parts, "an object")
		case openapi3.TypeNull:
			parts = append(parts, "null")
		}
	}
	return strings.Join(parts, " or ")
}

// num formats a schema bound (stored by kin-openapi as float64) without exponent or trailing zeros.
// Only schema limits pass through here, never money values from requests.
func num(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }
