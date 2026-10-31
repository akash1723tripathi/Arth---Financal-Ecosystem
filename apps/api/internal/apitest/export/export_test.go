package export_test

import (
	"context"
	"encoding/csv"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"arth/api/internal/db"
	"arth/api/internal/testutil"
)

func systemCategory(t *testing.T, srv *testutil.Server, key string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := srv.Deps.Pool.QueryRow(context.Background(),
		`SELECT id FROM categories WHERE key = $1 AND user_id IS NULL`, key).Scan(&id); err != nil {
		t.Fatalf("category %q: %v", key, err)
	}
	return id
}

func newAccount(t *testing.T, srv *testutil.Server, userID uuid.UUID, name, currency string) uuid.UUID {
	t.Helper()
	a, err := db.New(srv.Deps.Pool).CreateAccount(context.Background(), db.CreateAccountParams{
		UserID: userID, Name: name, Type: "BANK", Currency: currency,
	})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	return a.ID
}

func create(t *testing.T, srv *testutil.Server, token string, b map[string]any) {
	t.Helper()
	r := srv.Do(t, http.MethodPost, "/v1/transactions", token, b)
	if r.Status != http.StatusCreated {
		t.Fatalf("create: %d %s", r.Status, r.Body)
	}
}

func tx(account, category uuid.UUID, typ string, amount int64, on, desc string) map[string]any {
	b := map[string]any{"account_id": account, "category_id": category, "type": typ, "amount_minor": amount, "occurred_on": on}
	if desc != "" {
		b["description"] = desc
	}
	return b
}

func export(t *testing.T, srv *testutil.Server, token, query string) [][]string {
	t.Helper()
	r := srv.Do(t, http.MethodGet, "/v1/transactions/export?"+query, token, nil)
	if r.Status != http.StatusOK {
		t.Fatalf("export %s: %d %s", query, r.Status, r.Body)
	}
	recs, err := csv.NewReader(strings.NewReader(string(r.Body))).ReadAll()
	if err != nil {
		t.Fatalf("parse csv: %v\n%s", err, r.Body)
	}
	return recs
}

func expectCode(t *testing.T, r *testutil.Response, status int, code string) {
	t.Helper()
	if r.Status != status || r.Code() != code {
		t.Fatalf("want %d %s, got %d %s: %s", status, code, r.Status, r.Code(), r.Body)
	}
}

var header = []string{"date", "type", "amount", "currency", "category", "account", "description", "input_method"}

