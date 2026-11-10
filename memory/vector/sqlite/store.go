package sqlite

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	_ "modernc.org/sqlite" // init sqlite driver

	"github.com/vitaliipsl/crappy/memory"
	"github.com/vitaliipsl/crappy/memory/vector"
)

var _ vector.Store = (*Store)(nil)

const schema = `
CREATE TABLE IF NOT EXISTS vectors (
	id TEXT PRIMARY KEY,
	scope TEXT NOT NULL,
	version TEXT NOT NULL,
	space TEXT NOT NULL,
	embedding BLOB NOT NULL,
	metadata BLOB NOT NULL
) STRICT;
CREATE INDEX IF NOT EXISTS vectors_space_scope ON vectors(space, scope);
`

const (
	columns = "id, scope, version, space, embedding, metadata"

	getQuery    = "SELECT " + columns + " FROM vectors WHERE id = ?"
	listQuery   = "SELECT " + columns + " FROM vectors WHERE id > ?%s ORDER BY id"
	searchQuery = "SELECT " + columns + " FROM vectors WHERE space = ?%s"

	upsertQuery = `
INSERT INTO vectors (id, scope, version, space, embedding, metadata)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
	scope = excluded.scope,
	version = excluded.version,
	space = excluded.space,
	embedding = excluded.embedding,
	metadata = excluded.metadata
`

	removeQuery = "DELETE FROM vectors WHERE id = ?"
)

type Option func(*Store)

func WithSimilarity(similarity vector.Similarity) Option {
	return func(store *Store) {
		store.similarity = similarity
	}
}

type Store struct {
	db         *sql.DB
	similarity vector.Similarity
}

func New(path string, options ...Option) (*Store, error) {
	if path == "" {
		return nil, errors.New("vector database path is required")
	}

	path, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve vector database path: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create vector database directory: %w", err)
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create vector database: %w", err)
	}

	if err := file.Close(); err != nil {
		return nil, fmt.Errorf("close vector database file: %w", err)
	}

	dsn := url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
	parameters := url.Values{}
	parameters.Add("_pragma", "busy_timeout(5000)")
	parameters.Add("_pragma", "journal_mode(WAL)")
	parameters.Add("_pragma", "synchronous(FULL)")
	dsn.RawQuery = parameters.Encode()

	db, err := sql.Open("sqlite", dsn.String())
	if err != nil {
		return nil, fmt.Errorf("open vector database: %w", err)
	}

	db.SetMaxOpenConns(1)

	if _, err := db.Exec(schema); err != nil {
		return nil, errors.Join(fmt.Errorf("initialize vector database: %w", err), db.Close())
	}

	store := &Store{db: db, similarity: vector.Cosine}
	for _, option := range options {
		option(store)
	}

	return store, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) Get(ctx context.Context, id string) (vector.Record, error) {
	row := s.db.QueryRowContext(ctx, getQuery, id)

	record, err := scanRecord(row)
	if errors.Is(err, sql.ErrNoRows) {
		return vector.Record{}, vector.ErrNotFound
	}

	if err != nil {
		return vector.Record{}, fmt.Errorf("load vector %q: %w", id, err)
	}

	return record, nil
}

func (s *Store) List(ctx context.Context, filter memory.Filter) (vector.Page, error) {
	if filter.Limit < 0 {
		return vector.Page{}, errors.New("vector list limit cannot be negative")
	}

	limit := filter.Limit
	if limit == 0 {
		limit = memory.DefaultPageSize
	}

	where, arguments := filters(filter.Scopes, filter.IDs)
	arguments = append([]any{filter.AfterID}, arguments...)

	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(listQuery, where), arguments...)
	if err != nil {
		return vector.Page{}, fmt.Errorf("list vectors: %w", err)
	}

	defer func() { _ = rows.Close() }()

	page := vector.Page{Records: make([]vector.Record, 0)}
	for rows.Next() {
		if len(page.Records) == limit {
			page.NextID = page.Records[len(page.Records)-1].ID

			break
		}

		record, err := scanRecord(rows)
		if err != nil {
			return vector.Page{}, fmt.Errorf("read vector page: %w", err)
		}

		page.Records = append(page.Records, record)
	}

	if err := rows.Err(); err != nil {
		return vector.Page{}, fmt.Errorf("list vectors: %w", err)
	}

	return page, nil
}

