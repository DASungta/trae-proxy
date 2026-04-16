package hosts

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/zhangyc/trae-proxy/internal/privilege"
)

const marker = "# trae-proxy"

func HostsPath() string {
	if runtime.GOOS == "windows" {
		return `C:\Windows\System32\drivers\etc\hosts`
	}
	return "/etc/hosts"
}

func HasEntry(domain string) (bool, error) {
	data, err := os.ReadFile(HostsPath())
	if err != nil {
		return false, err
	}
	entry := fmt.Sprintf("127.0.0.1 %s", domain)
	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, entry) && strings.Contains(line, marker) {
			return true, nil
		}
	}
	return false, nil
}

func Add(domain string) error {
	has, err := HasEntry(domain)
	if err != nil {
		return err
	}
	if has {
		return nil
	}

	line := fmt.Sprintf("127.0.0.1 %s %s\n", domain, marker)

	if runtime.GOOS == "windows" {
		f, err := os.OpenFile(HostsPath(), os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return fmt.Errorf("open hosts file: %w (run as administrator)", err)
		}
		defer f.Close()
		_, err = f.WriteString(line)
		return err
	}

	if runtime.GOOS == "darwin" {
		script := fmt.Sprintf("printf '%%s' %s | tee -a %s > /dev/null && dscacheutil -flushcache && killall -HUP mDNSResponder",
			shellQuote(line), shellQuote(HostsPath()))
		if err := privilege.RunPrivileged(script); err != nil {
			return fmt.Errorf("add hosts entry: %w", err)
		}
		return nil
	}

	cmd := exec.Command("sudo", "tee", "-a", HostsPath())
	cmd.Stdin = strings.NewReader(line)
	cmd.Stdout = nil
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("add hosts entry: %w", err)
	}

	flushDNSCache()
	return nil
}

func Remove() error {
	data, err := os.ReadFile(HostsPath())
	if err != nil {
		return err
	}

	var filtered []string
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.Contains(line, marker) {
			filtered = append(filtered, line)
		}
	}
	content := strings.Join(filtered, "\n")

	if runtime.GOOS == "windows" {
		return os.WriteFile(HostsPath(), []byte(content), 0644)
	}

	if runtime.GOOS == "darwin" {
		tmpFile, err := os.CreateTemp("", "trae-proxy-hosts-*")
		if err != nil {
			return fmt.Errorf("create temp file: %w", err)
		}
		tmpPath := tmpFile.Name()
		if _, err := tmpFile.WriteString(content); err != nil {
			tmpFile.Close()
			_ = os.Remove(tmpPath)
			return fmt.Errorf("write temp file: %w", err)
		}
		if err := tmpFile.Close(); err != nil {
			_ = os.Remove(tmpPath)
			return fmt.Errorf("close temp file: %w", err)
		}
		defer os.Remove(tmpPath)

		script := fmt.Sprintf("cat %s > %s && rm -f %s && dscacheutil -flushcache && killall -HUP mDNSResponder",
			shellQuote(tmpPath), shellQuote(HostsPath()), shellQuote(tmpPath))
		if err := privilege.RunPrivileged(script); err != nil {
			return fmt.Errorf("remove hosts entry: %w", err)
		}
		return nil
	}

	cmd := exec.Command("sudo", "tee", HostsPath())
	cmd.Stdin = strings.NewReader(content)
	cmd.Stdout = nil
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("remove hosts entry: %w", err)
	}

	flushDNSCache()
	return nil
}

func flushDNSCache() {
	if runtime.GOOS == "darwin" {
		_ = privilege.RunPrivileged("dscacheutil -flushcache")
		_ = privilege.RunPrivileged("killall -HUP mDNSResponder")
	}
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
