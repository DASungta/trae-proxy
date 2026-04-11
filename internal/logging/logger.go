package logging

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"unicode/utf8"
)

// Level wraps slog.Level. Custom trace level sits below debug.
type Level = slog.Level

const (
	LevelTrace Level = -8
	LevelDebug Level = slog.LevelDebug // -4
	LevelInfo  Level = slog.LevelInfo  // 0
	LevelWarn  Level = slog.LevelWarn  // 4
	LevelError Level = slog.LevelError // 8
)

// ParseLevel converts a string to a Level. Case-insensitive.
func ParseLevel(s string) (Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "trace":
		return LevelTrace, nil
	case "debug":
		return LevelDebug, nil
	case "info", "":
		return LevelInfo, nil
	case "warn", "warning":
		return LevelWarn, nil
	case "error":
		return LevelError, nil
	default:
		return LevelInfo, fmt.Errorf("invalid log level %q (must be trace/debug/info/warn/error)", s)
	}
}

// Logger wraps slog.Logger and tracks the configured level and logBody setting.
type Logger struct {
	handler slog.Handler
	inner   *slog.Logger
	level   Level
	logBody bool
}

// New creates a Logger that writes text to out at the specified level.
func New(level Level, logBody bool, out io.Writer) *Logger {
	opts := &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			// Rename the slog "DEBUG-4" representation to "TRACE".
			if a.Key == slog.LevelKey {
				if lv, ok := a.Value.Any().(slog.Level); ok && lv == LevelTrace {
					a.Value = slog.StringValue("TRACE")
				}
			}
			return a
		},
	}
	h := slog.NewTextHandler(out, opts)
	return &Logger{
		handler: h,
		inner:   slog.New(h),
		level:   level,
		logBody: logBody,
	}
}

// Enabled reports whether the given level will be logged.
func (l *Logger) Enabled(lvl Level) bool {
	return l.handler.Enabled(context.Background(), lvl)
}

// LogBody reports whether full request/response bodies should be logged.
func (l *Logger) LogBody() bool {
	return l.logBody
}

// Trace logs at TRACE level.
func (l *Logger) Trace(msg string, args ...any) {
	if !l.Enabled(LevelTrace) {
		return
	}
	l.inner.Log(context.Background(), LevelTrace, msg, args...)
}

// Debug logs at DEBUG level.
func (l *Logger) Debug(msg string, args ...any) {
	l.inner.Debug(msg, args...)
}

// Info logs at INFO level.
func (l *Logger) Info(msg string, args ...any) {
	l.inner.Info(msg, args...)
}

// Warn logs at WARN level.
func (l *Logger) Warn(msg string, args ...any) {
	l.inner.Warn(msg, args...)
}

// Error logs at ERROR level.
func (l *Logger) Error(msg string, args ...any) {
	l.inner.Error(msg, args...)
}

// With returns a new Logger with additional fixed attributes.
func (l *Logger) With(args ...any) *Logger {
	return &Logger{
		handler: l.handler,
		inner:   l.inner.With(args...),
		level:   l.level,
		logBody: l.logBody,
	}
}

// sensitiveHeaders lists header names whose values are redacted in logs.
var sensitiveHeaders = []string{
	"Authorization",
	"X-Api-Key",
	"Cookie",
	"Set-Cookie",
	"Proxy-Authorization",
}

// RedactHeaders returns a copy of h with sensitive header values replaced by
// [REDACTED]. The original header map is not modified.
func RedactHeaders(h http.Header) http.Header {
	out := make(http.Header, len(h))
	for k, vv := range h {
		copied := make([]string, len(vv))
		copy(copied, vv)
		out[k] = copied
	}
	for _, name := range sensitiveHeaders {
		if _, ok := out[http.CanonicalHeaderKey(name)]; ok {
			out[http.CanonicalHeaderKey(name)] = []string{"[REDACTED]"}
		}
	}
	return out
}

// Snippet returns a concise preview of b for logging.
// If len(b) <= max, the full string is returned (non-UTF-8 bytes are escaped).
// Otherwise, the first max bytes are shown followed by "... (N more bytes)".
func Snippet(b []byte, max int) string {
	if len(b) == 0 {
		return ""
	}
	if len(b) <= max {
		return safeString(b)
	}
	return safeString(b[:max]) + fmt.Sprintf("... (%d more bytes)", len(b)-max)
}

// safeString converts bytes to a string, escaping non-printable / non-UTF-8 bytes.
func safeString(b []byte) string {
	if utf8.Valid(b) {
		return string(b)
	}
	var buf bytes.Buffer
	for len(b) > 0 {
		r, size := utf8.DecodeRune(b)
		if r == utf8.RuneError && size == 1 {
			fmt.Fprintf(&buf, "\\x%02x", b[0])
			b = b[1:]
		} else {
			buf.WriteRune(r)
			b = b[size:]
		}
	}
	return buf.String()
}

// AppendCapped writes p into buf unless buf has already reached cap bytes.
// The first time the cap is exceeded a truncation marker is appended instead.
func AppendCapped(buf *bytes.Buffer, p []byte, capBytes int) {
	if buf.Len() >= capBytes {
		return
	}
	remaining := capBytes - buf.Len()
	if len(p) <= remaining {
		buf.Write(p)
		return
	}
	buf.Write(p[:remaining])
	buf.WriteString("[truncated at 1MiB]")
}
