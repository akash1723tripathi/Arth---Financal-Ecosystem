package me_test

// Integration tests for GET/PATCH /v1/me and POST /v1/me/onboarding against a real Postgres.

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"arth/api/internal/db"
	"arth/api/internal/httpapi/gen"
	"arth/api/internal/testutil"
)

type problem struct {
	Code   string `json:"code"`
	Errors []struct {
		Field   string `json:"field"`
		Message string `json:"message"`
	} `json:"errors"`
}

func expectStatus(t *testing.T, r *testutil.Response, status int, code string) {
	t.Helper()
	if r.Status != status || (code != "" && r.Code() != code) {
		t.Fatalf("got %d %q, want %d %q; body=%s", r.Status, r.Code(), status, code, r.Body)
	}
}

func expectField(t *testing.T, r *testutil.Response, field string) {
	t.Helper()
	expectStatus(t, r, http.StatusUnprocessableEntity, "validation_failed")
	var p problem
	r.JSON(t, &p)
	for _, e := range p.Errors {
		if e.Field == field {
			return
		}
	}
	t.Fatalf("no error for field %q in %s", field, r.Body)
}

func getMe(t *testing.T, srv *testutil.Server, token string) gen.Me {
	t.Helper()
	r := srv.Do(t, http.MethodGet, "/v1/me", token, nil)
	expectStatus(t, r, http.StatusOK, "")
	var m gen.Me
	r.JSON(t, &m)
	return m
}

func account(t *testing.T, srv *testutil.Server, userID, id uuid.UUID) db.Account {
	t.Helper()
	a, err := srv.Deps.Q.GetAccount(context.Background(), db.GetAccountParams{ID: id, UserID: userID})
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	return a
}

// insertExpense inserts an EXPENSE transaction directly and moves the cached balance by its delta, so the
// balance invariant still holds.
func insertExpense(t *testing.T, srv *testutil.Server, userID, accountID uuid.UUID, amount int64, currency string) {
	t.Helper()
	ctx := context.Background()
	cat, err := srv.Deps.Q.GetSystemCategoryByKey(ctx, "groceries")
	if err != nil {
		t.Fatalf("category: %v", err)
	}
	if _, err := srv.Deps.Pool.Exec(ctx, `INSERT INTO transactions (user_id, account_id, category_id, type, amount_minor, currency, occurred_on)
		VALUES ($1, $2, $3, 'EXPENSE', $4, $5, CURRENT_DATE)`, userID, accountID, cat.ID, amount, currency); err != nil {
		t.Fatalf("insert transaction: %v", err)
	}
	n, err := srv.Deps.Q.ApplyBalanceDelta(ctx, db.ApplyBalanceDeltaParams{Delta: -amount, ID: accountID, UserID: userID})
	if err != nil || n != 1 {
		t.Fatalf("apply balance delta: rows=%d err=%v", n, err)
	}
}

func TestNoToken(t *testing.T) {
	srv := testutil.NewServer(t)
	cases := []struct{ method, path string }{
		{http.MethodGet, "/v1/me"},
		{http.MethodPatch, "/v1/me"},
		{http.MethodPost, "/v1/me/onboarding"},
	}
	for _, c := range cases {
		t.Run(c.method+" "+c.path, func(t *testing.T) {
			r := srv.Do(t, c.method, c.path, "", map[string]any{})
			expectStatus(t, r, http.StatusUnauthorized, "unauthorized")
		})
	}
}

func TestGetMe(t *testing.T) {
	srv := testutil.NewServer(t)
	u := srv.CreateUser(t)
	m := getMe(t, srv, u.AccessToken)
	if m.Id != u.ID || m.Email != u.Email {
		t.Fatalf("wrong user: %+v", m)
	}
	if !m.NeedsOnboarding || m.Currency != nil || m.AvatarUrl != nil {
		t.Fatalf("fresh user: needs_onboarding=%v currency=%v avatar=%v", m.NeedsOnboarding, m.Currency, m.AvatarUrl)
	}
	if m.Name == nil || *m.Name != "Test User" {
		t.Fatalf("name = %v", m.Name)
	}

	o := srv.CreateUser(t, testutil.UserOpts{Onboarded: true, Currency: "USD", Timezone: "America/New_York"})
	m = getMe(t, srv, o.AccessToken)
	if m.NeedsOnboarding || m.Currency == nil || *m.Currency != "USD" || m.Timezone != "America/New_York" {
		t.Fatalf("onboarded user: %+v", m)
	}
}

