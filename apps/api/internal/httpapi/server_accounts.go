package httpapi

// accounts slice. Stub file created by the lead: replace each stub with the real implementation.

import (
	"context"

	"arth/api/internal/httpapi/gen"
)

func (s *Server) ListAccounts(ctx context.Context, request gen.ListAccountsRequestObject) (gen.ListAccountsResponseObject, error) {
	return nil, notImplemented()
}

func (s *Server) CreateAccount(ctx context.Context, request gen.CreateAccountRequestObject) (gen.CreateAccountResponseObject, error) {
	return nil, notImplemented()
}

func (s *Server) DeleteAccount(ctx context.Context, request gen.DeleteAccountRequestObject) (gen.DeleteAccountResponseObject, error) {
	return nil, notImplemented()
}

func (s *Server) GetAccount(ctx context.Context, request gen.GetAccountRequestObject) (gen.GetAccountResponseObject, error) {
	return nil, notImplemented()
}

func (s *Server) UpdateAccount(ctx context.Context, request gen.UpdateAccountRequestObject) (gen.UpdateAccountResponseObject, error) {
	return nil, notImplemented()
}

func (s *Server) MakeDefaultAccount(ctx context.Context, request gen.MakeDefaultAccountRequestObject) (gen.MakeDefaultAccountResponseObject, error) {
	return nil, notImplemented()
}

func (s *Server) ListCategories(ctx context.Context, request gen.ListCategoriesRequestObject) (gen.ListCategoriesResponseObject, error) {
	return nil, notImplemented()
}
