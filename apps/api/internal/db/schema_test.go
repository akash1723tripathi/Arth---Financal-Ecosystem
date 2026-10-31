package db_test

// Integration tests for the schema (db/migrations) and the sqlc queries (db/queries) against REAL Postgres.
// Each run creates a throwaway database arth_dbtest_<random> on the TEST_DATABASE_URL server, migrates it with
// goose, and drops it at the end. The shared arth / arth_test databases are never migrated or modified here.

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"arth/api/db/migrations"
	"arth/api/internal/db"
)

const (
	pgUniqueViolation     = "23505"
	pgForeignKeyViolation = "23503"
	pgCheckViolation      = "23514"
)

// testDatabaseURL returns TEST_DATABASE_URL from the environment, else from apps/api/.env. Never printed.
func testDatabaseURL(t *testing.T) string {
	t.Helper()
	if v := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL")); v != "" {
		return v
	}
	f, err := os.Open(filepath.Join("..", "..", ".env"))
	if err != nil {
		t.Fatalf("TEST_DATABASE_URL is not set and apps/api/.env cannot be opened")
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		k, v, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(k) != "TEST_DATABASE_URL" {
			continue
		}
		v = strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
			v = v[1 : len(v)-1]
		}
		if v != "" {
			return v
		}
	}
	t.Fatalf("TEST_DATABASE_URL is not set (env or apps/api/.env)")
	return ""
}

type env struct {
	pool     *pgxpool.Pool
	q        *db.Queries
	provider *goose.Provider
}

// setup creates and migrates a throwaway database and registers its removal.
func setup(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()
	baseURL := testDatabaseURL(t)

	admin, err := pgx.Connect(ctx, baseURL)
	if err != nil {
		t.Fatalf("connect to test server failed (check TEST_DATABASE_URL / pnpm db:up): %v", redact(err))
	}
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	name := "arth_dbtest_" + hex.EncodeToString(b)
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		_ = admin.Close(ctx)
		t.Fatalf("create throwaway database: %v", redact(err))
	}
	t.Logf("throwaway database %s", name)

	connCfg, err := pgx.ParseConfig(baseURL)
	if err != nil {
		t.Fatal("parse TEST_DATABASE_URL: invalid connection string")
	}
	connCfg.Database = name
	sqlDB := stdlib.OpenDB(*connCfg)

	poolCfg, err := pgxpool.ParseConfig(baseURL)
	if err != nil {
		t.Fatal("parse TEST_DATABASE_URL: invalid connection string")
	}
	poolCfg.ConnConfig.Database = name
	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		t.Fatalf("open pool: %v", redact(err))
	}

	t.Cleanup(func() {
		pool.Close()
		_ = sqlDB.Close()
		if _, err := admin.Exec(context.Background(),
			"DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)"); err != nil {
			t.Errorf("drop throwaway database %s: %v", name, redact(err))
		}
		_ = admin.Close(context.Background())
	})

	provider, err := goose.NewProvider(goose.DialectPostgres, sqlDB, migrations.FS)
	if err != nil {
		t.Fatalf("goose provider: %v", err)
	}
	return &env{pool: pool, q: db.New(pool), provider: provider}
}

// redact keeps only the Postgres error code/message (never the connection string).
func redact(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code + " " + pgErr.Message
	}
	var cErr *pgconn.ConnectError
	if errors.As(err, &cErr) {
		return "connect error (details hidden)"
	}
	return err.Error()
}

func pgCode(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

func wantPgCode(t *testing.T, err error, code, what string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: expected error %s, got nil", what, code)
	}
	if got := pgCode(err); got != code {
		t.Fatalf("%s: expected SQLSTATE %s, got %q (%v)", what, code, got, err)
	}
}

func day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

func ptr[T any](v T) *T { return &v }

