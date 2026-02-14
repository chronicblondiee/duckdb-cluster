package rebalance

import (
	"context"
	"fmt"
	"log/slog"
	"testing"

	"github.com/chronicblondiee/duckdb-cluster/internal/router"
	"github.com/chronicblondiee/duckdb-cluster/internal/shard"
)

func setupManager(t *testing.T, numShards int) *shard.Manager {
	t.Helper()
	dir := t.TempDir()
	m, err := shard.NewManager(dir, numShards)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	t.Cleanup(func() { m.CloseAll() })
	return m
}

func createTable(t *testing.T, m *shard.Manager, table string) {
	t.Helper()
	ctx := context.Background()
	ddl := fmt.Sprintf("CREATE TABLE %s (id VARCHAR, name VARCHAR)", quoteIdent(table))
	if err := m.ExecuteOnAll(ctx, ddl); err != nil {
		t.Fatalf("create table: %v", err)
	}
}

func insertRow(t *testing.T, m *shard.Manager, table, id, name string, numShards int) {
	t.Helper()
	ctx := context.Background()
	shardID := router.HashRoute(id, numShards)
	s := m.GetShard(shardID)
	sql := fmt.Sprintf("INSERT INTO %s VALUES (?, ?)", quoteIdent(table))
	if _, err := s.Execute(ctx, sql, id, name); err != nil {
		t.Fatalf("insert %s=%s on shard %d: %v", id, name, shardID, err)
	}
}

func countAllRows(t *testing.T, m *shard.Manager, table string) int64 {
	t.Helper()
	ctx := context.Background()
	var total int64
	for i := 0; i < m.ShardCount(); i++ {
		s := m.GetShard(i)
		rows, err := s.Query(ctx, fmt.Sprintf("SELECT COUNT(*) AS cnt FROM %s", quoteIdent(table)))
		if err != nil {
			t.Fatalf("count on shard %d: %v", i, err)
		}
		if len(rows) > 0 {
			if v, ok := rows[0]["cnt"].(int64); ok {
				total += v
			}
		}
	}
	return total
}

// verifyCorrectPlacement checks that every row is on the shard it should be on.
func verifyCorrectPlacement(t *testing.T, m *shard.Manager, table, pkCol string) {
	t.Helper()
	ctx := context.Background()
	shardCount := m.ShardCount()
	for i := 0; i < shardCount; i++ {
		s := m.GetShard(i)
		rows, err := s.Query(ctx, fmt.Sprintf("SELECT CAST(%s AS VARCHAR) AS pk FROM %s", quoteIdent(pkCol), quoteIdent(table)))
		if err != nil {
			t.Fatalf("query shard %d: %v", i, err)
		}
		for _, row := range rows {
			pk := row["pk"].(string)
			expected := router.HashRoute(pk, shardCount)
			if expected != i {
				t.Errorf("row pk=%s on shard %d, should be on shard %d", pk, i, expected)
			}
		}
	}
}

func TestPlanComputation(t *testing.T) {
	m := setupManager(t, 3)
	createTable(t, m, "users")

	// Insert rows using routing for 3 shards
	ids := []string{"alice", "bob", "charlie", "dave", "eve", "frank", "grace", "henry"}
	for _, id := range ids {
		insertRow(t, m, "users", id, "name_"+id, 3)
	}

	rb := NewRebalancer(m, slog.Default())
	ctx := context.Background()

	// Plan with current shard count should show 0 rows to move
	plan, err := rb.Plan(ctx, Config{PartitionKeyColumn: "id"})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if plan.RowsToMove != 0 {
		t.Errorf("expected 0 rows to move with same shard count, got %d", plan.RowsToMove)
	}
	if plan.TotalRows != int64(len(ids)) {
		t.Errorf("expected %d total rows, got %d", len(ids), plan.TotalRows)
	}

	// Plan with different target shard count should show some rows to move
	plan, err = rb.Plan(ctx, Config{PartitionKeyColumn: "id", TargetShardCount: 4})
	if err != nil {
		t.Fatalf("Plan with target 4: %v", err)
	}
	if plan.RowsToMove == 0 {
		t.Error("expected some rows to move when changing shard count 3->4")
	}
}

