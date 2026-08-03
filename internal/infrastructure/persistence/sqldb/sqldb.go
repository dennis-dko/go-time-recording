// Package sqldb implements the domain repositories on top of a SQL database.
//
// One implementation serves every dialect GoFr supports rather than one
// package per engine: the queries are identical apart from placeholder syntax
// and how a generated id is read back, both of which are handled here.
package sqldb

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
	"time"
)

// Dialect names as understood by GoFr's DB_DIALECT setting.
const (
	DialectSQLite   = "sqlite"
	DialectPostgres = "postgres"
	DialectMySQL    = "mysql"
)

// DB is the subset of GoFr's container.DB the repositories use. Keeping it
// narrow lets tests drive the repositories with a plain *sql.DB.
type DB interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// base carries the wiring every repository shares.
type base struct {
	db      DB
	dialect string
}

func (b base) rebind(query string) string {
	return Rebind(b.dialect, query)
}

// txBeginner is a datasource that can start a transaction.
//
// Separate from DB and discovered by assertion rather than required, which is
// what keeps this package free of any dependency on GoFr. It works out because
// GoFr's *sql.DB is embedded in its own datasource type, so BeginTx is promoted
// onto it - and the *sql.Tx that comes back satisfies DB, so the same repository
// code runs inside a transaction and outside one without knowing which.
type txBeginner interface {
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
}

// withTx runs fn inside a transaction, so a sequence of statements either all
// land or none do.
//
// This matters wherever one logical change is several statements. Without it a
// connection lost half way through leaves the database describing something
// nobody asked for: a role stripped of its permissions between the delete and
// the insert that was meant to replace them, or a person's recorded hours gone
// while their account remains.
//
// fn is given a base bound to the transaction and must use only that. Reaching
// past it to the original datasource would take a second connection, which on
// SQLite means waiting for a lock the transaction itself is holding.
//
// A datasource that cannot begin one - a test double, say - runs fn directly.
// Silently, because the alternative is refusing to work at all in a test, and
// the guarantee a transaction adds is not one those tests are checking.
func (b base) withTx(ctx context.Context, fn func(tx base) error) error {
	beginner, ok := b.db.(txBeginner)
	if !ok {
		return fn(b)
	}

	tx, err := beginner.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	if err := fn(base{db: tx, dialect: b.dialect}); err != nil {
		// The original error is what the caller needs; a rollback that also
		// fails would only bury it, and the transaction is abandoned either way.
		_ = tx.Rollback()

		return err
	}

	return tx.Commit()
}

// Rebind rewrites the '?' placeholders used throughout this package into the
// dialect's own form. Only PostgreSQL differs; it wants ordinal $1, $2, ...
//
// Placeholders inside string literals would be rewritten too, so queries
// passed here must never contain a literal '?' in quoted text.
func Rebind(dialect, query string) string {
	if dialect != DialectPostgres {
		return query
	}

	var (
		out strings.Builder
		n   int
	)

	out.Grow(len(query) + 8)

	for i := range len(query) {
		if query[i] != '?' {
			out.WriteByte(query[i])

			continue
		}

		n++

		out.WriteByte('$')
		out.WriteString(strconv.Itoa(n))
	}

	return out.String()
}

// insert runs an INSERT and returns the generated primary key.
//
// PostgreSQL has no LastInsertId, so the id is read back with a RETURNING
// clause; the other dialects use the driver's LastInsertId.
func (b base) insert(ctx context.Context, query string, args ...any) (uint, error) {
	if b.dialect == DialectPostgres {
		var id uint

		row := b.db.QueryRowContext(ctx, b.rebind(query+" RETURNING id"), args...)
		if err := row.Scan(&id); err != nil {
			return 0, err
		}

		return id, nil
	}

	res, err := b.db.ExecContext(ctx, b.rebind(query), args...)
	if err != nil {
		return 0, err
	}

	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}

	return uint(id), nil
}

// exec runs a statement and reports how many rows it changed.
func (b base) exec(ctx context.Context, query string, args ...any) (int64, error) {
	res, err := b.db.ExecContext(ctx, b.rebind(query), args...)
	if err != nil {
		return 0, err
	}

	return res.RowsAffected()
}

// update runs an UPDATE and reports whether the row was there, which is not
// the same as whether anything changed.
//
// MySQL counts rows it actually *changed*; PostgreSQL and SQLite count rows
// they *matched*. So saving a record without editing any field reports zero
// affected rows on MySQL and one on the others - and treating zero as "no such
// row" turns an ordinary save into a 404. Re-saving a user's working hours at
// their current values did exactly that.
//
// The extra query only runs in the zero case, which on the other dialects
// means genuinely missing and on MySQL means missing or unchanged.
func (b base) update(
	ctx context.Context,
	table, query string,
	id uint,
	args ...any,
) (found bool, err error) {
	affected, err := b.exec(ctx, query, args...)
	if err != nil {
		return false, err
	}

	if affected > 0 {
		return true, nil
	}

	return b.exists(ctx, table, id)
}

// exists reports whether a row with this id is present.
//
// The table name is interpolated rather than bound: it never comes from a
// request, only from a caller in this package naming its own table, and a
// placeholder cannot stand in for an identifier anyway.
func (b base) exists(ctx context.Context, table string, id uint) (bool, error) {
	var found int

	err := b.db.QueryRowContext(ctx,
		b.rebind("SELECT COUNT(*) FROM "+table+" WHERE id = ?"), id).Scan(&found)
	if err != nil {
		return false, err
	}

	return found > 0, nil
}

// dateTime adapts date/timestamp columns across drivers. Depending on the
// dialect and driver a date arrives as a time.Time, a string, or a []byte, so
// scanning straight into time.Time is not portable.
type dateTime struct {
	Time  time.Time
	Valid bool
}

// dateLayouts covers what the supported drivers actually emit: RFC3339 and
// bare dates from SQLite, and space-separated timestamps from MySQL.
var dateLayouts = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02 15:04:05.999999999 -0700 MST",
	"2006-01-02 15:04:05.999999-07:00",
	"2006-01-02 15:04:05",
	"2006-01-02",
}

func (d *dateTime) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		d.Time, d.Valid = time.Time{}, false

		return nil
	case time.Time:
		d.Time, d.Valid = v, true

		return nil
	case []byte:
		return d.parse(string(v))
	case string:
		return d.parse(v)
	default:
		return &time.ParseError{Value: "unsupported date type"}
	}
}

func (d *dateTime) parse(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		d.Time, d.Valid = time.Time{}, false

		return nil
	}

	var lastErr error

	for _, layout := range dateLayouts {
		t, err := time.Parse(layout, raw)
		if err == nil {
			d.Time, d.Valid = t, true

			return nil
		}

		lastErr = err
	}

	return lastErr
}

// ptr returns a pointer to v, or nil when v is the zero value, matching how
// the domain models express "absent".
func ptr(d dateTime) *time.Time {
	if !d.Valid {
		return nil
	}

	t := d.Time

	return &t
}
