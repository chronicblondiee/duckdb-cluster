package shard

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"

	_ "github.com/duckdb/duckdb-go/v2"
)

// QueryResultSet contains query results with schema information
type QueryResultSet struct {
	Columns []string
	Types   []string // SQL types from rows.ColumnTypes()
	Rows    []map[string]any
}

type Shard struct {
	ID   int
	Path string
	db   *sql.DB
}

func NewShard(id int, dir string) (*Shard, error) {
	path := filepath.Join(dir, fmt.Sprintf("shard_%03d.duckdb", id))
	s := &Shard{ID: id, Path: path}
	if err := s.Open(); err != nil {
		return nil, fmt.Errorf("create shard %d: %w", id, err)
	}
	return s, nil
}

func (s *Shard) Open() error {
	db, err := sql.Open("duckdb", s.Path)
	if err != nil {
		return fmt.Errorf("open shard %d: %w", s.ID, err)
	}
	s.db = db
	return nil
}

func (s *Shard) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

func (s *Shard) Execute(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return s.db.ExecContext(ctx, query, args...)
}

func (s *Shard) Query(ctx context.Context, query string, args ...any) ([]map[string]any, error) {
	resultSet, err := s.QueryWithSchema(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return resultSet.Rows, nil
}

func (s *Shard) QueryWithSchema(ctx context.Context, query string, args ...any) (*QueryResultSet, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	colTypes, err := rows.ColumnTypes()
	if err != nil {
		return nil, err
	}

	types := make([]string, len(colTypes))
	for i, ct := range colTypes {
		types[i] = ct.DatabaseTypeName()
	}

	var results []map[string]any
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		row := make(map[string]any, len(cols))
		for i, col := range cols {
			row[col] = vals[i]
		}
		results = append(results, row)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return &QueryResultSet{
		Columns: cols,
		Types:   types,
		Rows:    results,
	}, nil
}