func TestRebalanceAfterAddShard(t *testing.T) {
	m := setupManager(t, 2)
	createTable(t, m, "users")

	// Insert rows using routing for 2 shards
	ids := []string{"alice", "bob", "charlie", "dave", "eve", "frank", "grace", "henry", "ivy", "jack"}
	for _, id := range ids {
		insertRow(t, m, "users", id, "name_"+id, 2)
	}

	totalBefore := countAllRows(t, m, "users")
	if totalBefore != int64(len(ids)) {
		t.Fatalf("expected %d rows before, got %d", len(ids), totalBefore)
	}

	// Add a third shard and create the table on it
	newShard, err := m.AddShard()
	if err != nil {
		t.Fatalf("AddShard: %v", err)
	}
	ctx := context.Background()
	if _, err := newShard.Execute(ctx, `CREATE TABLE "users" (id VARCHAR, name VARCHAR)`); err != nil {
		t.Fatalf("create table on new shard: %v", err)
	}

	// Rebalance
	rb := NewRebalancer(m, slog.Default())
	status, err := rb.Execute(ctx, Config{PartitionKeyColumn: "id"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if status.State != "completed" {
		t.Errorf("expected completed, got %s", status.State)
	}
	if status.RowsMoved == 0 {
		t.Error("expected some rows to be moved")
	}

	// Verify total row count unchanged
	totalAfter := countAllRows(t, m, "users")
	if totalAfter != totalBefore {
		t.Errorf("row count changed: %d -> %d", totalBefore, totalAfter)
	}

	// Verify all rows on correct shard
	verifyCorrectPlacement(t, m, "users", "id")
}

func TestRebalanceBeforeRemoveShard(t *testing.T) {
	m := setupManager(t, 3)
	createTable(t, m, "users")

	// Insert rows using routing for 3 shards
	ids := []string{"alice", "bob", "charlie", "dave", "eve", "frank", "grace", "henry"}
	for _, id := range ids {
		insertRow(t, m, "users", id, "name_"+id, 3)
	}

	totalBefore := countAllRows(t, m, "users")

	// Rebalance targeting 2 shards (preparing to remove shard 2)
	rb := NewRebalancer(m, slog.Default())
	ctx := context.Background()
	status, err := rb.Execute(ctx, Config{
		PartitionKeyColumn: "id",
		TargetShardCount:   2,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if status.State != "completed" {
		t.Errorf("expected completed, got %s", status.State)
	}

	// Verify shard 2 is now empty
	s := m.GetShard(2)
	rows, err := s.Query(ctx, `SELECT COUNT(*) AS cnt FROM "users"`)
	if err != nil {
		t.Fatalf("count shard 2: %v", err)
	}
	if cnt, ok := rows[0]["cnt"].(int64); ok && cnt != 0 {
		t.Errorf("shard 2 should be empty, has %d rows", cnt)
	}

	// Verify total row count is preserved across shards 0 and 1
	totalAfter := countAllRows(t, m, "users")
	if totalAfter != totalBefore {
		t.Errorf("row count changed: %d -> %d", totalBefore, totalAfter)
	}

	// Verify rows on shards 0-1 are correctly placed for 2-shard routing
	for i := 0; i < 2; i++ {
		sh := m.GetShard(i)
		rows, err := sh.Query(ctx, `SELECT CAST("id" AS VARCHAR) AS pk FROM "users"`)
		if err != nil {
			t.Fatalf("query shard %d: %v", i, err)
		}
		for _, row := range rows {
			pk := row["pk"].(string)
			expected := router.HashRoute(pk, 2)
			if expected != i {
				t.Errorf("row pk=%s on shard %d, should be on shard %d (for 2-shard routing)", pk, i, expected)
			}
		}
	}
}

func TestRebalanceIdempotent(t *testing.T) {
	m := setupManager(t, 3)
	createTable(t, m, "users")

	ids := []string{"alice", "bob", "charlie", "dave", "eve"}
	for _, id := range ids {
		insertRow(t, m, "users", id, "name_"+id, 3)
	}

	rb := NewRebalancer(m, slog.Default())
	ctx := context.Background()

	// First rebalance (should be a no-op since data is already correct)
	status, err := rb.Execute(ctx, Config{PartitionKeyColumn: "id"})
	if err != nil {
		t.Fatalf("Execute 1: %v", err)
	}
	if status.RowsMoved != 0 {
		t.Errorf("first run should move 0 rows (already correct), moved %d", status.RowsMoved)
	}

	// Second rebalance (also no-op)
	status, err = rb.Execute(ctx, Config{PartitionKeyColumn: "id"})
	if err != nil {
		t.Fatalf("Execute 2: %v", err)
	}
	if status.RowsMoved != 0 {
		t.Errorf("second run should move 0 rows, moved %d", status.RowsMoved)
	}
}

func TestRebalanceEmptyShards(t *testing.T) {
	m := setupManager(t, 3)
	createTable(t, m, "users")

	rb := NewRebalancer(m, slog.Default())
	ctx := context.Background()

	status, err := rb.Execute(ctx, Config{PartitionKeyColumn: "id"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if status.State != "completed" {
		t.Errorf("expected completed, got %s", status.State)
	}
	if status.RowsMoved != 0 {
		t.Errorf("expected 0 rows moved, got %d", status.RowsMoved)
	}
}

func TestPlanInvalidColumn(t *testing.T) {
	m := setupManager(t, 3)
	createTable(t, m, "users")

	// Insert a row so the table is non-empty
	insertRow(t, m, "users", "alice", "Alice", 3)

	rb := NewRebalancer(m, slog.Default())
	ctx := context.Background()

	// Plan with nonexistent column — should skip the table (column doesn't exist)
	plan, err := rb.Plan(ctx, Config{PartitionKeyColumn: "nonexistent"})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	// No rows scanned since column doesn't exist in any table
	if plan.TotalRows != 0 {
		t.Errorf("expected 0 total rows for nonexistent column, got %d", plan.TotalRows)
	}
}

func TestPlanMissingPartitionKeyColumn(t *testing.T) {
	m := setupManager(t, 3)
	rb := NewRebalancer(m, slog.Default())
	ctx := context.Background()

	_, err := rb.Plan(ctx, Config{})
	if err == nil {
		t.Error("expected error for empty partition_key_column")
	}
}

func TestWriteGate(t *testing.T) {
	g := &WriteGate{}

	if g.IsPaused() {
		t.Error("gate should start unpaused")
	}

	g.Pause()
	if !g.IsPaused() {
		t.Error("gate should be paused after Pause()")
	}

	g.Resume()
	if g.IsPaused() {
		t.Error("gate should be unpaused after Resume()")
	}
}

func TestWriteGateDuringExecute(t *testing.T) {
	m := setupManager(t, 2)
	createTable(t, m, "users")
	insertRow(t, m, "users", "alice", "Alice", 2)

	rb := NewRebalancer(m, slog.Default())

	if rb.WriteGateRef().IsPaused() {
		t.Error("gate should not be paused before execute")
	}

	ctx := context.Background()
	rb.Execute(ctx, Config{PartitionKeyColumn: "id"})

	if rb.WriteGateRef().IsPaused() {
		t.Error("gate should be unpaused after execute completes")
	}
}

func TestSubscribeReceivesUpdates(t *testing.T) {
	m := setupManager(t, 2)
	createTable(t, m, "users")

	ids := []string{"alice", "bob", "charlie", "dave"}
	for _, id := range ids {
		insertRow(t, m, "users", id, "name_"+id, 2)
	}

	// Add a third shard to force data movement
	newShard, err := m.AddShard()
	if err != nil {
		t.Fatalf("AddShard: %v", err)
	}
	ctx := context.Background()
	if _, err := newShard.Execute(ctx, `CREATE TABLE "users" (id VARCHAR, name VARCHAR)`); err != nil {
		t.Fatalf("create table on new shard: %v", err)
	}

	rb := NewRebalancer(m, slog.Default())

	ch, unsub := rb.Subscribe()
	defer unsub()

	rb.Execute(ctx, Config{PartitionKeyColumn: "id"})

	// Drain all updates and check we got terminal state
	var states []string
	for {
		select {
		case s, ok := <-ch:
			if !ok {
				t.Fatal("channel closed unexpectedly")
			}
			states = append(states, s.State)
			if s.State == "completed" || s.State == "failed" {
				goto done
			}
		default:
			goto done
		}
	}
done:

	if len(states) == 0 {
		t.Fatal("expected at least one status update")
	}

	lastState := states[len(states)-1]
	if lastState != "completed" {
		t.Errorf("expected last state to be completed, got %s", lastState)
	}

	// Check that we saw progression through states
	seenPlanning := false
	for _, s := range states {
		if s == "planning" {
			seenPlanning = true
		}
	}
	if !seenPlanning {
		t.Error("expected to see 'planning' state in updates")
	}
}
