ALTER TABLE network_metrics ADD COLUMN IF NOT EXISTS rx_dropped BIGINT;
ALTER TABLE network_metrics ADD COLUMN IF NOT EXISTS tx_dropped BIGINT;