func (s *Store) Upsert(ctx context.Context, records []vector.Record) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin vector upsert: %w", err)
	}

	defer func() { _ = tx.Rollback() }()

	statement, err := tx.PrepareContext(ctx, upsertQuery)
	if err != nil {
		return fmt.Errorf("prepare vector upsert: %w", err)
	}

	defer func() { _ = statement.Close() }()

	for _, record := range records {
		if record.ID == "" || record.Version == 0 {
			return errors.New("vector record ID and version are required")
		}

		if strings.TrimSpace(record.Space) == "" {
			return errors.New("vector record space is required")
		}

		if err := validateVector(record.Values); err != nil {
			return fmt.Errorf("vector %q: %w", record.ID, err)
		}

		metadata, err := json.Marshal(record.Metadata)
		if err != nil {
			return fmt.Errorf("encode vector %q metadata: %w", record.ID, err)
		}

		if _, err := statement.ExecContext(ctx, record.ID, record.Scope, strconv.FormatUint(record.Version, 10), record.Space, encodeVector(record.Values), metadata); err != nil {
			return fmt.Errorf("upsert vector %q: %w", record.ID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit vector upsert: %w", err)
	}

	return nil
}

func (s *Store) Remove(ctx context.Context, id string) error {
	if id == "" {
		return errors.New("removed vector ID is required")
	}

	if _, err := s.db.ExecContext(ctx, removeQuery, id); err != nil {
		return fmt.Errorf("remove vector %q: %w", id, err)
	}

	return nil
}

func (s *Store) Search(ctx context.Context, query vector.Query) ([]memory.Candidate, error) {
	if strings.TrimSpace(query.Space) == "" {
		return nil, errors.New("query space is required")
	}

	if query.Limit < 0 {
		return nil, errors.New("vector search limit cannot be negative")
	}

	if err := validateVector(query.Values); err != nil {
		return nil, fmt.Errorf("query vector: %w", err)
	}

	limit := query.Limit
	if limit == 0 {
		limit = memory.DefaultSearchLimit
	}

	where, arguments := filters(query.Scopes, query.IDs)
	arguments = append([]any{query.Space}, arguments...)

	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(searchQuery, where), arguments...)
	if err != nil {
		return nil, fmt.Errorf("search vectors: %w", err)
	}

	defer func() { _ = rows.Close() }()

	candidates := make([]memory.Candidate, 0)
	for rows.Next() {
		record, err := scanRecord(rows)
		if err != nil {
			return nil, fmt.Errorf("read search vector: %w", err)
		}

		if len(query.Values) != len(record.Values) {
			return nil, fmt.Errorf("vector %q has %d dimensions, query has %d", record.ID, len(record.Values), len(query.Values))
		}

		score := s.similarity(query.Values, record.Values)
		if math.IsNaN(score) || math.IsInf(score, 0) {
			return nil, fmt.Errorf("vector %q similarity score must be finite", record.ID)
		}

		candidates = append(candidates, memory.Candidate{ID: record.ID, Version: record.Version, Score: score})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("search vectors: %w", err)
	}

	slices.SortFunc(candidates, func(a, b memory.Candidate) int {
		if order := cmp.Compare(b.Score, a.Score); order != 0 {
			return order
		}

		return strings.Compare(a.ID, b.ID)
	})

	return candidates[:min(limit, len(candidates))], nil
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

func scanRecord(row interface{ Scan(...any) error }) (vector.Record, error) {
	var (
		record           vector.Record
		values, metadata []byte
	)

	if err := row.Scan(&record.ID, &record.Scope, &record.Version, &record.Space, &values, &metadata); err != nil {
		return vector.Record{}, err
	}

	if len(values) == 0 || len(values)%4 != 0 {
		return vector.Record{}, errors.New("stored vector must contain float32 values")
	}

	record.Values = make([]float32, len(values)/4)
	for position := range record.Values {
		record.Values[position] = math.Float32frombits(binary.LittleEndian.Uint32(values[position*4:]))
	}

	if err := validateVector(record.Values); err != nil {
		return vector.Record{}, err
	}

	if err := json.Unmarshal(metadata, &record.Metadata); err != nil {
		return vector.Record{}, fmt.Errorf("decode vector metadata: %w", err)
	}

	return record, nil
}

func encodeVector(values []float32) []byte {
	data := make([]byte, 0, len(values)*4)
	for _, value := range values {
		data = binary.LittleEndian.AppendUint32(data, math.Float32bits(value))
	}

	return data
}

func validateVector(values []float32) error {
	var squaredNorm float64
	for _, value := range values {
		coordinate := float64(value)
		if math.IsNaN(coordinate) || math.IsInf(coordinate, 0) {
			return errors.New("vector coordinates must be finite")
		}

		squaredNorm += coordinate * coordinate
	}

	if squaredNorm == 0 {
		return errors.New("vector must be non-zero")
	}

	return nil
}
