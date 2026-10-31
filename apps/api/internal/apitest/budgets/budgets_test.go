package budgets_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"arth/api/internal/testutil"
)

// ---------------------------------------------------------------- helpers

type progress struct {
	ID             uuid.UUID  `json:"id"`
	CategoryID     *uuid.UUID `json:"category_id"`
	AmountMinor    int64      `json:"amount_minor"`
	Currency       string     `json:"currency"`
	Month          string     `json:"month"`
	SpentMinor     int64      `json:"spent_minor"`
	RemainingMinor int64      `json:"remaining_minor"`
	PercentUsed    int32      `json:"percent_used"`
}

type progressList struct {
	Data []progress `json:"data"`
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

func customCategory(t *testing.T, srv *testutil.Server, userID uuid.UUID, name, kind string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := srv.Deps.Pool.QueryRow(context.Background(),
		`INSERT INTO categories (user_id, name, kind) VALUES ($1, $2, $3) RETURNING id`, userID, name, kind).Scan(&id); err != nil {
		t.Fatalf("custom category: %v", err)
	}
	return id
}

// insertTx writes a transaction directly (the transactions endpoint is another slice) and applies its
// delta to the cached balance, keeping the balance invariant.
func insertTx(t *testing.T, srv *testutil.Server, userID, accountID uuid.UUID, categoryID *uuid.UUID, typ string, amount int64, currency, on string) {
	t.Helper()
	ctx := context.Background()
	tx, err := srv.Deps.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	var transferID *uuid.UUID
	if typ == "TRANSFER_IN" || typ == "TRANSFER_OUT" {
		id := uuid.New()
		transferID = &id
	}
	var delta int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO transactions (user_id, account_id, category_id, type, amount_minor, currency, occurred_on, transfer_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7::date, $8) RETURNING delta_minor`,
		userID, accountID, categoryID, typ, amount, currency, on, transferID).Scan(&delta); err != nil {
		t.Fatalf("insert transaction: %v", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE accounts SET balance_minor = balance_minor + $1 WHERE id = $2 AND user_id = $3`,
		delta, accountID, userID); err != nil {
		t.Fatalf("apply delta: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func wantStatus(t *testing.T, r *testutil.Response, status int, code string) {
	t.Helper()
	if r.Status != status || (code != "" && r.Code() != code) {
		t.Fatalf("got %d %q, want %d %q: %s", r.Status, r.Code(), status, code, r.Body)
	}
}

func list(t *testing.T, srv *testutil.Server, token, month string) []progress {
	t.Helper()
	path := "/v1/budgets"
	if month != "" {
		path += "?month=" + month
	}
	r := srv.Do(t, http.MethodGet, path, token, nil)
	wantStatus(t, r, http.StatusOK, "")
	var l progressList
	r.JSON(t, &l)
	return l.Data
}

// ---------------------------------------------------------------- tests

func TestOverallBudgetUpsertUpdatesSameRow(t *testing.T) {
	srv := testutil.NewServer(t)
	a := srv.CreateUser(t, testutil.UserOpts{Onboarded: true})

	r := srv.Do(t, http.MethodPut, "/v1/budgets/overall", a.AccessToken, map[string]any{"amount_minor": 50000})
	wantStatus(t, r, http.StatusOK, "")
	var p1 progress
	r.JSON(t, &p1)
	if p1.AmountMinor != 50000 || p1.Currency != "INR" || p1.CategoryID != nil || p1.SpentMinor != 0 || p1.RemainingMinor != 50000 || p1.PercentUsed != 0 {
		t.Fatalf("first put: %+v", p1)
	}
	if want := srv.Clock.Now().In(mustLoc(t, "Asia/Kolkata")).Format("2006-01"); p1.Month != want {
		t.Fatalf("month = %q, want current local month %q", p1.Month, want)
	}

	r = srv.Do(t, http.MethodPut, "/v1/budgets/overall", a.AccessToken, map[string]any{"amount_minor": 70000})
	wantStatus(t, r, http.StatusOK, "")
	var p2 progress
	r.JSON(t, &p2)
	if p2.ID != p1.ID || p2.AmountMinor != 70000 {
		t.Fatalf("second put should update the same row: first=%+v second=%+v", p1, p2)
	}
	var n int
	if err := srv.Deps.Pool.QueryRow(context.Background(), `SELECT count(*) FROM budgets WHERE user_id = $1`, a.ID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("budget rows = %d, want 1", n)
	}
}

func TestBudgetValidation(t *testing.T) {
	srv := testutil.NewServer(t)
	a := srv.CreateUser(t, testutil.UserOpts{Onboarded: true})
	food := sysCategory(t, srv, "food")
	cases := []struct {
		name, method, path string
		body               any
		status             int
		code               string
	}{
		{"zero amount", http.MethodPut, "/v1/budgets/overall", map[string]any{"amount_minor": 0}, 422, "validation_failed"},
		{"negative amount", http.MethodPut, "/v1/budgets/overall", map[string]any{"amount_minor": -5}, 422, "validation_failed"},
		{"too large", http.MethodPut, "/v1/budgets/overall", map[string]any{"amount_minor": int64(1_000_000_000_000_000)}, 422, "validation_failed"},
		{"decimal amount", http.MethodPut, "/v1/budgets/overall", `{"amount_minor": 12.5}`, 422, "validation_failed"},
		{"missing amount", http.MethodPut, "/v1/budgets/overall", map[string]any{}, 422, "validation_failed"},
		{"unknown field", http.MethodPut, "/v1/budgets/overall", map[string]any{"amount_minor": 5, "user_id": a.ID}, 400, "bad_request"},
		{"malformed json", http.MethodPut, "/v1/budgets/overall", `{"amount_minor":`, 400, "bad_request"},
		{"category zero amount", http.MethodPut, "/v1/budgets/categories/" + food.String(), map[string]any{"amount_minor": 0}, 422, "validation_failed"},
		{"category bad uuid", http.MethodPut, "/v1/budgets/categories/not-a-uuid", map[string]any{"amount_minor": 5}, 422, "validation_failed"},
		{"bad month", http.MethodGet, "/v1/budgets?month=2026-13", nil, 422, "validation_failed"},
		{"bad month format", http.MethodGet, "/v1/budgets?month=2026-3", nil, 422, "validation_failed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := srv.Do(t, tc.method, tc.path, a.AccessToken, tc.body)
			wantStatus(t, r, tc.status, tc.code)
		})
	}
	if got := list(t, srv, a.AccessToken, ""); len(got) != 0 {
		t.Fatalf("invalid requests created budgets: %+v", got)
	}
}

func TestBudgetProgress(t *testing.T) {
	srv := testutil.NewServer(t)
	a := srv.CreateUser(t, testutil.UserOpts{Onboarded: true})
	food, transport, salary := sysCategory(t, srv, "food"), sysCategory(t, srv, "transport"), sysCategory(t, srv, "salary")
	acct := a.DefaultAccountID

	insertTx(t, srv, a.ID, acct, &food, "EXPENSE", 30000, "INR", "2026-03-01")      // first day: counted
	insertTx(t, srv, a.ID, acct, &transport, "EXPENSE", 25000, "INR", "2026-03-31") // last day: counted
	insertTx(t, srv, a.ID, acct, &salary, "INCOME", 100000, "INR", "2026-03-15")    // income: ignored
	insertTx(t, srv, a.ID, acct, nil, "TRANSFER_OUT", 9000, "INR", "2026-03-10")    // transfer: ignored
	insertTx(t, srv, a.ID, acct, &food, "EXPENSE", 7000, "INR", "2026-02-28")       // previous month
	insertTx(t, srv, a.ID, acct, &food, "EXPENSE", 11000, "INR", "2026-04-01")      // next month
	srv.AssertBalance(t, acct, -30000-25000+100000-9000-7000-11000)

	wantStatus(t, srv.Do(t, http.MethodPut, "/v1/budgets/overall", a.AccessToken, map[string]any{"amount_minor": 50000}), 200, "")
	wantStatus(t, srv.Do(t, http.MethodPut, "/v1/budgets/categories/"+food.String(), a.AccessToken, map[string]any{"amount_minor": 20000}), 200, "")
	wantStatus(t, srv.Do(t, http.MethodPut, "/v1/budgets/categories/"+transport.String(), a.AccessToken, map[string]any{"amount_minor": 75000}), 200, "")

	got := list(t, srv, a.AccessToken, "2026-03")
	if len(got) != 3 {
		t.Fatalf("got %d budgets, want 3: %+v", len(got), got)
	}
	overall := got[0]
	if overall.CategoryID != nil {
		t.Fatalf("overall budget must come first: %+v", got)
	}
	if overall.Month != "2026-03" || overall.SpentMinor != 55000 || overall.RemainingMinor != -5000 || overall.PercentUsed != 110 {
		t.Fatalf("overall progress (over budget): %+v", overall)
	}
	byCat := map[uuid.UUID]progress{}
	for _, p := range got[1:] {
		if p.CategoryID == nil {
			t.Fatalf("second overall budget: %+v", got)
		}
		byCat[*p.CategoryID] = p
	}
	if p := byCat[food]; p.SpentMinor != 30000 || p.RemainingMinor != -10000 || p.PercentUsed != 150 {
		t.Fatalf("food progress: %+v", p)
	}
	if p := byCat[transport]; p.SpentMinor != 25000 || p.RemainingMinor != 50000 || p.PercentUsed != 33 { // floor(33.33)
		t.Fatalf("transport progress (floor): %+v", p)
	}

	// February only sees the 28th; an empty month is zero.
	feb := list(t, srv, a.AccessToken, "2026-02")
	if feb[0].SpentMinor != 7000 || feb[0].PercentUsed != 14 || feb[0].Month != "2026-02" {
		t.Fatalf("february: %+v", feb[0])
	}
	jan := list(t, srv, a.AccessToken, "2026-01")
	if jan[0].SpentMinor != 0 || jan[0].RemainingMinor != 50000 || jan[0].PercentUsed != 0 {
		t.Fatalf("january: %+v", jan[0])
	}
}

func mustLoc(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

// The default month is the user's local month: at <next year>-04-01 03:00 UTC it is still March in Los
// Angeles but already April in Kolkata. The clock only moves forward (sessions expire against the DB's now()).
func TestDefaultMonthFollowsUserTimezone(t *testing.T) {
	srv := testutil.NewServer(t)
	year := srv.Clock.Now().Year() + 1
	at := time.Date(year, 4, 1, 3, 0, 0, 0, time.UTC)
	srv.Clock.Advance(at.Sub(srv.Clock.Now()))
	march, april := fmt.Sprintf("%d-03", year), fmt.Sprintf("%d-04", year)

	la := srv.CreateUser(t, testutil.UserOpts{Onboarded: true, Timezone: "America/Los_Angeles"})
	in := srv.CreateUser(t, testutil.UserOpts{Onboarded: true, Timezone: "Asia/Kolkata"})
	food := sysCategory(t, srv, "food")
	for _, u := range []testutil.User{la, in} {
		insertTx(t, srv, u.ID, u.DefaultAccountID, &food, "EXPENSE", 100, "INR", march+"-31")
		insertTx(t, srv, u.ID, u.DefaultAccountID, &food, "EXPENSE", 1000, "INR", april+"-01")
		srv.AssertBalance(t, u.DefaultAccountID, -1100)
	}

	for _, tc := range []struct {
		name  string
		u     testutil.User
		month string
		spent int64
	}{
		{"los angeles", la, march, 100},
		{"kolkata", in, april, 1000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := srv.Do(t, http.MethodPut, "/v1/budgets/overall", tc.u.AccessToken, map[string]any{"amount_minor": 10000})
			wantStatus(t, r, 200, "")
			var p progress
			r.JSON(t, &p)
			if p.Month != tc.month || p.SpentMinor != tc.spent {
				t.Fatalf("PUT progress: %+v, want month %s spent %d", p, tc.month, tc.spent)
			}
			got := list(t, srv, tc.u.AccessToken, "")
			if len(got) != 1 || got[0].Month != tc.month || got[0].SpentMinor != tc.spent {
				t.Fatalf("GET default month: %+v", got)
			}
		})
	}
}

func TestBudgetCurrencyBeforeOnboarding(t *testing.T) {
	srv := testutil.NewServer(t)
	u := srv.CreateUser(t) // not onboarded: users.currency is NULL
	r := srv.Do(t, http.MethodPut, "/v1/budgets/overall", u.AccessToken, map[string]any{"amount_minor": 100})
	wantStatus(t, r, 200, "")
	var p progress
	r.JSON(t, &p)
	if p.Currency != "INR" {
		t.Fatalf("currency = %q, want INR fallback", p.Currency)
	}

	usd := srv.CreateUser(t, testutil.UserOpts{Onboarded: true, Currency: "USD"})
	r = srv.Do(t, http.MethodPut, "/v1/budgets/overall", usd.AccessToken, map[string]any{"amount_minor": 100})
	wantStatus(t, r, 200, "")
	r.JSON(t, &p)
	if p.Currency != "USD" {
		t.Fatalf("currency = %q, want USD", p.Currency)
	}
}

func TestCategoryBudgetCategoryRules(t *testing.T) {
	srv := testutil.NewServer(t)
	a := srv.CreateUser(t, testutil.UserOpts{Onboarded: true})
	body := map[string]any{"amount_minor": 1000}

	salary := sysCategory(t, srv, "salary")
	r := srv.Do(t, http.MethodPut, "/v1/budgets/categories/"+salary.String(), a.AccessToken, body)
	wantStatus(t, r, http.StatusUnprocessableEntity, "validation_failed")

	ownIncome := customCategory(t, srv, a.ID, "Side gig", "INCOME")
	wantStatus(t, srv.Do(t, http.MethodPut, "/v1/budgets/categories/"+ownIncome.String(), a.AccessToken, body), 422, "validation_failed")

	archived := customCategory(t, srv, a.ID, "Old stuff", "EXPENSE")
	if _, err := srv.Deps.Pool.Exec(context.Background(), `UPDATE categories SET archived_at = now() WHERE id = $1`, archived); err != nil {
		t.Fatal(err)
	}
	wantStatus(t, srv.Do(t, http.MethodPut, "/v1/budgets/categories/"+archived.String(), a.AccessToken, body), 422, "validation_failed")

	wantStatus(t, srv.Do(t, http.MethodPut, "/v1/budgets/categories/"+uuid.NewString(), a.AccessToken, body), 404, "not_found")

	own := customCategory(t, srv, a.ID, "Pets", "EXPENSE")
	r = srv.Do(t, http.MethodPut, "/v1/budgets/categories/"+own.String(), a.AccessToken, body)
	wantStatus(t, r, 200, "")
	var p progress
	r.JSON(t, &p)
	if p.CategoryID == nil || *p.CategoryID != own || p.AmountMinor != 1000 {
		t.Fatalf("own category budget: %+v", p)
	}
	if got := list(t, srv, a.AccessToken, ""); len(got) != 1 {
		t.Fatalf("only the valid budget may exist: %+v", got)
	}
}

func TestDeleteBudgets(t *testing.T) {
	srv := testutil.NewServer(t)
	a := srv.CreateUser(t, testutil.UserOpts{Onboarded: true})
	food := sysCategory(t, srv, "food")

	// Deleting when none is set is still 204 (api/openapi.yaml: "Removed (also when none was set)").
	wantStatus(t, srv.Do(t, http.MethodDelete, "/v1/budgets/overall", a.AccessToken, nil), 204, "")
	wantStatus(t, srv.Do(t, http.MethodDelete, "/v1/budgets/categories/"+food.String(), a.AccessToken, nil), 204, "")

	wantStatus(t, srv.Do(t, http.MethodPut, "/v1/budgets/overall", a.AccessToken, map[string]any{"amount_minor": 500}), 200, "")
	wantStatus(t, srv.Do(t, http.MethodPut, "/v1/budgets/categories/"+food.String(), a.AccessToken, map[string]any{"amount_minor": 200}), 200, "")
	if got := list(t, srv, a.AccessToken, ""); len(got) != 2 {
		t.Fatalf("want 2 budgets: %+v", got)
	}

	wantStatus(t, srv.Do(t, http.MethodDelete, "/v1/budgets/overall", a.AccessToken, nil), 204, "")
	got := list(t, srv, a.AccessToken, "")
	if len(got) != 1 || got[0].CategoryID == nil || *got[0].CategoryID != food {
		t.Fatalf("after deleting overall: %+v", got)
	}
	wantStatus(t, srv.Do(t, http.MethodDelete, "/v1/budgets/categories/"+food.String(), a.AccessToken, nil), 204, "")
	if got := list(t, srv, a.AccessToken, ""); len(got) != 0 {
		t.Fatalf("after deleting all: %+v", got)
	}
	wantStatus(t, srv.Do(t, http.MethodDelete, "/v1/budgets/categories/not-a-uuid", a.AccessToken, nil), 422, "validation_failed")
}

func TestBudgetsAuthorization(t *testing.T) {
	srv := testutil.NewServer(t)
	a := srv.CreateUser(t, testutil.UserOpts{Onboarded: true})
	b := srv.CreateUser(t, testutil.UserOpts{Onboarded: true})
	food := sysCategory(t, srv, "food")
	aCustom := customCategory(t, srv, a.ID, "A's hobby", "EXPENSE")

	insertTx(t, srv, a.ID, a.DefaultAccountID, &food, "EXPENSE", 4000, "INR", "2026-03-05")
	insertTx(t, srv, b.ID, b.DefaultAccountID, &food, "EXPENSE", 999, "INR", "2026-03-06")
	srv.AssertBalance(t, a.DefaultAccountID, -4000)
	srv.AssertBalance(t, b.DefaultAccountID, -999)

	wantStatus(t, srv.Do(t, http.MethodPut, "/v1/budgets/overall", a.AccessToken, map[string]any{"amount_minor": 10000}), 200, "")
	wantStatus(t, srv.Do(t, http.MethodPut, "/v1/budgets/categories/"+aCustom.String(), a.AccessToken, map[string]any{"amount_minor": 3000}), 200, "")
	before := list(t, srv, a.AccessToken, "2026-03")

	// B sees none of A's budgets.
	if got := list(t, srv, b.AccessToken, "2026-03"); len(got) != 0 {
		t.Fatalf("B sees budgets: %+v", got)
	}
	// B cannot budget A's custom category: 404, not 422.
	wantStatus(t, srv.Do(t, http.MethodPut, "/v1/budgets/categories/"+aCustom.String(), b.AccessToken, map[string]any{"amount_minor": 1}), 404, "not_found")
	// B's delete of A's category budget and of "the" overall budget touches only B's rows.
	wantStatus(t, srv.Do(t, http.MethodDelete, "/v1/budgets/categories/"+aCustom.String(), b.AccessToken, nil), 204, "")
	wantStatus(t, srv.Do(t, http.MethodDelete, "/v1/budgets/overall", b.AccessToken, nil), 204, "")
	// B's own overall budget counts only B's spending.
	r := srv.Do(t, http.MethodPut, "/v1/budgets/overall", b.AccessToken, map[string]any{"amount_minor": 1})
	wantStatus(t, r, 200, "")
	bl := list(t, srv, b.AccessToken, "2026-03")
	if len(bl) != 1 || bl[0].SpentMinor != 999 || bl[0].ID == before[0].ID {
		t.Fatalf("B's budget: %+v", bl)
	}

	after := list(t, srv, a.AccessToken, "2026-03")
	if len(after) != len(before) {
		t.Fatalf("A's budgets changed: before=%+v after=%+v", before, after)
	}
	for i := range before {
		if !same(before[i], after[i]) {
			t.Fatalf("A's budget %d changed: before=%+v after=%+v", i, before[i], after[i])
		}
	}
	if before[0].SpentMinor != 4000 || before[0].AmountMinor != 10000 {
		t.Fatalf("A's overall budget: %+v", before[0])
	}
}

func TestBudgetsRequireToken(t *testing.T) {
	srv := testutil.NewServer(t)
	food := sysCategory(t, srv, "food")
	body := map[string]any{"amount_minor": 1}
	for _, tc := range []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, "/v1/budgets", nil},
		{http.MethodPut, "/v1/budgets/overall", body},
		{http.MethodDelete, "/v1/budgets/overall", nil},
		{http.MethodPut, "/v1/budgets/categories/" + food.String(), body},
		{http.MethodDelete, "/v1/budgets/categories/" + food.String(), nil},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			wantStatus(t, srv.Do(t, tc.method, tc.path, "", tc.body), 401, "unauthorized")
			wantStatus(t, srv.Do(t, tc.method, tc.path, "not-a-token", tc.body), 401, "unauthorized")
		})
	}
}

