package auth

import (
	"context"

	"github.com/google/uuid"
)

// Principal is the authenticated caller, taken only from a verified access token.
type Principal struct {
	UserID    uuid.UUID
	SessionID uuid.UUID
}

type principalKey struct{}

// WithPrincipal returns ctx carrying p (set by the auth middleware).
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

// PrincipalFrom returns the principal set by the auth middleware. ok is false when there is none
// (or it has a nil user id).
func PrincipalFrom(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	if !ok || p.UserID == uuid.Nil {
		return Principal{}, false
	}
	return p, true
}
