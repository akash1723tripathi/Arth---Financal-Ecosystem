package httpapi

// auth slice: register, login, refresh (rotation + reuse detection), logout, logout-all, change password and
// account deletion (docs/AUTH.md §3, decisions in docs/DECISIONS.md "Auth questions").
//
// Logging: only user_id / session_id and outcomes. Never passwords, tokens, token hashes or emails.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"arth/api/internal/apierr"
	"arth/api/internal/auth"
	"arth/api/internal/db"
	"arth/api/internal/httpapi/gen"
)

// Register creates the user, the default "Cash" account, a session and the first refresh token in ONE
// transaction, then returns 201 TokenPair with Location: /v1/me.
func (s *Server) Register(ctx context.Context, request gen.RegisterRequestObject) (gen.RegisterResponseObject, error) {
	body := request.Body
	email := strings.TrimSpace(body.Email)
	if n := utf8.RuneCountInString(email); n < 3 || len(email) > 254 {
		return nil, apierr.Validation(apierr.FieldError{Field: "email", Message: "must be a valid email address"})
	}
	if err := auth.CheckPassword(body.Password); err != nil {
		return nil, err
	}
	if ok, wait := s.d.Limits.Register(requestMeta(ctx).IP); !ok {
		return nil, apierr.RateLimited(wait)
	}
	hash, err := s.d.Hasher.Hash(body.Password)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}
	var name *string
	if n := strings.TrimSpace(body.Name); n != "" {
		name = &n
	}

	var (
		user db.User
		sess db.Session
		rt   issuedRefresh
	)
	err = db.InTx(ctx, s.d.Pool, func(q *db.Queries) error {
		var err error
		user, err = q.CreateUser(ctx, db.CreateUserParams{Email: email, PasswordHash: hash, Name: name})
		if err != nil {
			if isUniqueViolation(err, "users_email_lower_uq") {
				return apierr.EmailTaken()
			}
			return fmt.Errorf("create user: %w", err)
		}
		if _, err := q.CreateAccount(ctx, db.CreateAccountParams{
			UserID: user.ID, Name: defaultAccountName, Type: defaultAccountType, Currency: defaultAccountCurrency, IsDefault: true,
		}); err != nil {
			return fmt.Errorf("create default account: %w", err)
		}
		sess, rt, err = s.startSession(ctx, q, user.ID, body.Client)
		return err
	})
	if err != nil {
		return nil, err
	}
	pair, err := s.tokenPair(user, sess.ID, rt)
	if err != nil {
		return nil, err
	}
	s.d.Log.InfoContext(ctx, "auth: registered", "user_id", user.ID.String(), "session_id", sess.ID.String())
	return gen.Register201JSONResponse{Body: pair, Headers: gen.Register201ResponseHeaders{Location: "/v1/me"}}, nil
}

// Login verifies email + password and opens a new session. Unknown email and wrong password give the same
// 401 invalid_credentials body, and an unknown email still runs a full Argon2id verify (dummy hash).
func (s *Server) Login(ctx context.Context, request gen.LoginRequestObject) (gen.LoginResponseObject, error) {
	body := request.Body
	email := strings.TrimSpace(body.Email)
	if ok, wait := s.d.Limits.LoginAttempt(email, requestMeta(ctx).IP); !ok {
		return nil, apierr.RateLimited(wait)
	}
	user, err := s.d.Q.GetUserByEmail(ctx, email)
	if errors.Is(err, pgx.ErrNoRows) {
		s.d.Hasher.VerifyDummy(body.Password)
		s.d.Limits.LoginFailed(email)
		return nil, apierr.InvalidCredentials()
	}
	if err != nil {
		return nil, fmt.Errorf("get user by email: %w", err)
	}
	ok, needsRehash, err := s.d.Hasher.Verify(body.Password, user.PasswordHash)
	if err != nil {
		// A malformed stored hash is a server-side problem, but the client still gets the generic answer.
		s.d.Log.ErrorContext(ctx, "auth: stored password hash unreadable", "user_id", user.ID.String(), "err", err)
	}
	if !ok {
		s.d.Limits.LoginFailed(email)
		return nil, apierr.InvalidCredentials()
	}
	if needsRehash {
		if h, err := s.d.Hasher.Hash(body.Password); err == nil {
			if err := s.d.Q.RehashUserPassword(ctx, db.RehashUserPasswordParams{PasswordHash: h, ID: user.ID}); err != nil {
				s.d.Log.WarnContext(ctx, "auth: rehash failed", "user_id", user.ID.String(), "err", err)
			}
		}
	}

	var (
		sess db.Session
		rt   issuedRefresh
	)
	err = db.InTx(ctx, s.d.Pool, func(q *db.Queries) error {
		var err error
		sess, rt, err = s.startSession(ctx, q, user.ID, body.Client)
		return err
	})
	if err != nil {
		return nil, err
	}
	pair, err := s.tokenPair(user, sess.ID, rt)
	if err != nil {
		return nil, err
	}
	s.d.Log.InfoContext(ctx, "auth: login", "user_id", user.ID.String(), "session_id", sess.ID.String())
	return gen.Login200JSONResponse(pair), nil
}

