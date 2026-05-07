// Package store persists debts in SQLite. Each row is a single
// (creditor, debtor, currency) edge.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

const bootstrap = `
CREATE TABLE IF NOT EXISTS debts (
  creditor_id  TEXT    NOT NULL,
  debtor_id    TEXT    NOT NULL,
  currency     TEXT    NOT NULL,
  amount_minor INTEGER NOT NULL CHECK (amount_minor >= 0),
  inserted_at  INTEGER NOT NULL DEFAULT (unixepoch()),
  updated_at   INTEGER NOT NULL,
  PRIMARY KEY (creditor_id, debtor_id, currency)
) WITHOUT ROWID;
CREATE INDEX IF NOT EXISTS idx_debts_creditor ON debts(creditor_id);

CREATE TABLE IF NOT EXISTS payment_methods (
  user_id     TEXT    NOT NULL,
  method      TEXT    NOT NULL,
  value       TEXT    NOT NULL,
  inserted_at INTEGER NOT NULL DEFAULT (unixepoch()),
  updated_at  INTEGER NOT NULL,
  PRIMARY KEY (user_id, method)
) WITHOUT ROWID;
`

var migrations = []string{
	`ALTER TABLE payment_methods ADD COLUMN is_default INTEGER NOT NULL DEFAULT 0`,
}

var ErrUnknownMethod = errors.New("unknown payment method")

type Debt struct {
	Creditor    string
	Debtor      string
	Currency    string
	AmountMinor int64
	InsertedAt  int64
}

type PaymentMethod struct {
	UserID     string
	Method     string
	Value      string
	IsDefault  bool
	InsertedAt int64
}

type SQLite struct{ db *sql.DB }

// Open opens (or creates) the database at path and applies migrations.
func Open(path string) (*SQLite, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if _, err := db.Exec(bootstrap); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("bootstrap: %w", err)
	}
	if err := migrate(db, migrations); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &SQLite{db: db}, nil
}

func migrate(db *sql.DB, steps []string) error {
	var version int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return fmt.Errorf("read user_version: %w", err)
	}
	if version > len(steps) {
		return fmt.Errorf("database is at schema v%d but binary only knows v%d — downgrade is not supported", version, len(steps))
	}
	for i := version; i < len(steps); i++ {
		next := i + 1
		if err := runMigration(db, steps[i], next); err != nil {
			return fmt.Errorf("migration v%d: %w", next, err)
		}
	}
	return nil
}

func runMigration(db *sql.DB, stmt string, version int) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(stmt); err != nil {
		return err
	}
	if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", version)); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLite) Close() error { return s.db.Close() }

/*
 * Debt
 */

// AddDelta atomically applies delta to (creditor, debtor, currency), clamps
// the result at zero, and deletes the row if the result is zero. Returns the
// post-update amount (always >= 0).
func (s *SQLite) AddDelta(ctx context.Context, creditor, debtor, currency string, delta int64) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().Unix()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO debts(creditor_id, debtor_id, currency, amount_minor, inserted_at, updated_at)
		VALUES (?, ?, ?, MAX(0, ?), ?, ?)
		ON CONFLICT(creditor_id, debtor_id, currency)
		DO UPDATE SET
		  amount_minor = MAX(0, amount_minor + ?),
		  updated_at = excluded.updated_at`,
		creditor, debtor, currency, delta, now, now, delta,
	)
	if err != nil {
		return 0, err
	}

	var current int64
	err = tx.QueryRowContext(ctx,
		`SELECT amount_minor FROM debts WHERE creditor_id=? AND debtor_id=? AND currency=?`,
		creditor, debtor, currency,
	).Scan(&current)
	if err != nil && err != sql.ErrNoRows {
		return 0, err
	}

	if current == 0 {
		if _, err = tx.ExecContext(ctx,
			`DELETE FROM debts WHERE creditor_id=? AND debtor_id=? AND currency=?`,
			creditor, debtor, currency); err != nil {
			return 0, err
		}
		return 0, tx.Commit()
	}
	return current, tx.Commit()
}

func (s *SQLite) ResetPair(ctx context.Context, creditor, debtor string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM debts WHERE creditor_id=? AND debtor_id=?`, creditor, debtor)
	return err
}

