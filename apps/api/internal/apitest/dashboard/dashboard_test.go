package dashboard_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"arth/api/internal/db"
	"arth/api/internal/testutil"
)

// ---------------------------------------------------------------- helpers

type progress struct {
	ID          uuid.UUID  `json:"id"`
	CategoryID  *uuid.UUID `json:"category_id"`
	AmountMinor int64      `json:"amount_minor"`
	Currency    string     `json:"currency"`
	Month       string     `json:"month"`
	SpentMinor  int64      `json:"spent_minor"`
	Remaining   int64      `json:"remaining_minor"`
	PercentUsed int32      `json:"percent_used"`
}

type txn struct {
	ID         uuid.UUID `json:"id"`
	OccurredOn string    `json:"occurred_on"`
	AccountID  uuid.UUID `json:"account_id"`
	Type       string    `json:"type"`
}

type dashboard struct {
	Currency          string `json:"currency"`
	TotalBalanceMinor int64  `json:"total_balance_minor"`
	Month             struct {
		IncomeMinor  int64 `json:"income_minor"`
		ExpenseMinor int64 `json:"expense_minor"`
		NetMinor     int64 `json:"net_minor"`
	} `json:"month"`
	Budget            *progress `json:"budget"`
	ExpenseByCategory []struct {
		CategoryID  uuid.UUID `json:"category_id"`
		AmountMinor int64     `json:"amount_minor"`
	} `json:"expense_by_category"`
	Recent []txn `json:"recent"`
}

