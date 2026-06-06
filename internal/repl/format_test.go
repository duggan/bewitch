package repl

import (
	"testing"
	"unicode/utf8"
)

// TestPadRightRuneSafe guards the REPL table renderer against byte-boundary
// slicing: padding/truncating multibyte cell content must stay valid UTF-8 and
// measure width in runes, not bytes.
func TestPadRightRuneSafe(t *testing.T) {
	// Truncation path (rune count exceeds width).
	got := padRight("café日本語", 3)
	if !utf8.ValidString(got) {
		t.Errorf("padRight truncate = %q: not valid UTF-8", got)
	}
	if utf8.RuneCountInString(got) != 3 {
		t.Errorf("padRight truncate = %q: %d runes, want 3", got, utf8.RuneCountInString(got))
	}

	// Padding path (shorter than width).
	if got := padRight("ab", 5); utf8.RuneCountInString(got) != 5 {
		t.Errorf("padRight pad = %q: %d runes, want 5", got, utf8.RuneCountInString(got))
	}
}

// TestComputeWidthsRuneBased confirms column widths are measured in runes.
func TestComputeWidthsRuneBased(t *testing.T) {
	w := computeWidths([]string{"日本語"}, [][]string{{"x"}}, 200)
	if w[0] != 3 {
		t.Errorf("computeWidths header rune width = %d, want 3", w[0])
	}
}
