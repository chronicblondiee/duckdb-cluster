package index

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/chronicblondiee/duckdb-cluster/internal/shard"
)

func TestValidateName(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
	}{
		{"logs", false},
		{"my-index", false},
		{"index_v2", false},
		{"a", false},
		{"", true},
		{"_reserved", true},
		{"UPPERCASE", true},
		{"has spaces", true},
		{"123starts-with-number", true},
	}
	for _, tt := range tests {
		err := ValidateName(tt.name)
		if (err != nil) != tt.wantErr {
			t.Errorf("ValidateName(%q) error = %v, wantErr %v", tt.name, err, tt.wantErr)
		}
	}
}

func TestValidateShardCount(t *testing.T) {
	tests := []struct {
		n       int
		wantErr bool
	}{
		{1, false},
		{3, false},
		{1000, false},
		{0, true},
		{-1, true},
		{1001, true},
	}
	for _, tt := range tests {
		err := ValidateShardCount(tt.n)
		if (err != nil) != tt.wantErr {
			t.Errorf("ValidateShardCount(%d) error = %v, wantErr %v", tt.n, err, tt.wantErr)
		}
	}
}

func TestNewIndex(t *testing.T) {
	dir := t.TempDir()
	meta := Metadata{
		Name:     "test-idx",
		Settings: Settings{ShardCount: 3, PartitionKeyField: "_id"},
	}

	idx, err := NewIndex(dir, meta)
	if err != nil {
		t.Fatalf("NewIndex: %v", err)
	}
	defer idx.Close()

	if idx.Manager.ShardCount() != 3 {
		t.Errorf("expected 3 shards, got %d", idx.Manager.ShardCount())
	}
	if idx.Meta.State != StateOpen {
		t.Errorf("expected state open, got %s", idx.Meta.State)
	}

	// Verify shard files exist
	indexDir := filepath.Join(dir, "indices", "test-idx")
	for i := 0; i < 3; i++ {
		path := filepath.Join(indexDir, "shard_"+padID(i)+".duckdb")
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("shard file %s does not exist", path)
		}
	}
}

func TestOpenIndex(t *testing.T) {
	dir := t.TempDir()
	meta := Metadata{
		Name:     "reopen-idx",
		Settings: Settings{ShardCount: 2, PartitionKeyField: "_id"},
	}

	// Create
	idx, err := NewIndex(dir, meta)
	if err != nil {
		t.Fatalf("NewIndex: %v", err)
	}
	idx.Close()

	// Reopen
	meta.State = StateOpen
	idx2, err := OpenIndex(dir, meta)
	if err != nil {
		t.Fatalf("OpenIndex: %v", err)
	}
	defer idx2.Close()

	if idx2.Manager.ShardCount() != 2 {
		t.Errorf("expected 2 shards, got %d", idx2.Manager.ShardCount())
	}
}

func TestIndexCloseAndReopen(t *testing.T) {
	dir := t.TempDir()
	meta := Metadata{
		Name:     "close-idx",
		Settings: Settings{ShardCount: 2, PartitionKeyField: "_id"},
	}

	idx, err := NewIndex(dir, meta)
	if err != nil {
		t.Fatalf("NewIndex: %v", err)
	}

	if err := idx.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if idx.Meta.State != StateClosed {
		t.Errorf("expected closed, got %s", idx.Meta.State)
	}

	// Route should fail on closed index
	_, err = idx.Route(context.Background(), "SELECT 1", "")
	if err == nil {
		t.Error("expected error routing to closed index")
	}

	// Reopen
	if err := idx.Reopen(); err != nil {
		t.Fatalf("Reopen: %v", err)
	}
	if idx.Meta.State != StateOpen {
		t.Errorf("expected open, got %s", idx.Meta.State)
	}
}

func TestIndexDelete(t *testing.T) {
	dir := t.TempDir()
	meta := Metadata{
		Name:     "delete-idx",
		Settings: Settings{ShardCount: 1, PartitionKeyField: "_id"},
	}

	idx, err := NewIndex(dir, meta)
	if err != nil {
		t.Fatalf("NewIndex: %v", err)
	}

	dataDir := idx.DataDir()
	if err := idx.Delete(); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if _, err := os.Stat(dataDir); !os.IsNotExist(err) {
		t.Error("expected data directory to be removed")
	}
}

func TestRegistryCreateGet(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry(dir)

	// Create two indices
	idx1, err := reg.Create("logs", Settings{ShardCount: 3})
	if err != nil {
		t.Fatalf("Create logs: %v", err)
	}
	if idx1.Manager.ShardCount() != 3 {
		t.Errorf("logs: expected 3 shards, got %d", idx1.Manager.ShardCount())
	}

	idx2, err := reg.Create("metrics", Settings{ShardCount: 2})
	if err != nil {
		t.Fatalf("Create metrics: %v", err)
	}
	if idx2.Manager.ShardCount() != 2 {
		t.Errorf("metrics: expected 2 shards, got %d", idx2.Manager.ShardCount())
	}

	// Get
	got, err := reg.Get("logs")
	if err != nil {
		t.Fatalf("Get logs: %v", err)
	}
	if got.Meta.Name != "logs" {
		t.Errorf("expected logs, got %s", got.Meta.Name)
	}

	// Duplicate
	_, err = reg.Create("logs", Settings{ShardCount: 1})
	if err == nil {
		t.Error("expected error creating duplicate index")
	}

	// Not found
	_, err = reg.Get("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent index")
	}

	reg.CloseAll()
}

func TestRegistryList(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry(dir)

	reg.Create("a-idx", Settings{ShardCount: 1})
	reg.Create("b-idx", Settings{ShardCount: 2})

	list := reg.List()
	if len(list) != 2 {
		t.Errorf("expected 2 indices, got %d", len(list))
	}

	reg.CloseAll()
}