func TestExportCSV(t *testing.T) {
	srv := testutil.NewServer(t)
	u := srv.CreateUser(t, testutil.UserOpts{Onboarded: true})
	food := systemCategory(t, srv, "food")
	salary := systemCategory(t, srv, "salary")
	jpy := newAccount(t, srv, u.ID, "Yen wallet", "JPY")

	create(t, srv, u.AccessToken, tx(u.DefaultAccountID, salary, "INCOME", 5000000, "2026-10-01", "October salary"))
	create(t, srv, u.AccessToken, tx(u.DefaultAccountID, food, "EXPENSE", 12345, "2026-10-02", "Lunch, with \"quotes\""))
	create(t, srv, u.AccessToken, tx(u.DefaultAccountID, food, "EXPENSE", 5, "2026-10-03", ""))
	create(t, srv, u.AccessToken, tx(jpy, food, "EXPENSE", 1500, "2026-10-04", "Ramen"))
	create(t, srv, u.AccessToken, tx(u.DefaultAccountID, food, "EXPENSE", 999, "2026-11-01", "outside range"))
	srv.AssertBalance(t, u.DefaultAccountID, 5000000-12345-5-999)
	srv.AssertBalance(t, jpy, -1500)

	r := srv.Do(t, http.MethodGet, "/v1/transactions/export?format=csv&from=2026-10-01&to=2026-10-31", u.AccessToken, nil)
	if r.Status != http.StatusOK {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	if got := r.Header.Get("Content-Type"); got != "text/csv; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
	if got, want := r.Header.Get("Content-Disposition"), `attachment; filename="arth-transactions-2026-10-01-2026-10-31.csv"`; got != want {
		t.Errorf("Content-Disposition = %q, want %q", got, want)
	}

	recs := export(t, srv, u.AccessToken, "from=2026-10-01&to=2026-10-31")
	want := [][]string{
		header,
		{"2026-10-04", "EXPENSE", "1500", "JPY", "Food & Dining", "Yen wallet", "Ramen", "MANUAL"},
		{"2026-10-03", "EXPENSE", "0.05", "INR", "Food & Dining", "Cash", "", "MANUAL"},
		{"2026-10-02", "EXPENSE", "123.45", "INR", "Food & Dining", "Cash", "Lunch, with \"quotes\"", "MANUAL"},
		{"2026-10-01", "INCOME", "50000.00", "INR", "Salary", "Cash", "October salary", "MANUAL"},
	}
	assertRecords(t, recs, want)

	t.Run("account filter", func(t *testing.T) {
		recs := export(t, srv, u.AccessToken, "from=2026-10-01&to=2026-10-31&account_id="+jpy.String())
		assertRecords(t, recs, [][]string{header, want[1]})
	})
	t.Run("empty range has only the header", func(t *testing.T) {
		assertRecords(t, export(t, srv, u.AccessToken, "from=2025-01-01&to=2025-01-31"), [][]string{header})
	})
	t.Run("single day inclusive", func(t *testing.T) {
		assertRecords(t, export(t, srv, u.AccessToken, "from=2026-11-01&to=2026-11-01"),
			[][]string{header, {"2026-11-01", "EXPENSE", "9.99", "INR", "Food & Dining", "Cash", "outside range", "MANUAL"}})
	})
}

func TestExportInjectionGuard(t *testing.T) {
	srv := testutil.NewServer(t)
	u := srv.CreateUser(t, testutil.UserOpts{Onboarded: true})
	ctx := context.Background()
	evilAcct := newAccount(t, srv, u.ID, "=HYPERLINK(\"http://x\")", "INR")
	var evilCat uuid.UUID
	if err := srv.Deps.Pool.QueryRow(ctx, `INSERT INTO categories (user_id, name, kind) VALUES ($1, '@SUM(1+1)', 'EXPENSE') RETURNING id`, u.ID).Scan(&evilCat); err != nil {
		t.Fatal(err)
	}
	food := systemCategory(t, srv, "food")
	descs := []string{"=1+2", "+cmd", "-2+3", "@A1", "\tTab", "\rCR", "safe = text"}
	for i, d := range descs {
		create(t, srv, u.AccessToken, tx(u.DefaultAccountID, food, "EXPENSE", int64(i+1), "2026-10-0"+string(rune('1'+i)), d))
	}
	create(t, srv, u.AccessToken, tx(evilAcct, evilCat, "EXPENSE", 100, "2026-10-09", "x"))

	recs := export(t, srv, u.AccessToken, "from=2026-10-01&to=2026-10-31")
	if len(recs) != 1+len(descs)+1 {
		t.Fatalf("%d records", len(recs))
	}
	got := map[string]bool{}
	for _, rec := range recs[1:] {
		got[rec[6]] = true
		if rec[0] == "2026-10-09" {
			if rec[4] != "'@SUM(1+1)" || rec[5] != "'=HYPERLINK(\"http://x\")" {
				t.Errorf("category/account not guarded: %q %q", rec[4], rec[5])
			}
		}
	}
	for _, want := range []string{"'=1+2", "'+cmd", "'-2+3", "'@A1", "'\tTab", "'\rCR", "safe = text"} {
		if !got[want] {
			t.Errorf("missing guarded description %q in %v", want, got)
		}
	}
	srv.AssertBalance(t, evilAcct, -100)
}

func TestExportValidation(t *testing.T) {
	srv := testutil.NewServer(t)
	u := srv.CreateUser(t, testutil.UserOpts{Onboarded: true})
	path := "/v1/transactions/export?"
	expectCode(t, srv.Do(t, http.MethodGet, path+"from=2026-01-01&to=2027-01-02", u.AccessToken, nil), 422, "validation_failed") // 367 days
	expectCode(t, srv.Do(t, http.MethodGet, path+"from=2026-10-31&to=2026-10-01", u.AccessToken, nil), 422, "validation_failed")
	expectCode(t, srv.Do(t, http.MethodGet, path+"from=2026-10-01", u.AccessToken, nil), 422, "validation_failed")
	expectCode(t, srv.Do(t, http.MethodGet, path+"to=2026-10-01", u.AccessToken, nil), 422, "validation_failed")
	expectCode(t, srv.Do(t, http.MethodGet, path+"from=2026-10-01&to=2026-10-31&format=xlsx", u.AccessToken, nil), 422, "validation_failed")
	expectCode(t, srv.Do(t, http.MethodGet, path+"from=2026-10-01&to=2026-10-31&account_id="+uuid.NewString(), u.AccessToken, nil), 404, "not_found")
	expectCode(t, srv.Do(t, http.MethodGet, path+"from=2026-01-01&to=2026-12-31", "", nil), 401, "unauthorized")
	// 366 days (a leap year, both ends included) is allowed.
	if r := srv.Do(t, http.MethodGet, path+"from=2028-01-01&to=2028-12-31", u.AccessToken, nil); r.Status != http.StatusOK {
		t.Errorf("366 days: %d %s", r.Status, r.Body)
	}
	if r := srv.Do(t, http.MethodGet, path+"from=2026-01-01&to=2027-01-01", u.AccessToken, nil); r.Status != http.StatusOK {
		t.Errorf("366 days across years: %d %s", r.Status, r.Body)
	}
}

func TestExportOnlyOwnRows(t *testing.T) {
	srv := testutil.NewServer(t)
	a := srv.CreateUser(t, testutil.UserOpts{Onboarded: true})
	b := srv.CreateUser(t, testutil.UserOpts{Onboarded: true})
	food := systemCategory(t, srv, "food")
	create(t, srv, a.AccessToken, tx(a.DefaultAccountID, food, "EXPENSE", 100, "2026-10-01", "A-private"))
	create(t, srv, b.AccessToken, tx(b.DefaultAccountID, food, "EXPENSE", 200, "2026-10-01", "B-own"))

	recs := export(t, srv, b.AccessToken, "from=2026-10-01&to=2026-10-31")
	assertRecords(t, recs, [][]string{header, {"2026-10-01", "EXPENSE", "2.00", "INR", "Food & Dining", "Cash", "B-own", "MANUAL"}})
	expectCode(t, srv.Do(t, http.MethodGet, "/v1/transactions/export?from=2026-10-01&to=2026-10-31&account_id="+a.DefaultAccountID.String(), b.AccessToken, nil), 404, "not_found")
	srv.AssertBalance(t, a.DefaultAccountID, -100)
	srv.AssertBalance(t, b.DefaultAccountID, -200)
}

func assertRecords(t *testing.T, got, want [][]string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d records %q, want %d %q", len(got), got, len(want), want)
	}
	for i := range want {
		if strings.Join(got[i], "|") != strings.Join(want[i], "|") {
			t.Errorf("record %d = %q, want %q", i, got[i], want[i])
		}
	}
}
