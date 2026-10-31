// Package migrate runs the embedded goose migrations (db/migrations) and reports schema readiness.
package migrate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"text/tabwriter"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"

	"arth/api/db/migrations"
)

// VersionTable is goose's bookkeeping table (the goose default).
const VersionTable = "goose_db_version"

// openDB opens a database/sql handle through the pgx driver. The URL is a secret and is never logged.
func openDB(ctx context.Context, url string) (*sql.DB, error) {
	cfg, err := pgx.ParseConfig(url)
	if err != nil {
		return nil, errors.New("parse migrations database url: invalid connection string")
	}
	db := stdlib.OpenDB(*cfg)
	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("connect to migrations database: %w", err)
	}
	return db, nil
}

func newProvider(db *sql.DB) (*goose.Provider, error) {
	locker, err := lock.NewPostgresSessionLocker(
		lock.WithLockTimeout(5, 60), // retry every 5s, up to 60 times (5 min)
	)
	if err != nil {
		return nil, fmt.Errorf("create session locker: %w", err)
	}
	p, err := goose.NewProvider(goose.DialectPostgres, db, migrations.FS, goose.WithSessionLocker(locker))
	if err != nil {
		return nil, fmt.Errorf("create goose provider: %w", err)
	}
	return p, nil
}

func withProvider(ctx context.Context, url string, fn func(p *goose.Provider) error) error {
	db, err := openDB(ctx, url)
	if err != nil {
		return err
	}
	defer db.Close()
	p, err := newProvider(db)
	if err != nil {
		return err
	}
	return fn(p)
}

// Up applies every pending migration. Results are written to w (may be nil).
func Up(ctx context.Context, url string, w io.Writer) error {
	return withProvider(ctx, url, func(p *goose.Provider) error {
		res, err := p.Up(ctx)
		writeResults(w, res)
		if err != nil {
			return fmt.Errorf("migrate up: %w", err)
		}
		if len(res) == 0 && w != nil {
			fmt.Fprintln(w, "no pending migrations")
		}
		return nil
	})
}

// Down rolls back the most recently applied migration (one step).
func Down(ctx context.Context, url string, w io.Writer) error {
	return withProvider(ctx, url, func(p *goose.Provider) error {
		res, err := p.Down(ctx)
		if err != nil {
			if errors.Is(err, goose.ErrNoNextVersion) {
				if w != nil {
					fmt.Fprintln(w, "nothing to roll back")
				}
				return nil
			}
			return fmt.Errorf("migrate down: %w", err)
		}
		writeResults(w, []*goose.MigrationResult{res})
		return nil
	})
}

// Status prints every migration with its state.
func Status(ctx context.Context, url string, w io.Writer) error {
	return withProvider(ctx, url, func(p *goose.Provider) error {
		st, err := p.Status(ctx)
		if err != nil {
			return fmt.Errorf("migrate status: %w", err)
		}
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "VERSION\tSTATE\tAPPLIED AT\tFILE")
		for _, s := range st {
			applied := "-"
			if !s.AppliedAt.IsZero() {
				applied = s.AppliedAt.UTC().Format(time.RFC3339)
			}
			fmt.Fprintf(tw, "%d\t%s\t%s\t%s\n", s.Source.Version, s.State, applied, path.Base(s.Source.Path))
		}
		return tw.Flush()
	})
}

func writeResults(w io.Writer, res []*goose.MigrationResult) {
	if w == nil {
		return
	}
	for _, r := range res {
		fmt.Fprintln(w, r.String())
		if r.Error != nil {
			fmt.Fprintf(w, "  error: %v\n", r.Error)
		}
	}
}

// Latest returns the highest migration version embedded in the binary.
func Latest() (int64, error) {
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		return 0, fmt.Errorf("read embedded migrations: %w", err)
	}
	var latest int64
	for _, e := range entries {
		if e.IsDir() || path.Ext(e.Name()) != ".sql" {
			continue
		}
		v, err := goose.NumericComponent(e.Name())
		if err != nil {
			return 0, fmt.Errorf("migration %s: %w", e.Name(), err)
		}
		latest = max(latest, v)
	}
	if latest == 0 {
		return 0, errors.New("no embedded migrations")
	}
	return latest, nil
}

// Querier is satisfied by *pgxpool.Pool, *pgx.Conn and pgx.Tx.
type Querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Pending reports the database's current schema version and the latest embedded version, for /readyz.
// It only reads (it never creates goose's table), so the app role needs no DDL rights.
// current == 0 means no migration has been applied.
func Pending(ctx context.Context, q Querier) (current, latest int64, err error) {
	latest, err = Latest()
	if err != nil {
		return 0, 0, err
	}
	var exists bool
	if err := q.QueryRow(ctx, "SELECT to_regclass($1) IS NOT NULL", VersionTable).Scan(&exists); err != nil {
		return 0, latest, fmt.Errorf("check %s: %w", VersionTable, err)
	}
	if !exists {
		return 0, latest, nil
	}
	// goose deletes the row on down-migration, so the highest applied version is the current one.
	if err := q.QueryRow(ctx, "SELECT COALESCE(MAX(version_id), 0) FROM "+VersionTable+" WHERE is_applied").Scan(&current); err != nil {
		return 0, latest, fmt.Errorf("read schema version: %w", err)
	}
	return current, latest, nil
}
