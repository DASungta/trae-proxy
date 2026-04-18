package tlsutil

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

const rootCACommonName = "trae-proxy Root CA"

// certProfileVersion is stamped as Subject.OrganizationalUnit to enable migration detection.
const certProfileVersion = "v2"

var (
	currentGOOS        = runtime.GOOS
	execCombinedOutput = func(name string, args ...string) ([]byte, error) {
		return exec.Command(name, args...).CombinedOutput()
	}
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
			Organization:       []string{"trae-proxy"},
			OrganizationalUnit: []string{certProfileVersion},
			CommonName:         rootCACommonName,
		},
		NotBefore: time.Now(),
		NotAfter:  time.Now().Add(10 * 365 * 24 * time.Hour),
		// KeyUsageCertSign only — omitting CRLSign so Schannel does not expect
		// a CRL Distribution Point and fail with CRYPT_E_NO_REVOCATION_CHECK.
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            0,
		MaxPathLenZero:        true,
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
			Organization:       []string{"trae-proxy"},
			OrganizationalUnit: []string{certProfileVersion},
			CommonName:         domain,
		},
		DNSNames:  []string{domain},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore: time.Now(),
		NotAfter:  time.Now().Add(365 * 24 * time.Hour),
		// KeyEncipherment is required by strict Schannel / older TLS stacks even
		// when using ECDSA (ECDHE never uses encryption, but some validators still check).
		KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		// ClientAuth is included for maximum compatibility; it does not affect server role.
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IsCA:                  false,
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

// LoadServerTLSConfig loads the server cert and appends the CA cert to the
// TLS chain so clients (Schannel, Chromium) can build the chain deterministically
// without relying solely on AKI lookup in the system trust store.
func LoadServerTLSConfig(dir string) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(
		filepath.Join(dir, "server.pem"),
		filepath.Join(dir, "server-key.pem"),
	)
	if err != nil {
		return nil, err
	}

	caPEM, err := os.ReadFile(filepath.Join(dir, "root-ca.pem"))
	if err != nil {
		return nil, fmt.Errorf("read CA cert for chain: %w", err)
	}
	block, _ := pem.Decode(caPEM)
	if block == nil {
		return nil, fmt.Errorf("failed to decode CA cert PEM for chain")
	}
	cert.Certificate = append(cert.Certificate, block.Bytes)

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
	}, nil
}

// NeedsRegeneration reports whether the server cert should be re-issued.
// Returns true for missing/unreadable certs, expiring certs, domain mismatches,
// and legacy v1 profile certs (missing KeyEncipherment or OU version marker).
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
	if time.Until(cert.NotAfter) < 30*24*time.Hour {
		return true
	}
	if cert.NotAfter.Sub(cert.NotBefore) > 398*24*time.Hour {
		return true
	}
	if cert.KeyUsage&x509.KeyUsageKeyEncipherment == 0 {
		return true // legacy v1 profile
	}
	if !containsOU(cert.Subject.OrganizationalUnit, certProfileVersion) {
		return true // legacy v1 profile
	}
	for _, name := range cert.DNSNames {
		if name == domain {
			return false
		}
	}
	return true
}

// CANeedsRegeneration reports whether the root CA should be re-generated.
// Returns true for missing/unreadable CA files and legacy v1 profile CAs
// (those with CRLSign bit or missing OU version marker).
func CANeedsRegeneration(dir string) bool {
	caPEM, err := os.ReadFile(filepath.Join(dir, "root-ca.pem"))
	if err != nil {
		return true
	}
	block, _ := pem.Decode(caPEM)
	if block == nil {
		return true
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return true
	}
	if cert.KeyUsage&x509.KeyUsageCRLSign != 0 {
		return true // legacy v1 profile — CRLSign triggers Schannel revocation check
	}
	if !containsOU(cert.Subject.OrganizationalUnit, certProfileVersion) {
		return true // legacy v1 profile
	}
	return false
}

func containsOU(ous []string, target string) bool {
	for _, ou := range ous {
		if ou == target {
			return true
		}
	}
	return false
}

