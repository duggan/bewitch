//go:build !linux

package collector

// NewNetIOReader returns no reader on non-Linux platforms: per-process network I/O
// is eBPF-based and Linux-only. The process collector then leaves rx/tx rates at 0
// (the same graceful no-op as when eBPF fails to load on Linux). See netio_linux.go.
func NewNetIOReader() (NetIOReader, error) {
	return nil, nil
}