// refreshOutcome tells RefreshToken what happened inside the transaction. A reuse revocation must be
// COMMITTED, so the closure returns nil for it and the 401 is produced after InTx.
type refreshOutcome int

const (
	refreshRotated refreshOutcome = iota
	refreshReused
)

// RefreshToken rotates a refresh token (single use). Re-presenting a used token revokes the whole session
// (REFRESH_REUSE). There is no grace window (Q-8): clients serialise refreshes.
func (s *Server) RefreshToken(ctx context.Context, request gen.RefreshTokenRequestObject) (gen.RefreshTokenResponseObject, error) {
	hash, err := auth.ParseRefreshToken(request.Body.RefreshToken)
	if err != nil {
		return nil, apierr.RefreshInvalid()
	}
	var (
		outcome refreshOutcome
		row     db.GetRefreshTokenForUpdateRow
		next    issuedRefresh
	)
	err = db.InTx(ctx, s.d.Pool, func(q *db.Queries) error {
		var err error
		row, err = q.GetRefreshTokenForUpdate(ctx, hash)
		if errors.Is(err, pgx.ErrNoRows) {
			return apierr.RefreshInvalid()
		}
		if err != nil {
			return fmt.Errorf("get refresh token: %w", err)
		}
		now := s.d.Now()
		// docs/AUTH.md §3 Refresh step 2: revoked / past absolute session expiry / token expired → 401.
		if row.SessionRevokedAt != nil || !now.Before(row.SessionExpiresAt) || !now.Before(row.ExpiresAt) {
			return apierr.RefreshInvalid()
		}
		// Step 3: already used → reuse detected → revoke the session (committed below).
		if row.UsedAt != nil {
			return s.revokeForReuse(ctx, q, row, &outcome)
		}
		if ok, wait := s.d.Limits.Refresh(row.SessionID); !ok {
			return apierr.RateLimited(wait) // rolled back: the token stays unused, the client may retry later
		}
		n, err := q.MarkRefreshTokenUsed(ctx, row.ID)
		if err != nil {
			return fmt.Errorf("mark refresh token used: %w", err)
		}
		if n == 0 { // cannot happen under FOR UPDATE, but if it does it is a reuse
			return s.revokeForReuse(ctx, q, row, &outcome)
		}
		next, err = createRefreshToken(ctx, q, row.SessionID, refreshExpiry(now, s.d.Cfg.RefreshTokenTTL, row.SessionExpiresAt))
		if err != nil {
			return err
		}
		if err := q.SetRefreshTokenReplacedBy(ctx, db.SetRefreshTokenReplacedByParams{ReplacedBy: next.row.ID, ID: row.ID}); err != nil {
			return fmt.Errorf("set replaced_by: %w", err)
		}
		if _, err := q.TouchSession(ctx, db.TouchSessionParams{ID: row.SessionID, UserID: row.UserID}); err != nil {
			return fmt.Errorf("touch session: %w", err)
		}
		outcome = refreshRotated
		return nil
	})
	if err != nil {
		return nil, err
	}
	if outcome == refreshReused {
		s.d.Log.WarnContext(ctx, "security: refresh token reuse detected, session revoked",
			"user_id", row.UserID.String(), "session_id", row.SessionID.String())
		return nil, apierr.RefreshInvalid()
	}
	user, err := s.d.Q.GetUserByID(ctx, row.UserID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierr.RefreshInvalid() // user deleted concurrently
	}
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	pair, err := s.tokenPair(user, row.SessionID, next)
	if err != nil {
		return nil, err
	}
	return gen.RefreshToken200JSONResponse(pair), nil
}

// revokeForReuse revokes the token's session and marks the outcome; it returns nil so InTx commits.
func (s *Server) revokeForReuse(ctx context.Context, q *db.Queries, row db.GetRefreshTokenForUpdateRow, outcome *refreshOutcome) error {
	if _, err := q.RevokeSession(ctx, db.RevokeSessionParams{Reason: revokeRefreshReuse, ID: row.SessionID, UserID: row.UserID}); err != nil {
		return fmt.Errorf("revoke session on reuse: %w", err)
	}
	*outcome = refreshReused
	return nil
}

