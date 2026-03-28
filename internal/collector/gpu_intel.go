package collector

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// intelGPUSample is the JSON structure emitted by intel_gpu_top -J.
type intelGPUSample struct {
	Period    *intelPeriod              `json:"period"`
	Frequency *intelFrequency          `json:"frequency"`
	RC6       *intelRC6                `json:"rc6"`
	Power     map[string]json.RawMessage `json:"power"`
	Engines   map[string]*intelEngine  `json:"engines"`
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

	mu       sync.Mutex
	cmd      *exec.Cmd
	latest   *intelGPUSample
	seenFirst bool // first sample is discarded (no deltas)
	running  bool
	stopCh   chan struct{}
	doneCh   chan struct{}
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
	}
}

func (b *intelGPUBackend) start() error {
	b.cmd = exec.Command(b.toolPath, "-J", "-s", "1000")
	b.cmd.Stderr = nil // discard stderr

	stdout, err := b.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}

	if err := b.cmd.Start(); err != nil {
		return fmt.Errorf("start intel_gpu_top: %w", err)
	}

	b.running = true
	b.seenFirst = false
	b.doneCh = make(chan struct{})

	go b.readLoop(stdout)
	return nil
}

// readLoop reads JSON objects from intel_gpu_top's stdout stream.
// intel_gpu_top outputs a stream of JSON objects (one per period).
func (b *intelGPUBackend) readLoop(r io.Reader) {
	defer close(b.doneCh)

	scanner := bufio.NewScanner(r)
	// intel_gpu_top outputs one JSON object per sample, each on its own set of lines.
	// We accumulate lines between braces to form complete JSON objects.
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

	// Start subprocess if not running
	if !b.running {
		if err := b.start(); err != nil {
			return nil, err
		}
		// No data yet on first call
		return nil, nil
	}

	// Check if process died
	if b.cmd != nil && b.cmd.ProcessState != nil && b.cmd.ProcessState.Exited() {
		b.running = false
		// Will restart on next call
		return nil, fmt.Errorf("intel_gpu_top exited")
	}

	if b.latest == nil {
		return nil, nil // no data yet
	}

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
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.running {
		return
	}

	close(b.stopCh)
	if b.cmd != nil && b.cmd.Process != nil {
		b.cmd.Process.Kill()
	}
	<-b.doneCh
	b.running = false
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
