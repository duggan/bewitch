package collector

import (
	"math"
	"testing"

	"github.com/prometheus/procfs"
)

func TestComputeCPUPct(t *testing.T) {
	tests := []struct {
		name      string
		prev, cur procfs.CPUStat
		coreIdx   int
		wantUser  float64
		wantSys   float64
		wantIdle  float64
		wantIO    float64
	}{
		{
			"100% idle",
			procfs.CPUStat{User: 0, Nice: 0, System: 0, Idle: 0, Iowait: 0, IRQ: 0, SoftIRQ: 0, Steal: 0},
			procfs.CPUStat{User: 0, Nice: 0, System: 0, Idle: 100, Iowait: 0, IRQ: 0, SoftIRQ: 0, Steal: 0},
			0,
			0, 0, 100, 0,
		},
		{
			"50% user 50% idle",
			procfs.CPUStat{User: 0, Idle: 0},
			procfs.CPUStat{User: 50, Idle: 50},
			0,
			50, 0, 50, 0,
		},
		{
			"zero delta (no time elapsed)",
			procfs.CPUStat{User: 100, Idle: 100},
			procfs.CPUStat{User: 100, Idle: 100},
			0,
			0, 0, 0, 0,
		},
		{
			"mixed with IOWait",
			procfs.CPUStat{User: 0, System: 0, Idle: 0, Iowait: 0},
			procfs.CPUStat{User: 30, System: 20, Idle: 40, Iowait: 10},
			1,
			30, 20, 40, 10,
		},
		{
			"aggregate core index -1",
			procfs.CPUStat{},
			procfs.CPUStat{User: 25, System: 25, Idle: 50},
			-1,
			25, 25, 50, 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeCPUPct(tt.prev, tt.cur, tt.coreIdx)
			if got.Core != tt.coreIdx {
				t.Errorf("Core = %d, want %d", got.Core, tt.coreIdx)
			}
			if math.Abs(got.UserPct-tt.wantUser) > 0.01 {
				t.Errorf("UserPct = %.2f, want %.2f", got.UserPct, tt.wantUser)
			}
			if math.Abs(got.SystemPct-tt.wantSys) > 0.01 {
				t.Errorf("SystemPct = %.2f, want %.2f", got.SystemPct, tt.wantSys)
			}
			if math.Abs(got.IdlePct-tt.wantIdle) > 0.01 {
				t.Errorf("IdlePct = %.2f, want %.2f", got.IdlePct, tt.wantIdle)
			}
			if math.Abs(got.IOWaitPct-tt.wantIO) > 0.01 {
				t.Errorf("IOWaitPct = %.2f, want %.2f", got.IOWaitPct, tt.wantIO)
			}
		})
	}
}
