package store

import "testing"

// TestPinnedProcessPrefs covers the preference read the daemon's runtime-pins
// callback relies on: unset → nil, a valid JSON array → the patterns, and a
// malformed value → an error (rather than the silently-swallowed Unmarshal the
// inline closure used to do).
func TestPinnedProcessPrefs(t *testing.T) {
	s := newPruneTestStore(t)

	// Unset preference → nil, no error.
	if pins, err := s.PinnedProcessPrefs(); err != nil || pins != nil {
		t.Fatalf("unset: pins=%v err=%v, want nil,nil", pins, err)
	}

	setPref := func(v string) {
		t.Helper()
		if _, err := s.db.Exec(
			"INSERT INTO preferences (key, value) VALUES ('pinned_processes', ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value",
			v); err != nil {
			t.Fatalf("set preference: %v", err)
		}
	}

	// Valid JSON array → the patterns.
	setPref(`["nginx","postgres*","*/myapp"]`)
	pins, err := s.PinnedProcessPrefs()
	if err != nil {
		t.Fatalf("valid: unexpected error: %v", err)
	}
	want := []string{"nginx", "postgres*", "*/myapp"}
	if len(pins) != len(want) {
		t.Fatalf("valid: pins=%v, want %v", pins, want)
	}
	for i := range want {
		if pins[i] != want[i] {
			t.Errorf("valid: pins[%d]=%q, want %q", i, pins[i], want[i])
		}
	}

	// Empty string value → nil, no error.
	setPref("")
	if pins, err := s.PinnedProcessPrefs(); err != nil || pins != nil {
		t.Errorf("empty: pins=%v err=%v, want nil,nil", pins, err)
	}

	// Malformed JSON → an error is surfaced, not swallowed.
	setPref(`{not valid`)
	if _, err := s.PinnedProcessPrefs(); err == nil {
		t.Error("malformed: expected an error for invalid JSON, got nil")
	}
}
