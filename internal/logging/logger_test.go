package logging

import (
	"bytes"
	"net/http"
	"strings"
	"testing"
)

func TestParseLevel(t *testing.T) {
	cases := []struct {
		in      string
		want    Level
		wantErr bool
	}{
		{"trace", LevelTrace, false},
		{"TRACE", LevelTrace, false},
		{"debug", LevelDebug, false},
		{"info", LevelInfo, false},
		{"INFO", LevelInfo, false},
		{"", LevelInfo, false},
		{"warn", LevelWarn, false},
		{"warning", LevelWarn, false},
		{"error", LevelError, false},
		{"verbose", 0, true},
		{"fatal", 0, true},
	}
	for _, c := range cases {
		got, err := ParseLevel(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("ParseLevel(%q): expected error, got nil", c.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseLevel(%q): unexpected error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseLevel(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestRedactHeaders(t *testing.T) {
	orig := http.Header{
		"Authorization": {"Bearer secret"},
		"X-Api-Key":     {"sk-abc"},
		"Content-Type":  {"application/json"},
	}
	out := RedactHeaders(orig)

	// Sensitive values replaced.
	if got := out.Get("Authorization"); got != "[REDACTED]" {
		t.Errorf("Authorization = %q, want [REDACTED]", got)
	}
	if got := out.Get("X-Api-Key"); got != "[REDACTED]" {
		t.Errorf("X-Api-Key = %q, want [REDACTED]", got)
	}
	// Non-sensitive values preserved.
	if got := out.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	// Original not mutated.
	if got := orig.Get("Authorization"); got != "Bearer secret" {
		t.Errorf("original Authorization mutated: %q", got)
	}
}

func TestSnippet(t *testing.T) {
	// Short body returned as-is.
	b := []byte(`{"model":"test"}`)
	if got := Snippet(b, 512); got != string(b) {
		t.Errorf("Snippet short: %q", got)
	}

	// Long body truncated.
	long := bytes.Repeat([]byte("x"), 600)
	got := Snippet(long, 512)
	if !strings.HasPrefix(got, strings.Repeat("x", 512)) {
		t.Errorf("Snippet long prefix wrong")
	}
	if !strings.Contains(got, "88 more bytes") {
		t.Errorf("Snippet long suffix missing: %q", got)
	}

	// Empty.
	if got := Snippet(nil, 512); got != "" {
		t.Errorf("Snippet nil = %q, want empty", got)
	}
}

func TestLoggerLevelGating(t *testing.T) {
	var buf bytes.Buffer
	l := New(LevelInfo, false, &buf)

	l.Trace("should not appear")
	l.Debug("should not appear")
	l.Info("info message")
	l.Warn("warn message")
	l.Error("error message")

	out := buf.String()
	if strings.Contains(out, "should not appear") {
		t.Errorf("trace/debug leaked through info logger: %s", out)
	}
	if !strings.Contains(out, "info message") {
		t.Errorf("info message missing: %s", out)
	}
	if !strings.Contains(out, "warn message") {
		t.Errorf("warn message missing: %s", out)
	}
	if !strings.Contains(out, "error message") {
		t.Errorf("error message missing: %s", out)
	}
}

func TestLoggerTraceLevelOutput(t *testing.T) {
	var buf bytes.Buffer
	l := New(LevelTrace, false, &buf)
	l.Trace("trace message")
	out := buf.String()
	if !strings.Contains(out, "TRACE") {
		t.Errorf("TRACE level label missing: %s", out)
	}
	if !strings.Contains(out, "trace message") {
		t.Errorf("trace message missing: %s", out)
	}
}

func TestAppendCapped(t *testing.T) {
	var buf bytes.Buffer
	// Write data that fits within cap.
	AppendCapped(&buf, []byte("hello"), 8)
	if buf.String() != "hello" {
		t.Errorf("got %q", buf.String())
	}

	// Write more than remaining — triggers partial write + truncation marker.
	AppendCapped(&buf, []byte("overflow"), 8)
	out := buf.String()
	if !strings.HasPrefix(out, "hello") {
		t.Errorf("prefix missing: %q", out)
	}
	if !strings.Contains(out, "[truncated at 1MiB]") {
		t.Errorf("truncation marker missing: %q", out)
	}

	// Subsequent calls after cap do nothing.
	before := buf.String()
	AppendCapped(&buf, []byte("more"), 8)
	if buf.String() != before {
		t.Errorf("after cap: buf changed: %q", buf.String())
	}
}
