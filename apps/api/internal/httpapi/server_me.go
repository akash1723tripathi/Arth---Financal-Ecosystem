package httpapi

// me slice. Stub file created by the lead: replace each stub with the real implementation.

import (
	"context"

	"arth/api/internal/httpapi/gen"
)

func (s *Server) GetMe(ctx context.Context, request gen.GetMeRequestObject) (gen.GetMeResponseObject, error) {
	return nil, notImplemented()
}

func (s *Server) UpdateMe(ctx context.Context, request gen.UpdateMeRequestObject) (gen.UpdateMeResponseObject, error) {
	return nil, notImplemented()
}

func (s *Server) CompleteOnboarding(ctx context.Context, request gen.CompleteOnboardingRequestObject) (gen.CompleteOnboardingResponseObject, error) {
	return nil, notImplemented()
}
