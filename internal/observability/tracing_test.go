package observability

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestTracerProviderNoOp(t *testing.T) {
	// Test with tracing disabled
	cfg := TracingConfig{
		Enabled: false,
	}

	tp, err := NewTracerProvider(cfg, nil)
	if err != nil {
		t.Fatalf("expected no error with disabled tracing, got: %v", err)
	}

	if tp == nil {
		t.Fatal("expected tracer provider to be non-nil")
	}

	// Test shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := tp.Shutdown(ctx); err != nil {
		t.Errorf("expected no error on shutdown, got: %v", err)
	}
}

func TestStartSpan(t *testing.T) {
	// Create a test exporter
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(
		trace.WithSyncer(exporter),
	)
	otel.SetTracerProvider(tp)

	ctx := context.Background()
	_, span := StartSpan(ctx, "test-service", "test-operation")
	defer span.End()

	if !span.IsRecording() {
		t.Error("expected span to be recording")
	}

	// Add attributes
	span.SetAttributes(
		attribute.String("test.key", "test.value"),
		attribute.Int("test.count", 42),
	)

	// End span and verify it was recorded
	span.End()

	// Get exported spans
	spans := exporter.GetSpans()
	if len(spans) == 0 {
		t.Fatal("expected at least one span to be exported")
	}

	// Verify span name
	if spans[0].Name != "test-operation" {
		t.Errorf("expected span name 'test-operation', got '%s'", spans[0].Name)
	}
}

func TestAddSpanAttributes(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(
		trace.WithSyncer(exporter),
	)
	otel.SetTracerProvider(tp)

	ctx := context.Background()
	ctx, span := StartSpan(ctx, "test-service", "test-operation")

	// Add attributes via helper
	AddSpanAttributes(ctx,
		attribute.String("user.id", "user-123"),
		attribute.Bool("is_replicated", true),
	)

	span.End()

	spans := exporter.GetSpans()
	if len(spans) == 0 {
		t.Fatal("expected span to be exported")
	}

	// Verify attributes
	attrs := spans[0].Attributes
	foundUserID := false
	foundReplicated := false

	for _, attr := range attrs {
		if string(attr.Key) == "user.id" && attr.Value.AsString() == "user-123" {
			foundUserID = true
		}
		if string(attr.Key) == "is_replicated" && attr.Value.AsBool() {
			foundReplicated = true
		}
	}

	if !foundUserID {
		t.Error("expected to find user.id attribute")
	}
	if !foundReplicated {
		t.Error("expected to find is_replicated attribute")
	}
}

func TestAddSpanEvent(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(
		trace.WithSyncer(exporter),
	)
	otel.SetTracerProvider(tp)

	ctx := context.Background()
	ctx, span := StartSpan(ctx, "test-service", "test-operation")

	// Add event
	AddSpanEvent(ctx, "cache.hit", attribute.String("cache.key", "user-data"))
	AddSpanEvent(ctx, "database.query", attribute.Int("rows.count", 10))

	span.End()

	spans := exporter.GetSpans()
	if len(spans) == 0 {
		t.Fatal("expected span to be exported")
	}

	// Verify events
	events := spans[0].Events
	if len(events) != 2 {
		t.Errorf("expected 2 events, got %d", len(events))
	}

	if events[0].Name != "cache.hit" {
		t.Errorf("expected first event name 'cache.hit', got '%s'", events[0].Name)
	}
}

func TestRecordError(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(
		trace.WithSyncer(exporter),
	)
	otel.SetTracerProvider(tp)

	ctx := context.Background()
	ctx, span := StartSpan(ctx, "test-service", "test-operation")

	// Record error
	testErr := context.DeadlineExceeded
	RecordError(ctx, testErr, attribute.String("error.source", "test"))

	span.End()

	spans := exporter.GetSpans()
	if len(spans) == 0 {
		t.Fatal("expected span to be exported")
	}

	// Verify error was recorded
	events := spans[0].Events
	foundError := false
	for _, event := range events {
		if event.Name == "exception" {
			foundError = true
			break
		}
	}

	if !foundError {
		t.Error("expected error event to be recorded")
	}
}

func TestTraceIDExtraction(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(
		trace.WithSyncer(exporter),
	)
	otel.SetTracerProvider(tp)

	ctx := context.Background()
	ctx, span := StartSpan(ctx, "test-service", "test-operation")
	defer span.End()

	// Extract trace ID
	traceID := TraceID(ctx)
	if traceID == "" {
		t.Error("expected non-empty trace ID")
	}

	if len(traceID) != 32 { // Hex-encoded 16-byte trace ID
		t.Errorf("expected trace ID length 32, got %d", len(traceID))
	}

	// Extract span ID
	spanID := SpanID(ctx)
	if spanID == "" {
		t.Error("expected non-empty span ID")
	}

	if len(spanID) != 16 { // Hex-encoded 8-byte span ID
		t.Errorf("expected span ID length 16, got %d", len(spanID))
	}
}

func TestTraceIDNoSpan(t *testing.T) {
	ctx := context.Background()

	// Test with no span in context
	traceID := TraceID(ctx)
	if traceID != "" {
		t.Errorf("expected empty trace ID with no span, got '%s'", traceID)
	}

	spanID := SpanID(ctx)
	if spanID != "" {
		t.Errorf("expected empty span ID with no span, got '%s'", spanID)
	}
}