func InstallCA(caCertPath string) error {
	switch currentGOOS {
	case "darwin":
		// macOS 15+/26: SecTrustSettingsSetTrustSettings requires an interactive user session.
		// osascript "with administrator privileges" cannot satisfy this (errAuthorizationInteractionNotAllowed).
		// Using sudo lets security reuse the terminal's authorization session directly.
		cmd := exec.Command("sudo", "security", "add-trusted-cert",
			"-d",
			"-r", "trustRoot",
			"-p", "ssl",
			"-k", "/Library/Keychains/System.keychain",
			caCertPath)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	case "linux":
		dest := "/usr/local/share/ca-certificates/trae-proxy.crt"
		if err := elevatedExec("cp", caCertPath, dest); err != nil {
			return err
		}
		return elevatedExec("update-ca-certificates")
	case "windows":
		output, err := runCommandCombined("certutil", "-addstore", "-f", "ROOT", caCertPath)
		if err != nil {
			return formatCommandError("install CA with certutil", err, output)
		}

		installed, verifyOutput, err := windowsRootCAInstalled(rootCACommonName)
		if err != nil {
			return formatCommandError("verify CA in Windows ROOT store", err, []byte(verifyOutput))
		}
		if !installed {
			return fmt.Errorf("verify CA in Windows ROOT store: %s not found: %s", rootCACommonName, summarizeCommandOutput([]byte(verifyOutput)))
		}
		return nil
	default:
		return fmt.Errorf("unsupported OS: %s", currentGOOS)
	}
}

func UninstallCA(caCertPath string) error {
	switch currentGOOS {
	case "darwin":
		cmd := exec.Command("sudo", "security", "remove-trusted-cert", "-d", caCertPath)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	case "linux":
		elevatedExec("rm", "-f", "/usr/local/share/ca-certificates/trae-proxy.crt")
		return elevatedExec("update-ca-certificates", "--fresh")
	case "windows":
		return exec.Command("certutil", "-delstore", "ROOT", rootCACommonName).Run()
	default:
		return fmt.Errorf("unsupported OS: %s", currentGOOS)
	}
}

func elevatedExec(name string, args ...string) error {
	if currentGOOS == "windows" {
		return exec.Command(name, args...).Run()
	}
	cmdArgs := append([]string{name}, args...)
	cmd := exec.Command("sudo", cmdArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func writePEM(path string, blockType string, data []byte) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return pem.Encode(f, &pem.Block{Type: blockType, Bytes: data})
}

func runCommandCombined(name string, args ...string) ([]byte, error) {
	return execCombinedOutput(name, args...)
}

func windowsRootCAInstalled(commonName string) (bool, string, error) {
	output, err := runCommandCombined("certutil", "-store", "ROOT", commonName)
	text := strings.TrimSpace(decodeCommandOutput(output))
	if err != nil {
		return false, text, err
	}
	return strings.Contains(strings.ToLower(text), strings.ToLower(commonName)), text, nil
}

func formatCommandError(action string, err error, output []byte) error {
	summary := summarizeCommandOutput(output)
	if summary == "no command output" {
		return fmt.Errorf("%s: %w", action, err)
	}
	return fmt.Errorf("%s: %w: %s", action, err, summary)
}

func summarizeCommandOutput(output []byte) string {
	text := strings.TrimSpace(decodeCommandOutput(output))
	if text == "" {
		return "no command output"
	}
	return text
}

func decodeCommandOutput(output []byte) string {
	if len(output) == 0 {
		return ""
	}
	if utf8.Valid(output) {
		return string(output)
	}
	if decoded, ok := decodeUTF16(output); ok {
		return decoded
	}
	if currentGOOS == "windows" {
		if decoded, _, err := transform.Bytes(simplifiedchinese.GB18030.NewDecoder(), output); err == nil && utf8.Valid(decoded) {
			return string(decoded)
		}
	}
	return string(output)
}

func decodeUTF16(output []byte) (string, bool) {
	if len(output) < 2 {
		return "", false
	}
	if bytes.HasPrefix(output, []byte{0xff, 0xfe}) {
		return decodeUTF16Words(output[2:], true)
	}
	if bytes.HasPrefix(output, []byte{0xfe, 0xff}) {
		return decodeUTF16Words(output[2:], false)
	}
	if !looksLikeUTF16(output) {
		return "", false
	}
	return decodeUTF16Words(output, true)
}

func decodeUTF16Words(output []byte, littleEndian bool) (string, bool) {
	if len(output) < 2 || len(output)%2 != 0 {
		return "", false
	}
	words := make([]uint16, 0, len(output)/2)
	for i := 0; i < len(output); i += 2 {
		var w uint16
		if littleEndian {
			w = uint16(output[i]) | uint16(output[i+1])<<8
		} else {
			w = uint16(output[i])<<8 | uint16(output[i+1])
		}
		words = append(words, w)
	}
	return string(utf16.Decode(words)), true
}

func looksLikeUTF16(output []byte) bool {
	zeroes := 0
	sample := len(output)
	if sample > 16 {
		sample = 16
	}
	for i := 1; i < sample; i += 2 {
		if output[i] == 0 {
			zeroes++
		}
	}
	return zeroes >= 3
}
