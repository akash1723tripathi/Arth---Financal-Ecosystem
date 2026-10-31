package httpapi

// Helpers for the auth slice (server_auth.go): session + refresh-token creation and the TokenPair response.
// Nothing here logs passwords, tokens, token hashes or emails.

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"arth/api/internal/auth"
	"arth/api/internal/db"
	"arth/api/internal/httpapi/gen"
)

// Session revoke reasons (sessions.revoke_reason CHECK constraint).
const (
	revokeLogout          = "LOGOUT"
	revokeLogoutAll       = "LOGOUT_ALL"
	revokeRefreshReuse    = "REFRESH_REUSE"
	revokePasswordChanged = "PASSWORD_CHANGED"
)

// Default account created at register (docs/DECISIONS.md D-19: INR is a placeholder until onboarding).
const (
	defaultAccountName     = "Cash"
	defaultAccountType     = "CASH"
	defaultAccountCurrency = "INR"
)

// issuedRefresh is a freshly stored refresh token: the raw value goes to the client once.
type issuedRefresh struct {
	raw       string
	expiresAt time.Time
	row       db.RefreshToken
}

// createRefreshToken stores the SHA-256 of a new refresh token for session, expiring at expiresAt.
func createRefreshToken(ctx context.Context, q *db.Queries, sessionID uuid.UUID, expiresAt time.Time) (issuedRefresh, error) {
	raw, hash, err := auth.NewRefreshToken()
	if err != nil {
		return issuedRefresh{}, err
	}
	row, err := q.CreateRefreshToken(ctx, db.CreateRefreshTokenParams{SessionID: sessionID, TokenHash: hash, ExpiresAt: expiresAt})
	if err != nil {
		return issuedRefresh{}, fmt.Errorf("create refresh token: %w", err)
	}
	return issuedRefresh{raw: raw, expiresAt: row.ExpiresAt, row: row}, nil
}

// startSession creates a session (absolute lifetime Cfg.SessionMaxAge) and its first refresh token
// (idle lifetime Cfg.RefreshTokenTTL, never past the session's absolute expiry). Run it inside a tx.
func (s *Server) startSession(ctx context.Context, q *db.Queries, userID uuid.UUID, client gen.ClientType) (db.Session, issuedRefresh, error) {
	now := s.d.Now()
	meta := requestMeta(ctx)
	var ua *string
	if meta.UserAgent != "" {
		v := meta.UserAgent
		ua = &v
	}
	var ip *netip.Addr
	if a, err := netip.ParseAddr(meta.IP); err == nil {
		a = a.Unmap()
		ip = &a
	}
	sess, err := q.CreateSession(ctx, db.CreateSessionParams{
		UserID: userID, Client: string(client), UserAgent: ua, Ip: ip, ExpiresAt: now.Add(s.d.Cfg.SessionMaxAge),
	})
	if err != nil {
		return db.Session{}, issuedRefresh{}, fmt.Errorf("create session: %w", err)
	}
	rt, err := createRefreshToken(ctx, q, sess.ID, refreshExpiry(now, s.d.Cfg.RefreshTokenTTL, sess.ExpiresAt))
	if err != nil {
		return db.Session{}, issuedRefresh{}, err
	}
	return sess, rt, nil
}

// refreshExpiry is the sliding idle expiry (now + ttl), capped at the session's absolute expiry.
func refreshExpiry(now time.Time, ttl time.Duration, sessionExpiresAt time.Time) time.Time {
	exp := now.Add(ttl)
	if exp.After(sessionExpiresAt) {
		return sessionExpiresAt
	}
	return exp
}

// tokenPair mints an access token for (user, session) and builds the TokenPair response.
func (s *Server) tokenPair(u db.User, sessionID uuid.UUID, rt issuedRefresh) (gen.TokenPair, error) {
	access, expiresIn, err := s.d.Tokens.Issue(u.ID, sessionID)
	if err != nil {
		return gen.TokenPair{}, err
	}
	return gen.TokenPair{
		AccessToken:      access,
		AccessExpiresIn:  int32(expiresIn / time.Second),
		RefreshToken:     rt.raw,
		RefreshExpiresAt: rt.expiresAt.UTC(),
		User:             toMe(u),
	}, nil
}

// isUniqueViolation reports a 23505 on the named constraint/index.
func isUniqueViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == constraint
}
