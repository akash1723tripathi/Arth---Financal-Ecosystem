package httpapi

// transactions slice: list (keyset), create, get, update, delete, CSV export.
// Balance rules (docs/SCHEMA.md §5): every write that changes a balance runs in ONE db.InTx, balance updates
// are relative (ApplyBalanceDelta) and must affect exactly one row, several accounts are locked in ascending
// id order (LockAccounts).

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"arth/api/internal/apierr"
	"arth/api/internal/db"
	"arth/api/internal/httpapi/gen"
)

const (
	defaultTxPageSize = 50
	maxTxPageSize     = 200
	defaultTxInput    = "MANUAL"
)

func (s *Server) ListTransactions(ctx context.Context, request gen.ListTransactionsRequestObject) (gen.ListTransactionsResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	params := request.Params

	limit := int32(defaultTxPageSize)
	if params.Limit != nil {
		limit = *params.Limit
	}
	if limit < 1 || limit > maxTxPageSize {
		return nil, apierr.Validation(apierr.FieldError{Field: "query.limit", Message: "must be between 1 and 200"})
	}

	arg := db.ListTransactionsParams{
		UserID:     p.UserID,
		AccountID:  params.AccountId,
		CategoryID: params.CategoryId,
		Q:          params.Q,
		RowLimit:   limit + 1,
	}
	if params.Type != nil {
		t := string(*params.Type)
		arg.Type = &t
	}
	if params.From != nil {
		d := fromDate(*params.From)
		arg.FromDate = &d
	}
	if params.To != nil {
		d := fromDate(*params.To)
		arg.ToDate = &d
	}
	if arg.FromDate != nil && arg.ToDate != nil && arg.FromDate.After(*arg.ToDate) {
		return nil, apierr.Validation(apierr.FieldError{Field: "query.to", Message: "must be on or after from"})
	}

	filters := txFilterFingerprint(params)
	if params.Cursor != nil {
		c, err := decodeTxCursor(*params.Cursor, filters)
		if err != nil {
			return nil, err
		}
		arg.CursorOccurredOn = &c.OccurredOn
		arg.CursorCreatedAt = &c.CreatedAt
		arg.CursorID = &c.ID
	}

	rows, err := s.d.Q.ListTransactions(ctx, arg)
	if err != nil {
		return nil, fmt.Errorf("list transactions: %w", err)
	}

	var next *string
	if len(rows) > int(limit) {
		rows = rows[:limit]
		last := rows[len(rows)-1]
		c := encodeTxCursor(txCursor{OccurredOn: last.OccurredOn, CreatedAt: last.CreatedAt, ID: last.ID}, filters)
		next = &c
	}
	return gen.ListTransactions200JSONResponse{Data: toTransactions(rows), NextCursor: next}, nil
}

func (s *Server) CreateTransaction(ctx context.Context, request gen.CreateTransactionRequestObject) (gen.CreateTransactionResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	if request.Body == nil {
		return nil, apierr.BadRequest("request body is required")
	}
	b := *request.Body
	if b.AmountMinor <= 0 {
		return nil, apierr.Validation(apierr.FieldError{Field: "amount_minor", Message: "must be greater than 0"})
	}
	kind := string(b.Type)
	if !b.Type.Valid() {
		return nil, apierr.Validation(apierr.FieldError{Field: "type", Message: "must be INCOME or EXPENSE"})
	}
	inputMethod := defaultTxInput
	if b.InputMethod != nil {
		inputMethod = string(*b.InputMethod)
	}

	var (
		txn  db.Transaction
		acct db.Account
	)
	err = db.InTx(ctx, s.d.Pool, func(q *db.Queries) error {
		var err error
		acct, err = q.LockAccount(ctx, db.LockAccountParams{ID: b.AccountId, UserID: p.UserID})
		if err != nil {
			return txNotFoundOr(err, "lock account")
		}
		if acct.ArchivedAt != nil {
			return apierr.Conflict("conflict", "The account is archived.")
		}
		if err := txCheckCategory(ctx, q, b.CategoryId, p.UserID, kind); err != nil {
			return err
		}
		categoryID := b.CategoryId
		txn, err = q.InsertTransaction(ctx, db.InsertTransactionParams{
			UserID:          p.UserID,
			AccountID:       acct.ID,
			CategoryID:      &categoryID,
			Type:            kind,
			AmountMinor:     b.AmountMinor,
			Currency:        acct.Currency, // always the account's currency; clients never send one
			OccurredOn:      fromDate(b.OccurredOn),
			Description:     txEmptyToNil(b.Description),
			InputMethod:     inputMethod,
			VoiceTranscript: txEmptyToNil(b.VoiceTranscript),
		})
		if err != nil {
			return fmt.Errorf("insert transaction: %w", err)
		}
		if err := txApplyDelta(ctx, q, p.UserID, acct.ID, txn.DeltaMinor); err != nil {
			return err
		}
		// The account row is locked by this tx, so the new balance is exact.
		acct.BalanceMinor += txn.DeltaMinor
		// TODO(D-OPEN-9): enqueue the budget-check job in this same tx once a job queue exists.
		return nil
	})
	if err != nil {
		return nil, err
	}
	return gen.CreateTransaction201JSONResponse{
		Body:    gen.TransactionWithAccount{Transaction: toTransaction(txn), Account: toAccount(acct)},
		Headers: gen.CreateTransaction201ResponseHeaders{Location: "/v1/transactions/" + txn.ID.String()},
	}, nil
}

