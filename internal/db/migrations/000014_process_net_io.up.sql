-- Per-process network I/O rates (bytes/sec from the eBPF TCP send/recv hooks),
-- recorded for the Phase-2 enriched process set. Nullable: rows written before this
-- migration, and any deployment without the eBPF backend loaded (old kernel, no
-- CAP_BPF, BTF-less host, container), are NULL.
ALTER TABLE process_metrics ADD COLUMN net_rx_bytes_sec DOUBLE;
ALTER TABLE process_metrics ADD COLUMN net_tx_bytes_sec DOUBLE;
