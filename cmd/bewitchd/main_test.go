package main

import (
	"testing"
	"time"
)

// TestComputeTickInterval covers the scheduler-tick GCD and its floor: clean
// (round) intervals keep their exact GCD, while coprime sub-second combinations
// that would otherwise spin the scheduler at a tiny tick are capped at minTick.
func TestComputeTickInterval(t *testing.T) {
	ms := time.Millisecond
	s := time.Second
	cases := []struct {
		name      string
		intervals []time.Duration
		wantTick  time.Duration
		wantCap   bool
	}{
		{"typical round intervals", []time.Duration{1 * s, 5 * s, 30 * s}, 1 * s, false},
		{"sub-second clean divisor", []time.Duration{100 * ms, 500 * ms, 5 * s}, 100 * ms, false},
		{"single collector", []time.Duration{5 * s}, 5 * s, false},
		{"exactly at the floor", []time.Duration{100 * ms, 200 * ms}, 100 * ms, false},
		// GCD(250ms, 400ms) = 50ms < minTick → capped.
		{"coprime sub-second floored", []time.Duration{250 * ms, 400 * ms}, 100 * ms, true},
		// GCD(100ms, 105ms) = 5ms → capped (the churn case).
		{"near-coprime floored", []time.Duration{100 * ms, 105 * ms}, 100 * ms, true},
		{"empty", nil, 100 * ms, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tick, capped := computeTickInterval(c.intervals)
			if tick != c.wantTick {
				t.Errorf("tick = %s, want %s", tick, c.wantTick)
			}
			if capped != c.wantCap {
				t.Errorf("capped = %v, want %v", capped, c.wantCap)
			}
			// The tick must never exceed any interval (else tickMod rounds to 0/no-fire)
			// — except in the capped case, which is the documented approximation.
			if !capped {
				for _, iv := range c.intervals {
					if tick > iv {
						t.Errorf("uncapped tick %s exceeds interval %s", tick, iv)
					}
				}
			}
		})
	}
}
