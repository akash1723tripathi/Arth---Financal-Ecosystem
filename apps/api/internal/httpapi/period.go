package httpapi

// Shared helpers of the budgets, reports and AI slices: the caller's currency and timezone, and
// user-local calendar months.

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"arth/api/internal/apierr"
)

// fallbackCurrency is the currency of a user who has not onboarded yet (users.currency is NULL). It matches
// the placeholder currency of the default "Cash" account that register creates (blueprint decision).
const fallbackCurrency = "INR"

// caller is the authenticated user with the settings that aggregates depend on.
type caller struct {
	ID       uuid.UUID
	Currency string         // users.currency, or fallbackCurrency before onboarding
	Loc      *time.Location // users.timezone (UTC if the stored name cannot be loaded)
}

// loadCaller reads the principal's user row. A missing user (deleted after the session check) is 401.
func (s *Server) loadCaller(ctx context.Context) (caller, error) {
	p, err := principal(ctx)
	if err != nil {
		return caller{}, err
	}
	u, err := s.d.Q.GetUserByID(ctx, p.UserID)
	if errors.Is(err, pgx.ErrNoRows) {
		return caller{}, apierr.Unauthorized()
	}
	if err != nil {
		return caller{}, apierr.Internal(fmt.Errorf("get user: %w", err))
	}
	c := caller{ID: u.ID, Currency: fallbackCurrency, Loc: time.UTC}
	if u.Currency != nil && *u.Currency != "" {
		c.Currency = *u.Currency
	}
	if loc, err := time.LoadLocation(u.Timezone); err == nil {
		c.Loc = loc
	} else if s.d.Log != nil {
		// Timezones are validated on write; this only happens if the tz database lost a zone.
		s.d.Log.WarnContext(ctx, "unknown user timezone, using UTC", "user_id", u.ID.String())
	}
	return c, nil
}

// today is the caller's local calendar date as a DATE value (midnight UTC).
func (c caller) today(now time.Time) time.Time {
	l := now.In(c.Loc)
	return time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, time.UTC)
}

// monthSpan is one calendar month as DATE values: [Start, End) with End = first day of the next month.
type monthSpan struct {
	Label string // YYYY-MM
	Start time.Time
	End   time.Time
}

func spanOf(year int, month time.Month) monthSpan {
	start := time.Date(year, month, 1, 0, 0, 0, 0, time.UTC)
	return monthSpan{Label: start.Format("2006-01"), Start: start, End: start.AddDate(0, 1, 0)}
}

// currentMonth is the month containing the caller's local today.
func (c caller) currentMonth(now time.Time) monthSpan {
	t := c.today(now)
	return spanOf(t.Year(), t.Month())
}

// resolveMonth parses an optional `month` query parameter (YYYY-MM; the validator already checked the
// pattern). nil = the caller's current local month.
func (c caller) resolveMonth(param *string, now time.Time) (monthSpan, error) {
	if param == nil || *param == "" {
		return c.currentMonth(now), nil
	}
	t, err := time.Parse("2006-01", *param)
	if err != nil {
		return monthSpan{}, apierr.Validation(apierr.FieldError{Field: "query.month", Message: "must be YYYY-MM"})
	}
	return spanOf(t.Year(), t.Month()), nil
}
