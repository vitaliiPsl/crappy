package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/vitaliipsl/crappy/memory"
)

var _ memory.Store = (*Store)(nil)

const schema = `
CREATE TABLE IF NOT EXISTS entries (
	id TEXT PRIMARY KEY,
	scope TEXT NOT NULL,
	content TEXT NOT NULL,
	source TEXT NOT NULL,
	metadata BLOB NOT NULL,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	version TEXT NOT NULL
) STRICT;
CREATE INDEX IF NOT EXISTS entries_scope_id ON entries(scope, id);
`

const (
	columns = "id, scope, content, source, metadata, created_at, updated_at, version"

	createQuery = `
INSERT INTO entries (` + columns + `)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO NOTHING
`
	getQuery = "SELECT " + columns + " FROM entries WHERE id = ?"

	updateQuery = `
UPDATE entries
SET scope = ?, content = ?, source = ?, metadata = ?, created_at = ?, updated_at = ?, version = ?
WHERE id = ? AND version = ?
`
	deleteQuery = "DELETE FROM entries WHERE id = ? AND version = ?"
	listQuery   = "SELECT " + columns + " FROM entries WHERE id > ?%s ORDER BY id"
	existsQuery = "SELECT EXISTS(SELECT 1 FROM entries WHERE id = ?)"
)

type Store struct {
	db *sql.DB
}

func New(db *sql.DB) (*Store, error) {
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("initialize memory database: %w", err)
	}

	return &Store{db: db}, nil
}

func (s *Store) Create(ctx context.Context, entry memory.Entry) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if entry.ID == "" || entry.Version == 0 {
		return errors.New("memory entry ID and version are required")
	}

	metadata, err := json.Marshal(entry.Metadata)
	if err != nil {
		return fmt.Errorf("encode memory metadata: %w", err)
	}

	result, err := s.db.ExecContext(ctx, createQuery,
		entry.ID,
		entry.Scope,
		entry.Content,
		entry.Source,
		metadata,
		entry.CreatedAt.Format(time.RFC3339Nano),
		entry.UpdatedAt.Format(time.RFC3339Nano),
		strconv.FormatUint(entry.Version, 10),
	)
	if err != nil {
		return fmt.Errorf("create memory entry: %w", err)
	}

	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count created memory entries: %w", err)
	}

	if count == 0 {
		return memory.ErrAlreadyExists
	}

	return nil
}

func (s *Store) Get(ctx context.Context, id string) (memory.Entry, error) {
	entry, err := scanEntry(s.db.QueryRowContext(ctx, getQuery, id))
	if errors.Is(err, sql.ErrNoRows) {
		return memory.Entry{}, memory.ErrNotFound
	}

	if err != nil {
		return memory.Entry{}, fmt.Errorf("get memory entry: %w", err)
	}

	return entry, nil
}

func (s *Store) Update(ctx context.Context, entry memory.Entry, expectedVersion uint64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin memory update: %w", err)
	}

	defer func() {
		_ = tx.Rollback()
	}()

	metadata, err := json.Marshal(entry.Metadata)
	if err != nil {
		return fmt.Errorf("encode memory metadata: %w", err)
	}

	result, err := tx.ExecContext(ctx, updateQuery,
		entry.Scope,
		entry.Content,
		entry.Source,
		metadata,
		entry.CreatedAt.Format(time.RFC3339Nano),
		entry.UpdatedAt.Format(time.RFC3339Nano),
		strconv.FormatUint(entry.Version, 10),
		entry.ID,
		strconv.FormatUint(expectedVersion, 10),
	)
	if err != nil {
		return fmt.Errorf("update memory entry: %w", err)
	}

	if err := checkMutation(ctx, tx, result, entry.ID); err != nil {
		return err
	}

	if entry.Version <= expectedVersion {
		return errors.New("replacement memory entry version must increase")
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit memory update: %w", err)
	}

	return nil
}

func (s *Store) Delete(ctx context.Context, id string, expectedVersion uint64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin memory deletion: %w", err)
	}

	defer func() {
		_ = tx.Rollback()
	}()

	result, err := tx.ExecContext(ctx, deleteQuery, id, strconv.FormatUint(expectedVersion, 10))
	if err != nil {
		return fmt.Errorf("delete memory entry: %w", err)
	}

	if err := checkMutation(ctx, tx, result, id); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit memory deletion: %w", err)
	}

	return nil
}

func (s *Store) List(ctx context.Context, filter memory.Filter) (memory.Page, error) {
	if err := ctx.Err(); err != nil {
		return memory.Page{}, err
	}

	if filter.Limit < 0 {
		return memory.Page{}, errors.New("memory list limit cannot be negative")
	}

	limit := filter.Limit
	if limit == 0 {
		limit = memory.DefaultPageSize
	}

	where, arguments := filters(filter.Scopes, filter.IDs)
	arguments = append([]any{filter.AfterID}, arguments...)

	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(listQuery, where), arguments...)
	if err != nil {
		return memory.Page{}, fmt.Errorf("list memory entries: %w", err)
	}

	defer func() {
		_ = rows.Close()
	}()

	page := memory.Page{Entries: make([]memory.Entry, 0)}
	for rows.Next() {
		if len(page.Entries) == limit {
			page.NextID = page.Entries[len(page.Entries)-1].ID

			break
		}

		entry, err := scanEntry(rows)
		if err != nil {
			return memory.Page{}, fmt.Errorf("read memory entry: %w", err)
		}

		page.Entries = append(page.Entries, entry)
	}

	if err := rows.Err(); err != nil {
		return memory.Page{}, fmt.Errorf("list memory entries: %w", err)
	}

	return page, nil
}

func checkMutation(ctx context.Context, tx *sql.Tx, result sql.Result, id string) error {
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count changed memory entries: %w", err)
	}

	if count != 0 {
		return nil
	}

	var exists bool
	if err := tx.QueryRowContext(ctx, existsQuery, id).Scan(&exists); err != nil {
		return fmt.Errorf("check memory entry existence: %w", err)
	}

	if !exists {
		return memory.ErrNotFound
	}

	return memory.ErrConflict
}

func filters(scopes, ids []string) (string, []any) {
	var (
		where     string
		arguments []any
	)

	if len(scopes) > 0 {
		where += " AND scope IN (" + strings.TrimSuffix(strings.Repeat("?,", len(scopes)), ",") + ")"
		for _, scope := range scopes {
			arguments = append(arguments, scope)
		}
	}

	if len(ids) > 0 {
		where += " AND id IN (" + strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",") + ")"
		for _, id := range ids {
			arguments = append(arguments, id)
		}
	}

	return where, arguments
}

func scanEntry(row interface{ Scan(...any) error }) (memory.Entry, error) {
	var (
		entry                memory.Entry
		metadata             []byte
		createdAt, updatedAt string
	)
	if err := row.Scan(
		&entry.ID,
		&entry.Scope,
		&entry.Content,
		&entry.Source,
		&metadata,
		&createdAt,
		&updatedAt,
		&entry.Version,
	); err != nil {
		return memory.Entry{}, err
	}

	if err := json.Unmarshal(metadata, &entry.Metadata); err != nil {
		return memory.Entry{}, fmt.Errorf("decode memory metadata: %w", err)
	}

	var err error

	entry.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return memory.Entry{}, fmt.Errorf("parse memory creation time: %w", err)
	}

	entry.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return memory.Entry{}, fmt.Errorf("parse memory update time: %w", err)
	}

	return entry, nil
}
