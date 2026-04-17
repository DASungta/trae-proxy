package tlsutil

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGenerateCA_Profile(t *testing.T) {
	dir := t.TempDir()
	if err := GenerateCA(dir); err != nil {
		t.Fatalf("GenerateCA: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "root-ca.pem"))
	if err != nil {
		t.Fatalf("read root-ca.pem: %v", err)
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		t.Fatal("failed to decode root-ca.pem")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse CA cert: %v", err)
	}

	if !cert.IsCA {
		t.Error("expected IsCA = true")
	}
	if !cert.BasicConstraintsValid {
		t.Error("expected BasicConstraintsValid = true")
	}
	if cert.KeyUsage&x509.KeyUsageCRLSign != 0 {
		t.Error("CA must not have CRLSign: triggers Schannel CRYPT_E_NO_REVOCATION_CHECK on Win10")
	}
	if cert.KeyUsage&x509.KeyUsageCertSign == 0 {
		t.Error("expected KeyUsageCertSign on CA")
	}
	if !cert.MaxPathLenZero || cert.MaxPathLen != 0 {
		t.Errorf("expected MaxPathLen=0/MaxPathLenZero=true, got MaxPathLen=%d MaxPathLenZero=%v",
			cert.MaxPathLen, cert.MaxPathLenZero)
	}
	if !containsOU(cert.Subject.OrganizationalUnit, certProfileVersion) {
		t.Errorf("expected OU to contain %q, got %v", certProfileVersion, cert.Subject.OrganizationalUnit)
	}
}

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
		mustWriteCustomCert(t, dir, caCert, caKey, "example.com", 10*24*time.Hour, true)
		if !NeedsRegeneration(dir, "example.com") {
			t.Fatal("expected true for cert expiring in 10 days")
		}
	})

	t.Run("validity over 398 days", func(t *testing.T) {
		mustWriteCustomCert(t, dir, caCert, caKey, "example.com", 400*24*time.Hour, true)
		if !NeedsRegeneration(dir, "example.com") {
			t.Fatal("expected true for cert with validity > 398 days")
		}
	})

	t.Run("legacy v1 profile missing KeyEncipherment", func(t *testing.T) {
		mustWriteV1Cert(t, dir, caCert, caKey, "example.com")
		if !NeedsRegeneration(dir, "example.com") {
			t.Fatal("expected true for legacy v1 cert missing KeyEncipherment")
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
	if cert.KeyUsage&x509.KeyUsageKeyEncipherment == 0 {
		t.Error("expected KeyUsageKeyEncipherment on server cert for Win10 Schannel compat")
	}
	if cert.KeyUsage&x509.KeyUsageDigitalSignature == 0 {
		t.Error("expected KeyUsageDigitalSignature on server cert")
	}
	hasServerAuth, hasClientAuth := false, false
	for _, eku := range cert.ExtKeyUsage {
		if eku == x509.ExtKeyUsageServerAuth {
			hasServerAuth = true
		}
		if eku == x509.ExtKeyUsageClientAuth {
			hasClientAuth = true
		}
	}
	if !hasServerAuth {
		t.Error("expected ExtKeyUsageServerAuth on server cert")
	}
	if !hasClientAuth {
		t.Error("expected ExtKeyUsageClientAuth on server cert for max compat")
	}
	hasLoopback := false
	for _, ip := range cert.IPAddresses {
		if ip.Equal(net.ParseIP("127.0.0.1")) {
			hasLoopback = true
		}
	}
	if !hasLoopback {
		t.Error("expected 127.0.0.1 in IPAddresses SAN")
	}
	if !containsOU(cert.Subject.OrganizationalUnit, certProfileVersion) {
		t.Errorf("expected OU to contain %q, got %v", certProfileVersion, cert.Subject.OrganizationalUnit)
	}
}

func TestCANeedsRegeneration(t *testing.T) {
	t.Run("missing ca file returns true", func(t *testing.T) {
		if !CANeedsRegeneration(t.TempDir()) {
			t.Fatal("expected true for missing CA")
		}
	})

	t.Run("new profile CA returns false", func(t *testing.T) {
		dir := t.TempDir()
		if err := GenerateCA(dir); err != nil {
			t.Fatalf("GenerateCA: %v", err)
		}
		if CANeedsRegeneration(dir) {
			t.Fatal("expected false for fresh v2-profile CA")
		}
	})

	t.Run("legacy CA with CRLSign returns true", func(t *testing.T) {
		dir := t.TempDir()
		mustWriteLegacyCA(t, dir)
		if !CANeedsRegeneration(dir) {
			t.Fatal("expected true for legacy CA with CRLSign")
		}
	})

	t.Run("CA without OU version marker returns true", func(t *testing.T) {
		dir := t.TempDir()
		mustWriteNoOUCA(t, dir)
		if !CANeedsRegeneration(dir) {
			t.Fatal("expected true for CA missing OU version marker")
		}
	})
}

func TestLoadServerTLSConfig_Chain(t *testing.T) {
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

	cfg, err := LoadServerTLSConfig(dir)
	if err != nil {
		t.Fatalf("LoadServerTLSConfig: %v", err)
	}
	if len(cfg.Certificates) == 0 {
		t.Fatal("expected at least one certificate in TLS config")
	}
	chain := cfg.Certificates[0].Certificate
	if len(chain) != 2 {
		t.Errorf("expected 2 DER entries in chain (leaf + CA), got %d", len(chain))
	}
}

func TestInstallCAWindows(t *testing.T) {
	origGOOS := currentGOOS
	origExecCombinedOutput := execCombinedOutput
	t.Cleanup(func() {
		currentGOOS = origGOOS
		execCombinedOutput = origExecCombinedOutput
	})

	currentGOOS = "windows"

	t.Run("install command failure includes output", func(t *testing.T) {
		execCombinedOutput = func(name string, args ...string) ([]byte, error) {
			if name != "certutil" {
				t.Fatalf("unexpected command %q", name)
			}
			if len(args) != 4 || args[0] != "-addstore" || args[2] != "ROOT" {
				t.Fatalf("unexpected args: %#v", args)
			}
			return []byte("Access is denied."), errors.New("exit status 1")
		}

		err := InstallCA(`C:\Users\Alice\.config\trae-proxy\ca\root-ca.pem`)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "Access is denied.") {
			t.Fatalf("expected command output in error, got %v", err)
		}
	})

	t.Run("verification failure after successful install", func(t *testing.T) {
		call := 0
		execCombinedOutput = func(name string, args ...string) ([]byte, error) {
			call++
			if name != "certutil" {
				t.Fatalf("unexpected command %q", name)
			}
			switch call {
			case 1:
				if len(args) != 4 || args[0] != "-addstore" {
					t.Fatalf("unexpected install args: %#v", args)
				}
				return []byte("CertUtil: -addstore command completed successfully."), nil
			case 2:
				if len(args) != 3 || args[0] != "-store" || args[2] != rootCACommonName {
					t.Fatalf("unexpected verify args: %#v", args)
				}
				return []byte("CertUtil: -store command completed successfully."), nil
			default:
				t.Fatalf("unexpected extra call %d", call)
				return nil, nil
			}
		}

		err := InstallCA(`C:\Users\Alice\.config\trae-proxy\ca\root-ca.pem`)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "not found") {
			t.Fatalf("expected verification failure, got %v", err)
		}
	})

	t.Run("success when installed cert is present in ROOT store", func(t *testing.T) {
		call := 0
		execCombinedOutput = func(name string, args ...string) ([]byte, error) {
			call++
			if name != "certutil" {
				t.Fatalf("unexpected command %q", name)
			}
			switch call {
			case 1:
				return []byte("CertUtil: -addstore command completed successfully."), nil
			case 2:
				return []byte("Subject: CN=trae-proxy Root CA"), nil
			default:
				t.Fatalf("unexpected extra call %d", call)
				return nil, nil
			}
		}

		if err := InstallCA(`C:\Users\Alice\.config\trae-proxy\ca\root-ca.pem`); err != nil {
			t.Fatalf("InstallCA: %v", err)
		}
	})
}

