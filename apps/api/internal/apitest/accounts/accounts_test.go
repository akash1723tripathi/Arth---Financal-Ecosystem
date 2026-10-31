package accounts_test

// Integration tests for /v1/accounts and /v1/categories against a real Postgres.

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"arth/api/internal/db"
	"arth/api/internal/httpapi/gen"
	"arth/api/internal/testutil"
)

type problem struct {
	Code             string `json:"code"`
	TransactionCount *int64 `json:"transaction_count"`
	Errors           []struct {
		Field string `json:"field"`
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

func create(t *testing.T, srv *testutil.Server, token string, body map[string]any) gen.Account {
	t.Helper()
	r := srv.Do(t, http.MethodPost, "/v1/accounts", token, body)
	expectStatus(t, r, http.StatusCreated, "")
	var a gen.Account
	r.JSON(t, &a)
	if loc := r.Header.Get("Location"); loc != "/v1/accounts/"+a.Id.String() {
		t.Fatalf("Location = %q", loc)
	}
	return a
}

func list(t *testing.T, srv *testutil.Server, token string) []gen.Account {
	t.Helper()
	r := srv.Do(t, http.MethodGet, "/v1/accounts", token, nil)
	expectStatus(t, r, http.StatusOK, "")
	var l gen.AccountList
	r.JSON(t, &l)
	return l.Data
}

func getAccount(t *testing.T, srv *testutil.Server, token string, id uuid.UUID) gen.Account {
	t.Helper()
	r := srv.Do(t, http.MethodGet, "/v1/accounts/"+id.String(), token, nil)
	expectStatus(t, r, http.StatusOK, "")
	var a gen.Account
	r.JSON(t, &a)
	return a
}

func categories(t *testing.T, srv *testutil.Server, token, query string) []gen.Category {
	t.Helper()
	r := srv.Do(t, http.MethodGet, "/v1/categories"+query, token, nil)
	expectStatus(t, r, http.StatusOK, "")
	var l gen.CategoryList
	r.JSON(t, &l)
	return l.Data
}

// insertExpense inserts an EXPENSE transaction directly and applies its delta to the cached balance.
func insertExpense(t *testing.T, srv *testutil.Server, userID, accountID uuid.UUID, amount int64, currency string) {
	t.Helper()
	ctx := context.Background()
	cat, err := srv.Deps.Q.GetSystemCategoryByKey(ctx, "food")
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

func countTransactions(t *testing.T, srv *testutil.Server, accountID uuid.UUID) int64 {
	t.Helper()
	var n int64
	if err := srv.Deps.Pool.QueryRow(context.Background(), `SELECT count(*) FROM transactions WHERE account_id = $1`, accountID).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func defaults(as []gen.Account) []uuid.UUID {
	var out []uuid.UUID
	for _, a := range as {
		if a.IsDefault {
			out = append(out, a.Id)
		}
	}
	return out
}

func TestNoToken(t *testing.T) {
	srv := testutil.NewServer(t)
	id := uuid.NewString()
	cases := []struct{ method, path string }{
		{http.MethodGet, "/v1/accounts"},
		{http.MethodPost, "/v1/accounts"},
		{http.MethodGet, "/v1/accounts/" + id},
		{http.MethodPatch, "/v1/accounts/" + id},
		{http.MethodDelete, "/v1/accounts/" + id},
		{http.MethodPost, "/v1/accounts/" + id + "/make-default"},
		{http.MethodGet, "/v1/categories"},
	}
	for _, c := range cases {
		t.Run(c.method+" "+c.path, func(t *testing.T) {
			var body any
			if c.method == http.MethodPost || c.method == http.MethodPatch {
				body = map[string]any{"name": "x", "type": "CASH"}
			}
			expectStatus(t, srv.Do(t, c.method, c.path, "", body), http.StatusUnauthorized, "unauthorized")
		})
	}
}

func TestListAndCreate(t *testing.T) {
	srv := testutil.NewServer(t)

	t.Run("fresh user has the default Cash account", func(t *testing.T) {
		u := srv.CreateUser(t)
		as := list(t, srv, u.AccessToken)
		if len(as) != 1 || as[0].Id != u.DefaultAccountID || !as[0].IsDefault || as[0].Name != "Cash" || as[0].Type != gen.AccountType("CASH") {
			t.Fatalf("accounts: %+v", as)
		}
	})

	t.Run("create with opening balance", func(t *testing.T) {
		u := srv.CreateUser(t)
		a := create(t, srv, u.AccessToken, map[string]any{"name": "  HDFC Savings ", "type": "SAVINGS", "opening_balance_minor": 250000})
		if a.Name != "HDFC Savings" || a.Type != gen.AccountType("SAVINGS") || a.Currency != "INR" || a.IsDefault ||
			a.OpeningBalanceMinor != 250000 || a.BalanceMinor != 250000 {
			t.Fatalf("created: %+v", a)
		}
		srv.AssertBalance(t, a.Id, 250000)

		cc := create(t, srv, u.AccessToken, map[string]any{"name": "Card", "type": "CREDIT_CARD", "opening_balance_minor": -12345, "currency": "USD"})
		if cc.Currency != "USD" || cc.BalanceMinor != -12345 {
			t.Fatalf("credit card: %+v", cc)
		}
		srv.AssertBalance(t, cc.Id, -12345)

		z := create(t, srv, u.AccessToken, map[string]any{"name": "Wallet", "type": "CASH"})
		if z.OpeningBalanceMinor != 0 {
			t.Fatalf("default opening: %+v", z)
		}
		srv.AssertBalance(t, z.Id, 0)

		// Default first, then created_at.
		as := list(t, srv, u.AccessToken)
		want := []uuid.UUID{u.DefaultAccountID, a.Id, cc.Id, z.Id}
		if len(as) != len(want) {
			t.Fatalf("list len %d: %+v", len(as), as)
		}
		for i := range want {
			if as[i].Id != want[i] {
				t.Fatalf("order[%d] = %s, want %s", i, as[i].Id, want[i])
			}
		}
		if d := defaults(as); len(d) != 1 || d[0] != u.DefaultAccountID {
			t.Fatalf("defaults: %v", d)
		}
	})

	t.Run("currency defaults to the user currency", func(t *testing.T) {
		u := srv.CreateUser(t, testutil.UserOpts{Onboarded: true, Currency: "EUR", Timezone: "Europe/Berlin"})
		a := create(t, srv, u.AccessToken, map[string]any{"name": "Bank", "type": "BANK"})
		if a.Currency != "EUR" {
			t.Fatalf("currency = %s, want EUR", a.Currency)
		}
	})

	t.Run("validation", func(t *testing.T) {
		u := srv.CreateUser(t)
		cases := []struct {
			name  string
			body  any
			field string
		}{
			{"unsupported currency", map[string]any{"name": "A", "type": "BANK", "currency": "XYZ"}, "currency"},
			{"lower-case currency", map[string]any{"name": "A", "type": "BANK", "currency": "inr"}, "currency"},
			{"bad type", map[string]any{"name": "A", "type": "LOAN"}, "type"},
			{"missing type", map[string]any{"name": "A"}, "type"},
			{"missing name", map[string]any{"type": "BANK"}, "name"},
			{"empty name", map[string]any{"name": "", "type": "BANK"}, "name"},
			{"blank name", map[string]any{"name": "   ", "type": "BANK"}, "name"},
			{"name too long", map[string]any{"name": strings.Repeat("n", 61), "type": "BANK"}, "name"},
			{"opening out of range", map[string]any{"name": "A", "type": "BANK", "opening_balance_minor": int64(-1_000_000_000_000_000)}, "opening_balance_minor"},
			{"opening not integer", map[string]any{"name": "A", "type": "BANK", "opening_balance_minor": 10.5}, "opening_balance_minor"},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				expectField(t, srv.Do(t, http.MethodPost, "/v1/accounts", u.AccessToken, c.body), c.field)
			})
		}
		t.Run("unknown field", func(t *testing.T) {
			r := srv.Do(t, http.MethodPost, "/v1/accounts", u.AccessToken, map[string]any{"name": "A", "type": "BANK", "is_default": true})
			expectStatus(t, r, http.StatusBadRequest, "bad_request")
		})
		t.Run("malformed json", func(t *testing.T) {
			expectStatus(t, srv.Do(t, http.MethodPost, "/v1/accounts", u.AccessToken, `{"name":"A",`), http.StatusBadRequest, "bad_request")
		})
		if as := list(t, srv, u.AccessToken); len(as) != 1 {
			t.Fatalf("rejected creates inserted rows: %+v", as)
		}
	})
}

func TestGetAndUpdate(t *testing.T) {
	srv := testutil.NewServer(t)
	u := srv.CreateUser(t)
	a := create(t, srv, u.AccessToken, map[string]any{"name": "Bank", "type": "BANK", "opening_balance_minor": 500})

	t.Run("get", func(t *testing.T) {
		got := getAccount(t, srv, u.AccessToken, a.Id)
		if got.Id != a.Id || got.Name != "Bank" || got.BalanceMinor != 500 {
			t.Fatalf("get: %+v", got)
		}
		expectStatus(t, srv.Do(t, http.MethodGet, "/v1/accounts/"+uuid.NewString(), u.AccessToken, nil), http.StatusNotFound, "not_found")
		expectField(t, srv.Do(t, http.MethodGet, "/v1/accounts/not-a-uuid", u.AccessToken, nil), "path.id")
	})

	t.Run("patch name and type", func(t *testing.T) {
		r := srv.Do(t, http.MethodPatch, "/v1/accounts/"+a.Id.String(), u.AccessToken, map[string]any{"name": " Salary ", "type": "SAVINGS"})
		expectStatus(t, r, http.StatusOK, "")
		var got gen.Account
		r.JSON(t, &got)
		if got.Name != "Salary" || got.Type != gen.AccountType("SAVINGS") || got.Currency != "INR" || got.BalanceMinor != 500 {
			t.Fatalf("patched: %+v", got)
		}
		r = srv.Do(t, http.MethodPatch, "/v1/accounts/"+a.Id.String(), u.AccessToken, map[string]any{"type": "BANK"})
		expectStatus(t, r, http.StatusOK, "")
		r.JSON(t, &got)
		if got.Name != "Salary" || got.Type != gen.AccountType("BANK") {
			t.Fatalf("partial patch: %+v", got)
		}
		expectStatus(t, srv.Do(t, http.MethodPatch, "/v1/accounts/"+a.Id.String(), u.AccessToken, map[string]any{}), http.StatusOK, "")
		srv.AssertBalance(t, a.Id, 500)
	})

	t.Run("patch validation", func(t *testing.T) {
		p := "/v1/accounts/" + a.Id.String()
		expectField(t, srv.Do(t, http.MethodPatch, p, u.AccessToken, map[string]any{"name": strings.Repeat("x", 61)}), "name")
		expectField(t, srv.Do(t, http.MethodPatch, p, u.AccessToken, map[string]any{"name": "  "}), "name")
		expectField(t, srv.Do(t, http.MethodPatch, p, u.AccessToken, map[string]any{"type": "GOLD"}), "type")
		expectStatus(t, srv.Do(t, http.MethodPatch, p, u.AccessToken, map[string]any{"currency": "USD"}), http.StatusBadRequest, "bad_request")
		expectStatus(t, srv.Do(t, http.MethodPatch, p, u.AccessToken, map[string]any{"balance_minor": 1}), http.StatusBadRequest, "bad_request")
		expectStatus(t, srv.Do(t, http.MethodPatch, "/v1/accounts/"+uuid.NewString(), u.AccessToken, map[string]any{"name": "x"}), http.StatusNotFound, "not_found")
		if got := getAccount(t, srv, u.AccessToken, a.Id); got.Name != "Salary" || got.Currency != "INR" {
			t.Fatalf("rejected patches changed the account: %+v", got)
		}
	})
}

func TestMakeDefault(t *testing.T) {
	srv := testutil.NewServer(t)
	u := srv.CreateUser(t)
	b := create(t, srv, u.AccessToken, map[string]any{"name": "Bank", "type": "BANK"})

	r := srv.Do(t, http.MethodPost, "/v1/accounts/"+b.Id.String()+"/make-default", u.AccessToken, nil)
	expectStatus(t, r, http.StatusOK, "")
	var got gen.Account
	r.JSON(t, &got)
	if got.Id != b.Id || !got.IsDefault {
		t.Fatalf("make-default response: %+v", got)
	}
	as := list(t, srv, u.AccessToken)
	if d := defaults(as); len(d) != 1 || d[0] != b.Id || as[0].Id != b.Id {
		t.Fatalf("after make-default: defaults=%v first=%s", d, as[0].Id)
	}

	// Idempotent.
	expectStatus(t, srv.Do(t, http.MethodPost, "/v1/accounts/"+b.Id.String()+"/make-default", u.AccessToken, nil), http.StatusOK, "")
	if d := defaults(list(t, srv, u.AccessToken)); len(d) != 1 || d[0] != b.Id {
		t.Fatalf("after repeat: %v", d)
	}

	// The old default is now an ordinary account and can be deleted; the new default cannot.
	expectStatus(t, srv.Do(t, http.MethodDelete, "/v1/accounts/"+b.Id.String(), u.AccessToken, nil), http.StatusConflict, "default_account")
	expectStatus(t, srv.Do(t, http.MethodDelete, "/v1/accounts/"+u.DefaultAccountID.String(), u.AccessToken, nil), http.StatusNoContent, "")

	expectStatus(t, srv.Do(t, http.MethodPost, "/v1/accounts/"+uuid.NewString()+"/make-default", u.AccessToken, nil), http.StatusNotFound, "not_found")
	if d := defaults(list(t, srv, u.AccessToken)); len(d) != 1 || d[0] != b.Id {
		t.Fatalf("404 make-default changed the default: %v", d)
	}
}

// TestMakeDefaultConcurrent fires make-default for many accounts at once: every request must succeed
// (no unique-index violation) and exactly one default must remain.
func TestMakeDefaultConcurrent(t *testing.T) {
	srv := testutil.NewServer(t)
	u := srv.CreateUser(t)
	ids := []uuid.UUID{u.DefaultAccountID}
	for i := 0; i < 7; i++ {
		ids = append(ids, create(t, srv, u.AccessToken, map[string]any{"name": fmt.Sprintf("A%d", i), "type": "BANK"}).Id)
	}
	var wg sync.WaitGroup
	statuses := make([]int, len(ids)*2)
	for i := range statuses {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/accounts/"+ids[i%len(ids)].String()+"/make-default", bytes.NewReader(nil))
			req.Header.Set("Authorization", "Bearer "+u.AccessToken)
			res, err := srv.Client().Do(req)
			if err != nil {
				statuses[i] = -1
				return
			}
			res.Body.Close()
			statuses[i] = res.StatusCode
		}(i)
	}
	wg.Wait()
	for i, s := range statuses {
		if s != http.StatusOK {
			t.Fatalf("request %d: status %d", i, s)
		}
	}
	if d := defaults(list(t, srv, u.AccessToken)); len(d) != 1 {
		t.Fatalf("defaults after concurrent make-default: %v", d)
	}
}

func TestDelete(t *testing.T) {
	srv := testutil.NewServer(t)
	u := srv.CreateUser(t)

	t.Run("default account", func(t *testing.T) {
		expectStatus(t, srv.Do(t, http.MethodDelete, "/v1/accounts/"+u.DefaultAccountID.String(), u.AccessToken, nil), http.StatusConflict, "default_account")
		expectStatus(t, srv.Do(t, http.MethodDelete, "/v1/accounts/"+u.DefaultAccountID.String()+"?cascade=true", u.AccessToken, nil), http.StatusConflict, "default_account")
		getAccount(t, srv, u.AccessToken, u.DefaultAccountID)
	})

	t.Run("empty account", func(t *testing.T) {
		a := create(t, srv, u.AccessToken, map[string]any{"name": "Empty", "type": "CASH", "opening_balance_minor": 100})
		expectStatus(t, srv.Do(t, http.MethodDelete, "/v1/accounts/"+a.Id.String(), u.AccessToken, nil), http.StatusNoContent, "")
		expectStatus(t, srv.Do(t, http.MethodGet, "/v1/accounts/"+a.Id.String(), u.AccessToken, nil), http.StatusNotFound, "not_found")
		expectStatus(t, srv.Do(t, http.MethodDelete, "/v1/accounts/"+a.Id.String(), u.AccessToken, nil), http.StatusNotFound, "not_found")
	})

	t.Run("with transactions needs cascade", func(t *testing.T) {
		a := create(t, srv, u.AccessToken, map[string]any{"name": "Busy", "type": "BANK", "opening_balance_minor": 10000})
		insertExpense(t, srv, u.ID, a.Id, 1500, "INR")
		insertExpense(t, srv, u.ID, a.Id, 500, "INR")
		srv.AssertBalance(t, a.Id, 8000)

		r := srv.Do(t, http.MethodDelete, "/v1/accounts/"+a.Id.String(), u.AccessToken, nil)
		expectStatus(t, r, http.StatusConflict, "account_has_transactions")
		var p problem
		r.JSON(t, &p)
		if p.TransactionCount == nil || *p.TransactionCount != 2 {
			t.Fatalf("transaction_count missing or wrong: %s", r.Body)
		}
		expectStatus(t, srv.Do(t, http.MethodDelete, "/v1/accounts/"+a.Id.String()+"?cascade=false", u.AccessToken, nil), http.StatusConflict, "account_has_transactions")
		srv.AssertBalance(t, a.Id, 8000)
		if n := countTransactions(t, srv, a.Id); n != 2 {
			t.Fatalf("409 deleted transactions: %d left", n)
		}

		expectField(t, srv.Do(t, http.MethodDelete, "/v1/accounts/"+a.Id.String()+"?cascade=maybe", u.AccessToken, nil), "query.cascade")

		expectStatus(t, srv.Do(t, http.MethodDelete, "/v1/accounts/"+a.Id.String()+"?cascade=true", u.AccessToken, nil), http.StatusNoContent, "")
		if n := countTransactions(t, srv, a.Id); n != 0 {
			t.Fatalf("cascade left %d transactions", n)
		}
		expectStatus(t, srv.Do(t, http.MethodGet, "/v1/accounts/"+a.Id.String(), u.AccessToken, nil), http.StatusNotFound, "not_found")
		srv.AssertBalance(t, u.DefaultAccountID, 0)
	})
}

// TestAuthorization: user B can never see or change user A's accounts or custom categories.
func TestAuthorization(t *testing.T) {
	srv := testutil.NewServer(t)
	a := srv.CreateUser(t)
	b := srv.CreateUser(t)
	aBank := create(t, srv, a.AccessToken, map[string]any{"name": "A Bank", "type": "BANK", "opening_balance_minor": 777})
	insertExpense(t, srv, a.ID, aBank.Id, 77, "INR")

	for _, id := range []uuid.UUID{a.DefaultAccountID, aBank.Id} {
		p := "/v1/accounts/" + id.String()
		expectStatus(t, srv.Do(t, http.MethodGet, p, b.AccessToken, nil), http.StatusNotFound, "not_found")
		expectStatus(t, srv.Do(t, http.MethodPatch, p, b.AccessToken, map[string]any{"name": "pwned", "type": "CASH"}), http.StatusNotFound, "not_found")
		expectStatus(t, srv.Do(t, http.MethodPost, p+"/make-default", b.AccessToken, nil), http.StatusNotFound, "not_found")
		expectStatus(t, srv.Do(t, http.MethodDelete, p, b.AccessToken, nil), http.StatusNotFound, "not_found")
		expectStatus(t, srv.Do(t, http.MethodDelete, p+"?cascade=true", b.AccessToken, nil), http.StatusNotFound, "not_found")
	}

	// A's data is unchanged.
	got := getAccount(t, srv, a.AccessToken, aBank.Id)
	if got.Name != "A Bank" || got.Type != gen.AccountType("BANK") || got.IsDefault {
		t.Fatalf("A's account changed: %+v", got)
	}
	srv.AssertBalance(t, aBank.Id, 700)
	if n := countTransactions(t, srv, aBank.Id); n != 1 {
		t.Fatalf("A's transactions: %d", n)
	}
	aList := list(t, srv, a.AccessToken)
	if d := defaults(aList); len(d) != 1 || d[0] != a.DefaultAccountID || len(aList) != 2 {
		t.Fatalf("A's accounts changed: %+v", aList)
	}

	// B's own default was not cleared by the failed make-default, and B's list holds only B's account.
	bList := list(t, srv, b.AccessToken)
	if len(bList) != 1 || bList[0].Id != b.DefaultAccountID || !bList[0].IsDefault {
		t.Fatalf("B's accounts: %+v", bList)
	}
	for _, acc := range bList {
		if acc.Id == a.DefaultAccountID || acc.Id == aBank.Id {
			t.Fatalf("B sees A's account %s", acc.Id)
		}
	}

	// Custom categories are private to their owner.
	var catID uuid.UUID
	if err := srv.Deps.Pool.QueryRow(context.Background(),
		`INSERT INTO categories (user_id, name, kind, sort_order) VALUES ($1, 'A Secret', 'EXPENSE', 5) RETURNING id`, a.ID).Scan(&catID); err != nil {
		t.Fatalf("insert category: %v", err)
	}
	aCats := categories(t, srv, a.AccessToken, "")
	if len(aCats) != 22 || aCats[len(aCats)-1].Id != catID || aCats[len(aCats)-1].IsSystem {
		t.Fatalf("A's categories: %d, last=%+v", len(aCats), aCats[len(aCats)-1])
	}
	for _, q := range []string{"", "?kind=EXPENSE"} {
		for _, c := range categories(t, srv, b.AccessToken, q) {
			if c.Id == catID {
				t.Fatalf("B sees A's custom category (query %q)", q)
			}
		}
	}
	if n := len(categories(t, srv, b.AccessToken, "")); n != 21 {
		t.Fatalf("B's categories: %d", n)
	}
}

func TestListCategories(t *testing.T) {
	srv := testutil.NewServer(t)
	u := srv.CreateUser(t)

	all := categories(t, srv, u.AccessToken, "")
	if len(all) != 21 {
		t.Fatalf("system categories: got %d, want 21", len(all))
	}
	keys := map[string]bool{}
	for _, c := range all {
		if !c.IsSystem || c.Key == nil {
			t.Fatalf("expected system category with key: %+v", c)
		}
		keys[*c.Key] = true
	}
	for _, k := range []string{"food", "groceries", "rent"} {
		if !keys[k] {
			t.Fatalf("missing system category %q", k)
		}
	}

	for _, kind := range []string{"INCOME", "EXPENSE"} {
		t.Run(kind, func(t *testing.T) {
			cs := categories(t, srv, u.AccessToken, "?kind="+kind)
			if len(cs) == 0 {
				t.Fatal("no categories")
			}
			for _, c := range cs {
				if string(c.Kind) != kind {
					t.Fatalf("kind filter %s returned %+v", kind, c)
				}
			}
		})
	}
	if in, ex := len(categories(t, srv, u.AccessToken, "?kind=INCOME")), len(categories(t, srv, u.AccessToken, "?kind=EXPENSE")); in+ex != 21 || in != 6 || ex != 15 {
		t.Fatalf("income=%d expense=%d", in, ex)
	}
	expectField(t, srv.Do(t, http.MethodGet, "/v1/categories?kind=TRANSFER_IN", u.AccessToken, nil), "query.kind")
}
