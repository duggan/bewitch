//go:build linux

package collector

import (
	"errors"
	"fmt"

	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/rlimit"
)

// fentry programs carry no arch-specific pt_regs layout, so one little-endian object is
// correct on every LE target (amd64, arm64). Regenerate with `go generate
// ./internal/collector/` on a Linux host with clang + libbpf-dev.
//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -target bpfel -cc clang -no-strip netio ./bpf/netio.c

// ebpfNetIOReader is the Linux per-process network byte source. It loads the embedded
// BPF objects (netio.c, compiled by bpf2go into netio_bpfel.{go,o}) and attaches two
// fentry programs that accumulate per-TGID rx/tx into an LRU hash map read by Snapshot().
//
// All construction errors are returned to the caller, which degrades gracefully: the
// daemon logs a warning and leaves the process collector's netIO nil, so rx/tx rates
// stay 0 rather than the daemon failing to start. fentry needs BTF + Linux >= 5.5 and
// CAP_BPF + CAP_PERFMON (or root); without them load/attach fails and we degrade.
type ebpfNetIOReader struct {
	objs  netioObjects
	links []link.Link
}

// NewNetIOReader loads the BPF program and attaches the TCP fentry hooks. A non-nil
// error means per-process network I/O is unavailable on this host (missing capability,
// no BTF, kernel too old, or the traced symbols absent/inlined); callers treat that as
// "no reader" and leave per-process rx/tx at zero.
func NewNetIOReader() (NetIOReader, error) {
	if err := rlimit.RemoveMemlock(); err != nil {
		return nil, fmt.Errorf("remove memlock rlimit: %w", err)
	}

	var objs netioObjects
	if err := loadNetioObjects(&objs, nil); err != nil {
		return nil, fmt.Errorf("load bpf objects: %w", err)
	}

	r := &ebpfNetIOReader{objs: objs}

	send, err := link.AttachTracing(link.TracingOptions{Program: objs.FentryTcpSendmsg})
	if err != nil {
		_ = r.Close()
		return nil, fmt.Errorf("attach fentry tcp_sendmsg: %w", err)
	}
	r.links = append(r.links, send)

	recv, err := link.AttachTracing(link.TracingOptions{Program: objs.FentryTcpCleanupRbuf})
	if err != nil {
		_ = r.Close()
		return nil, fmt.Errorf("attach fentry tcp_cleanup_rbuf: %w", err)
	}
	r.links = append(r.links, recv)

	return r, nil
}

// Snapshot reads the whole per-TGID counter map into a Go map of cumulative byte totals.
// The process collector deltas these against the previous cycle to derive per-second
// rates. Iteration over an LRU hash is safe under concurrent kernel updates; a counter
// may be a few syscalls stale, which is immaterial at the collector's cadence.
func (r *ebpfNetIOReader) Snapshot() map[int32]NetIOCounters {
	out := make(map[int32]NetIOCounters)
	var (
		key uint32
		val netioNetioVal
	)
	it := r.objs.NetioCounters.Iterate()
	for it.Next(&key, &val) {
		out[int32(key)] = NetIOCounters{RxBytes: val.RxBytes, TxBytes: val.TxBytes}
	}
	// A partial map (iteration error) is still useful; the next cycle re-reads fresh.
	return out
}

// Close detaches the fentry links and unloads the BPF objects. Safe to call on a
// partially-constructed reader (NewNetIOReader calls it on attach failure).
func (r *ebpfNetIOReader) Close() error {
	var errs []error
	for _, l := range r.links {
		if l != nil {
			errs = append(errs, l.Close())
		}
	}
	errs = append(errs, r.objs.Close())
	return errors.Join(errs...)
}