// mustWriteCustomCert writes a server cert with arbitrary validity to dir/server.pem.
// When newProfile is true the cert has the v2 profile (KeyEncipherment + OU marker);
// when false it mimics the legacy v1 profile for migration tests.
func mustWriteCustomCert(t *testing.T, dir string, caCert *x509.Certificate, caKey *ecdsa.PrivateKey, domain string, validity time.Duration, newProfile bool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))

	ou := []string{certProfileVersion}
	ku := x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment
	if !newProfile {
		ou = nil
		ku = x509.KeyUsageDigitalSignature
	}

	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: domain, OrganizationalUnit: ou},
		DNSNames:              []string{domain},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(validity),
		KeyUsage:              ku,
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

// mustWriteV1Cert writes a legacy v1 profile server cert (no KeyEncipherment, no OU).
func mustWriteV1Cert(t *testing.T, dir string, caCert *x509.Certificate, caKey *ecdsa.PrivateKey, domain string) {
	t.Helper()
	mustWriteCustomCert(t, dir, caCert, caKey, domain, 365*24*time.Hour, false)
}

// mustWriteLegacyCA writes a legacy v1 CA with CRLSign set.
func mustWriteLegacyCA(t *testing.T, dir string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate CA key: %v", err)
	}
	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{Organization: []string{"trae-proxy"}, CommonName: rootCACommonName},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create legacy CA: %v", err)
	}
	if err := writePEM(filepath.Join(dir, "root-ca.pem"), "CERTIFICATE", certDER); err != nil {
		t.Fatalf("write legacy CA: %v", err)
	}
}

// mustWriteNoOUCA writes a CA cert with no OrganizationalUnit (missing version marker).
func mustWriteNoOUCA(t *testing.T, dir string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate CA key: %v", err)
	}
	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{Organization: []string{"trae-proxy"}, CommonName: rootCACommonName},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            0,
		MaxPathLenZero:        true,
	}
	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create no-OU CA: %v", err)
	}
	if err := writePEM(filepath.Join(dir, "root-ca.pem"), "CERTIFICATE", certDER); err != nil {
		t.Fatalf("write no-OU CA: %v", err)
	}
}

// tlsConfigHasTwoChainEntries is used only in TestLoadServerTLSConfig_Chain via the named return.
var _ = (*tls.Config)(nil)
