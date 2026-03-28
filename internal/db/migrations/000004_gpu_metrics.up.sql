CREATE TABLE IF NOT EXISTS gpu_metrics (
    ts TIMESTAMP NOT NULL,
    gpu_id SMALLINT NOT NULL,
    utilization_pct DOUBLE,
    memory_used_bytes BIGINT,
    memory_total_bytes BIGINT,
    temp_celsius DOUBLE,
    power_watts DOUBLE,
    frequency_mhz INTEGER,
    frequency_max_mhz INTEGER,
    throttle_pct DOUBLE
);
