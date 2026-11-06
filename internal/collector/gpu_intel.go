package collector

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/log"
)

// intelGPUSample is the JSON structure emitted by intel_gpu_top -J.
type intelGPUSample struct {
	Period    *intelPeriod               `json:"period"`
	Frequency *intelFrequency            `json:"frequency"`
	RC6       *intelRC6                  `json:"rc6"`
	Power     map[string]json.RawMessage `json:"power"`
	Engines   map[string]*intelEngine    `json:"engines"`
}

type intelPeriod struct {
	Duration float64 `json:"duration"`
	Unit     string  `json:"unit"`
}

type intelFrequency struct {
	Requested float64 `json:"requested"`
	Actual    float64 `json:"actual"`
	Unit      string  `json:"unit"`
}

type intelRC6 struct {
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
}

type intelEngine struct {
	Busy float64 `json:"busy"`
	Unit string  `json:"unit"`
}

type intelGPUBackend struct {
	toolPath string
	gpuName  string // detected GPU name from DRM sysfs

	mu        sync.Mutex
	cmd       *exec.Cmd
	latest    *intelGPUSample
	seenFirst bool // first sample is discarded (no deltas)
	running   bool
	stopping  bool
	stopCh    chan struct{}
	doneCh    chan struct{}

	// Set by readLoop once the process has ended and been reaped.
	exited  bool
	exitMsg string
	// Restart backoff after the tool exits: a host where it can't run (no
	// permission for the i915 PMU) would otherwise relaunch it every tick.
	failures int
	retryAt  time.Time
	lastWarn string
	now      func() time.Time
}

// intelRestartBackoff is the wait before relaunching intel_gpu_top after its
// n-th consecutive exit: 1m, doubling, capped at 1h.
func intelRestartBackoff(n int) time.Duration {
	if n > 7 {
		return time.Hour
	}
	return min(time.Minute<<(n-1), time.Hour)
}

func newIntelGPUBackend() *intelGPUBackend {
	toolPath, err := exec.LookPath("intel_gpu_top")
	if err != nil {
		return nil
	}

	// Verify an Intel GPU is present via DRM
	name := detectIntelGPUName()
	if name == "" {
		return nil
	}

	return &intelGPUBackend{
		toolPath: toolPath,
		gpuName:  name,
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
		now:      time.Now,
	}
}

func (b *intelGPUBackend) start() error {
	cmd := exec.Command(b.toolPath, "-J", "-s", "1000")
	// Keep the end of stderr: when the tool fails it says why there
	// ("Failed to initialize PMU! (Permission denied)").
	stderr := &tailBuffer{max: 2048}
	cmd.Stderr = stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start intel_gpu_top: %w", err)
	}

	b.cmd = cmd
	b.running = true
	b.exited = false
	b.seenFirst = false
	b.latest = nil
	b.doneCh = make(chan struct{})

	go b.readLoop(cmd, stdout, stderr, b.doneCh)
	return nil
}

// tailBuffer keeps the last max bytes written to it.
type tailBuffer struct {
	max int
	buf []byte
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.buf = append(t.buf, p...)
	if len(t.buf) > t.max {
		t.buf = t.buf[len(t.buf)-t.max:]
	}
	return len(p), nil
}

// describeIntelExit turns the process's exit into one log line, with a hint
// when it couldn't open the i915 PMU.
func describeIntelExit(waitErr error, stderr []byte) string {
	msg := ""
	for _, line := range bytes.Split(stderr, []byte("\n")) {
		if l := strings.TrimSpace(string(line)); l != "" {
			msg = l // first non-empty line names the failure
			break
		}
	}
	if msg == "" {
		msg = "exited"
		if waitErr != nil {
			msg = waitErr.Error()
		}
	}
	if bytes.Contains(stderr, []byte("Permission denied")) {
		msg += " (reading the i915 PMU needs CAP_PERFMON, or CAP_SYS_ADMIN where kernel.perf_event_paranoid > 2; Debian's default is 3)"
	}
	return msg
}

