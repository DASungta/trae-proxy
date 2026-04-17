package tlsutil

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNeedsRegeneration(t *testing.T) {
	dir := t.TempDir()

	t.Run("missing cert", func(t *testing.T) {
		if !NeedsRegeneration(dir, "example.com") {
			t.Fatal("expected true for missing cert")
		}
	})

	if err := GenerateCA(dir); err != nil {
		t.Fatalf("GenerateCA: %v", err)
	}
	caCert, caKey, err := LoadCA(dir)
	if err != nil {
		t.Fatalf("LoadCA: %v", err)
	}

	t.Run("valid cert with matching domain", func(t *testing.T) {
		if err := GenerateServerCert(dir, caCert, caKey, "example.com"); err != nil {
			t.Fatalf("GenerateServerCert: %v", err)
		}
		if NeedsRegeneration(dir, "example.com") {
			t.Fatal("expected false for valid cert with matching domain")
		}
	})

	t.Run("domain mismatch", func(t *testing.T) {
		if !NeedsRegeneration(dir, "other.com") {
			t.Fatal("expected true for domain mismatch")
		}
	})

	t.Run("expiry within 30 days", func(t *testing.T) {
		mustWriteCustomCert(t, dir, caCert, caKey, "example.com", 10*24*time.Hour)
		if !NeedsRegeneration(dir, "example.com") {
			t.Fatal("expected true for cert expiring in 10 days")
		}
	})

	t.Run("validity over 398 days", func(t *testing.T) {
		mustWriteCustomCert(t, dir, caCert, caKey, "example.com", 400*24*time.Hour)
		if !NeedsRegeneration(dir, "example.com") {
			t.Fatal("expected true for cert with validity > 398 days")
		}
	})
}

func TestGenerateServerCert_BasicConstraints(t *testing.T) {
	dir := t.TempDir()
	if err := GenerateCA(dir); err != nil {
		t.Fatalf("GenerateCA: %v", err)
	}
	caCert, caKey, err := LoadCA(dir)
	if err != nil {
		t.Fatalf("LoadCA: %v", err)
	}
	if err := GenerateServerCert(dir, caCert, caKey, "example.com"); err != nil {
		t.Fatalf("GenerateServerCert: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "server.pem"))
	if err != nil {
		t.Fatalf("read server.pem: %v", err)
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		t.Fatal("failed to decode server.pem")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}

	if !cert.BasicConstraintsValid {
		t.Error("expected BasicConstraintsValid = true")
	}
	if cert.IsCA {
		t.Error("expected IsCA = false for server cert")
	}
	found := false
	for _, name := range cert.DNSNames {
		if name == "example.com" {
			found = true
		}
	}
	if !found {
		t.Error("expected example.com in DNSNames")
	}
}

// mustWriteCustomCert writes a server cert with arbitrary validity to dir/server.pem.
func mustWriteCustomCert(t *testing.T, dir string, caCert *x509.Certificate, caKey *ecdsa.PrivateKey, domain string, validity time.Duration) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: domain},
		DNSNames:              []string{domain},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(validity),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, caCert, &key.PublicKey, caKey)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	if err := writePEM(filepath.Join(dir, "server.pem"), "CERTIFICATE", certDER); err != nil {
		t.Fatalf("write cert: %v", err)
	}
}