func newUser(t *testing.T, q *db.Queries, email string) db.User {
	t.Helper()
	u, err := q.CreateUser(context.Background(), db.CreateUserParams{
		Email: email, PasswordHash: "$argon2id$v=19$m=1,t=1,p=1$x$y", Name: ptr("Test"),
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	return u
}

func uniqueEmail(prefix string) string { return prefix + "+" + uuid.NewString()[:8] + "@example.com" }

func newAccount(t *testing.T, q *db.Queries, userID uuid.UUID, isDefault bool, opening int64) db.Account {
	t.Helper()
	a, err := q.CreateAccount(context.Background(), db.CreateAccountParams{
		UserID: userID, Name: "Cash", Type: "CASH", Currency: "INR", OpeningBalanceMinor: opening, IsDefault: isDefault,
	})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	return a
}

func systemCategory(t *testing.T, q *db.Queries, key string) db.Category {
	t.Helper()
	c, err := q.GetSystemCategoryByKey(context.Background(), key)
	if err != nil {
		t.Fatalf("system category %q: %v", key, err)
	}
	return c
}

func insertTx(t *testing.T, q *db.Queries, userID, accountID, categoryID uuid.UUID, typ string, amount int64,
	on time.Time, desc *string) (db.Transaction, error) {
	t.Helper()
	return q.InsertTransaction(context.Background(), db.InsertTransactionParams{
		UserID: userID, AccountID: accountID, CategoryID: &categoryID, Type: typ, AmountMinor: amount,
		Currency: "INR", OccurredOn: on, Description: desc, InputMethod: "MANUAL",
	})
}

func TestSchema(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	q := e.q

	// (a) goose up -> down -> up
	t.Run("goose up down up", func(t *testing.T) {
		if _, err := e.provider.Up(ctx); err != nil {
			t.Fatalf("up: %v", err)
		}
		v, err := e.provider.GetDBVersion(ctx)
		if err != nil || v != 2 {
			t.Fatalf("version after up = %d, %v; want 2", v, err)
		}
		if _, err := e.provider.DownTo(ctx, 0); err != nil {
			t.Fatalf("down to 0: %v", err)
		}
		var remaining int
		if err := e.pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.tables
			WHERE table_schema = 'public' AND table_name <> 'goose_db_version'`).Scan(&remaining); err != nil {
			t.Fatal(err)
		}
		if remaining != 0 {
			t.Fatalf("%d tables left after down", remaining)
		}
		if _, err := e.provider.Up(ctx); err != nil {
			t.Fatalf("second up: %v", err)
		}
		if v, _ := e.provider.GetDBVersion(ctx); v != 2 {
			t.Fatalf("version after second up = %d; want 2", v)
		}
	})
	if t.Failed() {
		t.FailNow()
	}

	// (b) case-insensitive unique email
	t.Run("email unique case-insensitive", func(t *testing.T) {
		email := "Alice." + uuid.NewString()[:8] + "@Example.com"
		u := newUser(t, q, email)
		_, err := q.CreateUser(ctx, db.CreateUserParams{Email: strings.ToUpper(email), PasswordHash: "h"})
		wantPgCode(t, err, pgUniqueViolation, "duplicate email in other case")
		got, err := q.GetUserByEmail(ctx, strings.ToLower(email))
		if err != nil || got.ID != u.ID {
			t.Fatalf("GetUserByEmail lower-case: %v", err)
		}
		if got.Email != email {
			t.Fatalf("email not stored as entered: %q", got.Email)
		}
		if got.Timezone != "UTC" || got.Currency != nil || got.OnboardedAt != nil {
			t.Fatalf("unexpected defaults: tz=%q currency=%v onboarded=%v", got.Timezone, got.Currency, got.OnboardedAt)
		}
	})

	// (c) single default account per user
	t.Run("one default account per user", func(t *testing.T) {
		u := newUser(t, q, uniqueEmail("def"))
		newAccount(t, q, u.ID, true, 0)
		_, err := q.CreateAccount(ctx, db.CreateAccountParams{
			UserID: u.ID, Name: "Bank", Type: "BANK", Currency: "INR", IsDefault: true,
		})
		wantPgCode(t, err, pgUniqueViolation, "second default account")
		second := newAccount(t, q, u.ID, false, 0)

		// make-default flow: clear then set, in one tx
		err = db.InTx(ctx, e.pool, func(tq *db.Queries) error {
			if err := tq.ClearDefaultAccount(ctx, u.ID); err != nil {
				return err
			}
			_, err := tq.SetDefaultAccount(ctx, db.SetDefaultAccountParams{ID: second.ID, UserID: u.ID})
			return err
		})
		if err != nil {
			t.Fatalf("make default: %v", err)
		}
		def, err := q.GetDefaultAccount(ctx, u.ID)
		if err != nil || def.ID != second.ID {
			t.Fatalf("default account = %v, %v; want %v", def.ID, err, second.ID)
		}
		list, err := q.ListAccounts(ctx, u.ID)
		if err != nil || len(list) != 2 || list[0].ID != second.ID {
			t.Fatalf("ListAccounts default first: %v %d", err, len(list))
		}
	})

	// (d) composite FK rejects another user's account
	t.Run("cross-user account reference rejected", func(t *testing.T) {
		a := newUser(t, q, uniqueEmail("a"))
		b := newUser(t, q, uniqueEmail("b"))
		bAcc := newAccount(t, q, b.ID, true, 0)
		food := systemCategory(t, q, "food")
		_, err := insertTx(t, q, a.ID, bAcc.ID, food.ID, "EXPENSE", 100, day(2026, 10, 1), nil)
		wantPgCode(t, err, pgForeignKeyViolation, "transaction on another user's account")
		// and the user-scoped reads do not see another user's rows
		if _, err := q.GetAccount(ctx, db.GetAccountParams{ID: bAcc.ID, UserID: a.ID}); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("GetAccount other user: want ErrNoRows, got %v", err)
		}
		if n, err := q.ApplyBalanceDelta(ctx, db.ApplyBalanceDeltaParams{Delta: 1, ID: bAcc.ID, UserID: a.ID}); err != nil || n != 0 {
			t.Fatalf("ApplyBalanceDelta other user: rows=%d err=%v; want 0", n, err)
		}
	})

	// (e) delta_minor sign from type
	t.Run("delta_minor generated from type", func(t *testing.T) {
		u := newUser(t, q, uniqueEmail("delta"))
		acc := newAccount(t, q, u.ID, true, 0)
		exp, err := insertTx(t, q, u.ID, acc.ID, systemCategory(t, q, "food").ID, "EXPENSE", 500, day(2026, 10, 2), nil)
		if err != nil {
			t.Fatal(err)
		}
		inc, err := insertTx(t, q, u.ID, acc.ID, systemCategory(t, q, "salary").ID, "INCOME", 700, day(2026, 10, 2), nil)
		if err != nil {
			t.Fatal(err)
		}
		if exp.DeltaMinor != -500 || inc.DeltaMinor != 700 {
			t.Fatalf("delta: expense=%d income=%d; want -500, 700", exp.DeltaMinor, inc.DeltaMinor)
		}
		// UpdateTransaction recomputes delta
		upd, err := q.UpdateTransaction(ctx, db.UpdateTransactionParams{
			ID: exp.ID, UserID: u.ID, AccountID: acc.ID, CategoryID: exp.CategoryID, Type: "EXPENSE", AmountMinor: 900,
			Currency: "INR", OccurredOn: exp.OccurredOn, InputMethod: "MANUAL",
		})
		if err != nil || upd.DeltaMinor != -900 {
			t.Fatalf("update delta = %d, %v; want -900", upd.DeltaMinor, err)
		}
		del, err := q.DeleteTransaction(ctx, db.DeleteTransactionParams{ID: inc.ID, UserID: u.ID})
		if err != nil || del.DeltaMinor != 700 || del.AccountID != acc.ID || del.TransferID != nil {
			t.Fatalf("DeleteTransaction returning = %+v, %v", del, err)
		}
	})

	// (f) amount CHECKs
	t.Run("amount_minor must be positive and bounded", func(t *testing.T) {
		u := newUser(t, q, uniqueEmail("amt"))
		acc := newAccount(t, q, u.ID, true, 0)
		food := systemCategory(t, q, "food")
		for _, amt := range []int64{0, -1, 1_000_000_000_000_000} {
			_, err := insertTx(t, q, u.ID, acc.ID, food.ID, "EXPENSE", amt, day(2026, 10, 3), nil)
			wantPgCode(t, err, pgCheckViolation, "amount")
		}
		// INCOME/EXPENSE require a category
		_, err := q.InsertTransaction(ctx, db.InsertTransactionParams{
			UserID: u.ID, AccountID: acc.ID, Type: "EXPENSE", AmountMinor: 1, Currency: "INR",
			OccurredOn: day(2026, 10, 3), InputMethod: "MANUAL",
		})
		wantPgCode(t, err, pgCheckViolation, "expense without category")
	})

	// (g) 21 seeded system categories
	t.Run("system categories seeded", func(t *testing.T) {
		u := newUser(t, q, uniqueEmail("cat"))
		all, err := q.ListCategories(ctx, db.ListCategoriesParams{UserID: u.ID})
		if err != nil {
			t.Fatal(err)
		}
		exp, _ := q.ListCategories(ctx, db.ListCategoriesParams{UserID: u.ID, Kind: ptr("EXPENSE")})
		inc, _ := q.ListCategories(ctx, db.ListCategoriesParams{UserID: u.ID, Kind: ptr("INCOME")})
		if len(all) != 21 || len(exp) != 15 || len(inc) != 6 {
			t.Fatalf("categories all=%d expense=%d income=%d; want 21/15/6", len(all), len(exp), len(inc))
		}
		want := []string{"food", "groceries", "transport", "shopping", "entertainment", "health", "utilities", "rent",
			"education", "travel", "insurance", "subscriptions", "emi", "personal_care", "other",
			"salary", "freelance", "business", "investment", "gift", "other_income"}
		var got []string
		for _, c := range all {
			if c.Key == nil || c.UserID != nil {
				t.Fatalf("system category without key or with user: %+v", c)
			}
			got = append(got, *c.Key)
		}
		if !slices.Equal(got, want) {
			t.Fatalf("category order/keys:\n got %v\nwant %v", got, want)
		}
		// GetUsableCategory enforces kind
		food := systemCategory(t, q, "food")
		if _, err := q.GetUsableCategory(ctx, db.GetUsableCategoryParams{ID: food.ID, UserID: u.ID, Kind: "INCOME"}); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("food as INCOME: want ErrNoRows, got %v", err)
		}
		if _, err := q.GetUsableCategory(ctx, db.GetUsableCategoryParams{ID: food.ID, UserID: u.ID, Kind: "EXPENSE"}); err != nil {
			t.Fatalf("food as EXPENSE: %v", err)
		}
		// the seed is idempotent
		if _, err := e.pool.Exec(ctx, `INSERT INTO categories (user_id, key, name, kind) VALUES (NULL, 'food', 'X', 'EXPENSE')
			ON CONFLICT (key) WHERE user_id IS NULL DO NOTHING`); err != nil {
			t.Fatalf("idempotent seed insert: %v", err)
		}
		if again := systemCategory(t, q, "food"); again.Name != food.Name {
			t.Fatalf("seed overwrote name")
		}
	})

	// (h) ApplyBalanceDelta is relative
	t.Run("ApplyBalanceDelta is relative", func(t *testing.T) {
		u := newUser(t, q, uniqueEmail("bal"))
		acc := newAccount(t, q, u.ID, true, 1000)
		if acc.BalanceMinor != 1000 || acc.OpeningBalanceMinor != 1000 {
			t.Fatalf("opening: %+v", acc)
		}
		for _, d := range []int64{250, -100} {
			n, err := q.ApplyBalanceDelta(ctx, db.ApplyBalanceDeltaParams{Delta: d, ID: acc.ID, UserID: u.ID})
			if err != nil || n != 1 {
				t.Fatalf("apply %d: rows=%d err=%v", d, n, err)
			}
		}
		got, _ := q.GetAccount(ctx, db.GetAccountParams{ID: acc.ID, UserID: u.ID})
		if got.BalanceMinor != 1150 {
			t.Fatalf("balance = %d; want 1150", got.BalanceMinor)
		}
		// onboarding opening moves the balance by (new - old opening)
		ob, err := q.SetDefaultAccountOpening(ctx, db.SetDefaultAccountOpeningParams{
			UserID: u.ID, Currency: "USD", OpeningBalanceMinor: 5000,
		})
		if err != nil || ob.BalanceMinor != 5150 || ob.OpeningBalanceMinor != 5000 || ob.Currency != "USD" {
			t.Fatalf("SetDefaultAccountOpening = %+v, %v; want balance 5150", ob, err)
		}
		sums, err := q.SumBalancesByCurrency(ctx, u.ID)
		if err != nil || len(sums) != 1 || sums[0].Currency != "USD" || sums[0].TotalMinor != 5150 {
			t.Fatalf("SumBalancesByCurrency = %+v, %v", sums, err)
		}
		// InTx rolls back on error
		sentinel := errors.New("boom")
		err = db.InTx(ctx, e.pool, func(tq *db.Queries) error {
			if _, err := tq.ApplyBalanceDelta(ctx, db.ApplyBalanceDeltaParams{Delta: 1, ID: acc.ID, UserID: u.ID}); err != nil {
				return err
			}
			return sentinel
		})
		if !errors.Is(err, sentinel) {
			t.Fatalf("InTx error = %v; want sentinel", err)
		}
		got, _ = q.GetAccount(ctx, db.GetAccountParams{ID: acc.ID, UserID: u.ID})
		if got.BalanceMinor != 5150 {
			t.Fatalf("balance after rollback = %d; want 5150", got.BalanceMinor)
		}
	})

	// (i) deleting a user cascades to everything
	t.Run("delete user cascades", func(t *testing.T) {
		u := newUser(t, q, uniqueEmail("del"))
		other := newUser(t, q, uniqueEmail("keep"))
		otherAcc := newAccount(t, q, other.ID, true, 0)
		acc := newAccount(t, q, u.ID, true, 0)
		food := systemCategory(t, q, "food")
		if _, err := insertTx(t, q, u.ID, acc.ID, food.ID, "EXPENSE", 100, day(2026, 10, 4), nil); err != nil {
			t.Fatal(err)
		}
		if _, err := insertTx(t, q, other.ID, otherAcc.ID, food.ID, "EXPENSE", 100, day(2026, 10, 4), nil); err != nil {
			t.Fatal(err)
		}
		ip := netip.MustParseAddr("203.0.113.7")
		s, err := q.CreateSession(ctx, db.CreateSessionParams{
			UserID: u.ID, Client: "MOBILE", UserAgent: ptr("test"), Ip: &ip, ExpiresAt: time.Now().Add(time.Hour),
		})
		if err != nil {
			t.Fatalf("create session: %v", err)
		}
		if s.Ip == nil || *s.Ip != ip {
			t.Fatalf("inet round-trip: %v", s.Ip)
		}
		if _, err := q.CreateRefreshToken(ctx, db.CreateRefreshTokenParams{
			SessionID: s.ID, TokenHash: []byte("hash-" + uuid.NewString()), ExpiresAt: time.Now().Add(time.Hour),
		}); err != nil {
			t.Fatalf("create refresh token: %v", err)
		}
		b1, err := q.UpsertBudget(ctx, db.UpsertBudgetParams{UserID: u.ID, AmountMinor: 10000, Currency: "INR"})
		if err != nil {
			t.Fatalf("upsert overall budget: %v", err)
		}
		b2, err := q.UpsertBudget(ctx, db.UpsertBudgetParams{UserID: u.ID, AmountMinor: 20000, Currency: "INR"})
		if err != nil || b2.ID != b1.ID || b2.AmountMinor != 20000 {
			t.Fatalf("second overall upsert must update the same row (NULLS NOT DISTINCT): %v", err)
		}
		if !slices.Equal(b1.AlertThresholds, []int16{80, 100}) {
			t.Fatalf("alert_thresholds = %v", b1.AlertThresholds)
		}
		if _, err := q.UpsertBudget(ctx, db.UpsertBudgetParams{UserID: u.ID, CategoryID: &food.ID, AmountMinor: 500, Currency: "INR"}); err != nil {
			t.Fatalf("category budget: %v", err)
		}
		var customID uuid.UUID
		if err := e.pool.QueryRow(ctx, `INSERT INTO categories (user_id, name, kind) VALUES ($1, 'Mine', 'EXPENSE') RETURNING id`, u.ID).Scan(&customID); err != nil {
			t.Fatalf("custom category: %v", err)
		}
		// a transaction that uses the custom category: the cascade must still succeed
		if _, err := insertTx(t, q, u.ID, acc.ID, customID, "EXPENSE", 42, day(2026, 10, 4), nil); err != nil {
			t.Fatal(err)
		}
		// a used category cannot be deleted directly (FK RESTRICT / NO ACTION)
		_, err = e.pool.Exec(ctx, `DELETE FROM categories WHERE id = $1`, customID)
		wantPgCode(t, err, pgForeignKeyViolation, "delete used category")
		if _, err := e.pool.Exec(ctx, `INSERT INTO notifications (user_id, kind, dedupe_key) VALUES ($1, 'WEEKLY_TIPS', 'weekly_tips:2026-W44')`, u.ID); err != nil {
			t.Fatalf("notification: %v", err)
		}

		n, err := q.DeleteUser(ctx, u.ID)
		if err != nil || n != 1 {
			t.Fatalf("DeleteUser rows=%d err=%v", n, err)
		}
		for _, table := range []string{"sessions", "categories", "accounts", "transactions", "budgets", "notifications"} {
			var c int
			if err := e.pool.QueryRow(ctx, "SELECT count(*) FROM "+table+" WHERE user_id = $1", u.ID).Scan(&c); err != nil {
				t.Fatal(err)
			}
			if c != 0 {
				t.Errorf("%s: %d rows left after user delete", table, c)
			}
		}
		var rt int
		_ = e.pool.QueryRow(ctx, "SELECT count(*) FROM refresh_tokens WHERE session_id = $1", s.ID).Scan(&rt)
		if rt != 0 {
			t.Errorf("refresh_tokens: %d rows left", rt)
		}
		// the other user is untouched
		var otherTx int
		_ = e.pool.QueryRow(ctx, "SELECT count(*) FROM transactions WHERE user_id = $1", other.ID).Scan(&otherTx)
		if otherTx != 1 {
			t.Errorf("other user's transactions = %d; want 1", otherTx)
		}
	})

	// Extra: sessions + refresh-token rotation queries
	t.Run("session and refresh token queries", func(t *testing.T) {
		u := newUser(t, q, uniqueEmail("sess"))
		s1, _ := q.CreateSession(ctx, db.CreateSessionParams{UserID: u.ID, Client: "WEB", ExpiresAt: time.Now().Add(time.Hour)})
		s2, _ := q.CreateSession(ctx, db.CreateSessionParams{UserID: u.ID, Client: "MOBILE", ExpiresAt: time.Now().Add(time.Hour)})
		hash := []byte("h-" + uuid.NewString())
		rt1, err := q.CreateRefreshToken(ctx, db.CreateRefreshTokenParams{SessionID: s1.ID, TokenHash: hash, ExpiresAt: time.Now().Add(time.Hour)})
		if err != nil {
			t.Fatal(err)
		}
		err = db.InTx(ctx, e.pool, func(tq *db.Queries) error {
			row, err := tq.GetRefreshTokenForUpdate(ctx, hash)
			if err != nil {
				return err
			}
			if row.UserID != u.ID || row.SessionID != s1.ID || row.UsedAt != nil || row.SessionRevokedAt != nil {
				t.Errorf("GetRefreshTokenForUpdate row = %+v", row)
			}
			if n, err := tq.MarkRefreshTokenUsed(ctx, row.ID); err != nil || n != 1 {
				t.Errorf("mark used rows=%d err=%v", n, err)
			}
			if n, _ := tq.MarkRefreshTokenUsed(ctx, row.ID); n != 0 {
				t.Errorf("second mark used rows=%d; want 0", n)
			}
			rt2, err := tq.CreateRefreshToken(ctx, db.CreateRefreshTokenParams{SessionID: s1.ID, TokenHash: []byte("h2-" + uuid.NewString()), ExpiresAt: time.Now().Add(time.Hour)})
			if err != nil {
				return err
			}
			return tq.SetRefreshTokenReplacedBy(ctx, db.SetRefreshTokenReplacedByParams{ReplacedBy: rt2.ID, ID: rt1.ID})
		})
		if err != nil {
			t.Fatalf("rotation tx: %v", err)
		}
		if n, err := q.RevokeOtherSessions(ctx, db.RevokeOtherSessionsParams{Reason: "PASSWORD_CHANGED", UserID: u.ID, KeepID: s1.ID}); err != nil || n != 1 {
			t.Fatalf("RevokeOtherSessions rows=%d err=%v", n, err)
		}
		if active, _ := q.IsSessionActive(ctx, db.IsSessionActiveParams{ID: s2.ID, UserID: u.ID}); active {
			t.Fatal("s2 should be revoked")
		}
		if active, _ := q.IsSessionActive(ctx, db.IsSessionActiveParams{ID: s1.ID, UserID: u.ID}); !active {
			t.Fatal("s1 should be active")
		}
		_, err = q.RevokeSession(ctx, db.RevokeSessionParams{Reason: "NOPE", ID: s1.ID, UserID: u.ID})
		wantPgCode(t, err, pgCheckViolation, "bad revoke reason")
		if n, _ := q.RevokeAllUserSessions(ctx, db.RevokeAllUserSessionsParams{Reason: "LOGOUT_ALL", UserID: u.ID}); n != 1 {
			t.Fatalf("RevokeAllUserSessions rows=%d; want 1", n)
		}
	})

	// Extra: user profile + onboarding
	t.Run("profile and onboarding", func(t *testing.T) {
		u := newUser(t, q, uniqueEmail("prof"))
		p, err := q.UpdateUserProfile(ctx, db.UpdateUserProfileParams{ID: u.ID, Timezone: ptr("Asia/Kolkata"), EmailWeeklyTips: ptr(false)})
		if err != nil || p.Timezone != "Asia/Kolkata" || p.EmailWeeklyTips || p.Name == nil || *p.Name != "Test" || !p.EmailBudgetAlerts {
			t.Fatalf("UpdateUserProfile partial = %+v, %v", p, err)
		}
		p, _ = q.UpdateUserProfile(ctx, db.UpdateUserProfileParams{ID: u.ID, Name: ptr("")})
		if p.Name != nil {
			t.Fatalf("empty name should clear to NULL, got %q", *p.Name)
		}
		o, err := q.CompleteOnboarding(ctx, db.CompleteOnboardingParams{ID: u.ID, Currency: "INR", Timezone: "Asia/Kolkata"})
		if err != nil || o.OnboardedAt == nil || o.Currency == nil || *o.Currency != "INR" {
			t.Fatalf("CompleteOnboarding = %+v, %v", o, err)
		}
		if _, err := q.CompleteOnboarding(ctx, db.CompleteOnboardingParams{ID: u.ID, Currency: "USD", Timezone: "UTC"}); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("second onboarding: want ErrNoRows, got %v", err)
		}
	})

	// Extra: list (keyset + filters), export, reports, budgets
	t.Run("list export reports budgets", func(t *testing.T) {
		u := newUser(t, q, uniqueEmail("rep"))
		acc := newAccount(t, q, u.ID, true, 0)
		acc2 := newAccount(t, q, u.ID, false, 0)
		food, rent, salary := systemCategory(t, q, "food"), systemCategory(t, q, "rent"), systemCategory(t, q, "salary")
		mk := func(a db.Account, c db.Category, typ string, amt int64, on time.Time, desc string) db.Transaction {
			tx, err := insertTx(t, q, u.ID, a.ID, c.ID, typ, amt, on, &desc)
			if err != nil {
				t.Fatal(err)
			}
			return tx
		}
		mk(acc, food, "EXPENSE", 300, day(2026, 10, 1), "lunch 50% off")
		mk(acc, food, "EXPENSE", 200, day(2026, 10, 3), "dinner 50 off")
		mk(acc2, rent, "EXPENSE", 1000, day(2026, 10, 3), "rent_oct")
		mk(acc, salary, "INCOME", 5000, day(2026, 10, 5), "salary")
		mk(acc, food, "EXPENSE", 999, day(2026, 9, 30), "september")
		// another user's data must never show up
		o := newUser(t, q, uniqueEmail("rep-other"))
		oAcc := newAccount(t, q, o.ID, true, 0)
		if _, err := insertTx(t, q, o.ID, oAcc.ID, food.ID, "EXPENSE", 77, day(2026, 10, 3), nil); err != nil {
			t.Fatal(err)
		}

		// keyset pagination over October
		from, to := day(2026, 10, 1), day(2026, 10, 31)
		var seen []uuid.UUID
		var cur *db.Transaction
		for range 10 {
			p := db.ListTransactionsParams{UserID: u.ID, FromDate: &from, ToDate: &to, RowLimit: 2}
			if cur != nil {
				p.CursorOccurredOn, p.CursorCreatedAt, p.CursorID = &cur.OccurredOn, &cur.CreatedAt, &cur.ID
			}
			page, err := q.ListTransactions(ctx, p)
			if err != nil {
				t.Fatal(err)
			}
			for _, r := range page {
				seen = append(seen, r.ID)
			}
			if len(page) < 2 {
				break
			}
			cur = &page[len(page)-1]
		}
		if len(seen) != 4 {
			t.Fatalf("keyset pages returned %d rows; want 4", len(seen))
		}
		// q matches '%' literally
		qr, _ := q.ListTransactions(ctx, db.ListTransactionsParams{UserID: u.ID, Q: ptr("50%"), RowLimit: 50})
		if len(qr) != 1 || *qr[0].Description != "lunch 50% off" {
			t.Fatalf("q=50%% matched %d rows; want 1", len(qr))
		}
		qr, _ = q.ListTransactions(ctx, db.ListTransactionsParams{UserID: u.ID, Q: ptr("T_O"), RowLimit: 50})
		if len(qr) != 1 {
			t.Fatalf("q=T_O (case-insensitive, literal _) matched %d rows; want 1", len(qr))
		}
		qr, _ = q.ListTransactions(ctx, db.ListTransactionsParams{UserID: u.ID, Type: ptr("INCOME"), AccountID: &acc.ID, RowLimit: 50})
		if len(qr) != 1 {
			t.Fatalf("type+account filter matched %d rows; want 1", len(qr))
		}

		ex, err := q.ExportTransactions(ctx, db.ExportTransactionsParams{UserID: u.ID, FromDate: from, ToDate: to})
		if err != nil || len(ex) != 4 || ex[0].AccountName != "Cash" || ex[0].CategoryKey == nil || *ex[0].CategoryKey != "food" {
			t.Fatalf("export = %d rows, %v", len(ex), err)
		}

		start, end := day(2026, 10, 1), day(2026, 11, 1)
		mt, err := q.MonthTotals(ctx, db.MonthTotalsParams{UserID: u.ID, Currency: "INR", StartOn: start, EndBefore: end})
		if err != nil || mt.IncomeMinor != 5000 || mt.ExpenseMinor != 1500 {
			t.Fatalf("MonthTotals = %+v, %v; want 5000/1500", mt, err)
		}
		empty, err := q.MonthTotals(ctx, db.MonthTotalsParams{UserID: u.ID, Currency: "INR", StartOn: day(2020, 1, 1), EndBefore: day(2020, 2, 1)})
		if err != nil || empty.IncomeMinor != 0 || empty.ExpenseMinor != 0 {
			t.Fatalf("empty MonthTotals = %+v, %v", empty, err)
		}
		bc, err := q.ExpenseByCategory(ctx, db.ExpenseByCategoryParams{UserID: u.ID, Currency: "INR", StartOn: start, EndBefore: end})
		if err != nil || len(bc) != 2 || *bc[0].CategoryID != rent.ID || bc[0].AmountMinor != 1000 || bc[1].AmountMinor != 500 {
			t.Fatalf("ExpenseByCategory = %+v, %v", bc, err)
		}
		daily, err := q.DailyTotals(ctx, db.DailyTotalsParams{UserID: u.ID, FromDate: day(2026, 10, 1), ToDate: day(2026, 10, 7), Currency: ptr("INR")})
		if err != nil || len(daily) != 7 {
			t.Fatalf("DailyTotals = %d rows, %v; want 7", len(daily), err)
		}
		if !daily[2].Day.Equal(day(2026, 10, 3)) || daily[2].ExpenseMinor != 1200 || daily[1].ExpenseMinor != 0 || daily[4].IncomeMinor != 5000 {
			t.Fatalf("DailyTotals values = %+v", daily)
		}
		dAcc, _ := q.DailyTotals(ctx, db.DailyTotalsParams{UserID: u.ID, FromDate: day(2026, 10, 3), ToDate: day(2026, 10, 3), AccountID: &acc2.ID})
		if len(dAcc) != 1 || dAcc[0].ExpenseMinor != 1000 {
			t.Fatalf("DailyTotals account filter = %+v", dAcc)
		}

		spent, err := q.SpentInRange(ctx, db.SpentInRangeParams{UserID: u.ID, Currency: "INR", StartOn: start, EndBefore: end})
		if err != nil || spent != 1500 {
			t.Fatalf("SpentInRange overall = %d, %v; want 1500", spent, err)
		}
		spent, _ = q.SpentInRange(ctx, db.SpentInRangeParams{UserID: u.ID, Currency: "INR", StartOn: start, EndBefore: end, CategoryID: &food.ID})
		if spent != 500 {
			t.Fatalf("SpentInRange food = %d; want 500", spent)
		}
		recent, _ := q.RecentTransactions(ctx, db.RecentTransactionsParams{UserID: u.ID, RowLimit: 5})
		if len(recent) != 5 || !recent[0].OccurredOn.Equal(day(2026, 10, 5)) {
			t.Fatalf("RecentTransactions = %d rows", len(recent))
		}
		if c, _ := q.CountAccountTransactions(ctx, db.CountAccountTransactionsParams{AccountID: acc.ID, UserID: u.ID}); c != 4 {
			t.Fatalf("CountAccountTransactions = %d; want 4", c)
		}

		// budgets
		if _, err := q.UpsertBudget(ctx, db.UpsertBudgetParams{UserID: u.ID, AmountMinor: 9000, Currency: "INR"}); err != nil {
			t.Fatal(err)
		}
		if _, err := q.UpsertBudget(ctx, db.UpsertBudgetParams{UserID: u.ID, CategoryID: &food.ID, AmountMinor: 600, Currency: "INR"}); err != nil {
			t.Fatal(err)
		}
		bs, _ := q.GetBudgets(ctx, u.ID)
		if len(bs) != 2 || bs[0].CategoryID != nil {
			t.Fatalf("GetBudgets = %+v", bs)
		}
		if _, err := q.GetBudget(ctx, db.GetBudgetParams{UserID: u.ID}); err != nil {
			t.Fatalf("GetBudget overall: %v", err)
		}
		if n, _ := q.DeleteBudget(ctx, db.DeleteBudgetParams{UserID: u.ID}); n != 1 {
			t.Fatalf("DeleteBudget overall rows=%d", n)
		}
		if _, err := q.GetBudget(ctx, db.GetBudgetParams{UserID: u.ID}); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("overall budget should be gone: %v", err)
		}
		if _, err := q.GetBudget(ctx, db.GetBudgetParams{UserID: u.ID, CategoryID: &food.ID}); err != nil {
			t.Fatalf("food budget should remain: %v", err)
		}

		// lock several accounts in id order
		err = db.InTx(ctx, e.pool, func(tq *db.Queries) error {
			locked, err := tq.LockAccounts(ctx, db.LockAccountsParams{UserID: u.ID, Ids: []uuid.UUID{acc2.ID, acc.ID, oAcc.ID}})
			if err != nil {
				return err
			}
			if len(locked) != 2 || strings.Compare(locked[0].ID.String(), locked[1].ID.String()) > 0 {
				t.Errorf("LockAccounts = %d rows, not ascending or included a foreign account", len(locked))
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}

		// account delete cascades its transactions
		if n, _ := q.DeleteAccount(ctx, db.DeleteAccountParams{ID: acc2.ID, UserID: u.ID}); n != 1 {
			t.Fatalf("DeleteAccount rows=%d", n)
		}
		if c, _ := q.CountAccountTransactions(ctx, db.CountAccountTransactionsParams{AccountID: acc2.ID, UserID: u.ID}); c != 0 {
			t.Fatalf("transactions left on deleted account: %d", c)
		}
	})
}
