package reliability

import (
	"context"
	"testing"
)

func TestNewAdmissionController(t *testing.T) {
	config := DefaultAdmissionConfig()
	ac := NewAdmissionController(config)
	
	if ac == nil {
		t.Fatal("NewAdmissionController() returned nil")
	}
	
	ac.Stop()
}

func TestAdmitNormal(t *testing.T) {
	config := DefaultAdmissionConfig()
	ac := NewAdmissionController(config)
	defer ac.Stop()
	
	ctx := context.Background()
	
	// Should admit when not overloaded
	err := ac.Admit(ctx, "read")
	if err != nil {
		t.Errorf("Admit() error = %v, want nil", err)
	}
}

func TestAdmitOverloaded(t *testing.T) {
	config := DefaultAdmissionConfig()
	ac := NewAdmissionController(config)
	defer ac.Stop()
	
	// Set system as overloaded
	ac.SetOverloaded(true)
	
	ctx := context.Background()
	
	// Should reject when overloaded
	err := ac.Admit(ctx, "write")
	if err == nil {
		t.Error("Admit() should fail when overloaded")
	}
}

func TestAdmitDisabled(t *testing.T) {
	config := AdmissionConfig{
		Enabled: false,
	}
	ac := NewAdmissionController(config)
	defer ac.Stop()
	
	// Set as overloaded
	ac.SetOverloaded(true)
	
	ctx := context.Background()
	
	// Should still admit when disabled
	err := ac.Admit(ctx, "write")
	if err != nil {
		t.Errorf("Admit() error = %v when disabled", err)
	}
}

func TestIsOverloaded(t *testing.T) {
	config := DefaultAdmissionConfig()
	ac := NewAdmissionController(config)
	defer ac.Stop()
	
	// Initially not overloaded
	if ac.IsOverloaded() {
		t.Error("IsOverloaded() should be false initially")
	}
	
	// Set as overloaded
	ac.SetOverloaded(true)
	
	if !ac.IsOverloaded() {
		t.Error("IsOverloaded() should be true after SetOverloaded(true)")
	}
	
	// Clear overloaded
	ac.SetOverloaded(false)
	
	if ac.IsOverloaded() {
		t.Error("IsOverloaded() should be false after SetOverloaded(false)")
	}
}
