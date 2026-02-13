package observability

import (
	"context"
	"log/slog"
	"os"
)

// Logger wraps slog.Logger with trace context integration
type Logger struct {
	*slog.Logger
}

// NewLogger creates a new logger with the specified level and format
func NewLogger(level slog.Level, format string) *Logger {
	var handler slog.Handler

	opts := &slog.HandlerOptions{
		Level: level,
	}

	switch format {
	case "json":
		handler = slog.NewJSONHandler(os.Stdout, opts)
	default:
		handler = slog.NewTextHandler(os.Stdout, opts)
	}

	return &Logger{
		Logger: slog.New(handler),
	}
}

// WithContext returns a logger that includes trace/span IDs from the context
func (l *Logger) WithContext(ctx context.Context) *slog.Logger {
	attrs := []any{}

	if traceID := TraceID(ctx); traceID != "" {
		attrs = append(attrs, "trace_id", traceID)
	}

	if spanID := SpanID(ctx); spanID != "" {
		attrs = append(attrs, "span_id", spanID)
	}

	if len(attrs) > 0 {
		return l.Logger.With(attrs...)
	}

	return l.Logger
}

// InfoContext logs at Info level with trace context
func (l *Logger) InfoContext(ctx context.Context, msg string, args ...any) {
	l.WithContext(ctx).Info(msg, args...)
}

// ErrorContext logs at Error level with trace context
func (l *Logger) ErrorContext(ctx context.Context, msg string, args ...any) {
	l.WithContext(ctx).Error(msg, args...)
}

// WarnContext logs at Warn level with trace context
func (l *Logger) WarnContext(ctx context.Context, msg string, args ...any) {
	l.WithContext(ctx).Warn(msg, args...)
}

// DebugContext logs at Debug level with trace context
func (l *Logger) DebugContext(ctx context.Context, msg string, args ...any) {
	l.WithContext(ctx).Debug(msg, args...)
}

// ParseLevel parses a string log level
func ParseLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
