package httpapi

// transactions slice. Stub file created by the lead: replace each stub with the real implementation.

import (
	"context"

	"arth/api/internal/httpapi/gen"
)

func (s *Server) ListTransactions(ctx context.Context, request gen.ListTransactionsRequestObject) (gen.ListTransactionsResponseObject, error) {
	return nil, notImplemented()
}

func (s *Server) CreateTransaction(ctx context.Context, request gen.CreateTransactionRequestObject) (gen.CreateTransactionResponseObject, error) {
	return nil, notImplemented()
}

func (s *Server) ExportTransactions(ctx context.Context, request gen.ExportTransactionsRequestObject) (gen.ExportTransactionsResponseObject, error) {
	return nil, notImplemented()
}

func (s *Server) DeleteTransaction(ctx context.Context, request gen.DeleteTransactionRequestObject) (gen.DeleteTransactionResponseObject, error) {
	return nil, notImplemented()
}

func (s *Server) GetTransaction(ctx context.Context, request gen.GetTransactionRequestObject) (gen.GetTransactionResponseObject, error) {
	return nil, notImplemented()
}

func (s *Server) UpdateTransaction(ctx context.Context, request gen.UpdateTransactionRequestObject) (gen.UpdateTransactionResponseObject, error) {
	return nil, notImplemented()
}
