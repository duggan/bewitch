-- Generic time-series storage for user-defined custom HTTP data sources.
-- source/metric are denormalized as VARCHAR (like smart_metrics.device) rather
-- than dimension_values FKs: cardinality is bounded by config (sources × declared
-- metrics), and a generic dimension category would bloat dimension_values without
-- benefit. Non-numeric [[status]] fields are live-only and never stored here.
CREATE TABLE IF NOT EXISTS custom_metrics (
    ts     TIMESTAMP NOT NULL,
    source VARCHAR   NOT NULL,
    metric VARCHAR   NOT NULL,
    value  DOUBLE
);
