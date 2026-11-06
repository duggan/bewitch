package collector

import (
	"os"
	"strings"
	"testing"
	"time"
)

func newFakeIntelBackend(t *testing.T, mode string) (*intelGPUBackend, *time.Time) {
	t.Helper()
	t.Setenv(fakeIntelGPUTopEnv, mode)
	clock := time.Now()
	b := &intelGPUBackend{
		toolPath: os.Args[0],
		gpuName:  "Fake Intel GPU",
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
		now:      func() time.Time { return clock },
	}
	t.Cleanup(b.stop)
	return b, &clock
}

// TestIntelGPUTopExitIsReapedAndBackedOff: when intel_gpu_top can't open the
// i915 PMU it exits at once. The backend used to check ProcessState without
// ever calling Wait, so the process stayed a zombie, the exit was never seen,
// and GPU data silently stopped (seen on Debian, whose perf_event_paranoid=3
// needs CAP_SYS_ADMIN).
func TestIntelGPUTopExitIsReapedAndBackedOff(t *testing.T) {
	b, clock := newFakeIntelBackend(t, "denied")

	if _, err := b.read(); err != nil { // starts the process
		t.Fatal(err)
	}
	first := b.cmd

	var err error
	deadline := time.Now().Add(5 * time.Second)
	for err == nil && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
		_, err = b.read()
	}
	if err == nil {
		t.Fatal("exit of intel_gpu_top never reported")
	}
	if !strings.Contains(err.Error(), "Permission denied") || !strings.Contains(err.Error(), "perf_event_paranoid") {
		t.Errorf("error %q should carry the tool's message and the permission hint", err)
	}
	if first.ProcessState == nil {
		t.Error("process was not reaped (Wait never called)")
	}

	// Within the backoff it must not relaunch.
	if _, err := b.read(); err != nil || b.cmd != first || b.running {
		t.Errorf("relaunched inside the backoff (err=%v, running=%v)", err, b.running)
	}
	*clock = clock.Add(intelRestartBackoff(1) + time.Second)
	if _, err := b.read(); err != nil {
		t.Fatal(err)
	}
	if b.cmd == first || !b.running {
		t.Error("not relaunched after the backoff")
	}
}

// TestIntelGPUTopStreamAndStop: samples flow (the first is discarded), and stop
// returns promptly and reaps the process.
func TestIntelGPUTopStreamAndStop(t *testing.T) {
	b, _ := newFakeIntelBackend(t, "stream")

	var devs []GPUDeviceSample
	deadline := time.Now().Add(5 * time.Second)
	for len(devs) == 0 && time.Now().Before(deadline) {
		var err error
		if devs, err = b.read(); err != nil {
			t.Fatal(err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(devs) != 1 || devs[0].UtilizationPct != 42.5 || devs[0].FrequencyMHz != 1100 {
		t.Fatalf("devs = %+v, want one device at 42.5%% / 1100 MHz", devs)
	}

	cmd := b.cmd
	stopped := make(chan struct{})
	go func() { b.stop(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("stop() hung")
	}
	if cmd.ProcessState == nil {
		t.Error("process was not reaped on stop")
	}
}

func TestIntelRestartBackoff(t *testing.T) {
	for n, want := range map[int]time.Duration{1: time.Minute, 2: 2 * time.Minute, 6: 32 * time.Minute, 7: time.Hour, 50: time.Hour} {
		if got := intelRestartBackoff(n); got != want {
			t.Errorf("intelRestartBackoff(%d) = %v, want %v", n, got, want)
		}
	}
}
