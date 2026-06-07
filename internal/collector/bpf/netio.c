//go:build ignore

// netio.c — per-process (per-TGID) network byte accounting via TCP BPF tracing.
//
// Mirrors the approach of bcc's tcptop: trace the two TCP entry points that carry an
// application-visible byte count and accumulate per thread-group-id (== userspace PID)
// into an LRU hash. In-kernel aggregation means one atomic add per syscall, not per
// packet — the lowest-overhead way to attribute bytes to a process. The LRU map bounds
// memory and evicts cold PIDs automatically, so PID churn can't grow it without bound
// (matching the daemon's other bounded-memory invariants).
//
// Attachment uses fentry (BPF trampolines) rather than kprobes. fentry reads typed
// function arguments straight from the trace context with no architecture-specific
// pt_regs layout, so a single object file is correct on both amd64 and arm64 and the
// build needs no kernel asm headers or a generated vmlinux.h. The cost is a higher
// kernel floor: fentry requires BTF and Linux >= 5.5. Hosts without that simply fail to
// attach and the daemon degrades to zero rx/tx rates (see netio_linux.go).
//
// Coverage is TCP only (tcp_sendmsg / tcp_cleanup_rbuf). UDP/raw traffic is not counted;
// for a process-level "is this thing talking to the network" view that's the right trade.
//
// Counters are cumulative; the userspace collector deltas them per cycle. A reused TGID
// inherits the prior process's total for at most one sample (the collector's d>0 guard
// drops the resulting negative/garbage delta).

// Minimal scalar typedefs so the libbpf headers compile without <linux/bpf.h> (which
// drags in arch-specific <asm/types.h>). These match the kernel UAPI definitions.
typedef unsigned char __u8;
typedef signed char __s8;
typedef unsigned short __u16;
typedef short __s16;
typedef unsigned int __u32;
typedef int __s32;
typedef unsigned long long __u64;
typedef long long __s64;
typedef __u16 __be16;
typedef __u32 __be32;
typedef __u16 __sum16;
typedef __u32 __wsum;

// UAPI map-type / update-flag constants normally provided by <linux/bpf.h>. Values are
// stable kernel ABI.
#define BPF_MAP_TYPE_LRU_HASH 9
#define BPF_ANY 0

#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>

char __license[] SEC("license") = "Dual MIT/GPL";

struct netio_val {
	__u64 rx_bytes;
	__u64 tx_bytes;
};

struct {
	__uint(type, BPF_MAP_TYPE_LRU_HASH);
	__uint(max_entries, 16384);
	__type(key, __u32);
	__type(value, struct netio_val);
} netio_counters SEC(".maps");

static __always_inline void account(__u32 tgid, __s64 rx, __s64 tx)
{
	struct netio_val *v = bpf_map_lookup_elem(&netio_counters, &tgid);
	if (v) {
		if (rx > 0)
			__sync_fetch_and_add(&v->rx_bytes, (__u64)rx);
		if (tx > 0)
			__sync_fetch_and_add(&v->tx_bytes, (__u64)tx);
		return;
	}
	struct netio_val nv = {};
	if (rx > 0)
		nv.rx_bytes = (__u64)rx;
	if (tx > 0)
		nv.tx_bytes = (__u64)tx;
	bpf_map_update_elem(&netio_counters, &tgid, &nv, BPF_ANY);
}

// tcp_sendmsg(struct sock *sk, struct msghdr *msg, size_t size).
// `size` is the bytes the application handed to the stack — the tx attribution. Pointer
// arguments are taken as void* (never dereferenced) so no kernel struct layout is needed.
SEC("fentry/tcp_sendmsg")
int BPF_PROG(fentry_tcp_sendmsg, void *sk, void *msg, __u64 size)
{
	__u32 tgid = bpf_get_current_pid_tgid() >> 32;
	account(tgid, 0, (__s64)size);
	return 0;
}

// tcp_cleanup_rbuf(struct sock *sk, int copied).
// `copied` is the bytes drained from the receive buffer by the application — the rx
// attribution. Called with copied <= 0 on some paths; account() guards on > 0.
SEC("fentry/tcp_cleanup_rbuf")
int BPF_PROG(fentry_tcp_cleanup_rbuf, void *sk, int copied)
{
	__u32 tgid = bpf_get_current_pid_tgid() >> 32;
	account(tgid, (__s64)copied, 0);
	return 0;
}
