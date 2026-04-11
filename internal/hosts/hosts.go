package hosts

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
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
		exec.Command("sudo", "dscacheutil", "-flushcache").Run()
		exec.Command("sudo", "killall", "-HUP", "mDNSResponder").Run()
	}
}
