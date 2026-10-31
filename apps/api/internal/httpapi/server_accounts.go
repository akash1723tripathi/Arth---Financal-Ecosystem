package httpapi

// accounts + categories slice (docs/API.md "Accounts", "Categories"; docs/SCHEMA.md §4–§5).
// Every query is scoped by the principal's user id; another user's account is 404 not_found.

import (
	"context"
	"fmt"
	"strings"

	"arth/api/internal/apierr"
	"arth/api/internal/db"
	"arth/api/internal/httpapi/gen"
	"arth/api/internal/money"
)

// acctFallbackCurrency is the currency of a new account when neither the request nor the user has one
// (D-19 placeholder, same as the register default account).
const acctFallbackCurrency = "INR"

func (s *Server) ListAccounts(ctx context.Context, request gen.ListAccountsRequestObject) (gen.ListAccountsResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.d.Q.ListAccounts(ctx, p.UserID)
	if err != nil {
		return nil, fmt.Errorf("list accounts: %w", err)
	}
	return gen.ListAccounts200JSONResponse{Data: toAccounts(rows)}, nil
}

func (s *Server) CreateAccount(ctx context.Context, request gen.CreateAccountRequestObject) (gen.CreateAccountResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	b := request.Body
	if b == nil {
		return nil, apierr.BadRequest("missing request body")
	}
	var fields []apierr.FieldError
	name := strings.TrimSpace(b.Name)
	if name == "" {
		fields = append(fields, apierr.FieldError{Field: "name", Message: "must not be blank"})
	}
	currency := ""
	if b.Currency != nil {
		currency = *b.Currency
		if !money.IsSupported(currency) {
			fields = append(fields, meUnsupportedCurrency("currency"))
		}
	}
	if len(fields) > 0 {
		return nil, apierr.Validation(fields...)
	}
	if currency == "" {
		u, err := s.d.Q.GetUserByID(ctx, p.UserID)
		if err != nil {
			return nil, meNoRows(err, "get user")
		}
		currency = acctFallbackCurrency
		if u.Currency != nil && money.IsSupported(*u.Currency) {
			currency = *u.Currency
		}
	}
	var opening int64
	if b.OpeningBalanceMinor != nil {
		opening = *b.OpeningBalanceMinor
	}
	a, err := s.d.Q.CreateAccount(ctx, db.CreateAccountParams{
		UserID: p.UserID, Name: name, Type: string(b.Type), Currency: currency,
		OpeningBalanceMinor: opening, IsDefault: false,
	})
	if err != nil {
		return nil, fmt.Errorf("create account: %w", err)
	}
	return gen.CreateAccount201JSONResponse{
		Body:    toAccount(a),
		Headers: gen.CreateAccount201ResponseHeaders{Location: "/v1/accounts/" + a.ID.String()},
	}, nil
}

func (s *Server) GetAccount(ctx context.Context, request gen.GetAccountRequestObject) (gen.GetAccountResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	a, err := s.d.Q.GetAccount(ctx, db.GetAccountParams{ID: request.Id, UserID: p.UserID})
	if err != nil {
		return nil, meNoRows(err, "get account")
	}
	return gen.GetAccount200JSONResponse(toAccount(a)), nil
}

func (s *Server) UpdateAccount(ctx context.Context, request gen.UpdateAccountRequestObject) (gen.UpdateAccountResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	b := request.Body
	if b == nil {
		return nil, apierr.BadRequest("missing request body")
	}
	params := db.UpdateAccountParams{ID: request.Id, UserID: p.UserID}
	if b.Name != nil {
		name := strings.TrimSpace(*b.Name)
		if name == "" {
			return nil, apierr.Validation(apierr.FieldError{Field: "name", Message: "must not be blank"})
		}
		params.Name = &name
	}
	if b.Type != nil {
		t := string(*b.Type)
		params.Type = &t
	}
	a, err := s.d.Q.UpdateAccount(ctx, params)
	if err != nil {
		return nil, meNoRows(err, "update account")
	}
	return gen.UpdateAccount200JSONResponse(toAccount(a)), nil
}

func (s *Server) MakeDefaultAccount(ctx context.Context, request gen.MakeDefaultAccountRequestObject) (gen.MakeDefaultAccountResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	var out db.Account
	err = db.InTx(ctx, s.d.Pool, func(q *db.Queries) error {
		// Serialise default changes per user: lock all of the user's accounts (ascending id), then the
		// clear+set below see every committed change and the one-default partial unique index holds.
		if _, err := q.LockUserAccounts(ctx, p.UserID); err != nil {
			return fmt.Errorf("lock user accounts: %w", err)
		}
		a, err := q.GetAccount(ctx, db.GetAccountParams{ID: request.Id, UserID: p.UserID})
		if err != nil {
			return meNoRows(err, "get account")
		}
		if a.IsDefault {
			out = a
			return nil
		}
		// The partial unique index is not deferrable: clear the old default before setting the new one.
		if err := q.ClearDefaultAccount(ctx, p.UserID); err != nil {
			return fmt.Errorf("clear default account: %w", err)
		}
		a, err = q.SetDefaultAccount(ctx, db.SetDefaultAccountParams{ID: request.Id, UserID: p.UserID})
		if err != nil {
			return meNoRows(err, "set default account")
		}
		out = a
		return nil
	})
	if err != nil {
		return nil, err
	}
	return gen.MakeDefaultAccount200JSONResponse(toAccount(out)), nil
}

func (s *Server) DeleteAccount(ctx context.Context, request gen.DeleteAccountRequestObject) (gen.DeleteAccountResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	cascade := request.Params.Cascade != nil && *request.Params.Cascade
	err = db.InTx(ctx, s.d.Pool, func(q *db.Queries) error {
		// The row lock blocks concurrent transaction writes on this account until we commit.
		a, err := q.LockAccount(ctx, db.LockAccountParams{ID: request.Id, UserID: p.UserID})
		if err != nil {
			return meNoRows(err, "lock account")
		}
		if a.IsDefault {
			return apierr.Conflict("default_account", "the default account cannot be deleted; make another account the default first")
		}
		n, err := q.CountAccountTransactions(ctx, db.CountAccountTransactionsParams{AccountID: a.ID, UserID: p.UserID})
		if err != nil {
			return fmt.Errorf("count account transactions: %w", err)
		}
		if n > 0 && !cascade {
			return apierr.Conflict("account_has_transactions", "the account has transactions; pass cascade=true to delete them too").
				WithExtra("transaction_count", n)
		}
		// Transactions go with the account (composite FK ON DELETE CASCADE). Transfers do not exist yet
		// (D-21); when they do, the peer legs in other accounts must be deleted and reversed first.
		rows, err := q.DeleteAccount(ctx, db.DeleteAccountParams{ID: a.ID, UserID: p.UserID})
		if err != nil {
			return fmt.Errorf("delete account: %w", err)
		}
		if rows != 1 {
			return apierr.NotFound()
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return gen.DeleteAccount204Response{}, nil
}

func (s *Server) ListCategories(ctx context.Context, request gen.ListCategoriesRequestObject) (gen.ListCategoriesResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	var kind *string
	if request.Params.Kind != nil {
		k := string(*request.Params.Kind)
		kind = &k
	}
	rows, err := s.d.Q.ListCategories(ctx, db.ListCategoriesParams{UserID: p.UserID, Kind: kind})
	if err != nil {
		return nil, fmt.Errorf("list categories: %w", err)
	}
	out := make([]gen.Category, 0, len(rows))
	for _, c := range rows {
		out = append(out, toCategory(c))
	}
	return gen.ListCategories200JSONResponse{Data: out}, nil
}
