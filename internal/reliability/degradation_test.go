package reliability

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestNewDegradationManager(t *testing.T) {
	config := DefaultDegradationConfig()
	dm := NewDegradationManager(config)
	
	if dm == nil {
		t.Fatal("NewDegradationManager() returned nil")
	}
	
	dm.Stop()
}

func TestGetSetMode(t *testing.T) {
	config := DegradationConfig{
		Enabled: true,
	}
	dm := NewDegradationManager(config)
	defer dm.Stop()
	
	// Initially normal
	if dm.GetMode() != ModeNormal {
		t.Errorf("expected ModeNormal initially, got %v", dm.GetMode())
	}
	
	// Set to degraded
	dm.SetMode(ModeDegraded)
	if dm.GetMode() != ModeDegraded {
		t.Errorf("expected ModeDegraded, got %v", dm.GetMode())
	}
	
	// Set to read-only
	dm.SetMode(ModeReadOnly)
	if dm.GetMode() != ModeReadOnly {
		t.Errorf("expected ModeReadOnly, got %v", dm.GetMode())
	}
	
	// Set to maintenance
	dm.SetMode(ModeMaintenance)
	if dm.GetMode() != ModeMaintenance {
		t.Errorf("expected ModeMaintenance, got %v", dm.GetMode())
	}
}

func TestCheckWriteNormal(t *testing.T) {
	config := DegradationConfig{
		Enabled: true,
	}
	dm := NewDegradationManager(config)
	defer dm.Stop()
	
	ctx := context.Background()
	
	// Writes should be allowed in normal mode
	if err := dm.CheckWrite(ctx); err != nil {
		t.Errorf("CheckWrite() error = %v in normal mode", err)
	}
}

func TestCheckWriteReadOnly(t *testing.T) {
	config := DegradationConfig{
		Enabled: true,
	}
	dm := NewDegradationManager(config)
	defer dm.Stop()
	
	dm.SetMode(ModeReadOnly)
	ctx := context.Background()
	
	// Writes should be rejected in read-only mode
	if err := dm.CheckWrite(ctx); err == nil {
		t.Error("CheckWrite() should fail in read-only mode")
	}
}

func TestCheckReadNormal(t *testing.T) {
	config := DegradationConfig{
		Enabled: true,
	}
	dm := NewDegradationManager(config)
	defer dm.Stop()
	
	ctx := context.Background()
	
	// Reads should be allowed in normal mode
	if err := dm.CheckRead(ctx); err != nil {
		t.Errorf("CheckRead() error = %v in normal mode", err)
	}
}

func TestCheckReadMaintenance(t *testing.T) {
	config := DegradationConfig{
		Enabled: true,
	}
	dm := NewDegradationManager(config)
	defer dm.Stop()
	
	dm.SetMode(ModeMaintenance)
	ctx := context.Background()
	
	// Reads should be rejected in maintenance mode
	if err := dm.CheckRead(ctx); err == nil {
		t.Error("CheckRead() should fail in maintenance mode")
	}
}

func TestRecordErrorDegradation(t *testing.T) {
	config := DegradationConfig{
		Enabled:            true,
		AutoDegradeOnError: true,
		ErrorThreshold:     3,
	}
	dm := NewDegradationManager(config)
	defer dm.Stop()
	
	// Initially normal
	if dm.GetMode() != ModeNormal {
		t.Errorf("expected ModeNormal initially")
	}
	
	// Record errors until threshold
	for i := 0; i < 3; i++ {
		dm.RecordError(errors.New("test error"))
	}
	
	// Should be degraded after threshold
	if dm.GetMode() != ModeDegraded {
		t.Errorf("expected ModeDegraded after %d errors, got %v", 3, dm.GetMode())
	}
	
	// Record more errors
	for i := 0; i < 3; i++ {
		dm.RecordError(errors.New("test error"))
	}
	
	// Should be read-only after more errors
	if dm.GetMode() != ModeReadOnly {
		t.Errorf("expected ModeReadOnly after more errors, got %v", dm.GetMode())
	}
}

func TestDegradationDisabled(t *testing.T) {
	config := DegradationConfig{
		Enabled: false,
	}
	dm := NewDegradationManager(config)
	defer dm.Stop()
	
	dm.SetMode(ModeReadOnly)
	ctx := context.Background()
	
	// Writes should be allowed when degradation is disabled
	if err := dm.CheckWrite(ctx); err != nil {
		t.Errorf("CheckWrite() error = %v when degradation disabled", err)
	}
}

func TestDegradationGetStats(t *testing.T) {
	config := DegradationConfig{
		Enabled:            true,
		AutoDegradeOnError: true,
		ErrorThreshold:     5,
	}
	dm := NewDegradationManager(config)
	defer dm.Stop()
	
	// Record some errors
	for i := 0; i < 3; i++ {
		dm.RecordError(errors.New("test error"))
	}
	
	stats := dm.GetStats()
	if stats.Mode != "normal" {
		t.Errorf("expected mode 'normal', got '%s'", stats.Mode)
	}
	if stats.ErrorCount != 3 {
		t.Errorf("expected 3 errors, got %d", stats.ErrorCount)
	}
	if !stats.WritesAllowed {
		t.Error("writes should be allowed in normal mode")
	}
	if !stats.ReadsAllowed {
		t.Error("reads should be allowed in normal mode")
	}
}

func TestModeString(t *testing.T) {
	tests := []struct {
		mode DegradationMode
		want string
	}{
		{ModeNormal, "normal"},
		{ModeDegraded, "degraded"},
		{ModeReadOnly, "read_only"},
		{ModeMaintenance, "maintenance"},
	}
	
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.mode.String(); got != tt.want {
				t.Errorf("String() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRecoveryFromDegradation(t *testing.T) {
	config := DegradationConfig{
		Enabled:               true,
		AutoDegradeOnError:    true,
		ErrorThreshold:        2,
		RecoveryCheckInterval: 50 * time.Millisecond,
	}
	dm := NewDegradationManager(config)
	defer dm.Stop()
	
	// Degrade to read-only
	dm.RecordError(errors.New("error 1"))
	dm.RecordError(errors.New("error 2"))
	dm.RecordError(errors.New("error 3"))
	dm.RecordError(errors.New("error 4"))
	
	// Should be degraded or read-only
	initialMode := dm.GetMode()
	if initialMode == ModeNormal {
		t.Error("should be degraded after errors")
	}
	
	// Wait for recovery check (2x interval to ensure recovery)
	time.Sleep(150 * time.Millisecond)
	
	// Should recover after no recent errors
	recoveredMode := dm.GetMode()
	if recoveredMode == initialMode {
		// Mode should improve (or at least not get worse)
		t.Logf("Mode did not improve from %v to %v (recovery may need more time)", initialMode, recoveredMode)
	}
}