func TestUpdateMe(t *testing.T) {
	srv := testutil.NewServer(t)

	t.Run("happy path", func(t *testing.T) {
		u := srv.CreateUser(t)
		r := srv.Do(t, http.MethodPatch, "/v1/me", u.AccessToken, map[string]any{
			"name": "  Akash  ", "currency": "EUR", "timezone": "Europe/Berlin",
			"email_budget_alerts": false, "email_weekly_tips": false,
		})
		expectStatus(t, r, http.StatusOK, "")
		var m gen.Me
		r.JSON(t, &m)
		if m.Name == nil || *m.Name != "Akash" || m.Currency == nil || *m.Currency != "EUR" ||
			m.Timezone != "Europe/Berlin" || m.EmailBudgetAlerts || m.EmailWeeklyTips {
			t.Fatalf("update not applied: %s", r.Body)
		}
		if got := getMe(t, srv, u.AccessToken); got.Timezone != "Europe/Berlin" || *got.Currency != "EUR" {
			t.Fatalf("not persisted: %+v", got)
		}
		// Changing the default currency does not touch existing accounts.
		if a := account(t, srv, u.ID, u.DefaultAccountID); a.Currency != "INR" {
			t.Fatalf("default account currency changed to %s", a.Currency)
		}
	})

	t.Run("partial update keeps other fields", func(t *testing.T) {
		u := srv.CreateUser(t, testutil.UserOpts{Onboarded: true, Currency: "INR"})
		r := srv.Do(t, http.MethodPatch, "/v1/me", u.AccessToken, map[string]any{"email_weekly_tips": false})
		expectStatus(t, r, http.StatusOK, "")
		var m gen.Me
		r.JSON(t, &m)
		if m.EmailWeeklyTips || !m.EmailBudgetAlerts || *m.Currency != "INR" || m.Timezone != "Asia/Kolkata" || *m.Name != "Test User" {
			t.Fatalf("partial update changed other fields: %s", r.Body)
		}
	})

	t.Run("validation", func(t *testing.T) {
		u := srv.CreateUser(t)
		before := getMe(t, srv, u.AccessToken)
		cases := []struct {
			name  string
			body  any
			field string
		}{
			{"unsupported currency", map[string]any{"currency": "XYZ"}, "currency"},
			{"lower-case currency", map[string]any{"currency": "usd"}, "currency"},
			{"unknown timezone", map[string]any{"timezone": "Mars/Olympus_Mons"}, "timezone"},
			{"Local timezone", map[string]any{"timezone": "Local"}, "timezone"},
			{"empty timezone", map[string]any{"timezone": ""}, "timezone"},
			{"path traversal timezone", map[string]any{"timezone": "../../etc/passwd"}, "timezone"},
			{"name too long", map[string]any{"name": strings.Repeat("a", 101)}, "name"},
			{"empty name", map[string]any{"name": ""}, "name"},
			{"blank name", map[string]any{"name": "   "}, "name"},
			{"wrong type", map[string]any{"email_weekly_tips": "yes"}, "email_weekly_tips"},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				expectField(t, srv.Do(t, http.MethodPatch, "/v1/me", u.AccessToken, c.body), c.field)
			})
		}
		t.Run("unknown field", func(t *testing.T) {
			r := srv.Do(t, http.MethodPatch, "/v1/me", u.AccessToken, map[string]any{"email": "x@example.test"})
			expectStatus(t, r, http.StatusBadRequest, "bad_request")
		})
		t.Run("null value", func(t *testing.T) {
			r := srv.Do(t, http.MethodPatch, "/v1/me", u.AccessToken, `{"name":null}`)
			if r.Status != http.StatusBadRequest && r.Status != http.StatusUnprocessableEntity {
				t.Fatalf("null name: got %d %s", r.Status, r.Body)
			}
		})
		t.Run("malformed json", func(t *testing.T) {
			expectStatus(t, srv.Do(t, http.MethodPatch, "/v1/me", u.AccessToken, `{"name":`), http.StatusBadRequest, "bad_request")
		})
		after := getMe(t, srv, u.AccessToken)
		if *after.Name != *before.Name || after.Timezone != before.Timezone || after.Currency != nil {
			t.Fatalf("rejected requests changed the user: before=%+v after=%+v", before, after)
		}
	})

	t.Run("currency locked once transactions exist", func(t *testing.T) {
		u := srv.CreateUser(t, testutil.UserOpts{Onboarded: true, Currency: "INR"})
		insertExpense(t, srv, u.ID, u.DefaultAccountID, 4999, "INR")
		r := srv.Do(t, http.MethodPatch, "/v1/me", u.AccessToken, map[string]any{"currency": "USD", "name": "Changed"})
		expectStatus(t, r, http.StatusConflict, "currency_locked")
		m := getMe(t, srv, u.AccessToken)
		if *m.Currency != "INR" || *m.Name != "Test User" {
			t.Fatalf("409 must not apply any change: %+v", m)
		}
		// Re-sending the same currency is not a change.
		r = srv.Do(t, http.MethodPatch, "/v1/me", u.AccessToken, map[string]any{"currency": "INR", "timezone": "UTC"})
		expectStatus(t, r, http.StatusOK, "")
		srv.AssertBalance(t, u.DefaultAccountID, -4999)
	})
}

