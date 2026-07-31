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
