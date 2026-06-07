package collector

import (
	"os"
	"path/filepath"
	"testing"
)

// TestECCPresence verifies the collector reports Present only when EDAC memory
// controllers exist — the signal that distinguishes "0 errors on ECC RAM" from
// "no ECC hardware" (so the TUI doesn't assert "ECC ok" on a Pi, and the alert
// form doesn't offer ECC rules that can never fire).
func TestECCPresence(t *testing.T) {
	old := edacGlob
	defer func() { edacGlob = old }()

	t.Run("no EDAC controllers", func(t *testing.T) {
		edacGlob = filepath.Join(t.TempDir(), "mc", "mc*")
		sample, err := (&ECCCollector{}).Collect()
		if err != nil {
			t.Fatalf("collect: %v", err)
		}
		data := sample.Data.(ECCData)
		if data.Present {
			t.Error("Present = true with no EDAC controllers, want false")
		}
	})

	t.Run("EDAC controllers present", func(t *testing.T) {
		root := t.TempDir()
		mc := filepath.Join(root, "mc", "mc0")
		if err := os.MkdirAll(mc, 0o755); err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(mc, "ce_count"), []byte("3\n"), 0o644)
		os.WriteFile(filepath.Join(mc, "ue_count"), []byte("1\n"), 0o644)
		edacGlob = filepath.Join(root, "mc", "mc*")

		sample, err := (&ECCCollector{}).Collect()
		if err != nil {
			t.Fatalf("collect: %v", err)
		}
		data := sample.Data.(ECCData)
		if !data.Present {
			t.Error("Present = false with an EDAC controller, want true")
		}
		if data.Corrected != 3 || data.Uncorrected != 1 {
			t.Errorf("counts = %d/%d, want 3/1", data.Corrected, data.Uncorrected)
		}
	})
}
