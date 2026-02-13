package reliability

import (
	"context"
	"testing"
	"time"
)

func TestWithQueryTimeout(t *testing.T) {
	config := TimeoutConfig{
		QueryTimeout: 100 * time.Millisecond,
	}
	
	ctx, cancel := WithQueryTimeout(context.Background(), config)
	defer cancel()
	
	// Check that deadline is set
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Error("context should have deadline")
	}
	
	// Deadline should be approximately now + timeout
	expected := time.Now().Add(config.QueryTimeout)
	if deadline.Before(expected.Add(-10*time.Millisecond)) || deadline.After(expected.Add(10*time.Millisecond)) {
		t.Errorf("deadline = %v, want ~%v", deadline, expected)
	}
}

func TestTimeoutExpiration(t *testing.T) {
	config := TimeoutConfig{
		QueryTimeout: 50 * time.Millisecond,
	}
	
	ctx, cancel := WithQueryTimeout(context.Background(), config)
	defer cancel()
	
	// Wait for timeout to expire
	<-ctx.Done()
	
	if ctx.Err() != context.DeadlineExceeded {
		t.Errorf("expected DeadlineExceeded, got %v", ctx.Err())
	}
}

func TestIsTimeoutError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "deadline exceeded",
			err:  context.DeadlineExceeded,
			want: true,
		},
		{
			name: "canceled",
			err:  context.Canceled,
			want: true,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsTimeoutError(tt.err); got != tt.want {
				t.Errorf("IsTimeoutError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWrapTimeoutError(t *testing.T) {
	err := context.DeadlineExceeded
	wrapped := WrapTimeoutError(err, "query execution", 5*time.Second)
	
	if wrapped == nil {
		t.Fatal("WrapTimeoutError() returned nil")
	}
	
	expectedMsg := "query execution timed out after 5s"
	if !contains(wrapped.Error(), expectedMsg) {
		t.Errorf("expected error message to contain '%s', got '%s'", expectedMsg, wrapped.Error())
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
