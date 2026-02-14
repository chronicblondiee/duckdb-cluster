package index

import (
	"testing"
)

func TestMatchGlob(t *testing.T) {
	tests := []struct {
		pattern string
		input   string
		want    bool
	}{
		{"logs-*", "logs-2024", true},
		{"logs-*", "logs-", true},
		{"logs-*", "events-2024", false},
		{"*-events", "app-events", true},
		{"*-events", "events", false},
		{"*search*", "my-search-idx", true},
		{"*search*", "searchable", true},
		{"*search*", "nope", false},
		{"*", "anything", true},
		{"exact", "exact", true},
		{"exact", "other", false},
	}

	for _, tt := range tests {
		got := MatchGlob(tt.pattern, tt.input)
		if got != tt.want {
			t.Errorf("MatchGlob(%q, %q) = %v, want %v", tt.pattern, tt.input, got, tt.want)
		}
	}
}

func TestTemplateManagerCRUD(t *testing.T) {
	dir := t.TempDir()
	tm := NewTemplateManager(dir)

	tmpl := &Template{
		Name:     "log-template",
		Pattern:  "logs-*",
		Priority: 10,
		Settings: Settings{ShardCount: 5},
	}

	if err := tm.Put(tmpl); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := tm.Get("log-template")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Pattern != "logs-*" {
		t.Errorf("expected pattern 'logs-*', got %q", got.Pattern)
	}
	if got.Settings.ShardCount != 5 {
		t.Errorf("expected 5 shards, got %d", got.Settings.ShardCount)
	}

	// List
	all := tm.List()
	if len(all) != 1 {
		t.Errorf("expected 1 template, got %d", len(all))
	}

	// Delete
	if err := tm.Delete("log-template"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := tm.Get("log-template"); err == nil {
		t.Error("expected error after delete")
	}
}

func TestTemplateManagerMatch(t *testing.T) {
	dir := t.TempDir()
	tm := NewTemplateManager(dir)

	tm.Put(&Template{Name: "low", Pattern: "logs-*", Priority: 1, Settings: Settings{ShardCount: 2}})
	tm.Put(&Template{Name: "high", Pattern: "logs-*", Priority: 10, Settings: Settings{ShardCount: 8}})
	tm.Put(&Template{Name: "events", Pattern: "*-events", Priority: 5, Settings: Settings{ShardCount: 3}})

	// Should match both log templates, high priority first
	matched := tm.Match("logs-2024")
	if len(matched) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(matched))
	}
	if matched[0].Name != "high" {
		t.Errorf("expected highest priority first, got %q", matched[0].Name)
	}

	// Should match events template
	matched = tm.Match("app-events")
	if len(matched) != 1 || matched[0].Name != "events" {
		t.Errorf("expected events template match")
	}

	// Should match nothing
	matched = tm.Match("users")
	if len(matched) != 0 {
		t.Errorf("expected 0 matches, got %d", len(matched))
	}
}

func TestTemplateManagerPersistence(t *testing.T) {
	dir := t.TempDir()

	tm1 := NewTemplateManager(dir)
	tm1.Put(&Template{Name: "t1", Pattern: "test-*", Priority: 1, Settings: Settings{ShardCount: 2}})

	tm2 := NewTemplateManager(dir)
	if err := tm2.LoadAll(); err != nil {
		t.Fatalf("LoadAll: %v", err)
	}

	got, err := tm2.Get("t1")
	if err != nil {
		t.Fatalf("Get after reload: %v", err)
	}
	if got.Settings.ShardCount != 2 {
		t.Errorf("expected 2 shards, got %d", got.Settings.ShardCount)
	}
}

func TestTemplateManagerValidation(t *testing.T) {
	dir := t.TempDir()
	tm := NewTemplateManager(dir)

	if err := tm.Put(&Template{Name: "", Pattern: "test-*"}); err == nil {
		t.Error("expected error for empty name")
	}
	if err := tm.Put(&Template{Name: "t1", Pattern: ""}); err == nil {
		t.Error("expected error for empty pattern")
	}
}