func TestCompleteOnboarding(t *testing.T) {
	srv := testutil.NewServer(t)

	t.Run("sets user and default account", func(t *testing.T) {
		u := srv.CreateUser(t)
		srv.AssertBalance(t, u.DefaultAccountID, 0)
		r := srv.Do(t, http.MethodPost, "/v1/me/onboarding", u.AccessToken, map[string]any{
			"currency": "USD", "timezone": "America/New_York", "opening_balance_minor": 150000,
		})
		expectStatus(t, r, http.StatusOK, "")
		var m gen.Me
		r.JSON(t, &m)
		if m.NeedsOnboarding || m.Currency == nil || *m.Currency != "USD" || m.Timezone != "America/New_York" {
			t.Fatalf("onboarding response: %s", r.Body)
		}
		a := account(t, srv, u.ID, u.DefaultAccountID)
		if a.Currency != "USD" || a.OpeningBalanceMinor != 150000 || !a.IsDefault {
			t.Fatalf("default account: %+v", a)
		}
		srv.AssertBalance(t, u.DefaultAccountID, 150000)

		t.Run("twice is 409 already_onboarded", func(t *testing.T) {
			r := srv.Do(t, http.MethodPost, "/v1/me/onboarding", u.AccessToken, map[string]any{
				"currency": "EUR", "timezone": "Europe/Paris", "opening_balance_minor": 1,
			})
			expectStatus(t, r, http.StatusConflict, "already_onboarded")
			m := getMe(t, srv, u.AccessToken)
			if *m.Currency != "USD" || m.Timezone != "America/New_York" {
				t.Fatalf("repeat onboarding changed the user: %+v", m)
			}
			if a := account(t, srv, u.ID, u.DefaultAccountID); a.Currency != "USD" {
				t.Fatalf("repeat onboarding changed the account: %+v", a)
			}
			srv.AssertBalance(t, u.DefaultAccountID, 150000)
		})
	})

	t.Run("negative opening and zero-exponent currency", func(t *testing.T) {
		u := srv.CreateUser(t)
		r := srv.Do(t, http.MethodPost, "/v1/me/onboarding", u.AccessToken, map[string]any{
			"currency": "JPY", "timezone": "Asia/Tokyo", "opening_balance_minor": -5000,
		})
		expectStatus(t, r, http.StatusOK, "")
		srv.AssertBalance(t, u.DefaultAccountID, -5000)
	})

	t.Run("validation", func(t *testing.T) {
		u := srv.CreateUser(t)
		ok := map[string]any{"currency": "INR", "timezone": "Asia/Kolkata", "opening_balance_minor": 0}
		with := func(k string, v any) map[string]any {
			m := map[string]any{}
			for kk, vv := range ok {
				m[kk] = vv
			}
			if v == nil {
				delete(m, k)
			} else {
				m[k] = v
			}
			return m
		}
		cases := []struct {
			name  string
			body  any
			field string
		}{
			{"unsupported currency", with("currency", "ABC"), "currency"},
			{"bad timezone", with("timezone", "Not/AZone"), "timezone"},
			{"Local timezone", with("timezone", "Local"), "timezone"},
			{"opening too large", with("opening_balance_minor", int64(1_000_000_000_000_000)), "opening_balance_minor"},
			{"opening not integer", with("opening_balance_minor", 1.5), "opening_balance_minor"},
			{"missing currency", with("currency", nil), "currency"},
			{"missing opening", with("opening_balance_minor", nil), "opening_balance_minor"},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				expectField(t, srv.Do(t, http.MethodPost, "/v1/me/onboarding", u.AccessToken, c.body), c.field)
			})
		}
		t.Run("unknown field", func(t *testing.T) {
			expectStatus(t, srv.Do(t, http.MethodPost, "/v1/me/onboarding", u.AccessToken, with("name", "x")), http.StatusBadRequest, "bad_request")
		})
		if m := getMe(t, srv, u.AccessToken); !m.NeedsOnboarding {
			t.Fatal("rejected onboarding requests onboarded the user")
		}
		srv.AssertBalance(t, u.DefaultAccountID, 0)
	})

	t.Run("currency locked when the default account has transactions", func(t *testing.T) {
		u := srv.CreateUser(t) // default account is INR (D-19 placeholder)
		insertExpense(t, srv, u.ID, u.DefaultAccountID, 2500, "INR")
		srv.AssertBalance(t, u.DefaultAccountID, -2500)

		r := srv.Do(t, http.MethodPost, "/v1/me/onboarding", u.AccessToken, map[string]any{
			"currency": "USD", "timezone": "UTC", "opening_balance_minor": 10000,
		})
		expectStatus(t, r, http.StatusConflict, "currency_locked")
		if m := getMe(t, srv, u.AccessToken); !m.NeedsOnboarding || m.Currency != nil {
			t.Fatalf("409 must roll back the user update: %+v", m)
		}
		if a := account(t, srv, u.ID, u.DefaultAccountID); a.Currency != "INR" || a.OpeningBalanceMinor != 0 {
			t.Fatalf("409 must roll back the account update: %+v", a)
		}
		srv.AssertBalance(t, u.DefaultAccountID, -2500)

		// Same currency as the account: allowed, and the opening balance moves the balance.
		r = srv.Do(t, http.MethodPost, "/v1/me/onboarding", u.AccessToken, map[string]any{
			"currency": "INR", "timezone": "Asia/Kolkata", "opening_balance_minor": 10000,
		})
		expectStatus(t, r, http.StatusOK, "")
		srv.AssertBalance(t, u.DefaultAccountID, 7500)
	})
}
