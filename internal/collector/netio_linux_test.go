//go:build linux

package collector

import (
	"io"
	"net"
	"os"
	"testing"
	"time"
)

// TestNetIOReaderTCPAttribution loads the real eBPF reader, drives TCP traffic from this
// test process over loopback, and asserts the per-TGID counters reflect it. It requires
// BTF + CAP_BPF/CAP_PERFMON (or root) and a kernel new enough for fentry; it skips
// cleanly when eBPF is unavailable, so it is a harmless no-op in CI and on unprivileged
// or older hosts. Run it privileged on a real Linux box (e.g. ms01) to exercise the
// kprobe-free fentry path end to end.
func TestNetIOReaderTCPAttribution(t *testing.T) {
	r, err := NewNetIOReader()
	if err != nil {
		t.Skipf("eBPF unavailable (need BTF + CAP_BPF/CAP_PERFMON, kernel >= 5.5): %v", err)
	}
	if r == nil {
		t.Skip("no eBPF reader on this platform")
	}
	defer r.Close()

	const payload = 1 << 20 // 1 MiB each direction

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	srvDone := make(chan struct{})
	go func() {
		defer close(srvDone)
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		buf := make([]byte, payload)
		if _, err := io.ReadFull(c, buf); err != nil {
			return
		}
		_, _ = c.Write(buf) // echo it back
	}()

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	if _, err := conn.Write(make([]byte, payload)); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := io.ReadFull(conn, make([]byte, payload)); err != nil {
		t.Fatalf("read echo: %v", err)
	}
	<-srvDone

	// Accounting is synchronous on the syscall, but allow a beat for the final reads.
	time.Sleep(50 * time.Millisecond)

	snap := r.Snapshot()
	pid := int32(os.Getpid())
	c, ok := snap[pid]
	if !ok {
		// A non-empty snapshot that lacks our PID means the kernel attributed the bytes to
		// a different (host/init-namespace) PID than os.Getpid() returns — i.e. we run in a
		// PID namespace (a container without --pid=host). That is an environment limitation,
		// not a code fault: production bewitchd runs in the host PID namespace. Skip. An
		// empty snapshot after driving real traffic is a genuine failure.
		if len(snap) > 0 {
			t.Skipf("our pid %d absent from a %d-entry snapshot — PID namespace mismatch (needs host PID ns)", pid, len(snap))
		}
		t.Fatalf("empty snapshot after driving %d bytes — accounting not working", payload)
	}
	// Both the client and server sockets live in this TGID, so each direction is counted
	// roughly twice; >= payload is the safe lower bound.
	if c.TxBytes < payload {
		t.Errorf("TxBytes = %d, want >= %d", c.TxBytes, payload)
	}
	if c.RxBytes < payload {
		t.Errorf("RxBytes = %d, want >= %d", c.RxBytes, payload)
	}
	t.Logf("pid %d: rx=%d tx=%d (drove %d bytes each way)", pid, c.RxBytes, c.TxBytes, payload)
}
