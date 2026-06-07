-- Per-rule aggregate function for threshold alert rules: 'avg' (default, the
-- original behaviour), 'max' (catch a transient spike that averages out), or
-- 'min'. Only the AVG-backed metrics honour it; the SMART metrics keep their
-- intrinsic MAX/COUNT aggregate and ignore this column. Existing rows default to
-- 'avg' so behaviour is unchanged on upgrade; the UPDATE backfills any NULLs in
-- case the engine ran an ADD COLUMN that left them unset.
ALTER TABLE alert_rule_threshold ADD COLUMN aggregate VARCHAR DEFAULT 'avg';
UPDATE alert_rule_threshold SET aggregate = 'avg' WHERE aggregate IS NULL;
