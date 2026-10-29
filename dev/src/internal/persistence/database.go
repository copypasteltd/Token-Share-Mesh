package persistence

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"p11.local/compute/shared/fault"
	"time"
)

type DB struct{ Pool *pgxpool.Pool }

func Open(ctx context.Context, dsn string) (*DB, error) {
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fault.Wrap(fault.Invalid, "Invalid database configuration", err)
	}
	config.MaxConns = 12
	config.MinConns = 0
	config.MaxConnLifetime = 30 * time.Minute
	config.ConnConfig.ConnectTimeout = 5 * time.Second
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fault.Wrap(fault.Unavailable, "Database unavailable", err)
	}
	if err = pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fault.Wrap(fault.Unavailable, "Database unavailable", err)
	}
	return &DB{Pool: pool}, nil
}
func (d *DB) Close() { d.Pool.Close() }

// Transaction never retries a callback. Callers must keep external I/O outside
// the transaction and use the original business key to resolve uncertain commits.
func (d *DB) Transaction(ctx context.Context, isolation pgx.TxIsoLevel, fn func(pgx.Tx) error) error {
	tx, err := d.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: isolation})
	if err != nil {
		return err
	}
	defer func() {
		rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(rollbackCtx)
	}()
	if _, err = tx.Exec(ctx, `SELECT set_config('lock_timeout','3000',true),set_config('statement_timeout','15000',true)`); err != nil {
		return err
	}
	if err = fn(tx); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		var pg *pgconn.PgError
		if errors.As(err, &pg) || errors.Is(err, pgx.ErrTxCommitRollback) {
			return err
		}
		return fault.Wrap(fault.CommitUnknown, "Commit result unknown; query the original operation", err)
	}
	return nil
}
