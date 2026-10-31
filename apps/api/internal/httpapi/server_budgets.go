package httpapi

// budgets slice. Stub file created by the lead: replace each stub with the real implementation.

import (
	"context"

	"arth/api/internal/httpapi/gen"
)

func (s *Server) ListBudgets(ctx context.Context, request gen.ListBudgetsRequestObject) (gen.ListBudgetsResponseObject, error) {
	return nil, notImplemented()
}

func (s *Server) DeleteCategoryBudget(ctx context.Context, request gen.DeleteCategoryBudgetRequestObject) (gen.DeleteCategoryBudgetResponseObject, error) {
	return nil, notImplemented()
}

func (s *Server) PutCategoryBudget(ctx context.Context, request gen.PutCategoryBudgetRequestObject) (gen.PutCategoryBudgetResponseObject, error) {
	return nil, notImplemented()
}

func (s *Server) DeleteOverallBudget(ctx context.Context, request gen.DeleteOverallBudgetRequestObject) (gen.DeleteOverallBudgetResponseObject, error) {
	return nil, notImplemented()
}

func (s *Server) PutOverallBudget(ctx context.Context, request gen.PutOverallBudgetRequestObject) (gen.PutOverallBudgetResponseObject, error) {
	return nil, notImplemented()
}

func (s *Server) GetDashboard(ctx context.Context, request gen.GetDashboardRequestObject) (gen.GetDashboardResponseObject, error) {
	return nil, notImplemented()
}

func (s *Server) GetDailyReport(ctx context.Context, request gen.GetDailyReportRequestObject) (gen.GetDailyReportResponseObject, error) {
	return nil, notImplemented()
}