type daily struct {
	Data []struct {
		Date         string `json:"date"`
		IncomeMinor  int64  `json:"income_minor"`
		ExpenseMinor int64  `json:"expense_minor"`
	} `json:"data"`
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

func createAccount(t *testing.T, srv *testutil.Server, userID uuid.UUID, name, currency string, opening int64) uuid.UUID {
	t.Helper()
	a, err := srv.Deps.Q.CreateAccount(context.Background(), db.CreateAccountParams{
		UserID: userID, Name: name, Type: "BANK", Currency: currency, OpeningBalanceMinor: opening,
	})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	return a.ID
}

// insertTx writes a transaction directly (the transactions endpoint is another slice) and applies its
// delta to the cached balance, keeping the balance invariant. Returns the new id.
func insertTx(t *testing.T, srv *testutil.Server, userID, accountID uuid.UUID, categoryID *uuid.UUID, typ string, amount int64, currency, on string) uuid.UUID {
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
	var id uuid.UUID
	var delta int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO transactions (user_id, account_id, category_id, type, amount_minor, currency, occurred_on, transfer_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7::date, $8) RETURNING id, delta_minor`,
		userID, accountID, categoryID, typ, amount, currency, on, transferID).Scan(&id, &delta); err != nil {
		t.Fatalf("insert transaction: %v", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE accounts SET balance_minor = balance_minor + $1 WHERE id = $2 AND user_id = $3`,
		delta, accountID, userID); err != nil {
		t.Fatalf("apply delta: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	return id
}

func wantStatus(t *testing.T, r *testutil.Response, status int, code string) {
	t.Helper()
	if r.Status != status || (code != "" && r.Code() != code) {
		t.Fatalf("got %d %q, want %d %q: %s", r.Status, r.Code(), status, code, r.Body)
	}
}

func getDashboard(t *testing.T, srv *testutil.Server, token, month string) dashboard {
	t.Helper()
	path := "/v1/dashboard"
	if month != "" {
		path += "?month=" + month
	}
	r := srv.Do(t, http.MethodGet, path, token, nil)
	wantStatus(t, r, http.StatusOK, "")
	var d dashboard
	r.JSON(t, &d)
	return d
}

func getDaily(t *testing.T, srv *testutil.Server, token, query string) daily {
	t.Helper()
	r := srv.Do(t, http.MethodGet, "/v1/reports/daily?"+query, token, nil)
	wantStatus(t, r, http.StatusOK, "")
	var d daily
	r.JSON(t, &d)
	return d
}

// seed gives user u a known March 2026 (plus noise outside it) and returns the second INR account.
//
//	INR Cash (default): opening 0
//	INR Bank: opening 10 000
//	USD Wallet: opening 5 000 (other currency: excluded from every INR total)
func seed(t *testing.T, srv *testutil.Server, u testutil.User) (bank, usd uuid.UUID) {
	t.Helper()
	food, transport, salary := sysCategory(t, srv, "food"), sysCategory(t, srv, "transport"), sysCategory(t, srv, "salary")
	cash := u.DefaultAccountID
	bank = createAccount(t, srv, u.ID, "Bank", "INR", 10000)
	usd = createAccount(t, srv, u.ID, "Wallet", "USD", 5000)

	insertTx(t, srv, u.ID, bank, &salary, "INCOME", 100000, "INR", "2026-03-01")
	insertTx(t, srv, u.ID, cash, &food, "EXPENSE", 30000, "INR", "2026-03-05")
	insertTx(t, srv, u.ID, bank, &transport, "EXPENSE", 20000, "INR", "2026-03-05")
	insertTx(t, srv, u.ID, cash, &food, "EXPENSE", 5000, "INR", "2026-03-31")
	insertTx(t, srv, u.ID, cash, nil, "TRANSFER_OUT", 1500, "INR", "2026-03-10") // excluded from aggregates
	insertTx(t, srv, u.ID, cash, &food, "EXPENSE", 7000, "INR", "2026-02-28")    // other month
	insertTx(t, srv, u.ID, usd, &food, "EXPENSE", 300, "USD", "2026-03-06")      // other currency
	srv.AssertBalance(t, cash, -30000-5000-1500-7000)
	srv.AssertBalance(t, bank, 10000+100000-20000)
	srv.AssertBalance(t, usd, 5000-300)
	return bank, usd
}

// ---------------------------------------------------------------- dashboard

func TestDashboardTotals(t *testing.T) {
	srv := testutil.NewServer(t)
	u := srv.CreateUser(t, testutil.UserOpts{Onboarded: true})
	seed(t, srv, u)
	food, transport := sysCategory(t, srv, "food"), sysCategory(t, srv, "transport")

	d := getDashboard(t, srv, u.AccessToken, "2026-03")
	if d.Currency != "INR" {
		t.Fatalf("currency = %q", d.Currency)
	}
	if want := int64(-30000 - 5000 - 1500 - 7000 + 10000 + 100000 - 20000); d.TotalBalanceMinor != want {
		t.Fatalf("total balance = %d, want %d (INR accounts only)", d.TotalBalanceMinor, want)
	}
	if d.Month.IncomeMinor != 100000 || d.Month.ExpenseMinor != 55000 || d.Month.NetMinor != 45000 {
		t.Fatalf("month totals: %+v", d.Month)
	}
	if d.Budget != nil {
		t.Fatalf("budget should be null: %+v", d.Budget)
	}
	if len(d.ExpenseByCategory) != 2 ||
		d.ExpenseByCategory[0].CategoryID != food || d.ExpenseByCategory[0].AmountMinor != 35000 ||
		d.ExpenseByCategory[1].CategoryID != transport || d.ExpenseByCategory[1].AmountMinor != 20000 {
		t.Fatalf("expense by category (largest first): %+v", d.ExpenseByCategory)
	}

	// February: only the 28th.
	feb := getDashboard(t, srv, u.AccessToken, "2026-02")
	if feb.Month.IncomeMinor != 0 || feb.Month.ExpenseMinor != 7000 || feb.Month.NetMinor != -7000 || len(feb.ExpenseByCategory) != 1 {
		t.Fatalf("february: %+v", feb)
	}
	if feb.TotalBalanceMinor != d.TotalBalanceMinor {
		t.Fatalf("total balance must not depend on the month: %d vs %d", feb.TotalBalanceMinor, d.TotalBalanceMinor)
	}
	// An empty month has zero totals and an empty (not null) breakdown.
	empty := getDashboard(t, srv, u.AccessToken, "2025-01")
	if empty.Month.IncomeMinor != 0 || empty.Month.ExpenseMinor != 0 || empty.ExpenseByCategory == nil || len(empty.ExpenseByCategory) != 0 {
		t.Fatalf("empty month: %+v", empty)
	}

	// With an overall budget the dashboard carries its progress for the requested month.
	wantStatus(t, srv.Do(t, http.MethodPut, "/v1/budgets/overall", u.AccessToken, map[string]any{"amount_minor": 40000}), 200, "")
	d = getDashboard(t, srv, u.AccessToken, "2026-03")
	if d.Budget == nil || d.Budget.Month != "2026-03" || d.Budget.SpentMinor != 55000 || d.Budget.Remaining != -15000 || d.Budget.PercentUsed != 137 {
		t.Fatalf("budget progress: %+v", d.Budget)
	}
}

func TestDashboardRecent(t *testing.T) {
	srv := testutil.NewServer(t)
	u := srv.CreateUser(t, testutil.UserOpts{Onboarded: true})
	food := sysCategory(t, srv, "food")

	d := getDashboard(t, srv, u.AccessToken, "")
	if d.Recent == nil || len(d.Recent) != 0 {
		t.Fatalf("recent should be an empty array: %+v", d.Recent)
	}

	days := []string{"2026-01-03", "2026-03-09", "2025-12-31", "2026-02-14", "2026-03-01", "2026-01-20", "2026-03-08"}
	ids := map[string]uuid.UUID{}
	for _, day := range days {
		ids[day] = insertTx(t, srv, u.ID, u.DefaultAccountID, &food, "EXPENSE", 100, "INR", day)
	}
	srv.AssertBalance(t, u.DefaultAccountID, -700)

	d = getDashboard(t, srv, u.AccessToken, "")
	want := []string{"2026-03-09", "2026-03-08", "2026-03-01", "2026-02-14", "2026-01-20"}
	if len(d.Recent) != len(want) {
		t.Fatalf("recent: got %d, want 5: %+v", len(d.Recent), d.Recent)
	}
	for i, day := range want {
		if d.Recent[i].OccurredOn != day || d.Recent[i].ID != ids[day] {
			t.Fatalf("recent[%d] = %+v, want %s (newest first, any month)", i, d.Recent[i], day)
		}
	}
}

func TestDashboardBeforeOnboarding(t *testing.T) {
	srv := testutil.NewServer(t)
	u := srv.CreateUser(t) // currency NULL → INR
	d := getDashboard(t, srv, u.AccessToken, "")
	if d.Currency != "INR" || d.TotalBalanceMinor != 0 || d.Budget != nil {
		t.Fatalf("dashboard before onboarding: %+v", d)
	}
}

func TestDashboardDefaultMonthUsesTimezone(t *testing.T) {
	srv := testutil.NewServer(t)
	year := srv.Clock.Now().Year() + 1 // the clock only moves forward (sessions expire against the DB's now())
	srv.Clock.Advance(time.Date(year, 4, 1, 3, 0, 0, 0, time.UTC).Sub(srv.Clock.Now()))
	food := sysCategory(t, srv, "food")

	la := srv.CreateUser(t, testutil.UserOpts{Onboarded: true, Timezone: "America/Los_Angeles"})
	insertTx(t, srv, la.ID, la.DefaultAccountID, &food, "EXPENSE", 100, "INR", fmt.Sprintf("%d-03-31", year))
	insertTx(t, srv, la.ID, la.DefaultAccountID, &food, "EXPENSE", 1000, "INR", fmt.Sprintf("%d-04-01", year))
	srv.AssertBalance(t, la.DefaultAccountID, -1100)
	if d := getDashboard(t, srv, la.AccessToken, ""); d.Month.ExpenseMinor != 100 {
		t.Fatalf("LA is still in March: %+v", d.Month)
	}
}

func TestDashboardValidation(t *testing.T) {
	srv := testutil.NewServer(t)
	u := srv.CreateUser(t, testutil.UserOpts{Onboarded: true})
	for _, m := range []string{"2026-00", "2026-13", "26-03", "2026-3", "march"} {
		wantStatus(t, srv.Do(t, http.MethodGet, "/v1/dashboard?month="+m, u.AccessToken, nil), 422, "validation_failed")
	}
}

// ---------------------------------------------------------------- daily report

func TestDailyReport(t *testing.T) {
	srv := testutil.NewServer(t)
	u := srv.CreateUser(t, testutil.UserOpts{Onboarded: true})
	bank, usd := seed(t, srv, u)

	rep := getDaily(t, srv, u.AccessToken, "from=2026-03-01&to=2026-03-31")
	if len(rep.Data) != 31 {
		t.Fatalf("rows = %d, want 31 (zero-filled)", len(rep.Data))
	}
	start := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	byDay := map[string][2]int64{}
	for i, row := range rep.Data {
		if want := start.AddDate(0, 0, i).Format(time.DateOnly); row.Date != want {
			t.Fatalf("row %d date %s, want %s (ascending)", i, row.Date, want)
		}
		byDay[row.Date] = [2]int64{row.IncomeMinor, row.ExpenseMinor}
	}
	checks := map[string][2]int64{
		"2026-03-01": {100000, 0},
		"2026-03-05": {0, 50000},
		"2026-03-06": {0, 0}, // USD expense excluded (user currency INR)
		"2026-03-10": {0, 0}, // transfer excluded
		"2026-03-31": {0, 5000},
		"2026-03-15": {0, 0},
	}
	for day, want := range checks {
		if byDay[day] != want {
			t.Errorf("%s = %v, want %v", day, byDay[day], want)
		}
	}

	// Single day.
	if one := getDaily(t, srv, u.AccessToken, "from=2026-02-28&to=2026-02-28"); len(one.Data) != 1 || one.Data[0].ExpenseMinor != 7000 {
		t.Fatalf("single day: %+v", one)
	}
	// Exactly 93 days is allowed: 2026-01-01 .. 2026-04-03.
	if long := getDaily(t, srv, u.AccessToken, "from=2026-01-01&to=2026-04-03"); len(long.Data) != 93 {
		t.Fatalf("93-day range: %d rows", len(long.Data))
	}

	// Account filter: only the bank account.
	b := getDaily(t, srv, u.AccessToken, "from=2026-03-01&to=2026-03-31&account_id="+bank.String())
	var inc, exp int64
	for _, row := range b.Data {
		inc += row.IncomeMinor
		exp += row.ExpenseMinor
	}
	if len(b.Data) != 31 || inc != 100000 || exp != 20000 {
		t.Fatalf("bank account: rows=%d income=%d expense=%d", len(b.Data), inc, exp)
	}
	// A USD account reports in its own currency.
	ud := getDaily(t, srv, u.AccessToken, "from=2026-03-06&to=2026-03-06&account_id="+usd.String())
	if len(ud.Data) != 1 || ud.Data[0].ExpenseMinor != 300 {
		t.Fatalf("usd account: %+v", ud)
	}
}

func TestDailyReportValidation(t *testing.T) {
	srv := testutil.NewServer(t)
	u := srv.CreateUser(t, testutil.UserOpts{Onboarded: true})
	for _, tc := range []struct {
		name, query string
		status      int
		code        string
	}{
		{"94 days", "from=2026-01-01&to=2026-04-04", 422, "validation_failed"},
		{"a year", "from=2025-01-01&to=2025-12-31", 422, "validation_failed"},
		{"to before from", "from=2026-03-02&to=2026-03-01", 422, "validation_failed"},
		{"missing from", "to=2026-03-01", 422, "validation_failed"},
		{"missing to", "from=2026-03-01", 422, "validation_failed"},
		{"not a date", "from=yesterday&to=2026-03-01", 422, "validation_failed"},
		// Pattern-valid but impossible dates pass kin-openapi's date regex and fail in the generated
		// parameter binder, which the shared wiring (api.go) maps to 400 bad_request.
		{"impossible date", "from=2026-02-30&to=2026-03-01", 400, "bad_request"},
		{"bad account id", "from=2026-03-01&to=2026-03-02&account_id=nope", 422, "validation_failed"},
		{"unknown account", "from=2026-03-01&to=2026-03-02&account_id=" + uuid.NewString(), 404, "not_found"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wantStatus(t, srv.Do(t, http.MethodGet, "/v1/reports/daily?"+tc.query, u.AccessToken, nil), tc.status, tc.code)
		})
	}
}

// ---------------------------------------------------------------- authorization

func TestDashboardAndReportAuthorization(t *testing.T) {
	srv := testutil.NewServer(t)
	a := srv.CreateUser(t, testutil.UserOpts{Onboarded: true})
	b := srv.CreateUser(t, testutil.UserOpts{Onboarded: true})
	aBank, _ := seed(t, srv, a)
	wantStatus(t, srv.Do(t, http.MethodPut, "/v1/budgets/overall", a.AccessToken, map[string]any{"amount_minor": 40000}), 200, "")
	before := getDashboard(t, srv, a.AccessToken, "2026-03")
	beforeRep := getDaily(t, srv, a.AccessToken, "from=2026-03-01&to=2026-03-31")

	// B sees none of A's numbers.
	d := getDashboard(t, srv, b.AccessToken, "2026-03")
	if d.TotalBalanceMinor != 0 || d.Month.IncomeMinor != 0 || d.Month.ExpenseMinor != 0 || d.Month.NetMinor != 0 ||
		d.Budget != nil || len(d.ExpenseByCategory) != 0 || len(d.Recent) != 0 {
		t.Fatalf("B's dashboard leaks A's data: %+v", d)
	}
	rep := getDaily(t, srv, b.AccessToken, "from=2026-03-01&to=2026-03-31")
	if len(rep.Data) != 31 {
		t.Fatalf("B rows = %d", len(rep.Data))
	}
	for _, row := range rep.Data {
		if row.IncomeMinor != 0 || row.ExpenseMinor != 0 {
			t.Fatalf("B's report leaks A's data: %+v", row)
		}
	}
	// B filtering by A's account → 404.
	wantStatus(t, srv.Do(t, http.MethodGet, "/v1/reports/daily?from=2026-03-01&to=2026-03-31&account_id="+aBank.String(), b.AccessToken, nil), 404, "not_found")
	wantStatus(t, srv.Do(t, http.MethodGet, "/v1/reports/daily?from=2026-03-01&to=2026-03-31&account_id="+a.DefaultAccountID.String(), b.AccessToken, nil), 404, "not_found")

	// A's numbers are unchanged.
	after := getDashboard(t, srv, a.AccessToken, "2026-03")
	if after.TotalBalanceMinor != before.TotalBalanceMinor || after.Month != before.Month ||
		after.Budget == nil || after.Budget.SpentMinor != before.Budget.SpentMinor || len(after.Recent) != len(before.Recent) {
		t.Fatalf("A's dashboard changed: before=%+v after=%+v", before, after)
	}
	afterRep := getDaily(t, srv, a.AccessToken, "from=2026-03-01&to=2026-03-31")
	for i := range beforeRep.Data {
		if beforeRep.Data[i] != afterRep.Data[i] {
			t.Fatalf("A's report changed at %d: %+v vs %+v", i, beforeRep.Data[i], afterRep.Data[i])
		}
	}
}

func TestDashboardAndReportRequireToken(t *testing.T) {
	srv := testutil.NewServer(t)
	for _, path := range []string{"/v1/dashboard", "/v1/dashboard?month=2026-03", "/v1/reports/daily?from=2026-03-01&to=2026-03-02"} {
		t.Run(path, func(t *testing.T) {
			wantStatus(t, srv.Do(t, http.MethodGet, path, "", nil), 401, "unauthorized")
			wantStatus(t, srv.Do(t, http.MethodGet, path, "garbage", nil), 401, "unauthorized")
		})
	}
}
