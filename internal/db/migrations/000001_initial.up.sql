-- Unified lookup table for dimension strings (sensor names, interfaces, mounts, zones, devices)
-- Using SMALLINT id allows up to 32K unique values per category, which is plenty
CREATE TABLE IF NOT EXISTS dimension_values (
    id SMALLINT NOT NULL,
    category VARCHAR NOT NULL,  -- 'sensor', 'interface', 'mount', 'device', 'zone'
    value VARCHAR NOT NULL,
    PRIMARY KEY (category, id)
);
CREATE INDEX IF NOT EXISTS idx_dimension_lookup ON dimension_values(category, value);

CREATE TABLE IF NOT EXISTS cpu_metrics (
    ts TIMESTAMP NOT NULL,
    core TINYINT NOT NULL,
    user_pct DOUBLE,
    system_pct DOUBLE,
    idle_pct DOUBLE,
    iowait_pct DOUBLE
);

CREATE TABLE IF NOT EXISTS memory_metrics (
    ts TIMESTAMP NOT NULL,
    total_bytes BIGINT,
    used_bytes BIGINT,
    available_bytes BIGINT,
    buffers_bytes BIGINT,
    cached_bytes BIGINT,
    swap_total_bytes BIGINT,
    swap_used_bytes BIGINT
);

-- Normalized disk_metrics: mount and device are now IDs referencing dimension_values
CREATE TABLE IF NOT EXISTS disk_metrics (
    ts TIMESTAMP NOT NULL,
    mount_id SMALLINT NOT NULL,
    device_id SMALLINT,
    total_bytes BIGINT,
    used_bytes BIGINT,
    free_bytes BIGINT,
    read_bytes_sec DOUBLE,
    write_bytes_sec DOUBLE,
    read_iops DOUBLE,
    write_iops DOUBLE
);

-- Normalized network_metrics: interface is now an ID
CREATE TABLE IF NOT EXISTS network_metrics (
    ts TIMESTAMP NOT NULL,
    interface_id SMALLINT NOT NULL,
    rx_bytes_sec DOUBLE,
    tx_bytes_sec DOUBLE,
    rx_packets_sec DOUBLE,
    tx_packets_sec DOUBLE,
    rx_errors BIGINT,
    tx_errors BIGINT
);

CREATE TABLE IF NOT EXISTS ecc_metrics (
    ts TIMESTAMP NOT NULL,
    corrected BIGINT,
    uncorrected BIGINT
);

-- Normalized temperature_metrics: sensor is now an ID
CREATE TABLE IF NOT EXISTS temperature_metrics (
    ts TIMESTAMP NOT NULL,
    sensor_id SMALLINT NOT NULL,
    temp_celsius DOUBLE
);

-- Normalized power_metrics: zone is now an ID
CREATE TABLE IF NOT EXISTS power_metrics (
    ts TIMESTAMP NOT NULL,
    zone_id SMALLINT NOT NULL,
    watts DOUBLE
);

-- Lookup table for static process attributes (normalized to reduce storage)
CREATE TABLE IF NOT EXISTS process_info (
    pid INTEGER NOT NULL,
    start_time BIGINT NOT NULL,
    ppid INTEGER,
    name VARCHAR NOT NULL,
    cmdline VARCHAR,
    uid INTEGER,
    first_seen TIMESTAMP NOT NULL,
    PRIMARY KEY (pid, start_time)
);

-- Slimmed down process metrics (dynamic data only, references process_info)
CREATE TABLE IF NOT EXISTS process_metrics (
    ts TIMESTAMP NOT NULL,
    pid INTEGER NOT NULL,
    start_time BIGINT NOT NULL,
    state VARCHAR(1),
    cpu_user_pct DOUBLE,
    cpu_system_pct DOUBLE,
    rss_bytes BIGINT,
    num_fds INTEGER,
    num_threads INTEGER
);


CREATE TABLE IF NOT EXISTS preferences (
    key VARCHAR PRIMARY KEY,
    value VARCHAR NOT NULL
);

CREATE SEQUENCE IF NOT EXISTS alert_id_seq START 1;

CREATE TABLE IF NOT EXISTS alerts (
    id INTEGER DEFAULT nextval('alert_id_seq'),
    ts TIMESTAMP NOT NULL,
    rule_name VARCHAR NOT NULL,
    severity VARCHAR NOT NULL,
    message VARCHAR NOT NULL,
    acknowledged BOOLEAN DEFAULT false
);

CREATE SEQUENCE IF NOT EXISTS alert_rule_id_seq START 1;

-- Base alert rules table (common fields only)
CREATE TABLE IF NOT EXISTS alert_rules (
    id INTEGER DEFAULT nextval('alert_rule_id_seq'),
    name VARCHAR NOT NULL,
    type VARCHAR NOT NULL,
    severity VARCHAR NOT NULL,
    enabled BOOLEAN DEFAULT true,
    created_at TIMESTAMP DEFAULT current_timestamp
);

-- Type-specific tables for alert rule parameters
CREATE TABLE IF NOT EXISTS alert_rule_threshold (
    rule_id INTEGER PRIMARY KEY,
    metric VARCHAR NOT NULL,
    operator VARCHAR NOT NULL,
    value DOUBLE NOT NULL,
    duration VARCHAR NOT NULL,
    mount VARCHAR,
    interface_name VARCHAR,
    sensor VARCHAR
);

CREATE TABLE IF NOT EXISTS alert_rule_predictive (
    rule_id INTEGER PRIMARY KEY,
    metric VARCHAR NOT NULL,
    mount VARCHAR NOT NULL,
    predict_hours INTEGER NOT NULL,
    threshold_pct DOUBLE NOT NULL
);

CREATE TABLE IF NOT EXISTS alert_rule_variance (
    rule_id INTEGER PRIMARY KEY,
    metric VARCHAR NOT NULL,
    delta_threshold DOUBLE NOT NULL,
    min_count INTEGER NOT NULL,
    duration VARCHAR NOT NULL
);

CREATE TABLE IF NOT EXISTS alert_rule_process_down (
    rule_id INTEGER PRIMARY KEY,
    process_name VARCHAR NOT NULL,
    process_pattern VARCHAR,
    min_instances INTEGER NOT NULL DEFAULT 1,
    check_duration VARCHAR NOT NULL
);

CREATE TABLE IF NOT EXISTS alert_rule_process_thrashing (
    rule_id INTEGER PRIMARY KEY,
    process_name VARCHAR NOT NULL,
    process_pattern VARCHAR,
    restart_threshold INTEGER NOT NULL,
    restart_window VARCHAR NOT NULL
);

CREATE TABLE IF NOT EXISTS archive_state (
    table_name VARCHAR PRIMARY KEY,
    last_archived_ts TIMESTAMP NOT NULL
);

CREATE TABLE IF NOT EXISTS scheduled_jobs (
    job_name VARCHAR PRIMARY KEY,
    last_run_ts TIMESTAMP NOT NULL
);
