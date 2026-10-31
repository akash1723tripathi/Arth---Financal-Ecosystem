package httpapi

// budgets slice + dashboard and daily report (docs/API.md "Budgets", "Dashboard & reports").
//
// Every aggregate is in ONE currency (no FX): budgets use their own currency, the dashboard and the default
// daily report use the caller's currency (INR before onboarding), an account-filtered daily report uses
// the account's currency. Transfers never count: the queries only sum INCOME/EXPENSE rows. Months are
// user-local calendar months [first day, first day of next month) in users.timezone.

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"

	"arth/api/internal/apierr"
	"arth/api/internal/db"
	"arth/api/internal/httpapi/gen"
)

// maxReportDays is the longest range GET /v1/reports/daily accepts (inclusive day count).
const maxReportDays = 93

// recentCount is the number of transactions on the dashboard.
const recentCount = 5

// percentUsed is floor(spent × 100 / amount) in integer arithmetic (no floats), clamped to int32.
// It is computed as q×100 + r×100/amount so spent×100 can never overflow int64.
func percentUsed(spent, amount int64) int32 {
	if amount <= 0 || spent <= 0 {
		return 0
	}
	q, r := spent/amount, spent%amount
	if q > math.MaxInt32/100 {
		return math.MaxInt32
	}
	p := q*100 + r*100/amount // r < amount ≤ 999 999 999 999 999, so r×100 fits in int64
	if p > math.MaxInt32 {
		return math.MaxInt32
	}
	return int32(p)
}

// budgetProgress computes a budget's progress for one month (EXPENSE in the budget currency; a category
// budget counts only its category).
func (s *Server) budgetProgress(ctx context.Context, q *db.Queries, b db.Budget, m monthSpan) (gen.BudgetProgress, error) {
	spent, err := q.SpentInRange(ctx, db.SpentInRangeParams{
		UserID: b.UserID, Currency: b.Currency, StartOn: m.Start, EndBefore: m.End, CategoryID: b.CategoryID,
	})
	if err != nil {
		return gen.BudgetProgress{}, apierr.Internal(fmt.Errorf("spent in range: %w", err))
	}
	return gen.BudgetProgress{
		Id:             b.ID,
		CategoryId:     b.CategoryID,
		AmountMinor:    b.AmountMinor,
		Currency:       b.Currency,
		Month:          m.Label,
		SpentMinor:     spent,
		RemainingMinor: b.AmountMinor - spent,
		PercentUsed:    percentUsed(spent, b.AmountMinor),
	}, nil
}

