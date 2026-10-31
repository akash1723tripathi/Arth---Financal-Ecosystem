package httpapi

import (
	"context"

	"arth/api/internal/httpapi/gen"
)

func (s *Server) ExportTransactions(ctx context.Context, request gen.ExportTransactionsRequestObject) (gen.ExportTransactionsResponseObject, error) {
	return nil, notImplemented()
}