// same compares two progress values by content (CategoryID is a pointer).
func same(x, y progress) bool {
	cx, cy := x.CategoryID, y.CategoryID
	x.CategoryID, y.CategoryID = nil, nil
	if (cx == nil) != (cy == nil) || (cx != nil && *cx != *cy) {
		return false
	}
	return x == y
}

// percent_used is integer math clamped to int32: a huge overspend must not overflow or go negative.
func TestPercentUsedClampsWithoutOverflow(t *testing.T) {
	srv := testutil.NewServer(t)
	a := srv.CreateUser(t, testutil.UserOpts{Onboarded: true})
	food := sysCategory(t, srv, "food")
	const maxAmount = int64(999_999_999_999_999)
	insertTx(t, srv, a.ID, a.DefaultAccountID, &food, "EXPENSE", maxAmount, "INR", "2026-05-10")
	insertTx(t, srv, a.ID, a.DefaultAccountID, &food, "EXPENSE", maxAmount, "INR", "2026-05-11")
	srv.AssertBalance(t, a.DefaultAccountID, -2*maxAmount)
	wantStatus(t, srv.Do(t, http.MethodPut, "/v1/budgets/overall", a.AccessToken, map[string]any{"amount_minor": 1}), 200, "")
	wantStatus(t, srv.Do(t, http.MethodPut, "/v1/budgets/categories/"+food.String(), a.AccessToken, map[string]any{"amount_minor": 3 * 100_000_000_000}), 200, "")
	got := list(t, srv, a.AccessToken, "2026-05")
	if got[0].SpentMinor != 2*maxAmount || got[0].RemainingMinor != 1-2*maxAmount || got[0].PercentUsed != 2147483647 {
		t.Fatalf("clamped progress: %+v", got[0])
	}
	// 1 999 999 999 999 998 × 100 / 300 000 000 000 = 666 666.66… → 666 666 (exact floor, no float rounding).
	if got[1].PercentUsed != 666666 {
		t.Fatalf("large exact percent: %+v", got[1])
	}
}
