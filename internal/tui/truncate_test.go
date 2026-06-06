package tui

import (
	"testing"
	"unicode/utf8"
)

// TestTruncateRuneSafe guards against the byte-boundary slicing bug: truncating
// multibyte content (process names, cmdlines) must never split a UTF-8 codepoint
// and must respect the rune budget (max-1 content runes + the ellipsis).
func TestTruncateRuneSafe(t *testing.T) {
	in := "café—señor—日本語サーバ" // ASCII + 2- and 3-byte runes
	for _, max := range []int{0, 1, 2, 3, 5, 8, 100} {
		got := truncate(in, max)
		if !utf8.ValidString(got) {
			t.Errorf("truncate(%q, %d) = %q: not valid UTF-8", in, max, got)
		}
		if max > 0 && utf8.RuneCountInString(got) > max {
			t.Errorf("truncate(%q, %d) = %q: %d runes, want <= %d", in, max, got, utf8.RuneCountInString(got), max)
		}
	}
	if got := truncate("hello", 10); got != "hello" {
		t.Errorf("truncate within budget changed string: %q", got)
	}
}
