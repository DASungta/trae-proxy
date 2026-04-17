package updater

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"
)

const (
	repo    = "DASungta/trae-proxy"
	apiBase = "https://api.github.com"
	ghBase  = "https://github.com"
)

// Updater handles fetching and installing new releases.
type Updater struct {
	Client *http.Client
}

// New returns an Updater with a sensible timeout.
func New() *Updater {
	return &Updater{
		Client: &http.Client{Timeout: 60 * time.Second},
	}
}

// AssetName returns the platform-specific release binary name.
// Returns an error for unsupported or Windows platforms.
func AssetName() (string, error) {
	switch {
	case runtime.GOOS == "darwin" && runtime.GOARCH == "arm64":
		return "trae-proxy-darwin-arm64", nil
	case runtime.GOOS == "darwin" && runtime.GOARCH == "amd64":
		return "trae-proxy-darwin-amd64", nil
	case runtime.GOOS == "linux" && runtime.GOARCH == "amd64":
		return "trae-proxy-linux-amd64", nil
	case runtime.GOOS == "windows":
		return "", fmt.Errorf("auto-update is not supported on Windows; please download manually from https://github.com/%s/releases/latest", repo)
	default:
		return "", fmt.Errorf("unsupported platform: %s/%s", runtime.GOOS, runtime.GOARCH)
	}
}

// LatestTag fetches the latest release tag name from GitHub.
func (u *Updater) LatestTag() (string, error) {
	url := fmt.Sprintf("%s/repos/%s/releases/latest", apiBase, repo)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := u.Client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch latest release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API returned HTTP %d", resp.StatusCode)
	}

	var result struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode release response: %w", err)
	}
	if result.TagName == "" {
		return "", fmt.Errorf("no tag_name in release response")
	}
	return result.TagName, nil
}

// FetchChecksum downloads checksums.txt for the given tag and returns the
// expected sha256 hex for assetName.
func (u *Updater) FetchChecksum(tag, assetName string) (string, error) {
	url := fmt.Sprintf("%s/%s/releases/download/%s/checksums.txt", ghBase, repo, tag)
	resp, err := u.Client.Get(url)
	if err != nil {
		return "", fmt.Errorf("fetch checksums.txt: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("checksums.txt returned HTTP %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read checksums.txt: %w", err)
	}

	// Format: "<sha256>  <filename>" (two spaces, matching shasum -a 256 output)
	for _, line := range strings.Split(string(data), "\n") {
		parts := strings.Fields(line)
		if len(parts) == 2 && parts[1] == assetName {
			return parts[0], nil
		}
	}
	return "", fmt.Errorf("checksum for %q not found in checksums.txt", assetName)
}

// Download fetches the release asset to a temp file in os.TempDir() and returns
// the temp path. Using the system temp dir avoids permission issues when the
// current binary lives in a root-owned directory (e.g. /usr/local/bin).
func (u *Updater) Download(tag, assetName, _ string) (string, error) {
	url := fmt.Sprintf("%s/%s/releases/download/%s/%s", ghBase, repo, tag, assetName)
	resp, err := u.Client.Get(url)
	if err != nil {
		return "", fmt.Errorf("download %s: %w", assetName, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download returned HTTP %d", resp.StatusCode)
	}

	f, err := os.CreateTemp("", "trae-proxy-*.new")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := f.Name()

	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return "", fmt.Errorf("write download: %w", err)
	}
	f.Close()
	return tmpPath, nil
}

// Verify checks that path has the expected sha256 hex checksum.
func Verify(path, expectedSHA string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("hash file: %w", err)
	}
	got := hex.EncodeToString(h.Sum(nil))
	if got != expectedSHA {
		return fmt.Errorf("checksum mismatch: got %s, expected %s", got, expectedSHA)
	}
	return nil
}

// Replace atomically replaces currentExe with newBinary.
func Replace(currentExe, newBinary string) error {
	if err := os.Chmod(newBinary, 0755); err != nil {
		return fmt.Errorf("chmod: %w", err)
	}
	if err := os.Rename(newBinary, currentExe); err != nil {
		return fmt.Errorf("replace binary (try running with sudo): %w", err)
	}
	return nil
}
