-- Per-process disk I/O rates (bytes/sec from /proc/[pid]/io read_bytes/write_bytes),
-- recorded for the Phase-2 enriched process set. Nullable: rows written before this
-- migration, and processes the daemon can't read I/O for (no CAP_SYS_PTRACE), are NULL.
ALTER TABLE process_metrics ADD COLUMN read_bytes_sec DOUBLE;
ALTER TABLE process_metrics ADD COLUMN write_bytes_sec DOUBLE;
