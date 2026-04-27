package store

import (
	"database/sql"
	"fmt"

	"github.com/duggan/bewitch/internal/db"
)

// Stats holds operational statistics about the live DuckDB.
type Stats struct {
	Tables             []TableMetricStats
	Dimensions         map[string]int64
	Processes          int64
	AlertRulesEnabled  int
	AlertRulesDisabled int
	AlertsFiredTotal   int
	AlertsFiredUnacked int
}

// TableMetricStats holds row count and time-range info for a single metric table.
type TableMetricStats struct {
	Name     string
	Rows     int64
	OldestTs int64 // unix nanos; 0 if table empty
	NewestTs int64 // unix nanos; 0 if table empty
}

// Stats returns counts and time ranges for each metric table, dimension counts,
// process count, and alert rule/fired counts. All queries hit the live DuckDB
// only — archived data is not included (the handler folds that in via the
// archive directory listing).
func (s *Store) Stats() (*Stats, error) {
	out := &Stats{
		Dimensions: make(map[string]int64),
	}

	for _, table := range db.MetricTables {
		ts, err := s.tableStats(table)
		if err != nil {
			return nil, fmt.Errorf("stats for %s: %w", table, err)
		}
		out.Tables = append(out.Tables, ts)
	}

	rows, err := s.db.Query(`SELECT category, COUNT(*) FROM dimension_values GROUP BY category`)
	if err != nil {
		return nil, fmt.Errorf("dimension counts: %w", err)
	}
	for rows.Next() {
		var cat string
		var n int64
		if err := rows.Scan(&cat, &n); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan dimension count: %w", err)
		}
		out.Dimensions[cat] = n
	}
	rows.Close()

	if err := s.db.QueryRow(`SELECT COUNT(*) FROM process_info`).Scan(&out.Processes); err != nil {
		return nil, fmt.Errorf("process count: %w", err)
	}

	if err := s.db.QueryRow(
		`SELECT
			COUNT(*) FILTER (WHERE enabled),
			COUNT(*) FILTER (WHERE NOT enabled)
		 FROM alert_rules`,
	).Scan(&out.AlertRulesEnabled, &out.AlertRulesDisabled); err != nil {
		return nil, fmt.Errorf("alert rule counts: %w", err)
	}

	if err := s.db.QueryRow(
		`SELECT COUNT(*), COUNT(*) FILTER (WHERE NOT acknowledged) FROM alerts`,
	).Scan(&out.AlertsFiredTotal, &out.AlertsFiredUnacked); err != nil {
		return nil, fmt.Errorf("alert counts: %w", err)
	}

	return out, nil
}

func (s *Store) tableStats(table string) (TableMetricStats, error) {
	var (
		rowCount int64
		minTs    sql.NullTime
		maxTs    sql.NullTime
	)
	q := fmt.Sprintf(`SELECT COUNT(*), MIN(ts), MAX(ts) FROM %s`, table)
	if err := s.db.QueryRow(q).Scan(&rowCount, &minTs, &maxTs); err != nil {
		return TableMetricStats{}, err
	}
	out := TableMetricStats{Name: table, Rows: rowCount}
	if minTs.Valid {
		out.OldestTs = minTs.Time.UnixNano()
	}
	if maxTs.Valid {
		out.NewestTs = maxTs.Time.UnixNano()
	}
	return out, nil
}