func (s *Server) GetTransaction(ctx context.Context, request gen.GetTransactionRequestObject) (gen.GetTransactionResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	t, err := s.d.Q.GetTransaction(ctx, db.GetTransactionParams{ID: request.Id, UserID: p.UserID})
	if err != nil {
		return nil, txNotFoundOr(err, "get transaction")
	}
	return gen.GetTransaction200JSONResponse(toTransaction(t)), nil
}

func (s *Server) UpdateTransaction(ctx context.Context, request gen.UpdateTransactionRequestObject) (gen.UpdateTransactionResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	if request.Body == nil {
		return nil, apierr.BadRequest("request body is required")
	}
	b := *request.Body
	if b.AmountMinor != nil && *b.AmountMinor <= 0 {
		return nil, apierr.Validation(apierr.FieldError{Field: "amount_minor", Message: "must be greater than 0"})
	}

	var (
		txn      db.Transaction
		affected []db.Account
	)
	err = db.InTx(ctx, s.d.Pool, func(q *db.Queries) error {
		old, err := q.GetTransactionForUpdate(ctx, db.GetTransactionForUpdateParams{ID: request.Id, UserID: p.UserID})
		if err != nil {
			return txNotFoundOr(err, "lock transaction")
		}
		if old.TransferID != nil {
			return apierr.Conflict("conflict", "Transfer legs cannot be edited here.")
		}

		// Merge the PATCH onto the locked row.
		next := db.UpdateTransactionParams{
			ID:              old.ID,
			UserID:          p.UserID,
			AccountID:       old.AccountID,
			CategoryID:      old.CategoryID,
			Type:            old.Type,
			AmountMinor:     old.AmountMinor,
			Currency:        old.Currency,
			OccurredOn:      old.OccurredOn,
			Description:     old.Description,
			InputMethod:     old.InputMethod,
			VoiceTranscript: old.VoiceTranscript,
		}
		if b.AccountId != nil {
			next.AccountID = *b.AccountId
		}
		if b.CategoryId != nil {
			c := *b.CategoryId
			next.CategoryID = &c
		}
		if b.Type != nil {
			next.Type = string(*b.Type)
		}
		if b.AmountMinor != nil {
			next.AmountMinor = *b.AmountMinor
		}
		if b.OccurredOn != nil {
			next.OccurredOn = fromDate(*b.OccurredOn)
		}
		if b.Description != nil {
			next.Description = txEmptyToNil(b.Description) // "" clears
		}
		if b.InputMethod != nil {
			next.InputMethod = string(*b.InputMethod)
		}
		if b.VoiceTranscript != nil {
			next.VoiceTranscript = txEmptyToNil(b.VoiceTranscript) // "" clears
		}

		// Lock the old and the new account in ascending id order (deadlock-free).
		ids := []uuid.UUID{old.AccountID}
		if next.AccountID != old.AccountID {
			ids = append(ids, next.AccountID)
		}
		locked, err := q.LockAccounts(ctx, db.LockAccountsParams{UserID: p.UserID, Ids: ids})
		if err != nil {
			return fmt.Errorf("lock accounts: %w", err)
		}
		if len(locked) != len(ids) {
			return apierr.NotFound() // the new account is missing or not yours
		}
		if next.AccountID != old.AccountID {
			target := txFindAccount(locked, next.AccountID)
			if target.ArchivedAt != nil {
				return apierr.Conflict("conflict", "The account is archived.")
			}
			if target.Currency != old.Currency {
				return apierr.Conflict("currency_mismatch", "The new account has a different currency.")
			}
			next.Currency = target.Currency
		}

		// Re-validate the category when it or the type changed (an unchanged pair stays valid even if the
		// category was archived later).
		if b.CategoryId != nil || next.Type != old.Type {
			if next.CategoryID == nil {
				return apierr.Validation(apierr.FieldError{Field: "category_id", Message: "is required"})
			}
			if err := txCheckCategory(ctx, q, *next.CategoryID, p.UserID, next.Type); err != nil {
				return err
			}
		}

		if err := txApplyDelta(ctx, q, p.UserID, old.AccountID, -old.DeltaMinor); err != nil {
			return err
		}
		txn, err = q.UpdateTransaction(ctx, next)
		if err != nil {
			return txNotFoundOr(err, "update transaction")
		}
		if err := txApplyDelta(ctx, q, p.UserID, txn.AccountID, txn.DeltaMinor); err != nil {
			return err
		}
		// Re-read the (still locked) accounts so the response carries the balances after the update.
		affected, err = q.LockAccounts(ctx, db.LockAccountsParams{UserID: p.UserID, Ids: ids})
		if err != nil {
			return fmt.Errorf("reload accounts: %w", err)
		}
		// TODO(D-OPEN-9): enqueue the budget-check job in this same tx once a job queue exists.
		return nil
	})
	if err != nil {
		return nil, err
	}
	return gen.UpdateTransaction200JSONResponse{Transaction: toTransaction(txn), Accounts: toAccounts(affected)}, nil
}

