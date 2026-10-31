package httpapi

// auth slice. Stub file created by the lead: replace each stub with the real implementation.

import (
	"context"

	"arth/api/internal/httpapi/gen"
)

func (s *Server) Login(ctx context.Context, request gen.LoginRequestObject) (gen.LoginResponseObject, error) {
	return nil, notImplemented()
}

func (s *Server) Logout(ctx context.Context, request gen.LogoutRequestObject) (gen.LogoutResponseObject, error) {
	return nil, notImplemented()
}

func (s *Server) LogoutAll(ctx context.Context, request gen.LogoutAllRequestObject) (gen.LogoutAllResponseObject, error) {
	return nil, notImplemented()
}

func (s *Server) ChangePassword(ctx context.Context, request gen.ChangePasswordRequestObject) (gen.ChangePasswordResponseObject, error) {
	return nil, notImplemented()
}

func (s *Server) RefreshToken(ctx context.Context, request gen.RefreshTokenRequestObject) (gen.RefreshTokenResponseObject, error) {
	return nil, notImplemented()
}

func (s *Server) Register(ctx context.Context, request gen.RegisterRequestObject) (gen.RegisterResponseObject, error) {
	return nil, notImplemented()
}

func (s *Server) DeleteMe(ctx context.Context, request gen.DeleteMeRequestObject) (gen.DeleteMeResponseObject, error) {
	return nil, notImplemented()
}
