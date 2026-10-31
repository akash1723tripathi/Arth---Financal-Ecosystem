package httpapi

import (
	"context"

	"arth/api/internal/httpapi/gen"
)

// /healthz and /readyz are served by router.go before the generated API is reached; these methods exist only
// to satisfy the generated interface.

func (s *Server) GetHealthz(ctx context.Context, request gen.GetHealthzRequestObject) (gen.GetHealthzResponseObject, error) {
	return gen.GetHealthz200JSONResponse{Status: gen.Ok}, nil
}

func (s *Server) GetReadyz(ctx context.Context, request gen.GetReadyzRequestObject) (gen.GetReadyzResponseObject, error) {
	return nil, notImplemented()
}
