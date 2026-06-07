-- Persist per-core hypervisor steal time so it can be alerted on directly (the
-- cpu.steal metric), not just folded into 100 - idle. Nullable: rows written
-- before this migration have no steal value, and AVG/MAX/MIN ignore NULLs, so an
-- alert window made entirely of pre-migration rows simply doesn't fire (no data)
-- rather than reading 0.
ALTER TABLE cpu_metrics ADD COLUMN steal_pct DOUBLE;
