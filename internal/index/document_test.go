package index

import (
	"context"
	"testing"
)

func TestIndexJSONDocument(t *testing.T) {
	dir := t.TempDir()
	meta := Metadata{
		Name:     "doc-test",
		Settings: Settings{ShardCount: 2, PartitionKeyField: "_id"},
	}

	idx, err := NewIndex(dir, meta)
	if err != nil {
		t.Fatalf("NewIndex: %v", err)
	}
	defer idx.Close()

	ctx := context.Background()

	// First document should auto-create table and mapping
	doc := map[string]any{
		"_id":     "doc1",
		"message": "hello world",
		"level":   float64(1),
	}

	result, err := idx.IndexDocument(ctx, doc)
	if err != nil {
		t.Fatalf("IndexDocument: %v", err)
	}
	if result.RowsAffected != 1 {
		t.Errorf("expected 1 row affected, got %d", result.RowsAffected)
	}
	if result.ShardID < 0 || result.ShardID >= 2 {
		t.Errorf("unexpected shard ID: %d", result.ShardID)
	}

	// Mapping should now have fields
	if len(idx.Mapping.Fields) != 3 {
		t.Errorf("expected 3 mapping fields, got %d", len(idx.Mapping.Fields))
	}
	if idx.Mapping.Fields["message"].Type != FieldTypeVarchar {
		t.Errorf("message type = %s, want VARCHAR", idx.Mapping.Fields["message"].Type)
	}
}

func TestAutoCreateTable(t *testing.T) {
	dir := t.TempDir()
	meta := Metadata{
		Name:     "auto-create",
		Settings: Settings{ShardCount: 1, PartitionKeyField: "_id"},
	}

	idx, err := NewIndex(dir, meta)
	if err != nil {
		t.Fatalf("NewIndex: %v", err)
	}
	defer idx.Close()

	ctx := context.Background()

	// Index a document
	doc := map[string]any{"_id": "1", "name": "test"}
	if _, err := idx.IndexDocument(ctx, doc); err != nil {
		t.Fatalf("IndexDocument: %v", err)
	}

	// Query to verify table was created and data inserted
	result, err := idx.Route(ctx, "SELECT * FROM _docs", "")
	if err != nil {
		t.Fatalf("SELECT: %v", err)
	}
	if len(result.Rows) != 1 {
		t.Errorf("expected 1 row, got %d", len(result.Rows))
	}
}

func TestPartitionKeyExtraction(t *testing.T) {
	dir := t.TempDir()
	meta := Metadata{
		Name:     "pk-test",
		Settings: Settings{ShardCount: 3, PartitionKeyField: "user_id"},
	}

	idx, err := NewIndex(dir, meta)
	if err != nil {
		t.Fatalf("NewIndex: %v", err)
	}
	defer idx.Close()

	ctx := context.Background()

	// Document with custom partition key field
	doc := map[string]any{"user_id": "user123", "data": "test"}
	result, err := idx.IndexDocument(ctx, doc)
	if err != nil {
		t.Fatalf("IndexDocument: %v", err)
	}
	if result.ShardID < 0 || result.ShardID >= 3 {
		t.Errorf("unexpected shard ID: %d", result.ShardID)
	}

	// Document without partition key should fail
	doc2 := map[string]any{"data": "no-pk"}
	_, err = idx.IndexDocument(ctx, doc2)
	if err == nil {
		t.Error("expected error for missing partition key")
	}
}

func TestBulkIndexDocuments(t *testing.T) {
	dir := t.TempDir()
	meta := Metadata{
		Name:     "bulk-test",
		Settings: Settings{ShardCount: 2, PartitionKeyField: "_id"},
	}

	idx, err := NewIndex(dir, meta)
	if err != nil {
		t.Fatalf("NewIndex: %v", err)
	}
	defer idx.Close()

	ctx := context.Background()

	docs := []map[string]any{
		{"_id": "1", "msg": "first"},
		{"_id": "2", "msg": "second"},
		{"_id": "3", "msg": "third"},
	}

	affected, failed, err := idx.IndexDocumentBulk(ctx, docs)
	if err != nil {
		t.Fatalf("IndexDocumentBulk: %v", err)
	}
	if affected != 3 {
		t.Errorf("expected 3 affected, got %d", affected)
	}
	if failed != 0 {
		t.Errorf("expected 0 failed, got %d", failed)
	}

	// Verify all data present
	result, err := idx.Route(ctx, "SELECT * FROM _docs", "")
	if err != nil {
		t.Fatalf("SELECT: %v", err)
	}
	if len(result.Rows) != 3 {
		t.Errorf("expected 3 rows, got %d", len(result.Rows))
	}
}

func TestSchemaEvolutionOnDocument(t *testing.T) {
	dir := t.TempDir()
	meta := Metadata{
		Name:     "evolve-test",
		Settings: Settings{ShardCount: 1, PartitionKeyField: "_id"},
	}

	idx, err := NewIndex(dir, meta)
	if err != nil {
		t.Fatalf("NewIndex: %v", err)
	}
	defer idx.Close()

	ctx := context.Background()

	// First document establishes schema
	doc1 := map[string]any{"_id": "1", "name": "Alice"}
	if _, err := idx.IndexDocument(ctx, doc1); err != nil {
		t.Fatalf("IndexDocument 1: %v", err)
	}

	// Second document adds a new field
	doc2 := map[string]any{"_id": "2", "name": "Bob", "age": float64(30)}
	if _, err := idx.IndexDocument(ctx, doc2); err != nil {
		t.Fatalf("IndexDocument 2: %v", err)
	}

	// Mapping should have 3 fields now
	if len(idx.Mapping.Fields) != 3 {
		t.Errorf("expected 3 fields after evolution, got %d", len(idx.Mapping.Fields))
	}

	// Both documents should be queryable
	result, err := idx.Route(ctx, "SELECT * FROM _docs", "")
	if err != nil {
		t.Fatalf("SELECT: %v", err)
	}
	if len(result.Rows) != 2 {
		t.Errorf("expected 2 rows, got %d", len(result.Rows))
	}
}
