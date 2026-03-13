package tui

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// knownHostsPathFn returns the path to the known_hosts file.
// Overridden in tests.
var knownHostsPathFn = defaultKnownHostsPath

func defaultKnownHostsPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "bewitch", "known_hosts"), nil
}

// LoadKnownHosts reads the known_hosts file and returns a map of addr → fingerprint.
func LoadKnownHosts() (map[string]string, error) {
	path, err := knownHostsPathFn()
	if err != nil {
		return nil, err
	}

	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]string), nil
		}
		return nil, err
	}
	defer f.Close()

	hosts := make(map[string]string)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, " ", 2)
		if len(parts) == 2 {
			hosts[parts[0]] = parts[1]
		}
	}
	return hosts, scanner.Err()
}

// SaveKnownHost adds or updates a fingerprint entry for the given address.
func SaveKnownHost(addr, fingerprint string) error {
	hosts, err := LoadKnownHosts()
	if err != nil {
		return err
	}
	hosts[addr] = fingerprint
	return writeKnownHosts(hosts)
}

// RemoveKnownHost removes the entry for the given address.
func RemoveKnownHost(addr string) error {
	hosts, err := LoadKnownHosts()
	if err != nil {
		return err
	}
	delete(hosts, addr)
	return writeKnownHosts(hosts)
}

func writeKnownHosts(hosts map[string]string) error {
	path, err := knownHostsPathFn()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}

	var b strings.Builder
	for addr, fp := range hosts {
		fmt.Fprintf(&b, "%s %s\n", addr, fp)
	}
	return os.WriteFile(path, []byte(b.String()), 0600)
}