func (s *SQLite) ResetPairCurrency(ctx context.Context, creditor, debtor, currency string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM debts WHERE creditor_id=? AND debtor_id=? AND currency=?`,
		creditor, debtor, currency)
	return err
}

func (s *SQLite) ListPair(ctx context.Context, creditor, debtor string) ([]Debt, error) {
	return s.query(ctx, `
		SELECT creditor_id, debtor_id, currency, amount_minor, inserted_at FROM debts
		WHERE creditor_id=? AND debtor_id=? AND amount_minor > 0
		ORDER BY currency`,
		creditor, debtor)
}

func (s *SQLite) ListByCreditor(ctx context.Context, creditor string) ([]Debt, error) {
	return s.query(ctx, `
		SELECT creditor_id, debtor_id, currency, amount_minor, inserted_at FROM debts
		WHERE creditor_id=? AND amount_minor > 0
		ORDER BY debtor_id, currency`,
		creditor)
}

func (s *SQLite) ListByDebtor(ctx context.Context, debtor string) ([]Debt, error) {
	return s.query(ctx, `
		SELECT creditor_id, debtor_id, currency, amount_minor, inserted_at FROM debts
		WHERE debtor_id=? AND amount_minor > 0
		ORDER BY creditor_id, currency`,
		debtor)
}

/*
 * Payment
 */

func (s *SQLite) SetPaymentMethod(ctx context.Context, userID, method, value string) error {
	now := time.Now().Unix()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO payment_methods(user_id, method, value, inserted_at, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(user_id, method)
		DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		userID, method, value, now, now)
	return err
}

func (s *SQLite) SetDefaultPaymentMethod(ctx context.Context, userID, method string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var dummy int
	err = tx.QueryRowContext(ctx,
		`SELECT 1 FROM payment_methods WHERE user_id=? AND method=?`,
		userID, method,
	).Scan(&dummy)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrUnknownMethod
	}
	if err != nil {
		return err
	}

	now := time.Now().Unix()
	if _, err := tx.ExecContext(ctx, `
		UPDATE payment_methods
		SET is_default = CASE WHEN method = ? THEN 1 ELSE 0 END,
		    updated_at = ?
		WHERE user_id = ?`,
		method, now, userID,
	); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLite) RemovePaymentMethod(ctx context.Context, userID, method string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM payment_methods WHERE user_id=? AND method=?`, userID, method)
	return err
}

func (s *SQLite) ClearPaymentMethods(ctx context.Context, userID string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM payment_methods WHERE user_id=?`, userID)
	return err
}

func (s *SQLite) DefaultPaymentMethod(ctx context.Context, userID string) (PaymentMethod, bool, error) {
	var p PaymentMethod
	err := s.db.QueryRowContext(ctx, `
		SELECT user_id, method, value, is_default, inserted_at FROM payment_methods
		WHERE user_id=? AND is_default=1`,
		userID,
	).Scan(&p.UserID, &p.Method, &p.Value, &p.IsDefault, &p.InsertedAt)

	if errors.Is(err, sql.ErrNoRows) {
		return PaymentMethod{}, false, nil
	}
	if err != nil {
		return PaymentMethod{}, false, err
	}
	return p, true, nil
}

func (s *SQLite) ListPaymentMethods(ctx context.Context, userID string) ([]PaymentMethod, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT user_id, method, value, is_default, inserted_at FROM payment_methods
		WHERE user_id=?
		ORDER BY is_default DESC, method`,
		userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []PaymentMethod
	for rows.Next() {
		var p PaymentMethod
		if err := rows.Scan(&p.UserID, &p.Method, &p.Value, &p.IsDefault, &p.InsertedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

/*
 * Helpers
 */

func (s *SQLite) query(ctx context.Context, q string, args ...any) ([]Debt, error) {
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Debt
	for rows.Next() {
		var d Debt
		if err := rows.Scan(&d.Creditor, &d.Debtor, &d.Currency, &d.AmountMinor, &d.InsertedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
