//go:build linux

package collector

import (
	"encoding/binary"
	"fmt"
	"unsafe"

	"golang.org/x/sys/unix"
)

// perfCounter is an open system-wide perf event counting on one CPU.
type perfCounter struct {
	fd    int
	scale float64
}

// openPerfCounter opens ev system-wide (pid -1) on its package's CPU. This needs
// CAP_PERFMON (or perf_event_paranoid <= 0).
func openPerfCounter(ev perfRAPLEvent) (*perfCounter, error) {
	attr := unix.PerfEventAttr{
		Type:   ev.pmu,
		Config: ev.config,
	}
	attr.Size = uint32(unsafe.Sizeof(attr))
	fd, err := unix.PerfEventOpen(&attr, -1, ev.cpu, -1, unix.PERF_FLAG_FD_CLOEXEC)
	if err != nil {
		return nil, fmt.Errorf("perf_event_open %s on cpu%d: %w", ev.zone, ev.cpu, err)
	}
	return &perfCounter{fd: fd, scale: ev.scale}, nil
}

// joules returns the counter's cumulative energy in joules.
func (p *perfCounter) joules() (float64, error) {
	var buf [8]byte
	n, err := unix.Read(p.fd, buf[:])
	if err != nil {
		return 0, err
	}
	if n != len(buf) {
		return 0, fmt.Errorf("short perf read (%d bytes)", n)
	}
	return float64(binary.NativeEndian.Uint64(buf[:])) * p.scale, nil
}

func (p *perfCounter) close() { unix.Close(p.fd) }
