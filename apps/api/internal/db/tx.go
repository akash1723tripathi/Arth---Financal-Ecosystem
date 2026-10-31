package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// InTx runs fn inside one database transaction (READ COMMITTED, the Postgres default).
// fn's error is returned unchanged (so errors.As / errors.Is on domain errors keep working) after a rollback;
// begin and commit failures are wrapped. The rollback also runs if fn panics, and it is not cut short by a
// cancelled request context.
func InTx(ctx context.Context, pool *pgxpool.Pool, fn func(q *Queries) error) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	// Rollback after a successful Commit is a no-op (pgx.ErrTxClosed), so it is safe to always defer it.
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	if err := fn(New(tx)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}