func (s *Server) ListBudgets(ctx context.Context, request gen.ListBudgetsRequestObject) (gen.ListBudgetsResponseObject, error) {
	c, err := s.loadCaller(ctx)
	if err != nil {
		return nil, err
	}
	m, err := c.resolveMonth(request.Params.Month, s.d.Now())
	if err != nil {
		return nil, err
	}
	rows, err := s.d.Q.GetBudgets(ctx, c.ID) // overall first
	if err != nil {
		return nil, apierr.Internal(fmt.Errorf("get budgets: %w", err))
	}
	out := make([]gen.BudgetProgress, 0, len(rows))
	for _, b := range rows {
		p, err := s.budgetProgress(ctx, s.d.Q, b, m)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return gen.ListBudgets200JSONResponse{Data: out}, nil
}

func (s *Server) PutOverallBudget(ctx context.Context, request gen.PutOverallBudgetRequestObject) (gen.PutOverallBudgetResponseObject, error) {
	c, err := s.loadCaller(ctx)
	if err != nil {
		return nil, err
	}
	if request.Body == nil {
		return nil, apierr.BadRequest("missing request body")
	}
	b, err := s.d.Q.UpsertBudget(ctx, db.UpsertBudgetParams{
		UserID: c.ID, CategoryID: nil, AmountMinor: request.Body.AmountMinor, Currency: c.Currency,
	})
	if err != nil {
		return nil, apierr.Internal(fmt.Errorf("upsert overall budget: %w", err))
	}
	p, err := s.budgetProgress(ctx, s.d.Q, b, c.currentMonth(s.d.Now()))
	if err != nil {
		return nil, err
	}
	return gen.PutOverallBudget200JSONResponse(p), nil
}

// DeleteOverallBudget is idempotent: 204 also when no overall budget was set (api/openapi.yaml).
func (s *Server) DeleteOverallBudget(ctx context.Context, request gen.DeleteOverallBudgetRequestObject) (gen.DeleteOverallBudgetResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := s.d.Q.DeleteBudget(ctx, db.DeleteBudgetParams{UserID: p.UserID, CategoryID: nil}); err != nil {
		return nil, apierr.Internal(fmt.Errorf("delete overall budget: %w", err))
	}
	return gen.DeleteOverallBudget204Response{}, nil
}

func (s *Server) PutCategoryBudget(ctx context.Context, request gen.PutCategoryBudgetRequestObject) (gen.PutCategoryBudgetResponseObject, error) {
	c, err := s.loadCaller(ctx)
	if err != nil {
		return nil, err
	}
	if request.Body == nil {
		return nil, apierr.BadRequest("missing request body")
	}
	catID := request.CategoryId
	if _, err := s.d.Q.GetUsableCategory(ctx, db.GetUsableCategoryParams{ID: catID, UserID: c.ID, Kind: "EXPENSE"}); err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return nil, apierr.Internal(fmt.Errorf("get usable category: %w", err))
		}
		// Not usable: tell "not yours / unknown" (404) apart from "visible but wrong kind or archived" (422).
		cat, err := s.d.Q.GetCategory(ctx, db.GetCategoryParams{ID: catID, UserID: c.ID})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apierr.NotFound()
		}
		if err != nil {
			return nil, apierr.Internal(fmt.Errorf("get category: %w", err))
		}
		msg := "must be an EXPENSE category"
		if cat.Kind == "EXPENSE" && cat.ArchivedAt != nil {
			msg = "category is archived"
		}
		return nil, apierr.Validation(apierr.FieldError{Field: "path.category_id", Message: msg})
	}
	b, err := s.d.Q.UpsertBudget(ctx, db.UpsertBudgetParams{
		UserID: c.ID, CategoryID: &catID, AmountMinor: request.Body.AmountMinor, Currency: c.Currency,
	})
	if err != nil {
		return nil, apierr.Internal(fmt.Errorf("upsert category budget: %w", err))
	}
	p, err := s.budgetProgress(ctx, s.d.Q, b, c.currentMonth(s.d.Now()))
	if err != nil {
		return nil, err
	}
	return gen.PutCategoryBudget200JSONResponse(p), nil
}

// DeleteCategoryBudget is idempotent: 204 also when none was set (api/openapi.yaml). The delete is scoped
// by user_id, so another user's category id deletes nothing and reveals nothing.
func (s *Server) DeleteCategoryBudget(ctx context.Context, request gen.DeleteCategoryBudgetRequestObject) (gen.DeleteCategoryBudgetResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	catID := request.CategoryId
	if _, err := s.d.Q.DeleteBudget(ctx, db.DeleteBudgetParams{UserID: p.UserID, CategoryID: &catID}); err != nil {
		return nil, apierr.Internal(fmt.Errorf("delete category budget: %w", err))
	}
	return gen.DeleteCategoryBudget204Response{}, nil
}

