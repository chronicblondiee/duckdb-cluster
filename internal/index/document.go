package index

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/chronicblondiee/duckdb-cluster/internal/router"
)

// IndexDocument indexes a JSON document into the index.
// It handles auto-table creation, schema evolution, and shard routing.
func (idx *Index) IndexDocument(ctx context.Context, doc map[string]any) (*router.QueryResult, error) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	if idx.Meta.State != StateOpen {
		return nil, fmt.Errorf("index %q is closed", idx.Meta.Name)
	}

	// Extract partition key
	pkField := idx.Meta.Settings.PartitionKeyField
	if pkField == "" {
		pkField = "_id"
	}
	partitionKey := ""
	if pk, ok := doc[pkField]; ok {
		partitionKey = fmt.Sprintf("%v", pk)
	}
	if partitionKey == "" {
		return nil, fmt.Errorf("document missing partition key field %q", pkField)
	}

	tableName := DefaultDocTable

	// If no mapping fields exist, this is the first document — infer and create table
	if len(idx.Mapping.Fields) == 0 {
		inferred := InferMappingFromDocument(doc)
		idx.Mapping.Fields = inferred.Fields

		createSQL := idx.Mapping.GenerateCreateTableSQL(tableName)
		if createSQL != "" {
			if err := idx.Manager.ExecuteOnAll(ctx, createSQL); err != nil {
				return nil, fmt.Errorf("create table from first document: %w", err)
			}
		}
	} else {
		// Evolve schema for new fields
		if err := idx.Mapping.EvolveSchema(ctx, idx.Manager, tableName, doc); err != nil {
			return nil, fmt.Errorf("evolve schema: %w", err)
		}
	}

	// Build INSERT SQL
	insertSQL, args := buildInsertSQL(tableName, idx.Mapping, doc)

	// Route to shard
	shardID := router.HashRoute(partitionKey, idx.Manager.ShardCount())
	s := idx.Manager.GetShard(shardID)
	result, err := s.Execute(ctx, insertSQL, args...)
	if err != nil {
		return nil, fmt.Errorf("insert document: %w", err)
	}

	affected, _ := result.RowsAffected()
	return &router.QueryResult{
		RowsAffected: affected,
		ShardID:      shardID,
	}, nil
}

// IndexDocumentBulk indexes multiple documents in a single call.
// Documents are grouped by target shard for efficient execution.
func (idx *Index) IndexDocumentBulk(ctx context.Context, docs []map[string]any) (int64, int, error) {
	if len(docs) == 0 {
		return 0, 0, nil
	}

	var totalAffected int64
	var failed int

	for _, doc := range docs {
		result, err := idx.IndexDocument(ctx, doc)
		if err != nil {
			failed++
			continue
		}
		totalAffected += result.RowsAffected
	}

	return totalAffected, failed, nil
}

// buildInsertSQL constructs an INSERT statement from the mapping and document.
func buildInsertSQL(tableName string, mapping *Mapping, doc map[string]any) (string, []any) {
	// Use mapping field order for consistency
	names := make([]string, 0, len(mapping.Fields))
	for n := range mapping.Fields {
		names = append(names, n)
	}
	sort.Strings(names)

	columns := make([]string, 0, len(names))
	placeholders := make([]string, 0, len(names))
	args := make([]any, 0, len(names))

	for _, name := range names {
		val, ok := doc[name]
		if !ok {
			continue // skip fields not in this document
		}
		columns = append(columns, quoteIdent(name))
		placeholders = append(placeholders, "?")
		args = append(args, convertValue(mapping.Fields[name], val))
	}

	if len(columns) == 0 {
		return "", nil
	}

	sql := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		quoteIdent(tableName),
		strings.Join(columns, ", "),
		strings.Join(placeholders, ", "))

	return sql, args
}

// convertValue converts a Go value to a DuckDB-compatible value based on the field mapping.
func convertValue(fm *FieldMapping, val any) any {
	if val == nil {
		return nil
	}

	switch fm.Type {
	case FieldTypeStruct:
		// DuckDB accepts structs as maps via the Go driver
		if m, ok := val.(map[string]any); ok {
			return m
		}
		return val
	case FieldTypeList:
		// DuckDB accepts lists as slices via the Go driver
		if arr, ok := val.([]any); ok {
			return arr
		}
		return val
	default:
		return val
	}
}
