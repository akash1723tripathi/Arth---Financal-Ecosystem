package transactions_test

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"arth/api/internal/db"
	"arth/api/internal/testutil"
)

// ---------------------------------------------------------------- helpers

type account struct {
	ID           uuid.UUID `json:"id"`
	Currency     string    `json:"currency"`
	BalanceMinor int64     `json:"balance_minor"`
}

type txn struct {
	ID              uuid.UUID  `json:"id"`
	AccountID       uuid.UUID  `json:"account_id"`
	CategoryID      *uuid.UUID `json:"category_id"`
	Type            string     `json:"type"`
	AmountMinor     int64      `json:"amount_minor"`
	Currency        string     `json:"currency"`
	OccurredOn      string     `json:"occurred_on"`
	Description     *string    `json:"description"`
	InputMethod     string     `json:"input_method"`
	VoiceTranscript *string    `json:"voice_transcript"`
	TransferID      *uuid.UUID `json:"transfer_id"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type created struct {
	Transaction txn     `json:"transaction"`
	Account     account `json:"account"`
}

type updated struct {
	Transaction txn       `json:"transaction"`
	Accounts    []account `json:"accounts"`
}

type page struct {
	Data       []txn   `json:"data"`
	NextCursor *string `json:"next_cursor"`
}

type problem struct {
	Code   string `json:"code"`
	Errors []struct {
		Field   string `json:"field"`
		Message string `json:"message"`
	} `json:"errors"`
}

func systemCategory(t *testing.T, srv *testutil.Server, key string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := srv.Deps.Pool.QueryRow(context.Background(),
		`SELECT id FROM categories WHERE key = $1 AND user_id IS NULL`, key).Scan(&id); err != nil {
		t.Fatalf("category %q: %v", key, err)
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

func newAccount(t *testing.T, srv *testutil.Server, userID uuid.UUID, name, currency string, opening int64) uuid.UUID {
	t.Helper()
	a, err := db.New(srv.Deps.Pool).CreateAccount(context.Background(), db.CreateAccountParams{
		UserID: userID, Name: name, Type: "BANK", Currency: currency, OpeningBalanceMinor: opening,
	})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	return a.ID
}

func body(accountID, categoryID uuid.UUID, typ string, amount int64, on string, extra ...map[string]any) map[string]any {
	b := map[string]any{
		"account_id":   accountID,
		"category_id":  categoryID,
		"type":         typ,
		"amount_minor": amount,
		"occurred_on":  on,
	}
	for _, e := range extra {
		for k, v := range e {
			b[k] = v
		}
	}
	return b
}

func create(t *testing.T, srv *testutil.Server, token string, b map[string]any) created {
	t.Helper()
	r := srv.Do(t, http.MethodPost, "/v1/transactions", token, b)
	if r.Status != http.StatusCreated {
		t.Fatalf("create: status %d body %s", r.Status, r.Body)
	}
	var c created
	r.JSON(t, &c)
	return c
}

func expectProblem(t *testing.T, r *testutil.Response, status int, code string) problem {
	t.Helper()
	if r.Status != status || r.Code() != code {
		t.Fatalf("want %d %s, got %d %s: %s", status, code, r.Status, r.Code(), r.Body)
	}
	var p problem
	r.JSON(t, &p)
	return p
}

func hasField(p problem, field string) bool {
	for _, e := range p.Errors {
		if e.Field == field {
			return true
		}
	}
	return false
}

func list(t *testing.T, srv *testutil.Server, token string, q url.Values) page {
	t.Helper()
	r := srv.Do(t, http.MethodGet, "/v1/transactions?"+q.Encode(), token, nil)
	if r.Status != http.StatusOK {
		t.Fatalf("list %s: status %d body %s", q.Encode(), r.Status, r.Body)
	}
	var p page
	r.JSON(t, &p)
	return p
}

func listAll(t *testing.T, srv *testutil.Server, token string, q url.Values) []txn {
	t.Helper()
	var out []txn
	for i := 0; i < 1000; i++ {
		p := list(t, srv, token, q)
		out = append(out, p.Data...)
		if p.NextCursor == nil {
			return out
		}
		q.Set("cursor", *p.NextCursor)
	}
	t.Fatal("pagination did not terminate")
	return nil
}

func ids(rows []txn) map[uuid.UUID]bool {
	m := map[uuid.UUID]bool{}
	for _, r := range rows {
		m[r.ID] = true
	}
	return m
}

// ---------------------------------------------------------------- create / update / delete

func TestCreateIncomeAndExpense(t *testing.T) {
	srv := testutil.NewServer(t)
	u := srv.CreateUser(t, testutil.UserOpts{Onboarded: true})
	salary := systemCategory(t, srv, "salary")
	food := systemCategory(t, srv, "food")

	r := srv.Do(t, http.MethodPost, "/v1/transactions", u.AccessToken, body(u.DefaultAccountID, salary, "INCOME", 500000, "2026-10-01"))
	if r.Status != http.StatusCreated {
		t.Fatalf("status %d: %s", r.Status, r.Body)
	}
	var inc created
	r.JSON(t, &inc)
	if got, want := r.Header.Get("Location"), "/v1/transactions/"+inc.Transaction.ID.String(); got != want {
		t.Errorf("Location = %q, want %q", got, want)
	}
	if inc.Transaction.Currency != "INR" || inc.Transaction.InputMethod != "MANUAL" || inc.Transaction.Type != "INCOME" ||
		inc.Transaction.AmountMinor != 500000 || inc.Transaction.OccurredOn != "2026-10-01" || inc.Transaction.Description != nil ||
		inc.Transaction.TransferID != nil || inc.Transaction.CategoryID == nil || *inc.Transaction.CategoryID != salary {
		t.Errorf("unexpected transaction %+v", inc.Transaction)
	}
	if inc.Account.ID != u.DefaultAccountID || inc.Account.BalanceMinor != 500000 {
		t.Errorf("account after create = %+v", inc.Account)
	}
	srv.AssertBalance(t, u.DefaultAccountID, 500000)

	exp := create(t, srv, u.AccessToken, body(u.DefaultAccountID, food, "EXPENSE", 12345, "2026-10-02",
		map[string]any{"description": "Lunch", "input_method": "RECEIPT_SCAN"}))
	if exp.Account.BalanceMinor != 500000-12345 || *exp.Transaction.Description != "Lunch" || exp.Transaction.InputMethod != "RECEIPT_SCAN" {
		t.Errorf("expense = %+v", exp)
	}
	srv.AssertBalance(t, u.DefaultAccountID, 500000-12345)

	g := srv.Do(t, http.MethodGet, "/v1/transactions/"+exp.Transaction.ID.String(), u.AccessToken, nil)
	if g.Status != http.StatusOK {
		t.Fatalf("get: %d %s", g.Status, g.Body)
	}
	var got txn
	g.JSON(t, &got)
	if got.ID != exp.Transaction.ID || got.AmountMinor != 12345 || got.Type != "EXPENSE" {
		t.Errorf("get = %+v", got)
	}
	expectProblem(t, srv.Do(t, http.MethodGet, "/v1/transactions/"+uuid.NewString(), u.AccessToken, nil), 404, "not_found")
}

func TestCreateOnArchivedAccountConflicts(t *testing.T) {
	srv := testutil.NewServer(t)
	u := srv.CreateUser(t, testutil.UserOpts{Onboarded: true})
	acct := newAccount(t, srv, u.ID, "Old bank", "INR", 1000)
	if _, err := srv.Deps.Pool.Exec(context.Background(), `UPDATE accounts SET archived_at = now() WHERE id = $1`, acct); err != nil {
		t.Fatal(err)
	}
	r := srv.Do(t, http.MethodPost, "/v1/transactions", u.AccessToken, body(acct, systemCategory(t, srv, "food"), "EXPENSE", 100, "2026-10-01"))
	expectProblem(t, r, 409, "conflict")
	srv.AssertBalance(t, acct, 1000)
}

func TestUpdate(t *testing.T) {
	srv := testutil.NewServer(t)
	u := srv.CreateUser(t, testutil.UserOpts{Onboarded: true})
	food := systemCategory(t, srv, "food")
	travel := systemCategory(t, srv, "travel")
	salary := systemCategory(t, srv, "salary")
	cash := u.DefaultAccountID
	bank := newAccount(t, srv, u.ID, "Bank", "INR", 100000)

	c := create(t, srv, u.AccessToken, body(cash, food, "EXPENSE", 1000, "2026-10-01", map[string]any{"description": "Snacks"}))
	srv.AssertBalance(t, cash, -1000)
	path := "/v1/transactions/" + c.Transaction.ID.String()

	t.Run("amount change", func(t *testing.T) {
		r := srv.Do(t, http.MethodPatch, path, u.AccessToken, map[string]any{"amount_minor": 2500})
		if r.Status != http.StatusOK {
			t.Fatalf("%d %s", r.Status, r.Body)
		}
		var up updated
		r.JSON(t, &up)
		if up.Transaction.AmountMinor != 2500 || len(up.Accounts) != 1 || up.Accounts[0].ID != cash || up.Accounts[0].BalanceMinor != -2500 {
			t.Errorf("update = %+v", up)
		}
		if *up.Transaction.Description != "Snacks" || up.Transaction.OccurredOn != "2026-10-01" {
			t.Errorf("omitted fields changed: %+v", up.Transaction)
		}
		srv.AssertBalance(t, cash, -2500)
	})

	t.Run("type flip needs a matching category", func(t *testing.T) {
		r := srv.Do(t, http.MethodPatch, path, u.AccessToken, map[string]any{"type": "INCOME"})
		p := expectProblem(t, r, 422, "validation_failed")
		if !hasField(p, "category_id") {
			t.Errorf("errors = %+v", p.Errors)
		}
		srv.AssertBalance(t, cash, -2500)
	})

	t.Run("type flip", func(t *testing.T) {
		r := srv.Do(t, http.MethodPatch, path, u.AccessToken, map[string]any{"type": "INCOME", "category_id": salary})
		if r.Status != http.StatusOK {
			t.Fatalf("%d %s", r.Status, r.Body)
		}
		var up updated
		r.JSON(t, &up)
		if up.Transaction.Type != "INCOME" || up.Accounts[0].BalanceMinor != 2500 {
			t.Errorf("update = %+v", up)
		}
		srv.AssertBalance(t, cash, 2500)
	})

	t.Run("wrong-kind category", func(t *testing.T) {
		r := srv.Do(t, http.MethodPatch, path, u.AccessToken, map[string]any{"category_id": travel})
		p := expectProblem(t, r, 422, "validation_failed")
		if !hasField(p, "category_id") {
			t.Errorf("errors = %+v", p.Errors)
		}
		srv.AssertBalance(t, cash, 2500)
	})

	t.Run("move between accounts", func(t *testing.T) {
		r := srv.Do(t, http.MethodPatch, path, u.AccessToken, map[string]any{
			"account_id": bank, "type": "EXPENSE", "category_id": travel, "amount_minor": 4000,
		})
		if r.Status != http.StatusOK {
			t.Fatalf("%d %s", r.Status, r.Body)
		}
		var up updated
		r.JSON(t, &up)
		if up.Transaction.AccountID != bank || len(up.Accounts) != 2 {
			t.Fatalf("update = %+v", up)
		}
		bal := map[uuid.UUID]int64{}
		for _, a := range up.Accounts {
			bal[a.ID] = a.BalanceMinor
		}
		if bal[cash] != 0 || bal[bank] != 100000-4000 {
			t.Errorf("balances in response = %v", bal)
		}
		srv.AssertBalance(t, cash, 0)
		srv.AssertBalance(t, bank, 96000)
	})

	t.Run("clear description and change date", func(t *testing.T) {
		r := srv.Do(t, http.MethodPatch, path, u.AccessToken, map[string]any{"description": "", "occurred_on": "2026-09-30"})
		if r.Status != http.StatusOK {
			t.Fatalf("%d %s", r.Status, r.Body)
		}
		var up updated
		r.JSON(t, &up)
		if up.Transaction.Description != nil || up.Transaction.OccurredOn != "2026-09-30" || len(up.Accounts) != 1 {
			t.Errorf("update = %+v", up)
		}
		srv.AssertBalance(t, cash, 0)
		srv.AssertBalance(t, bank, 96000)
	})

	t.Run("move to another currency", func(t *testing.T) {
		jpy := newAccount(t, srv, u.ID, "Yen wallet", "JPY", 0)
		expectProblem(t, srv.Do(t, http.MethodPatch, path, u.AccessToken, map[string]any{"account_id": jpy}), 409, "currency_mismatch")
		srv.AssertBalance(t, bank, 96000)
		srv.AssertBalance(t, jpy, 0)
	})

	t.Run("move to archived account", func(t *testing.T) {
		arch := newAccount(t, srv, u.ID, "Archived", "INR", 0)
		if _, err := srv.Deps.Pool.Exec(context.Background(), `UPDATE accounts SET archived_at = now() WHERE id = $1`, arch); err != nil {
			t.Fatal(err)
		}
		expectProblem(t, srv.Do(t, http.MethodPatch, path, u.AccessToken, map[string]any{"account_id": arch}), 409, "conflict")
		srv.AssertBalance(t, bank, 96000)
	})

	t.Run("unknown account and transaction", func(t *testing.T) {
		expectProblem(t, srv.Do(t, http.MethodPatch, path, u.AccessToken, map[string]any{"account_id": uuid.New()}), 404, "not_found")
		expectProblem(t, srv.Do(t, http.MethodPatch, "/v1/transactions/"+uuid.NewString(), u.AccessToken, map[string]any{"amount_minor": 1}), 404, "not_found")
		expectProblem(t, srv.Do(t, http.MethodPatch, path, u.AccessToken, map[string]any{"amount_minor": 0}), 422, "validation_failed")
		expectProblem(t, srv.Do(t, http.MethodPatch, path, u.AccessToken, map[string]any{"currency": "USD"}), 400, "bad_request")
		srv.AssertBalance(t, bank, 96000)
	})
}

func TestDelete(t *testing.T) {
	srv := testutil.NewServer(t)
	u := srv.CreateUser(t, testutil.UserOpts{Onboarded: true})
	food := systemCategory(t, srv, "food")
	a := create(t, srv, u.AccessToken, body(u.DefaultAccountID, food, "EXPENSE", 700, "2026-10-01"))
	b := create(t, srv, u.AccessToken, body(u.DefaultAccountID, systemCategory(t, srv, "gift"), "INCOME", 5000, "2026-10-01"))
	srv.AssertBalance(t, u.DefaultAccountID, 4300)

	r := srv.Do(t, http.MethodDelete, "/v1/transactions/"+a.Transaction.ID.String(), u.AccessToken, nil)
	if r.Status != http.StatusNoContent || len(r.Body) != 0 {
		t.Fatalf("delete: %d %s", r.Status, r.Body)
	}
	srv.AssertBalance(t, u.DefaultAccountID, 5000)
	expectProblem(t, srv.Do(t, http.MethodDelete, "/v1/transactions/"+a.Transaction.ID.String(), u.AccessToken, nil), 404, "not_found")
	expectProblem(t, srv.Do(t, http.MethodGet, "/v1/transactions/"+a.Transaction.ID.String(), u.AccessToken, nil), 404, "not_found")

	if r := srv.Do(t, http.MethodDelete, "/v1/transactions/"+b.Transaction.ID.String(), u.AccessToken, nil); r.Status != http.StatusNoContent {
		t.Fatalf("delete: %d %s", r.Status, r.Body)
	}
	srv.AssertBalance(t, u.DefaultAccountID, 0)
}

// Transfers are not built yet; the legs are inserted directly to check that deleting one leg deletes the
// whole transfer (docs/API.md) and that legs cannot be edited.
func TestTransferLegs(t *testing.T) {
	srv := testutil.NewServer(t)
	u := srv.CreateUser(t, testutil.UserOpts{Onboarded: true})
	cash := u.DefaultAccountID
	bank := newAccount(t, srv, u.ID, "Bank", "INR", 10000)
	ctx := context.Background()
	transfer := uuid.New()
	var outID uuid.UUID
	err := db.InTx(ctx, srv.Deps.Pool, func(q *db.Queries) error {
		out, err := q.InsertTransaction(ctx, db.InsertTransactionParams{UserID: u.ID, AccountID: bank, Type: "TRANSFER_OUT",
			AmountMinor: 3000, Currency: "INR", OccurredOn: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), InputMethod: "MANUAL", TransferID: &transfer})
		if err != nil {
			return err
		}
		outID = out.ID
		if _, err := q.InsertTransaction(ctx, db.InsertTransactionParams{UserID: u.ID, AccountID: cash, Type: "TRANSFER_IN",
			AmountMinor: 3000, Currency: "INR", OccurredOn: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), InputMethod: "MANUAL", TransferID: &transfer}); err != nil {
			return err
		}
		if _, err := q.ApplyBalanceDelta(ctx, db.ApplyBalanceDeltaParams{Delta: -3000, ID: bank, UserID: u.ID}); err != nil {
			return err
		}
		_, err = q.ApplyBalanceDelta(ctx, db.ApplyBalanceDeltaParams{Delta: 3000, ID: cash, UserID: u.ID})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	srv.AssertBalance(t, bank, 7000)
	srv.AssertBalance(t, cash, 3000)

	expectProblem(t, srv.Do(t, http.MethodPatch, "/v1/transactions/"+outID.String(), u.AccessToken, map[string]any{"amount_minor": 1}), 409, "conflict")
	srv.AssertBalance(t, bank, 7000)

	if r := srv.Do(t, http.MethodDelete, "/v1/transactions/"+outID.String(), u.AccessToken, nil); r.Status != http.StatusNoContent {
		t.Fatalf("delete leg: %d %s", r.Status, r.Body)
	}
	var n int
	if err := srv.Deps.Pool.QueryRow(ctx, `SELECT count(*) FROM transactions WHERE transfer_id = $1`, transfer).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("%d transfer legs left, want 0", n)
	}
	srv.AssertBalance(t, bank, 10000)
	srv.AssertBalance(t, cash, 0)
}

func TestConcurrentCreatesKeepExactBalance(t *testing.T) {
	srv := testutil.NewServer(t)
	u := srv.CreateUser(t, testutil.UserOpts{Onboarded: true})
	food := systemCategory(t, srv, "food")
	const n = 20
	var wg sync.WaitGroup
	statuses := make([]int, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r := srv.Do(t, http.MethodPost, "/v1/transactions", u.AccessToken, body(u.DefaultAccountID, food, "EXPENSE", int64(100+i), "2026-10-01"))
			statuses[i] = r.Status
		}(i)
	}
	wg.Wait()
	var want int64
	for i := 0; i < n; i++ {
		if statuses[i] != http.StatusCreated {
			t.Errorf("request %d: status %d", i, statuses[i])
		}
		want -= int64(100 + i)
	}
	srv.AssertBalance(t, u.DefaultAccountID, want)
}

// ---------------------------------------------------------------- validation

func TestCreateValidation(t *testing.T) {
	srv := testutil.NewServer(t)
	u := srv.CreateUser(t, testutil.UserOpts{Onboarded: true})
	food := systemCategory(t, srv, "food")
	salary := systemCategory(t, srv, "salary")
	acct := u.DefaultAccountID

	t.Run("amount zero", func(t *testing.T) {
		p := expectProblem(t, srv.Do(t, http.MethodPost, "/v1/transactions", u.AccessToken, body(acct, food, "EXPENSE", 0, "2026-10-01")), 422, "validation_failed")
		if !hasField(p, "amount_minor") {
			t.Errorf("errors = %+v", p.Errors)
		}
	})
	t.Run("amount over max", func(t *testing.T) {
		p := expectProblem(t, srv.Do(t, http.MethodPost, "/v1/transactions", u.AccessToken, body(acct, food, "EXPENSE", 1_000_000_000_000_000, "2026-10-01")), 422, "validation_failed")
		if !hasField(p, "amount_minor") {
			t.Errorf("errors = %+v", p.Errors)
		}
	})
	t.Run("wrong-kind category", func(t *testing.T) {
		p := expectProblem(t, srv.Do(t, http.MethodPost, "/v1/transactions", u.AccessToken, body(acct, salary, "EXPENSE", 100, "2026-10-01")), 422, "validation_failed")
		if !hasField(p, "category_id") {
			t.Errorf("errors = %+v", p.Errors)
		}
	})
	t.Run("unknown category", func(t *testing.T) {
		p := expectProblem(t, srv.Do(t, http.MethodPost, "/v1/transactions", u.AccessToken, body(acct, uuid.New(), "EXPENSE", 100, "2026-10-01")), 422, "validation_failed")
		if !hasField(p, "category_id") {
			t.Errorf("errors = %+v", p.Errors)
		}
	})
	t.Run("transfer type not allowed", func(t *testing.T) {
		expectProblem(t, srv.Do(t, http.MethodPost, "/v1/transactions", u.AccessToken, body(acct, food, "TRANSFER_OUT", 100, "2026-10-01")), 422, "validation_failed")
	})
	t.Run("unknown field", func(t *testing.T) {
		expectProblem(t, srv.Do(t, http.MethodPost, "/v1/transactions", u.AccessToken, body(acct, food, "EXPENSE", 100, "2026-10-01", map[string]any{"currency": "USD"})), 400, "bad_request")
	})
	t.Run("malformed json", func(t *testing.T) {
		expectProblem(t, srv.Do(t, http.MethodPost, "/v1/transactions", u.AccessToken, `{"amount_minor":`), 400, "bad_request")
	})
	t.Run("bad date", func(t *testing.T) {
		p := expectProblem(t, srv.Do(t, http.MethodPost, "/v1/transactions", u.AccessToken, body(acct, food, "EXPENSE", 100, "2026-13-45")), 422, "validation_failed")
		if !hasField(p, "occurred_on") {
			t.Errorf("errors = %+v", p.Errors)
		}
	})
	t.Run("missing required", func(t *testing.T) {
		expectProblem(t, srv.Do(t, http.MethodPost, "/v1/transactions", u.AccessToken, map[string]any{"account_id": acct}), 422, "validation_failed")
	})
	t.Run("unknown account", func(t *testing.T) {
		expectProblem(t, srv.Do(t, http.MethodPost, "/v1/transactions", u.AccessToken, body(uuid.New(), food, "EXPENSE", 100, "2026-10-01")), 404, "not_found")
	})
	t.Run("description too long", func(t *testing.T) {
		long := make([]byte, 201)
		for i := range long {
			long[i] = 'x'
		}
		expectProblem(t, srv.Do(t, http.MethodPost, "/v1/transactions", u.AccessToken, body(acct, food, "EXPENSE", 100, "2026-10-01", map[string]any{"description": string(long)})), 422, "validation_failed")
	})
	srv.AssertBalance(t, acct, 0)
}

// ---------------------------------------------------------------- list

func TestKeysetPagination(t *testing.T) {
	srv := testutil.NewServer(t)
	u := srv.CreateUser(t, testutil.UserOpts{Onboarded: true})
	food := systemCategory(t, srv, "food")
	// 8 rows; several share a date so the created_at/id tie-breakers matter.
	dates := []string{"2026-10-05", "2026-10-05", "2026-10-05", "2026-10-04", "2026-10-06", "2026-10-04", "2026-10-01", "2026-10-05"}
	want := map[uuid.UUID]bool{}
	var sum int64
	for i, d := range dates {
		c := create(t, srv, u.AccessToken, body(u.DefaultAccountID, food, "EXPENSE", int64(i+1), d))
		want[c.Transaction.ID] = true
		sum += int64(i + 1)
		srv.AssertBalance(t, u.DefaultAccountID, -sum)
	}

	var all []txn
	q := url.Values{"limit": {"3"}}
	pages := 0
	for {
		p := list(t, srv, u.AccessToken, q)
		pages++
		if len(p.Data) > 3 {
			t.Fatalf("page has %d rows", len(p.Data))
		}
		all = append(all, p.Data...)
		if p.NextCursor == nil {
			break
		}
		if pages > 10 {
			t.Fatal("too many pages")
		}
		q.Set("cursor", *p.NextCursor)
	}
	if pages != 3 || len(all) != len(dates) {
		t.Fatalf("pages=%d rows=%d", pages, len(all))
	}
	seen := map[uuid.UUID]bool{}
	for i, r := range all {
		if seen[r.ID] || !want[r.ID] {
			t.Fatalf("row %d (%s) duplicated or unexpected", i, r.ID)
		}
		seen[r.ID] = true
		if i > 0 {
			prev := all[i-1]
			ordered := prev.OccurredOn > r.OccurredOn ||
				(prev.OccurredOn == r.OccurredOn && prev.CreatedAt.After(r.CreatedAt)) ||
				(prev.OccurredOn == r.OccurredOn && prev.CreatedAt.Equal(r.CreatedAt) && prev.ID.String() > r.ID.String())
			if !ordered {
				t.Errorf("rows %d and %d out of order: %+v then %+v", i-1, i, prev, r)
			}
		}
	}
	// Same order as the database.
	rows, err := srv.Deps.Pool.Query(context.Background(),
		`SELECT id FROM transactions WHERE user_id = $1 ORDER BY occurred_on DESC, created_at DESC, id DESC`, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	i := 0
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		if all[i].ID != id {
			t.Errorf("position %d: got %s want %s", i, all[i].ID, id)
		}
		i++
	}

	// Exactly a multiple of the page size: the last full page has next_cursor null only if nothing follows.
	if p := list(t, srv, u.AccessToken, url.Values{"limit": {"8"}}); p.NextCursor != nil || len(p.Data) != 8 {
		t.Errorf("limit=8: rows=%d next=%v", len(p.Data), p.NextCursor)
	}
	if p := list(t, srv, u.AccessToken, url.Values{}); p.NextCursor != nil || len(p.Data) != 8 {
		t.Errorf("default limit: rows=%d next=%v", len(p.Data), p.NextCursor)
	}
}

func TestListFilters(t *testing.T) {
	srv := testutil.NewServer(t)
	u := srv.CreateUser(t, testutil.UserOpts{Onboarded: true})
	food := systemCategory(t, srv, "food")
	travel := systemCategory(t, srv, "travel")
	salary := systemCategory(t, srv, "salary")
	cash := u.DefaultAccountID
	bank := newAccount(t, srv, u.ID, "Bank", "INR", 0)

	pct := create(t, srv, u.AccessToken, body(cash, food, "EXPENSE", 100, "2026-10-01", map[string]any{"description": "100% Cotton shirt"}))
	plain := create(t, srv, u.AccessToken, body(cash, food, "EXPENSE", 200, "2026-10-02", map[string]any{"description": "100 cotton socks"}))
	under := create(t, srv, u.AccessToken, body(bank, travel, "EXPENSE", 300, "2026-10-03", map[string]any{"description": "train 50_50"}))
	inc := create(t, srv, u.AccessToken, body(bank, salary, "INCOME", 9000, "2026-10-31"))
	srv.AssertBalance(t, cash, -300)
	srv.AssertBalance(t, bank, 8700)

	check := func(name string, q url.Values, want ...txn) {
		t.Helper()
		got := listAll(t, srv, u.AccessToken, q)
		if len(got) != len(want) {
			t.Errorf("%s: got %d rows, want %d", name, len(got), len(want))
			return
		}
		g := ids(got)
		for _, w := range want {
			if !g[w.ID] {
				t.Errorf("%s: missing %s", name, w.ID)
			}
		}
	}
	check("type", url.Values{"type": {"INCOME"}}, inc.Transaction)
	check("account", url.Values{"account_id": {bank.String()}}, under.Transaction, inc.Transaction)
	check("category", url.Values{"category_id": {food.String()}}, pct.Transaction, plain.Transaction)
	check("range inclusive", url.Values{"from": {"2026-10-02"}, "to": {"2026-10-03"}}, plain.Transaction, under.Transaction)
	check("from only", url.Values{"from": {"2026-10-03"}}, under.Transaction, inc.Transaction)
	check("to only", url.Values{"to": {"2026-10-01"}}, pct.Transaction)
	check("q case-insensitive", url.Values{"q": {"COTTON"}}, pct.Transaction, plain.Transaction)
	check("q percent literal", url.Values{"q": {"%"}}, pct.Transaction)
	check("q underscore literal", url.Values{"q": {"_"}}, under.Transaction)
	check("combined", url.Values{"type": {"EXPENSE"}, "account_id": {cash.String()}, "q": {"socks"}}, plain.Transaction)
	check("paged filter", url.Values{"account_id": {cash.String()}, "limit": {"1"}}, pct.Transaction, plain.Transaction)

	p := expectProblem(t, srv.Do(t, http.MethodGet, "/v1/transactions?from=2026-10-05&to=2026-10-01", u.AccessToken, nil), 422, "validation_failed")
	if len(p.Errors) == 0 {
		t.Errorf("from>to: no field errors")
	}
	expectProblem(t, srv.Do(t, http.MethodGet, "/v1/transactions?limit=0", u.AccessToken, nil), 422, "validation_failed")
	expectProblem(t, srv.Do(t, http.MethodGet, "/v1/transactions?limit=201", u.AccessToken, nil), 422, "validation_failed")
	expectProblem(t, srv.Do(t, http.MethodGet, "/v1/transactions?type=REFUND", u.AccessToken, nil), 422, "validation_failed")
}

func TestBadCursor(t *testing.T) {
	srv := testutil.NewServer(t)
	u := srv.CreateUser(t, testutil.UserOpts{Onboarded: true})
	food := systemCategory(t, srv, "food")
	for i := 0; i < 3; i++ {
		create(t, srv, u.AccessToken, body(u.DefaultAccountID, food, "EXPENSE", 10, "2026-10-01"))
	}
	enc := func(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
	for name, c := range map[string]string{
		"not base64":   "!!!***",
		"not json":     enc("hello"),
		"bad date":     enc(`{"d":"2026-99-01","c":"2026-10-01T00:00:00Z","i":"` + uuid.NewString() + `","f":""}`),
		"bad time":     enc(`{"d":"2026-10-01","c":"yesterday","i":"` + uuid.NewString() + `","f":""}`),
		"bad id":       enc(`{"d":"2026-10-01","c":"2026-10-01T00:00:00Z","i":"nope","f":""}`),
		"extra field":  enc(`{"d":"2026-10-01","c":"2026-10-01T00:00:00Z","i":"` + uuid.NewString() + `","f":"","x":1}`),
		"empty object": enc(`{}`),
	} {
		t.Run(name, func(t *testing.T) {
			expectProblem(t, srv.Do(t, http.MethodGet, "/v1/transactions?cursor="+url.QueryEscape(c), u.AccessToken, nil), 400, "bad_request")
		})
	}
	t.Run("different filters", func(t *testing.T) {
		p := list(t, srv, u.AccessToken, url.Values{"limit": {"1"}})
		if p.NextCursor == nil {
			t.Fatal("expected a next cursor")
		}
		r := srv.Do(t, http.MethodGet, "/v1/transactions?type=EXPENSE&limit=1&cursor="+url.QueryEscape(*p.NextCursor), u.AccessToken, nil)
		expectProblem(t, r, 400, "bad_request")
		// Same filters with another page size is fine.
		r = srv.Do(t, http.MethodGet, "/v1/transactions?limit=5&cursor="+url.QueryEscape(*p.NextCursor), u.AccessToken, nil)
		if r.Status != http.StatusOK {
			t.Fatalf("%d %s", r.Status, r.Body)
		}
	})
}

// ---------------------------------------------------------------- authorization

func TestAuthorization(t *testing.T) {
	srv := testutil.NewServer(t)
	a := srv.CreateUser(t, testutil.UserOpts{Onboarded: true})
	b := srv.CreateUser(t, testutil.UserOpts{Onboarded: true})
	food := systemCategory(t, srv, "food")
	aCustom := customCategory(t, srv, a.ID, "A's hobby", "EXPENSE")
	aBank := newAccount(t, srv, a.ID, "A bank", "INR", 50000)

	// A's own custom category works for A.
	at := create(t, srv, a.AccessToken, body(a.DefaultAccountID, aCustom, "EXPENSE", 1500, "2026-10-01", map[string]any{"description": "secret-of-A"}))
	create(t, srv, a.AccessToken, body(aBank, food, "EXPENSE", 2000, "2026-10-02", map[string]any{"description": "secret-of-A"}))
	srv.AssertBalance(t, a.DefaultAccountID, -1500)
	srv.AssertBalance(t, aBank, 48000)
	bt := create(t, srv, b.AccessToken, body(b.DefaultAccountID, food, "EXPENSE", 300, "2026-10-01", map[string]any{"description": "mine-of-B"}))
	srv.AssertBalance(t, b.DefaultAccountID, -300)

	path := "/v1/transactions/" + at.Transaction.ID.String()
	t.Run("get", func(t *testing.T) {
		expectProblem(t, srv.Do(t, http.MethodGet, path, b.AccessToken, nil), 404, "not_found")
	})
	t.Run("patch", func(t *testing.T) {
		expectProblem(t, srv.Do(t, http.MethodPatch, path, b.AccessToken, map[string]any{"amount_minor": 1}), 404, "not_found")
	})
	t.Run("patch own transaction onto A's account", func(t *testing.T) {
		expectProblem(t, srv.Do(t, http.MethodPatch, "/v1/transactions/"+bt.Transaction.ID.String(), b.AccessToken, map[string]any{"account_id": aBank}), 404, "not_found")
	})
	t.Run("patch own transaction with A's category", func(t *testing.T) {
		expectProblem(t, srv.Do(t, http.MethodPatch, "/v1/transactions/"+bt.Transaction.ID.String(), b.AccessToken, map[string]any{"category_id": aCustom}), 422, "validation_failed")
	})
	t.Run("delete", func(t *testing.T) {
		expectProblem(t, srv.Do(t, http.MethodDelete, path, b.AccessToken, nil), 404, "not_found")
	})
	t.Run("create on A's account", func(t *testing.T) {
		expectProblem(t, srv.Do(t, http.MethodPost, "/v1/transactions", b.AccessToken, body(a.DefaultAccountID, food, "EXPENSE", 100, "2026-10-01")), 404, "not_found")
		expectProblem(t, srv.Do(t, http.MethodPost, "/v1/transactions", b.AccessToken, body(aBank, food, "EXPENSE", 100, "2026-10-01")), 404, "not_found")
	})
	t.Run("create with A's custom category", func(t *testing.T) {
		p := expectProblem(t, srv.Do(t, http.MethodPost, "/v1/transactions", b.AccessToken, body(b.DefaultAccountID, aCustom, "EXPENSE", 100, "2026-10-01")), 422, "validation_failed")
		if !hasField(p, "category_id") {
			t.Errorf("errors = %+v", p.Errors)
		}
	})
	t.Run("list never shows A's rows", func(t *testing.T) {
		for _, q := range []url.Values{
			{},
			{"account_id": {a.DefaultAccountID.String()}},
			{"account_id": {aBank.String()}},
			{"category_id": {aCustom.String()}},
			{"q": {"secret-of-A"}},
		} {
			rows := listAll(t, srv, b.AccessToken, q)
			for _, r := range rows {
				if r.ID != bt.Transaction.ID {
					t.Errorf("B's list %s contains %s", q.Encode(), r.ID)
				}
			}
		}
	})
	t.Run("export never shows A's rows", func(t *testing.T) {
		r := srv.Do(t, http.MethodGet, "/v1/transactions/export?from=2026-01-01&to=2026-12-31", b.AccessToken, nil)
		if r.Status != http.StatusOK {
			t.Fatalf("%d %s", r.Status, r.Body)
		}
		if s := string(r.Body); strings.Contains(s, "secret-of-A") || !strings.Contains(s, "mine-of-B") {
			t.Errorf("B's export: %s", s)
		}
		expectProblem(t, srv.Do(t, http.MethodGet, "/v1/transactions/export?from=2026-01-01&to=2026-12-31&account_id="+aBank.String(), b.AccessToken, nil), 404, "not_found")
	})

	// A's data is unchanged after all of B's attempts.
	srv.AssertBalance(t, a.DefaultAccountID, -1500)
	srv.AssertBalance(t, aBank, 48000)
	srv.AssertBalance(t, b.DefaultAccountID, -300)
	g := srv.Do(t, http.MethodGet, path, a.AccessToken, nil)
	var got txn
	g.JSON(t, &got)
	if g.Status != http.StatusOK || got.AmountMinor != 1500 || got.AccountID != a.DefaultAccountID {
		t.Errorf("A's transaction changed: %d %+v", g.Status, got)
	}
}

func TestNoToken(t *testing.T) {
	srv := testutil.NewServer(t)
	id := uuid.NewString()
	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/v1/transactions"},
		{http.MethodPost, "/v1/transactions"},
		{http.MethodGet, "/v1/transactions/" + id},
		{http.MethodPatch, "/v1/transactions/" + id},
		{http.MethodDelete, "/v1/transactions/" + id},
		{http.MethodGet, "/v1/transactions/export?from=2026-01-01&to=2026-01-31"},
	} {
		t.Run(fmt.Sprintf("%s %s", c.method, c.path), func(t *testing.T) {
			var b any
			if c.method == http.MethodPost || c.method == http.MethodPatch {
				b = map[string]any{}
			}
			expectProblem(t, srv.Do(t, c.method, c.path, "", b), 401, "unauthorized")
		})
	}
}
