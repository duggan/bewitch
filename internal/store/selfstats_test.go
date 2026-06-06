package store

import "testing"

// TestPauseDroppedSamplesTotalAccumulates guards the self-metrics fix: bufDropped
// is zeroed by resume() (so an API scrape between pauses almost always saw 0),
// while the lifetime accumulator must keep climbing across pauses.
func TestPauseDroppedSamplesTotalAccumulates(t *testing.T) {
	s := newPruneTestStore(t)

	if got := s.PauseDroppedSamplesTotal(); got != 0 {
		t.Fatalf("initial total = %d, want 0", got)
	}

	// First pause drops 10 over the cap. resume() zeroes bufDropped but must fold
	// it into the lifetime total. Empty buffer => no DB flush needed.
	s.pause()
	s.mu.Lock()
	s.bufDropped = 10
	s.mu.Unlock()
	s.resume()

	if got := s.PauseDroppedSamplesTotal(); got != 10 {
		t.Errorf("after first pause: total = %d, want 10", got)
	}
	s.mu.Lock()
	perPause := s.bufDropped
	s.mu.Unlock()
	if perPause != 0 {
		t.Errorf("bufDropped after resume = %d, want 0 (per-pause counter must reset)", perPause)
	}

	// A second pause keeps accumulating rather than resetting.
	s.pause()
	s.mu.Lock()
	s.bufDropped = 5
	s.mu.Unlock()
	s.resume()

	if got := s.PauseDroppedSamplesTotal(); got != 15 {
		t.Errorf("after second pause: total = %d, want 15 (must accumulate)", got)
	}
}
