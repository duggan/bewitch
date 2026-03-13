package tui

import (
	"os"
	"path/filepath"
	"testing"
)

func withTempKnownHosts(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "known_hosts")
	old := knownHostsPathFn
	knownHostsPathFn = func() (string, error) { return path, nil }
	t.Cleanup(func() { knownHostsPathFn = old })
}

func TestKnownHostsRoundTrip(t *testing.T) {
	withTempKnownHosts(t)

	// Initially empty
	hosts, err := LoadKnownHosts()
	if err != nil {
		t.Fatalf("LoadKnownHosts() error = %v", err)
	}
	if len(hosts) != 0 {
		t.Fatalf("expected empty hosts, got %d", len(hosts))
	}

	// Save a host
	if err := SaveKnownHost("server1:9119", "sha256:aabbccdd"); err != nil {
		t.Fatalf("SaveKnownHost() error = %v", err)
	}

	// Reload and verify
	hosts, err = LoadKnownHosts()
	if err != nil {
		t.Fatalf("LoadKnownHosts() error = %v", err)
	}
	if fp, ok := hosts["server1:9119"]; !ok || fp != "sha256:aabbccdd" {
		t.Errorf("expected sha256:aabbccdd, got %q (ok=%v)", fp, ok)
	}

	// Save another host
	if err := SaveKnownHost("server2:9119", "sha256:11223344"); err != nil {
		t.Fatalf("SaveKnownHost() error = %v", err)
	}

	hosts, err = LoadKnownHosts()
	if err != nil {
		t.Fatalf("LoadKnownHosts() error = %v", err)
	}
	if len(hosts) != 2 {
		t.Fatalf("expected 2 hosts, got %d", len(hosts))
	}

	// Update existing host
	if err := SaveKnownHost("server1:9119", "sha256:newfingerprint"); err != nil {
		t.Fatalf("SaveKnownHost() error = %v", err)
	}

	hosts, err = LoadKnownHosts()
	if err != nil {
		t.Fatalf("LoadKnownHosts() error = %v", err)
	}
	if fp := hosts["server1:9119"]; fp != "sha256:newfingerprint" {
		t.Errorf("expected updated fingerprint, got %q", fp)
	}
	if len(hosts) != 2 {
		t.Fatalf("expected still 2 hosts after update, got %d", len(hosts))
	}
}

func TestRemoveKnownHost(t *testing.T) {
	withTempKnownHosts(t)

	SaveKnownHost("server1:9119", "sha256:aabb")
	SaveKnownHost("server2:9119", "sha256:ccdd")

	if err := RemoveKnownHost("server1:9119"); err != nil {
		t.Fatalf("RemoveKnownHost() error = %v", err)
	}

	hosts, err := LoadKnownHosts()
	if err != nil {
		t.Fatalf("LoadKnownHosts() error = %v", err)
	}
	if _, ok := hosts["server1:9119"]; ok {
		t.Error("server1 should have been removed")
	}
	if _, ok := hosts["server2:9119"]; !ok {
		t.Error("server2 should still exist")
	}
}

func TestLoadKnownHosts_SkipsCommentsAndBlankLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "known_hosts")
	old := knownHostsPathFn
	knownHostsPathFn = func() (string, error) { return path, nil }
	t.Cleanup(func() { knownHostsPathFn = old })

	// Manually write a file with comments and blank lines
	content := "# This is a comment\n\nserver1:9119 sha256:aabb\n# Another comment\nserver2:9119 sha256:ccdd\n"
	os.WriteFile(path, []byte(content), 0600)

	hosts, err := LoadKnownHosts()
	if err != nil {
		t.Fatalf("LoadKnownHosts() error = %v", err)
	}
	if len(hosts) != 2 {
		t.Fatalf("expected 2 hosts, got %d", len(hosts))
	}
	if hosts["server1:9119"] != "sha256:aabb" {
		t.Errorf("server1 fingerprint = %q", hosts["server1:9119"])
	}
	if hosts["server2:9119"] != "sha256:ccdd" {
		t.Errorf("server2 fingerprint = %q", hosts["server2:9119"])
	}
}
