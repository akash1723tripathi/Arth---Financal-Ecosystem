package httpapi

// me slice: GET/PATCH /v1/me and POST /v1/me/onboarding (docs/API.md "Me", docs/DECISIONS.md D-19).

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	_ "time/tzdata" // timezone validation must not depend on the host's zoneinfo

	"github.com/jackc/pgx/v5"

	"arth/api/internal/apierr"
	"arth/api/internal/db"
	"arth/api/internal/httpapi/gen"
	"arth/api/internal/money"
)

func (s *Server) GetMe(ctx context.Context, request gen.GetMeRequestObject) (gen.GetMeResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	u, err := s.d.Q.GetUserByID(ctx, p.UserID)
	if err != nil {
		return nil, meNoRows(err, "get user")
	}
	return gen.GetMe200JSONResponse(toMe(u)), nil
}

func (s *Server) UpdateMe(ctx context.Context, request gen.UpdateMeRequestObject) (gen.UpdateMeResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	b := request.Body
	if b == nil {
		return nil, apierr.BadRequest("missing request body")
	}
	params := db.UpdateUserProfileParams{
		ID:                p.UserID,
		EmailBudgetAlerts: b.EmailBudgetAlerts,
		EmailWeeklyTips:   b.EmailWeeklyTips,
	}
	var fields []apierr.FieldError
	if b.Name != nil {
		// The spec requires minLength 1; a whitespace-only name is rejected rather than stored.
		name := strings.TrimSpace(*b.Name)
		if name == "" {
			fields = append(fields, apierr.FieldError{Field: "name", Message: "must not be blank"})
		}
		params.Name = &name
	}
	if b.Currency != nil {
		if !money.IsSupported(*b.Currency) {
			fields = append(fields, meUnsupportedCurrency("currency"))
		}
		params.Currency = b.Currency
	}
	if b.Timezone != nil {
		if fe, ok := meValidTimezone("timezone", *b.Timezone); !ok {
			fields = append(fields, fe)
		}
		params.Timezone = b.Timezone
	}
	if len(fields) > 0 {
		return nil, apierr.Validation(fields...)
	}

	var out db.User
	err = db.InTx(ctx, s.d.Pool, func(q *db.Queries) error {
		if params.Currency != nil {
			cur, err := q.GetUserByID(ctx, p.UserID)
			if err != nil {
				return meNoRows(err, "get user")
			}
			if cur.Currency == nil || *cur.Currency != *params.Currency {
				has, err := q.UserHasTransactions(ctx, p.UserID)
				if err != nil {
					return fmt.Errorf("user has transactions: %w", err)
				}
				if has {
					return apierr.Conflict("currency_locked", "the default currency cannot change once transactions exist")
				}
			}
		}
		u, err := q.UpdateUserProfile(ctx, params)
		if err != nil {
			return meNoRows(err, "update user profile")
		}
		out = u
		return nil
	})
	if err != nil {
		return nil, err
	}
	return gen.UpdateMe200JSONResponse(toMe(out)), nil
}

func (s *Server) CompleteOnboarding(ctx context.Context, request gen.CompleteOnboardingRequestObject) (gen.CompleteOnboardingResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	b := request.Body
	if b == nil {
		return nil, apierr.BadRequest("missing request body")
	}
	var fields []apierr.FieldError
	if !money.IsSupported(b.Currency) {
		fields = append(fields, meUnsupportedCurrency("currency"))
	}
	if fe, ok := meValidTimezone("timezone", b.Timezone); !ok {
		fields = append(fields, fe)
	}
	if len(fields) > 0 {
		return nil, apierr.Validation(fields...)
	}

	var out db.User
	err = db.InTx(ctx, s.d.Pool, func(q *db.Queries) error {
		u, err := q.CompleteOnboarding(ctx, db.CompleteOnboardingParams{Currency: b.Currency, Timezone: b.Timezone, ID: p.UserID})
		if errors.Is(err, pgx.ErrNoRows) {
			// The principal's user exists (the session check passed), so no row means already onboarded.
			return apierr.Conflict("already_onboarded", "onboarding was already completed")
		}
		if err != nil {
			return fmt.Errorf("complete onboarding: %w", err)
		}
		def, err := q.GetDefaultAccount(ctx, p.UserID)
		if err != nil {
			return fmt.Errorf("get default account: %w", err)
		}
		// Lock it so no transaction can be added between the count and the currency change.
		def, err = q.LockAccount(ctx, db.LockAccountParams{ID: def.ID, UserID: p.UserID})
		if err != nil {
			return fmt.Errorf("lock default account: %w", err)
		}
		if def.Currency != b.Currency {
			n, err := q.CountAccountTransactions(ctx, db.CountAccountTransactionsParams{AccountID: def.ID, UserID: p.UserID})
			if err != nil {
				return fmt.Errorf("count default account transactions: %w", err)
			}
			if n > 0 {
				return apierr.Conflict("currency_locked", "the default account already has transactions in "+def.Currency)
			}
		}
		if _, err := q.SetDefaultAccountOpening(ctx, db.SetDefaultAccountOpeningParams{
			Currency: b.Currency, OpeningBalanceMinor: b.OpeningBalanceMinor, UserID: p.UserID,
		}); err != nil {
			return fmt.Errorf("set default account opening: %w", err)
		}
		out = u
		return nil
	})
	if err != nil {
		return nil, err
	}
	return gen.CompleteOnboarding200JSONResponse(toMe(out)), nil
}

// meValidTimezone accepts an IANA zone name that the embedded tz database knows. "Local" (the server's own
// zone) is not a user timezone and is rejected.
func meValidTimezone(field, tz string) (apierr.FieldError, bool) {
	if tz == "" || tz == "Local" {
		return apierr.FieldError{Field: field, Message: "must be an IANA time zone name"}, false
	}
	if _, err := time.LoadLocation(tz); err != nil {
		return apierr.FieldError{Field: field, Message: "must be an IANA time zone name"}, false
	}
	return apierr.FieldError{}, true
}

func meUnsupportedCurrency(field string) apierr.FieldError {
	return apierr.FieldError{Field: field, Message: "is not a supported currency"}
}

// meNoRows maps pgx.ErrNoRows to 404 not_found and wraps any other error.
func meNoRows(err error, what string) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return apierr.NotFound()
	}
	return fmt.Errorf("%s: %w", what, err)
}
