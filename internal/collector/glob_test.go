package collector

import (
	"path/filepath"
	"testing"
)

// TestGlobMatchAny verifies the cmdline matcher: anchored, '*' crosses '/', '?' is
// one char — mirroring the alert engine's SQL LIKE so a ProcessPattern enriches the
// same processes the rule counts.
func TestGlobMatchAny(t *testing.T) {
	cases := []struct {
		pattern, s string
		want       bool
	}{
		{"*", "anything/at/all", true},
		{"", "", true},
		{"", "x", false},
		{"nginx", "nginx", true},
		{"nginx", "nginx-worker", false}, // anchored: no trailing match
		{"nginx*", "nginx-worker", true},
		{"*/myserver*", "/usr/bin/myserver --foo bar", true}, // '*' crosses '/'
		{"*myserver*", "/usr/local/sbin/myserver", true},
		{"*/myserver", "/usr/bin/myserver", true},
		{"*/myserver", "/usr/bin/myserver --foo", false}, // anchored: trailing args fail
		{"post?res*", "postgres: writer", true},
		{"post?res", "postgresql", false},
		{"/usr/bin/*", "/usr/bin/python3", true},
		{"/usr/bin/*", "/usr/local/python3", false},
		{"a*b*c", "axxbyyc", true},
		{"a*b*c", "axxbyy", false},
	}
	for _, c := range cases {
		if got := globMatchAny(c.pattern, c.s); got != c.want {
			t.Errorf("globMatchAny(%q, %q) = %v, want %v", c.pattern, c.s, got, c.want)
		}
	}
}

// TestGlobMatchAnyCrossesSlash documents the key difference from filepath.Match
// (which is path-aware): the cmdline matcher must let '*' span '/'.
func TestGlobMatchAnyCrossesSlash(t *testing.T) {
	const pattern, cmd = "*/myapp*", "/opt/bin/myapp --serve"
	if !globMatchAny(pattern, cmd) {
		t.Errorf("globMatchAny should match cmdline across '/': %q vs %q", pattern, cmd)
	}
	if matched, _ := filepath.Match(pattern, cmd); matched {
		t.Errorf("precondition: filepath.Match is path-aware and should NOT match %q vs %q", pattern, cmd)
	}
}
