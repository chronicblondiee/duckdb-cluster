package reliability

import (
	"context"
	"testing"
	"time"
)

func TestNewBackpressureManager(t *testing.T) {
	config := BackpressureConfig{
		Enabled:             true,
		MaxConcurrentWrites: 10,
		MaxConcurrentReads:  100,
		MaxQueueSize:        50,
		QueueTimeout:        5 * time.Second,
	}
	
	bpm := NewBackpressureManager(config)
	if bpm == nil {
		t.Fatal("NewBackpressureManager() returned nil")
	}
}

func TestAcquireWriteSuccess(t *testing.T) {
	config := BackpressureConfig{
		Enabled:             true,
		MaxConcurrentWrites: 5,
		MaxQueueSize:        10,
		QueueTimeout:        1 * time.Second,
	}
	
	bpm := NewBackpressureManager(config)
	ctx := context.Background()
	
	// Acquire write permit
	release, err := bpm.AcquireWrite(ctx)
	if err != nil {
		t.Fatalf("AcquireWrite() error = %v", err)
	}
	
	if release == nil {
		t.Fatal("AcquireWrite() returned nil release function")
	}
	
	// Release permit
	release()
	
	// Stats should show no active writes
	stats := bpm.GetStats()
	if stats.ActiveWrites != 0 {
		t.Errorf("expected 0 active writes, got %d", stats.ActiveWrites)
	}
}

func TestAcquireWriteConcurrency(t *testing.T) {
	config := BackpressureConfig{
		Enabled:             true,
		MaxConcurrentWrites: 3,
		MaxQueueSize:        10,
		QueueTimeout:        1 * time.Second,
	}
	
	bpm := NewBackpressureManager(config)
	ctx := context.Background()
	
	// Acquire all available permits
	releases := make([]func(), 0, 3)
	for i := 0; i < 3; i++ {
		release, err := bpm.AcquireWrite(ctx)
		if err != nil {
			t.Fatalf("AcquireWrite(%d) error = %v", i, err)
		}
		releases = append(releases, release)
	}
	
	// Stats should show 3 active writes
	stats := bpm.GetStats()
	if stats.ActiveWrites != 3 {
		t.Errorf("expected 3 active writes, got %d", stats.ActiveWrites)
	}
	
	// Try to acquire one more (should timeout quickly)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	
	_, err := bpm.AcquireWrite(ctx)
	if err == nil {
		t.Error("expected error when exceeding max concurrent writes")
	}
	
	// Release all permits
	for _, release := range releases {
		release()
	}
	
	// Now should be able to acquire again
	ctx2 := context.Background()
	release, err := bpm.AcquireWrite(ctx2)
	if err != nil {
		t.Errorf("AcquireWrite() after release error = %v", err)
	}
	release()
}

func TestAcquireReadSuccess(t *testing.T) {
	config := BackpressureConfig{
		Enabled:            true,
		MaxConcurrentReads: 10,
		MaxQueueSize:       20,
		QueueTimeout:       1 * time.Second,
	}
	
	bpm := NewBackpressureManager(config)
	ctx := context.Background()
	
	// Acquire read permit
	release, err := bpm.AcquireRead(ctx)
	if err != nil {
		t.Fatalf("AcquireRead() error = %v", err)
	}
	
	if release == nil {
		t.Fatal("AcquireRead() returned nil release function")
	}
	
	// Release permit
	release()
}

func TestBackpressureDisabled(t *testing.T) {
	config := BackpressureConfig{
		Enabled: false,
	}
	
	bpm := NewBackpressureManager(config)
	ctx := context.Background()
	
	// Should be able to acquire unlimited permits when disabled
	for i := 0; i < 1000; i++ {
		release, err := bpm.AcquireWrite(ctx)
		if err != nil {
			t.Fatalf("AcquireWrite(%d) error = %v when disabled", i, err)
		}
		release()
	}
}

func TestGetStats(t *testing.T) {
	config := BackpressureConfig{
		Enabled:             true,
		MaxConcurrentWrites: 5,
		MaxConcurrentReads:  10,
		MaxQueueSize:        20,
		QueueTimeout:        1 * time.Second,
	}
	
	bpm := NewBackpressureManager(config)
	ctx := context.Background()
	
	// Initially no activity
	stats := bpm.GetStats()
	if stats.ActiveWrites != 0 || stats.ActiveReads != 0 {
		t.Error("expected no activity initially")
	}
	
	// Acquire some permits
	writeRelease, _ := bpm.AcquireWrite(ctx)
	readRelease, _ := bpm.AcquireRead(ctx)
	
	stats = bpm.GetStats()
	if stats.ActiveWrites != 1 {
		t.Errorf("expected 1 active write, got %d", stats.ActiveWrites)
	}
	if stats.ActiveReads != 1 {
		t.Errorf("expected 1 active read, got %d", stats.ActiveReads)
	}
	
	// Release
	writeRelease()
	readRelease()
	
	stats = bpm.GetStats()
	if stats.ActiveWrites != 0 {
		t.Errorf("expected 0 active writes after release, got %d", stats.ActiveWrites)
	}
	if stats.ActiveReads != 0 {
		t.Errorf("expected 0 active reads after release, got %d", stats.ActiveReads)
	}
}