func (s *Server) GetDashboard(ctx context.Context, request gen.GetDashboardRequestObject) (gen.GetDashboardResponseObject, error) {
	c, err := s.loadCaller(ctx)
	if err != nil {
		return nil, err
	}
	m, err := c.resolveMonth(request.Params.Month, s.d.Now())
	if err != nil {
		return nil, err
	}
	q := s.d.Q

	balances, err := q.SumBalancesByCurrency(ctx, c.ID)
	if err != nil {
		return nil, apierr.Internal(fmt.Errorf("sum balances: %w", err))
	}
	var total int64
	for _, b := range balances {
		if b.Currency == c.Currency {
			total = b.TotalMinor
		}
	}

	totals, err := q.MonthTotals(ctx, db.MonthTotalsParams{UserID: c.ID, Currency: c.Currency, StartOn: m.Start, EndBefore: m.End})
	if err != nil {
		return nil, apierr.Internal(fmt.Errorf("month totals: %w", err))
	}

	var budget *gen.BudgetProgress
	ob, err := q.GetBudget(ctx, db.GetBudgetParams{UserID: c.ID, CategoryID: nil})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return nil, apierr.Internal(fmt.Errorf("get overall budget: %w", err))
	default:
		p, err := s.budgetProgress(ctx, q, ob, m)
		if err != nil {
			return nil, err
		}
		budget = &p
	}

	cats, err := q.ExpenseByCategory(ctx, db.ExpenseByCategoryParams{UserID: c.ID, Currency: c.Currency, StartOn: m.Start, EndBefore: m.End})
	if err != nil {
		return nil, apierr.Internal(fmt.Errorf("expense by category: %w", err))
	}
	byCat := make([]gen.CategoryAmount, 0, len(cats))
	for _, r := range cats {
		if r.CategoryID == nil { // impossible for EXPENSE rows (CHECK), but the column is nullable
			continue
		}
		byCat = append(byCat, gen.CategoryAmount{CategoryId: *r.CategoryID, AmountMinor: r.AmountMinor})
	}

	recent, err := q.RecentTransactions(ctx, db.RecentTransactionsParams{UserID: c.ID, RowLimit: recentCount})
	if err != nil {
		return nil, apierr.Internal(fmt.Errorf("recent transactions: %w", err))
	}

	return gen.GetDashboard200JSONResponse{
		Currency:          c.Currency,
		TotalBalanceMinor: total,
		Month: gen.MonthTotals{
			IncomeMinor:  totals.IncomeMinor,
			ExpenseMinor: totals.ExpenseMinor,
			NetMinor:     totals.IncomeMinor - totals.ExpenseMinor,
		},
		Budget:            budget,
		ExpenseByCategory: byCat,
		Recent:            toTransactions(recent),
	}, nil
}

func (s *Server) GetDailyReport(ctx context.Context, request gen.GetDailyReportRequestObject) (gen.GetDailyReportResponseObject, error) {
	c, err := s.loadCaller(ctx)
	if err != nil {
		return nil, err
	}
	from, to := fromDate(request.Params.From), fromDate(request.Params.To)
	if to.Before(from) {
		return nil, apierr.Validation(apierr.FieldError{Field: "query.to", Message: "must not be before from"})
	}
	if days := int(to.Sub(from)/(24*time.Hour)) + 1; days > maxReportDays {
		return nil, apierr.Validation(apierr.FieldError{Field: "query.to", Message: fmt.Sprintf("range must be at most %d days", maxReportDays)})
	}

	currency := c.Currency
	if id := request.Params.AccountId; id != nil {
		acct, err := s.d.Q.GetAccount(ctx, db.GetAccountParams{ID: *id, UserID: c.ID})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apierr.NotFound()
		}
		if err != nil {
			return nil, apierr.Internal(fmt.Errorf("get account: %w", err))
		}
		currency = acct.Currency
	}

	rows, err := s.d.Q.DailyTotals(ctx, db.DailyTotalsParams{
		FromDate: from, ToDate: to, UserID: c.ID, Currency: &currency, AccountID: request.Params.AccountId,
	})
	if err != nil {
		return nil, apierr.Internal(fmt.Errorf("daily totals: %w", err))
	}
	out := make([]gen.DailyTotals, 0, len(rows))
	for _, r := range rows {
		out = append(out, gen.DailyTotals{Date: toDate(r.Day), IncomeMinor: r.IncomeMinor, ExpenseMinor: r.ExpenseMinor})
	}
	return gen.GetDailyReport200JSONResponse{Data: out}, nil
}