// Logout revokes the session that owns refresh_token, but ONLY if that session belongs to the authenticated
// user. An unknown, malformed or foreign token is a silent no-op that still answers 204, so logout is not an
// oracle for other people's tokens. (Chosen over "revoke the caller's current session": the body names the
// session to end, which is what the spec says; the current session is ended by sending its own token.)
func (s *Server) Logout(ctx context.Context, request gen.LogoutRequestObject) (gen.LogoutResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	hash, err := auth.ParseRefreshToken(request.Body.RefreshToken)
	if err != nil {
		return gen.Logout204Response{}, nil
	}
	var revoked uuid.UUID
	err = db.InTx(ctx, s.d.Pool, func(q *db.Queries) error {
		row, err := q.GetRefreshTokenForUpdate(ctx, hash)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("get refresh token: %w", err)
		}
		if row.UserID != p.UserID {
			return nil // someone else's token: do nothing (RevokeSession is user-scoped anyway)
		}
		n, err := q.RevokeSession(ctx, db.RevokeSessionParams{Reason: revokeLogout, ID: row.SessionID, UserID: p.UserID})
		if err != nil {
			return fmt.Errorf("revoke session: %w", err)
		}
		if n > 0 {
			revoked = row.SessionID
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if revoked != uuid.Nil {
		s.d.Log.InfoContext(ctx, "auth: logout", "user_id", p.UserID.String(), "session_id", revoked.String())
	}
	return gen.Logout204Response{}, nil
}

// LogoutAll revokes every session of the user, including the current one.
func (s *Server) LogoutAll(ctx context.Context, request gen.LogoutAllRequestObject) (gen.LogoutAllResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	n, err := s.d.Q.RevokeAllUserSessions(ctx, db.RevokeAllUserSessionsParams{Reason: revokeLogoutAll, UserID: p.UserID})
	if err != nil {
		return nil, fmt.Errorf("revoke all sessions: %w", err)
	}
	s.d.Log.InfoContext(ctx, "auth: logout all", "user_id", p.UserID.String(), "sessions_revoked", n)
	return gen.LogoutAll204Response{}, nil
}

// verifyCurrentPassword re-checks the caller's password for a sensitive action. Failures count against the
// same per-email/per-IP login limits, so a stolen access token cannot be used to brute-force the password.
func (s *Server) verifyCurrentPassword(ctx context.Context, userID uuid.UUID, password string) (db.User, error) {
	user, err := s.d.Q.GetUserByID(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return db.User{}, apierr.Unauthorized()
	}
	if err != nil {
		return db.User{}, fmt.Errorf("get user: %w", err)
	}
	if ok, wait := s.d.Limits.LoginAttempt(user.Email, requestMeta(ctx).IP); !ok {
		return db.User{}, apierr.RateLimited(wait)
	}
	ok, _, err := s.d.Hasher.Verify(password, user.PasswordHash)
	if err != nil {
		s.d.Log.ErrorContext(ctx, "auth: stored password hash unreadable", "user_id", user.ID.String(), "err", err)
	}
	if !ok {
		s.d.Limits.LoginFailed(user.Email)
		return db.User{}, apierr.InvalidCredentials()
	}
	return user, nil
}

// ChangePassword verifies the current password, stores the new hash and revokes all OTHER sessions.
func (s *Server) ChangePassword(ctx context.Context, request gen.ChangePasswordRequestObject) (gen.ChangePasswordResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := s.verifyCurrentPassword(ctx, p.UserID, request.Body.CurrentPassword); err != nil {
		return nil, err
	}
	if err := auth.CheckPassword(request.Body.NewPassword); err != nil {
		return nil, err
	}
	hash, err := s.d.Hasher.Hash(request.Body.NewPassword)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}
	var revoked int64
	err = db.InTx(ctx, s.d.Pool, func(q *db.Queries) error {
		if err := q.UpdateUserPasswordHash(ctx, db.UpdateUserPasswordHashParams{PasswordHash: hash, ID: p.UserID}); err != nil {
			return fmt.Errorf("update password hash: %w", err)
		}
		n, err := q.RevokeOtherSessions(ctx, db.RevokeOtherSessionsParams{Reason: revokePasswordChanged, UserID: p.UserID, KeepID: p.SessionID})
		if err != nil {
			return fmt.Errorf("revoke other sessions: %w", err)
		}
		revoked = n
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.d.Log.InfoContext(ctx, "auth: password changed", "user_id", p.UserID.String(), "session_id", p.SessionID.String(), "sessions_revoked", revoked)
	return gen.ChangePassword204Response{}, nil
}

// DeleteMe re-verifies the password and deletes the user; ON DELETE CASCADE removes accounts, transactions,
// budgets, categories, notifications, sessions and refresh tokens. The access token then fails the per-request
// session check (the session row is gone) → 401.
func (s *Server) DeleteMe(ctx context.Context, request gen.DeleteMeRequestObject) (gen.DeleteMeResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := s.verifyCurrentPassword(ctx, p.UserID, request.Body.Password); err != nil {
		return nil, err
	}
	n, err := s.d.Q.DeleteUser(ctx, p.UserID)
	if err != nil {
		return nil, fmt.Errorf("delete user: %w", err)
	}
	if n == 0 {
		return nil, apierr.Unauthorized()
	}
	s.d.Log.InfoContext(ctx, "auth: account deleted", "user_id", p.UserID.String())
	return gen.DeleteMe204Response{}, nil
}
