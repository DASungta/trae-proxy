package proxy

import (
	"bytes"
	"strings"
	"testing"

	"github.com/zhangyc/trae-proxy/internal/logging"
)

func TestServerErrorLogWriterWrite(t *testing.T) {
	var buf bytes.Buffer
	writer := &serverErrorLogWriter{
		logger: logging.New(logging.LevelDebug, false, &buf),
	}

	line := "http: TLS handshake error from 127.0.0.1:49959: EOF\n"
	n, err := writer.Write([]byte(line))
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if n != len(line) {
		t.Fatalf("Write() n = %d, want %d", n, len(line))
	}

	logged := buf.String()
	if !strings.Contains(logged, "level=WARN") {
		t.Fatalf("expected warn log, got %q", logged)
	}
	if !strings.Contains(logged, "server error") {
		t.Fatalf("expected server error message, got %q", logged)
	}
	if !strings.Contains(logged, "TLS handshake error") {
		t.Fatalf("expected handshake detail, got %q", logged)
	}
	if strings.Contains(logged, "EOF\n") {
		t.Fatalf("expected trailing newline to be trimmed, got %q", logged)
	}
}

func TestServerErrorLogWriterWriteIgnoresEmptyInput(t *testing.T) {
	var buf bytes.Buffer
	writer := &serverErrorLogWriter{
		logger: logging.New(logging.LevelDebug, false, &buf),
	}

	line := " \n\t "
	n, err := writer.Write([]byte(line))
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if n != len(line) {
		t.Fatalf("Write() n = %d, want %d", n, len(line))
	}
	if buf.Len() != 0 {
		t.Fatalf("expected no log output, got %q", buf.String())
	}
}