func TestRegistryDelete(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry(dir)

	reg.Create("to-delete", Settings{ShardCount: 1})
	if err := reg.Delete("to-delete"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, err := reg.Get("to-delete")
	if err == nil {
		t.Error("expected error after delete")
	}

	// Verify directory removed
	indexDir := filepath.Join(dir, "indices", "to-delete")
	if _, err := os.Stat(indexDir); !os.IsNotExist(err) {
		t.Error("expected directory to be removed")
	}
}

func TestRegistryLoadAll(t *testing.T) {
	dir := t.TempDir()

	// Create and populate
	reg1 := NewRegistry(dir)
	reg1.Create("persist-a", Settings{ShardCount: 2})
	reg1.Create("persist-b", Settings{ShardCount: 3})
	reg1.CloseAll()

	// Reload
	reg2 := NewRegistry(dir)
	if err := reg2.LoadAll(); err != nil {
		t.Fatalf("LoadAll: %v", err)
	}

	list := reg2.List()
	if len(list) != 2 {
		t.Fatalf("expected 2 indices after reload, got %d", len(list))
	}

	idxA, err := reg2.Get("persist-a")
	if err != nil {
		t.Fatalf("Get persist-a: %v", err)
	}
	if idxA.Manager.ShardCount() != 2 {
		t.Errorf("persist-a: expected 2 shards, got %d", idxA.Manager.ShardCount())
	}

	reg2.CloseAll()
}

func TestRegistryMigration(t *testing.T) {
	dir := t.TempDir()

	// Create flat shard files (old layout) using shard.NewManager for valid DuckDB files
	m, err := shard.NewManager(dir, 3)
	if err != nil {
		t.Fatalf("create flat shards: %v", err)
	}
	m.CloseAll()

	// LoadAll should migrate
	reg := NewRegistry(dir)
	if err := reg.LoadAll(); err != nil {
		t.Fatalf("LoadAll (migration): %v", err)
	}

	// Should have _default index
	idx, err := reg.Get("_default")
	if err != nil {
		t.Fatalf("Get _default: %v", err)
	}
	if idx.Manager.ShardCount() != 3 {
		t.Errorf("expected 3 shards after migration, got %d", idx.Manager.ShardCount())
	}

	// Flat files should be gone
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".duckdb" {
			t.Errorf("flat shard file still exists: %s", e.Name())
		}
	}

	// Metadata file should exist
	metaPath := filepath.Join(dir, "indices", catalogFile)
	if _, err := os.Stat(metaPath); os.IsNotExist(err) {
		t.Error("metadata file not created")
	}

	reg.CloseAll()
}

func TestIndexScopedRouting(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry(dir)

	idx1, err := reg.Create("idx-a", Settings{ShardCount: 2, PartitionKeyField: "_id"})
	if err != nil {
		t.Fatalf("Create idx-a: %v", err)
	}
	idx2, err := reg.Create("idx-b", Settings{ShardCount: 3, PartitionKeyField: "_id"})
	if err != nil {
		t.Fatalf("Create idx-b: %v", err)
	}

	ctx := context.Background()

	// Create tables in each index
	if _, err := idx1.Route(ctx, "CREATE TABLE data (id INTEGER, val VARCHAR)", ""); err != nil {
		t.Fatalf("CREATE TABLE idx-a: %v", err)
	}
	if _, err := idx2.Route(ctx, "CREATE TABLE data (id INTEGER, val VARCHAR)", ""); err != nil {
		t.Fatalf("CREATE TABLE idx-b: %v", err)
	}

	// Insert into idx-a
	if _, err := idx1.Route(ctx, "INSERT INTO data VALUES (1, 'a')", "1"); err != nil {
		t.Fatalf("INSERT idx-a: %v", err)
	}

	// Insert into idx-b
	if _, err := idx2.Route(ctx, "INSERT INTO data VALUES (2, 'b')", "2"); err != nil {
		t.Fatalf("INSERT idx-b: %v", err)
	}

	// Read from idx-a — should only have 1 row
	resA, err := idx1.Route(ctx, "SELECT * FROM data", "")
	if err != nil {
		t.Fatalf("SELECT idx-a: %v", err)
	}
	if len(resA.Rows) != 1 {
		t.Errorf("idx-a: expected 1 row, got %d", len(resA.Rows))
	}

	// Read from idx-b — should only have 1 row
	resB, err := idx2.Route(ctx, "SELECT * FROM data", "")
	if err != nil {
		t.Fatalf("SELECT idx-b: %v", err)
	}
	if len(resB.Rows) != 1 {
		t.Errorf("idx-b: expected 1 row, got %d", len(resB.Rows))
	}

	reg.CloseAll()
}

func TestRegistryCloseAndOpenIndex(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry(dir)

	reg.Create("closeable", Settings{ShardCount: 1})

	if err := reg.CloseIndex("closeable"); err != nil {
		t.Fatalf("CloseIndex: %v", err)
	}

	// Should not be gettable as open
	_, err := reg.Get("closeable")
	if err == nil {
		t.Error("expected error getting closed index")
	}

	// Should be gettable as any
	idx, err := reg.GetAny("closeable")
	if err != nil {
		t.Fatalf("GetAny: %v", err)
	}
	if idx.Meta.State != StateClosed {
		t.Errorf("expected closed, got %s", idx.Meta.State)
	}

	// Reopen
	if err := reg.OpenIndex("closeable"); err != nil {
		t.Fatalf("OpenIndex: %v", err)
	}

	_, err = reg.Get("closeable")
	if err != nil {
		t.Fatalf("Get after reopen: %v", err)
	}

	reg.CloseAll()
}

func padID(id int) string {
	return fmt.Sprintf("%03d", id)
}
