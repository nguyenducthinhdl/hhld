package warehouse

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/nguyenducthinhdl/hhld/src/exchange"
	_ "modernc.org/sqlite"
)

const sqliteSchema = `
CREATE TABLE IF NOT EXISTS books (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	venue TEXT NOT NULL,
	symbol TEXT NOT NULL,
	kind TEXT NOT NULL,
	bids_json TEXT NOT NULL,
	asks_json TEXT NOT NULL,
	time_unix_nano INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_books_symbol_time ON books(symbol, time_unix_nano);

CREATE TABLE IF NOT EXISTS ticks (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	venue TEXT NOT NULL,
	symbol TEXT NOT NULL,
	kind TEXT NOT NULL,
	price TEXT NOT NULL,
	size TEXT NOT NULL,
	side TEXT NOT NULL,
	time_unix_nano INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_ticks_symbol_time ON ticks(symbol, time_unix_nano);
`

// SQLite is a local file-backed warehouse.Store (spec/tech-stack.md Data warehouse).
type SQLite struct {
	db *sql.DB
}

// Ensure SQLite implements Store.
var _ Store = (*SQLite)(nil)

// OpenSQLite opens (or creates) a SQLite warehouse at path.
func OpenSQLite(path string) (*SQLite, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("warehouse: open sqlite %q: %w", path, err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(sqliteSchema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("warehouse: init schema: %w", err)
	}
	return &SQLite{db: db}, nil
}

// Close releases the database handle.
func (s *SQLite) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *SQLite) AppendBook(ctx context.Context, b exchange.Book) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	bids, err := json.Marshal(b.Bids)
	if err != nil {
		return fmt.Errorf("warehouse: marshal bids: %w", err)
	}
	asks, err := json.Marshal(b.Asks)
	if err != nil {
		return fmt.Errorf("warehouse: marshal asks: %w", err)
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO books (venue, symbol, kind, bids_json, asks_json, time_unix_nano)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		string(b.Venue), string(b.Symbol), string(b.Kind), string(bids), string(asks), b.Time.UnixNano(),
	)
	if err != nil {
		return fmt.Errorf("warehouse: insert book: %w", err)
	}
	return nil
}

func (s *SQLite) AppendTick(ctx context.Context, t exchange.Tick) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO ticks (venue, symbol, kind, price, size, side, time_unix_nano)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		string(t.Venue), string(t.Symbol), string(t.Kind), t.Price, t.Size, string(t.Side), t.Time.UnixNano(),
	)
	if err != nil {
		return fmt.Errorf("warehouse: insert tick: %w", err)
	}
	return nil
}

func (s *SQLite) QueryBooks(ctx context.Context, symbol exchange.Symbol, from, to time.Time) ([]exchange.Book, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	fromNano := from.UnixNano()
	toNano := to.UnixNano()
	rows, err := s.db.QueryContext(ctx,
		`SELECT venue, symbol, kind, bids_json, asks_json, time_unix_nano
		 FROM books
		 WHERE symbol = ? AND time_unix_nano >= ? AND time_unix_nano <= ?
		 ORDER BY time_unix_nano ASC, id ASC`,
		string(symbol), fromNano, toNano,
	)
	if err != nil {
		return nil, fmt.Errorf("warehouse: query books: %w", err)
	}
	defer rows.Close()

	var out []exchange.Book
	for rows.Next() {
		var venue, sym, kind, bidsJSON, asksJSON string
		var ts int64
		if err := rows.Scan(&venue, &sym, &kind, &bidsJSON, &asksJSON, &ts); err != nil {
			return nil, fmt.Errorf("warehouse: scan book: %w", err)
		}
		var bids, asks []exchange.Level
		if err := json.Unmarshal([]byte(bidsJSON), &bids); err != nil {
			return nil, fmt.Errorf("warehouse: unmarshal bids: %w", err)
		}
		if err := json.Unmarshal([]byte(asksJSON), &asks); err != nil {
			return nil, fmt.Errorf("warehouse: unmarshal asks: %w", err)
		}
		out = append(out, exchange.Book{
			Venue:  exchange.VenueID(venue),
			Symbol: exchange.Symbol(sym),
			Kind:   exchange.Kind(kind),
			Bids:   bids,
			Asks:   asks,
			Time:   time.Unix(0, ts).UTC(),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *SQLite) QueryTicks(ctx context.Context, symbol exchange.Symbol, from, to time.Time) ([]exchange.Tick, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	fromNano := from.UnixNano()
	toNano := to.UnixNano()
	rows, err := s.db.QueryContext(ctx,
		`SELECT venue, symbol, kind, price, size, side, time_unix_nano
		 FROM ticks
		 WHERE symbol = ? AND time_unix_nano >= ? AND time_unix_nano <= ?
		 ORDER BY time_unix_nano ASC, id ASC`,
		string(symbol), fromNano, toNano,
	)
	if err != nil {
		return nil, fmt.Errorf("warehouse: query ticks: %w", err)
	}
	defer rows.Close()

	var out []exchange.Tick
	for rows.Next() {
		var venue, sym, kind, price, size, side string
		var ts int64
		if err := rows.Scan(&venue, &sym, &kind, &price, &size, &side, &ts); err != nil {
			return nil, fmt.Errorf("warehouse: scan tick: %w", err)
		}
		out = append(out, exchange.Tick{
			Venue:  exchange.VenueID(venue),
			Symbol: exchange.Symbol(sym),
			Kind:   exchange.Kind(kind),
			Price:  price,
			Size:   size,
			Side:   exchange.Side(side),
			Time:   time.Unix(0, ts).UTC(),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
