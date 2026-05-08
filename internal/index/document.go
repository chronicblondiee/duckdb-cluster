package index

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/chronicblondiee/duckdb-cluster/internal/router"
)

// IndexDocument indexes a JSON document into the index.
// It handles auto-table creation, schema evolution, and shard routing.
func (idx *Index) IndexDocument(ctx context.Context, doc map[string]any) (*router.QueryResult, error) {
	// Phase 1: Hold lock only for state check, schema evolution, and SQL building.
	idx.mu.Lock()

	if idx.Meta.State != StateOpen {
		idx.mu.Unlock()
		return nil, fmt.Errorf("index %q is closed", idx.Meta.Name)
	}

	pkField := idx.Meta.Settings.PartitionKeyField
	if pkField == "" {
		pkField = "_id"
	}
	partitionKey := ""
	if pk, ok := doc[pkField]; ok {
		partitionKey = fmt.Sprintf("%v", pk)
	}
	if partitionKey == "" {
		idx.mu.Unlock()
		return nil, fmt.Errorf("document missing partition key field %q", pkField)
	}

	tableName := DefaultDocTable

	if len(idx.Mapping.Fields) == 0 {
		inferred := InferMappingFromDocument(doc)
		idx.Mapping.Fields = inferred.Fields
		idx.Mapping.dirty = true

		createSQL := idx.Mapping.GenerateCreateTableSQL(tableName)
		if createSQL != "" {
			if err := idx.Manager.ExecuteOnAll(ctx, createSQL); err != nil {
				idx.mu.Unlock()
				return nil, fmt.Errorf("create table from first document: %w", err)
			}
		}
	} else {
		if err := idx.Mapping.EvolveSchema(ctx, idx.Manager, tableName, doc); err != nil {
			idx.mu.Unlock()
			return nil, fmt.Errorf("evolve schema: %w", err)
		}
	}

	insertSQL, args := buildInsertSQL(tableName, idx.Mapping, doc)
	shardID := router.HashRoute(partitionKey, idx.Manager.ShardCount())

	idx.mu.Unlock()

	// Phase 2: Execute insert without holding the lock.
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
// Documents are grouped by target shard, then inserted with a single
// multi-row INSERT per shard executed in parallel.
func (idx *Index) IndexDocumentBulk(ctx context.Context, docs []map[string]any) (int64, int, error) {
	if len(docs) == 0 {
		return 0, 0, nil
	}

	// Phase 1: Hold lock for schema evolution + routing
	idx.mu.Lock()

	if idx.Meta.State != StateOpen {
		idx.mu.Unlock()
		return 0, 0, fmt.Errorf("index %q is closed", idx.Meta.Name)
	}

	pkField := idx.Meta.Settings.PartitionKeyField
	if pkField == "" {
		pkField = "_id"
	}
	tableName := DefaultDocTable
	numShards := idx.Manager.ShardCount()

	var failed int

	for _, doc := range docs {
		if len(idx.Mapping.Fields) == 0 {
			inferred := InferMappingFromDocument(doc)
			idx.Mapping.Fields = inferred.Fields
			idx.Mapping.dirty = true
			createSQL := idx.Mapping.GenerateCreateTableSQL(tableName)
			if createSQL != "" {
				if err := idx.Manager.ExecuteOnAll(ctx, createSQL); err != nil {
					idx.mu.Unlock()
					return 0, len(docs), fmt.Errorf("create table from first document: %w", err)
				}
			}
		} else {
			if err := idx.Mapping.EvolveSchema(ctx, idx.Manager, tableName, doc); err != nil {
				failed++
				continue
			}
		}
	}

	// Build sorted column list once for consistent ordering
	colNames := make([]string, 0, len(idx.Mapping.Fields))
	for n := range idx.Mapping.Fields {
		colNames = append(colNames, n)
	}
	sort.Strings(colNames)
	mappingSnapshot := idx.Mapping

	idx.mu.Unlock()

	// Phase 2: Group docs by shard
	shardDocs := make(map[int][]map[string]any, numShards)
	for _, doc := range docs {
		pk, ok := doc[pkField]
		if !ok || fmt.Sprintf("%v", pk) == "" {
			failed++
			continue
		}
		partitionKey := fmt.Sprintf("%v", pk)
		shardID := router.HashRoute(partitionKey, numShards)
		shardDocs[shardID] = append(shardDocs[shardID], doc)
	}

	// Phase 3: Build and execute one multi-row INSERT per shard, in parallel
	quotedCols := make([]string, len(colNames))
	for i, n := range colNames {
		quotedCols[i] = quoteIdent(n)
	}
	colList := strings.Join(quotedCols, ", ")
	rowPlaceholder := "(" + strings.Repeat("?, ", len(colNames)-1) + "?)"

	type shardResult struct {
		affected int64
		failed   int
	}

	var wg sync.WaitGroup
	results := make([]shardResult, numShards)

	for shardID, groupDocs := range shardDocs {
		wg.Add(1)
		go func(sid int, gdocs []map[string]any) {
			defer wg.Done()

			s := idx.Manager.GetShard(sid)
			if s == nil {
				results[sid] = shardResult{failed: len(gdocs)}
				return
			}

			// Build multi-row INSERT
			placeholders := make([]string, len(gdocs))
			values := make([]any, 0, len(gdocs)*len(colNames))
			for i, doc := range gdocs {
				placeholders[i] = rowPlaceholder
				for _, col := range colNames {
					val := doc[col]
					values = append(values, convertValue(mappingSnapshot.Fields[col], val))
				}
			}

			insertSQL := fmt.Sprintf("INSERT INTO %s (%s) VALUES %s",
				quoteIdent(tableName), colList, strings.Join(placeholders, ", "))

			result, err := s.Execute(ctx, insertSQL, values...)
			if err != nil {
				results[sid] = shardResult{failed: len(gdocs)}
				return
			}
			affected, _ := result.RowsAffected()
			results[sid] = shardResult{affected: affected}
		}(shardID, groupDocs)
	}

	wg.Wait()

	var totalAffected int64
	for _, r := range results {
		totalAffected += r.affected
		failed += r.failed
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
