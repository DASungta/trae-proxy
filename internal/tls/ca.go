package tlsutil

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/zhangyc/trae-proxy/internal/privilege"
)

func GenerateCA(dir string) error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("generate CA key: %w", err)
	}

	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			Organization: []string{"trae-proxy"},
			CommonName:   "trae-proxy Root CA",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return fmt.Errorf("create CA cert: %w", err)
	}

	if err := writePEM(filepath.Join(dir, "root-ca.pem"), "CERTIFICATE", certDER); err != nil {
		return err
	}

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return fmt.Errorf("marshal CA key: %w", err)
	}
	return writePEM(filepath.Join(dir, "root-ca-key.pem"), "EC PRIVATE KEY", keyDER)
}

func LoadCA(dir string) (*x509.Certificate, *ecdsa.PrivateKey, error) {
	certPEM, err := os.ReadFile(filepath.Join(dir, "root-ca.pem"))
	if err != nil {
		return nil, nil, err
	}
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return nil, nil, fmt.Errorf("failed to decode CA cert PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, nil, err
	}

	keyPEM, err := os.ReadFile(filepath.Join(dir, "root-ca-key.pem"))
	if err != nil {
		return nil, nil, err
	}
	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		return nil, nil, fmt.Errorf("failed to decode CA key PEM")
	}
	key, err := x509.ParseECPrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, nil, err
	}

	return cert, key, nil
}

func GenerateServerCert(dir string, caCert *x509.Certificate, caKey *ecdsa.PrivateKey, domain string) error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("generate server key: %w", err)
	}

	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			Organization: []string{"trae-proxy"},
			CommonName:   domain,
		},
		DNSNames:    []string{domain},
		NotBefore:   time.Now(),
		NotAfter:    time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:    x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, caCert, &key.PublicKey, caKey)
	if err != nil {
		return fmt.Errorf("create server cert: %w", err)
	}

	if err := writePEM(filepath.Join(dir, "server.pem"), "CERTIFICATE", certDER); err != nil {
		return err
	}

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return fmt.Errorf("marshal server key: %w", err)
	}
	return writePEM(filepath.Join(dir, "server-key.pem"), "EC PRIVATE KEY", keyDER)
}

func LoadServerTLSConfig(dir string) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(
		filepath.Join(dir, "server.pem"),
		filepath.Join(dir, "server-key.pem"),
	)
	if err != nil {
		return nil, err
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
	}, nil
}

func NeedsRegeneration(dir string, domain string) bool {
	certPEM, err := os.ReadFile(filepath.Join(dir, "server.pem"))
	if err != nil {
		return true
	}
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return true
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return true
	}
	for _, name := range cert.DNSNames {
		if name == domain {
			return false
		}
	}
	return true
}

func InstallCA(caCertPath string) error {
	switch runtime.GOOS {
	case "darwin":
		return elevatedExec("security", "add-trusted-cert", "-d", "-r", "trustRoot",
			"-k", "/Library/Keychains/System.keychain", caCertPath)
	case "linux":
		dest := "/usr/local/share/ca-certificates/trae-proxy.crt"
		if err := elevatedExec("cp", caCertPath, dest); err != nil {
			return err
		}
		return elevatedExec("update-ca-certificates")
	case "windows":
		return exec.Command("certutil", "-addstore", "-f", "ROOT", caCertPath).Run()
	default:
		return fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
}

func UninstallCA(caCertPath string) error {
	switch runtime.GOOS {
	case "darwin":
		return elevatedExec("security", "remove-trusted-cert", "-d", caCertPath)
	case "linux":
		elevatedExec("rm", "-f", "/usr/local/share/ca-certificates/trae-proxy.crt")
		return elevatedExec("update-ca-certificates", "--fresh")
	case "windows":
		return exec.Command("certutil", "-delstore", "ROOT", "trae-proxy Root CA").Run()
	default:
		return fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
}

func elevatedExec(name string, args ...string) error {
	if runtime.GOOS == "darwin" {
		parts := append([]string{name}, args...)
		escaped := make([]string, len(parts))
		for i, p := range parts {
			escaped[i] = shellQuote(p)
		}
		return privilege.RunPrivileged(strings.Join(escaped, " "))
	}
	if runtime.GOOS == "windows" {
		return exec.Command(name, args...).Run()
	}
	cmdArgs := append([]string{name}, args...)
	cmd := exec.Command("sudo", cmdArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func writePEM(path string, blockType string, data []byte) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return pem.Encode(f, &pem.Block{Type: blockType, Bytes: data})
}