func (s *Server) DeleteTransaction(ctx context.Context, request gen.DeleteTransactionRequestObject) (gen.DeleteTransactionResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	err = db.InTx(ctx, s.d.Pool, func(q *db.Queries) error {
		row, err := q.DeleteTransaction(ctx, db.DeleteTransactionParams{ID: request.Id, UserID: p.UserID})
		if err != nil {
			return txNotFoundOr(err, "delete transaction")
		}
		if err := txApplyDelta(ctx, q, p.UserID, row.AccountID, -row.DeltaMinor); err != nil {
			return err
		}
		if row.TransferID != nil {
			// A transfer leg deletes the whole transfer (docs/API.md): remove the peer and reverse it too.
			peers, err := q.DeleteTransferPeer(ctx, db.DeleteTransferPeerParams{UserID: p.UserID, TransferID: row.TransferID, ID: request.Id})
			if err != nil {
				return fmt.Errorf("delete transfer peer: %w", err)
			}
			for _, peer := range peers {
				if err := txApplyDelta(ctx, q, p.UserID, peer.AccountID, -peer.DeltaMinor); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return gen.DeleteTransaction204Response{}, nil
}

// txCheckCategory validates a category reference: visible to the user (system or own), of the given kind,
// not archived. Anything else is a 422 on category_id (not a 404: the transaction is the resource).
func txCheckCategory(ctx context.Context, q *db.Queries, id, userID uuid.UUID, kind string) error {
	if _, err := q.GetUsableCategory(ctx, db.GetUsableCategoryParams{ID: id, UserID: userID, Kind: kind}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return apierr.Validation(apierr.FieldError{Field: "category_id", Message: "must be an existing category whose kind matches type"})
		}
		return fmt.Errorf("get category: %w", err)
	}
	return nil
}

// txApplyDelta changes an account balance relatively; anything but exactly one affected row is an error.
func txApplyDelta(ctx context.Context, q *db.Queries, userID, accountID uuid.UUID, delta int64) error {
	n, err := q.ApplyBalanceDelta(ctx, db.ApplyBalanceDeltaParams{Delta: delta, ID: accountID, UserID: userID})
	if err != nil {
		return fmt.Errorf("apply balance delta: %w", err)
	}
	if n != 1 {
		return fmt.Errorf("apply balance delta: %d rows affected, want 1", n)
	}
	return nil
}

// txNotFoundOr maps pgx.ErrNoRows to 404 and wraps anything else.
func txNotFoundOr(err error, op string) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return apierr.NotFound()
	}
	return fmt.Errorf("%s: %w", op, err)
}

func txFindAccount(rows []db.Account, id uuid.UUID) db.Account {
	for _, a := range rows {
		if a.ID == id {
			return a
		}
	}
	return db.Account{}
}

// txEmptyToNil maps an omitted or empty string to NULL.
func txEmptyToNil(s *string) *string {
	if s == nil || *s == "" {
		return nil
	}
	v := *s
	return &v
}