// readLoop reads JSON objects from intel_gpu_top's stdout stream.
// intel_gpu_top outputs a stream of JSON objects (one per period).
func (b *intelGPUBackend) readLoop(cmd *exec.Cmd, r io.Reader, stderr *tailBuffer, done chan struct{}) {
	defer close(done)
	// Reap the process whichever way the loop ends. Without Wait the process
	// stayed a zombie and ProcessState was never set, so a dead intel_gpu_top
	// looked alive forever and GPU data silently stopped.
	defer func() {
		waitErr := cmd.Wait()
		b.mu.Lock()
		if !b.stopping {
			b.exited = true
			b.exitMsg = describeIntelExit(waitErr, stderr.buf)
		}
		b.mu.Unlock()
	}()

	scanner := bufio.NewScanner(r)
	// intel_gpu_top outputs one JSON object per sample, each on its own set of lines.
	// We accumulate lines between braces to form complete JSON objects.
	const maxBufBytes = 1 << 20 // 1MB cap to bound memory if braces never rebalance
	var depth int
	var buf strings.Builder

	for scanner.Scan() {
		select {
		case <-b.stopCh:
			return
		default:
		}

		line := scanner.Text()
		for _, ch := range line {
			if ch == '{' {
				depth++
			} else if ch == '}' {
				depth--
			}
		}
		buf.WriteString(line)
		buf.WriteByte('\n')

		// Guard against unbounded growth from a malformed/unbalanced stream:
		// if we've buffered more than the cap without closing the object,
		// discard and resync rather than accumulating forever.
		if buf.Len() > maxBufBytes {
			buf.Reset()
			depth = 0
			continue
		}

		if depth == 0 && buf.Len() > 2 {
			raw := buf.String()
			buf.Reset()

			// Strip leading/trailing brackets or commas that intel_gpu_top may add
			raw = strings.TrimSpace(raw)
			raw = strings.TrimLeft(raw, "[,")
			raw = strings.TrimRight(raw, "],")
			raw = strings.TrimSpace(raw)
			if raw == "" {
				continue
			}

			var sample intelGPUSample
			if err := json.Unmarshal([]byte(raw), &sample); err != nil {
				continue
			}

			b.mu.Lock()
			if !b.seenFirst {
				// Discard first sample (needs prior period for deltas)
				b.seenFirst = true
			} else {
				b.latest = &sample
			}
			b.mu.Unlock()
		}
	}
}

func (b *intelGPUBackend) read() ([]GPUDeviceSample, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.exited {
		b.exited = false
		b.running = false
		b.latest = nil
		b.failures++
		delay := intelRestartBackoff(b.failures)
		b.retryAt = b.now().Add(delay)
		if b.exitMsg != b.lastWarn {
			log.Warnf("gpu: intel_gpu_top exited: %s; retrying in %v", b.exitMsg, delay)
			b.lastWarn = b.exitMsg
		}
		return nil, fmt.Errorf("intel_gpu_top exited: %s", b.exitMsg)
	}

	// Start subprocess if not running
	if !b.running {
		if b.now().Before(b.retryAt) {
			return nil, nil
		}
		if err := b.start(); err != nil {
			return nil, err
		}
		// No data yet on first call
		return nil, nil
	}

	if b.latest == nil {
		return nil, nil // no data yet
	}
	b.failures = 0
	b.lastWarn = ""

	s := b.latest

	// Compute utilization as max engine busy %
	var maxBusy float64
	for _, eng := range s.Engines {
		if eng != nil && eng.Busy > maxBusy {
			maxBusy = eng.Busy
		}
	}

	var freqActual, freqMax float64
	if s.Frequency != nil {
		freqActual = s.Frequency.Actual
		freqMax = s.Frequency.Requested
		// Use requested as "max" since intel_gpu_top reports requested vs actual
	}

	var rc6 float64
	if s.RC6 != nil {
		rc6 = s.RC6.Value
	}

	var gpuPower float64
	if s.Power != nil {
		// Try to extract GPU power (it's a float, not a struct)
		if raw, ok := s.Power["GPU"]; ok {
			var val float64
			if json.Unmarshal(raw, &val) == nil {
				gpuPower = val
			}
		}
	}

	dev := GPUDeviceSample{
		Name:            b.gpuName,
		Index:           0,
		Vendor:          "intel",
		UtilizationPct:  maxBusy,
		PowerWatts:      gpuPower,
		FrequencyMHz:    uint32(freqActual),
		FrequencyMaxMHz: uint32(freqMax),
		ThrottlePct:     rc6,
	}

	return []GPUDeviceSample{dev}, nil
}

func (b *intelGPUBackend) stop() {
	// Don't hold mu while waiting for readLoop: it takes mu to publish samples
	// and to record the exit, so waiting under the lock could deadlock.
	b.mu.Lock()
	if !b.running {
		b.mu.Unlock()
		return
	}
	b.stopping = true
	b.running = false
	cmd, done := b.cmd, b.doneCh
	close(b.stopCh)
	b.mu.Unlock()

	if cmd != nil && cmd.Process != nil {
		cmd.Process.Kill()
	}
	<-done
}

// detectIntelGPUName checks DRM sysfs for an Intel GPU and returns its name.
func detectIntelGPUName() string {
	cards, _ := filepath.Glob("/sys/class/drm/card[0-9]*")
	for _, card := range cards {
		// Skip render nodes (card0-render, etc.)
		base := filepath.Base(card)
		if strings.Contains(base, "-") {
			continue
		}

		driverLink, err := os.Readlink(filepath.Join(card, "device", "driver"))
		if err != nil {
			continue
		}
		driverName := filepath.Base(driverLink)
		if driverName != "i915" && driverName != "xe" {
			continue
		}

		// Try to get a human-readable name
		if label := readString(filepath.Join(card, "device", "label")); label != "" {
			return label
		}

		// Fall back to vendor/device from sysfs
		vendor := readString(filepath.Join(card, "device", "vendor"))
		device := readString(filepath.Join(card, "device", "device"))
		if vendor == "0x8086" && device != "" {
			return fmt.Sprintf("Intel GPU %s", device)
		}

		return "Intel GPU"
	}
	return ""
}
