CREATE TABLE IF NOT EXISTS smart_metrics (
    ts TIMESTAMP NOT NULL,
    device VARCHAR NOT NULL,
    healthy BOOLEAN,
    temperature BIGINT,
    power_on_hours BIGINT,
    power_cycles BIGINT,
    reallocated_sectors BIGINT,
    pending_sectors BIGINT,
    uncorrectable_errs BIGINT,
    read_error_rate BIGINT,
    available_spare SMALLINT,
    percent_used SMALLINT
);
