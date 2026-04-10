package repl

import (
	"strings"
	"testing"
	"time"

	"github.com/duggan/bewitch/internal/api"
	"github.com/duggan/bewitch/internal/format"
)

func TestFormatValue(t *testing.T) {
	tests := []struct {
		name string
		val  any
		want string
	}{
		{"nil", nil, "NULL"},
		{"string", "hello", "hello"},
		{"int", 42, "42"},
		{"int64", int64(1234567890), "1234567890"},
		{"float whole", float64(100), "100"},
		{"float fractional", float64(12.345), "12.35"},
		{"float small", float64(0.1), "0.10"},
		{"float32", float32(3.14), "3.14"},
		{"bytes", []byte("binary"), "binary"},
		{"bool true", true, "true"},
		{"bool false", false, "false"},
		{"time", time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC), "2025-01-15 10:30:00"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatValue(tt.val)
			if got != tt.want {
				t.Errorf("formatValue(%v) = %q, want %q", tt.val, got, tt.want)
			}
		})
	}
}

func TestComputeWidths(t *testing.T) {
	tests := []struct {
		name      string
		headers   []string
		data      [][]string
		termWidth int
		wantMin   []int
	}{
		{
			name:      "header wider than data",
			headers:   []string{"long_column_name", "x"},
			data:      [][]string{{"a", "b"}},
			termWidth: 120,
			wantMin:   []int{16, 1},
		},
		{
			name:      "data wider than header",
			headers:   []string{"id", "name"},
			data:      [][]string{{"12345", "a very long name value"}},
			termWidth: 120,
			wantMin:   []int{5, 22},
		},
		{
			name:      "single column",
			headers:   []string{"count"},
			data:      [][]string{{"42"}},
			termWidth: 120,
			wantMin:   []int{5},
		},
		{
			name:      "empty data uses header widths",
			headers:   []string{"foo", "bar"},
			data:      nil,
			termWidth: 120,
			wantMin:   []int{3, 3},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeWidths(tt.headers, tt.data, tt.termWidth)
			if len(got) != len(tt.wantMin) {
				t.Fatalf("got %d widths, want %d", len(got), len(tt.wantMin))
			}
			for i, min := range tt.wantMin {
				if got[i] < min {
					t.Errorf("width[%d] = %d, want >= %d", i, got[i], min)
				}
			}
		})
	}
}

func TestComputeWidthsTruncation(t *testing.T) {
	headers := []string{"a", "b", "c"}
	data := [][]string{
		{
			"this is a very long column value that should get truncated",
			"another very long column value",
			"yet another one",
		},
	}

	widths := computeWidths(headers, data, 60)

	// Total rendered width: leading space + columns + separators
	overhead := 2 + 3*(len(widths)-1) // " " prefix + " | " between cols
	totalData := 0
	for _, w := range widths {
		totalData += w
	}
	total := overhead + totalData
	if total > 60 {
		t.Errorf("total width %d exceeds terminal width 60", total)
	}
}

func TestPadRight(t *testing.T) {
	tests := []struct {
		s     string
		width int
		want  string
	}{
		{"abc", 5, "abc  "},
		{"abc", 3, "abc"},
		{"abcdef", 3, "abc"},
		{"", 4, "    "},
	}

	for _, tt := range tests {
		got := padRight(tt.s, tt.width)
		if got != tt.want {
			t.Errorf("padRight(%q, %d) = %q, want %q", tt.s, tt.width, got, tt.want)
		}
	}
}

func TestRenderHeader(t *testing.T) {
	var buf strings.Builder
	renderHeader(&buf, []string{"id", "name", "value"}, []int{2, 10, 8})
	got := buf.String()
	if got != " id | name       | value   \n" {
		t.Errorf("unexpected header:\n%q", got)
	}
}

func TestRenderSeparator(t *testing.T) {
	var buf strings.Builder
	renderSeparator(&buf, []int{2, 10, 8})
	got := buf.String()
	if got != "----+------------+---------\n" {
		t.Errorf("unexpected separator:\n%q", got)
	}
}

func TestRenderRow(t *testing.T) {
	var buf strings.Builder
	renderRow(&buf, []string{"42", "hello", "world"}, []int{4, 10, 8})
	got := buf.String()
	if got != " 42   | hello      | world   \n" {
		t.Errorf("unexpected row:\n%q", got)
	}
}

func TestRenderRowTruncation(t *testing.T) {
	var buf strings.Builder
	renderRow(&buf, []string{"this is way too long"}, []int{10})
	got := buf.String()
	// Should truncate with ~ indicator
	if got != " this is w~\n" {
		t.Errorf("unexpected truncated row:\n%q", got)
	}
}

func TestFormatExplain(t *testing.T) {
	// EXPLAIN results have (explain_key, explain_value) columns —
	// formatExplain should print just the value, no labels.
	got := formatExplain([][]string{
		{"physical_plan", "┌───────┐\n│ TOP_N │\n└───────┘"},
	})
	want := "\n┌───────┐\n│ TOP_N │\n└───────┘\n"
	if got != want {
		t.Errorf("formatExplain:\ngot:  %q\nwant: %q", got, want)
	}
}

func TestFormatExplainViaQueryResponse(t *testing.T) {
	qr := &api.QueryResponse{
		Columns: []string{"explain_key", "explain_value"},
		Rows:    [][]any{{"physical_plan", "┌───────┐\n│ TOP_N │\n└───────┘"}},
	}
	got := formatQueryResponse(qr)
	// Should print the plan tree directly, not "explain_key: physical_plan"
	if strings.Contains(got, "explain_key") {
		t.Errorf("EXPLAIN output should not contain column labels, got:\n%s", got)
	}
	if !strings.Contains(got, "TOP_N") {
		t.Errorf("EXPLAIN output should contain the plan tree, got:\n%s", got)
	}
}

func TestFormatExpandedSingleRecord(t *testing.T) {
	got := formatExpanded(
		[]string{"col_a", "col_b"},
		[][]string{{"value1", "line1\nline2"}},
	)
	want := "\ncol_a: value1\ncol_b: line1\nline2\n"
	if got != want {
		t.Errorf("formatExpanded single record:\ngot:  %q\nwant: %q", got, want)
	}
}

func TestFormatExpandedMultiRecord(t *testing.T) {
	got := formatExpanded(
		[]string{"key", "value"},
		[][]string{{"a", "line1\nline2"}, {"b", "line3\nline4"}},
	)
	want := "\n-[ RECORD 1 ]\nkey: a\nvalue: line1\nline2\n-[ RECORD 2 ]\nkey: b\nvalue: line3\nline4\n"
	if got != want {
		t.Errorf("formatExpanded multi record:\ngot:  %q\nwant: %q", got, want)
	}
}

func TestFormatQueryResponseMultiline(t *testing.T) {
	qr := &api.QueryResponse{
		Columns: []string{"plan"},
		Rows:    [][]any{{"┌─────┐\n│ SEQ │\n└─────┘"}},
	}
	got := formatQueryResponse(qr)
	// Should use expanded format, not table format
	if !strings.Contains(got, "plan:") {
		t.Errorf("expected expanded format for multiline result, got:\n%s", got)
	}
	if strings.Contains(got, "---") {
		t.Error("multiline result should not have table separator")
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		b    int64
		want string
	}{
		{0, "0 B"},
		{500, "500 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1048576, "1.0 MB"},
		{1073741824, "1.0 GB"},
		{1610612736, "1.5 GB"},
	}

	for _, tt := range tests {
		got := format.BytesLong(tt.b)
		if got != tt.want {
			t.Errorf("format.BytesLong(%d) = %q, want %q", tt.b, got, tt.want)
		}
	}
}

func TestParseExportArgs(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantSQL string
		wantPath string
		wantErr bool
	}{
		{
			name:     "table and path",
			input:    "cpu_metrics /tmp/cpu.csv",
			wantSQL:  "SELECT * FROM cpu_metrics",
			wantPath: "/tmp/cpu.csv",
		},
		{
			name:     "sql query and path",
			input:    "(SELECT * FROM cpu_metrics LIMIT 10) /tmp/cpu.csv",
			wantSQL:  "SELECT * FROM cpu_metrics LIMIT 10",
			wantPath: "/tmp/cpu.csv",
		},
		{
			name:     "nested parens in query",
			input:    "(SELECT COUNT(*) FROM cpu_metrics WHERE ts > now() - INTERVAL '1 hour') /tmp/count.csv",
			wantSQL:  "SELECT COUNT(*) FROM cpu_metrics WHERE ts > now() - INTERVAL '1 hour'",
			wantPath: "/tmp/count.csv",
		},
		{
			name:     "parquet extension",
			input:    "cpu_metrics /tmp/cpu.parquet",
			wantSQL:  "SELECT * FROM cpu_metrics",
			wantPath: "/tmp/cpu.parquet",
		},
		{
			name:    "empty input",
			input:   "",
			wantErr: true,
		},
		{
			name:    "only whitespace",
			input:   "   ",
			wantErr: true,
		},
		{
			name:    "table without path",
			input:   "cpu_metrics",
			wantErr: true,
		},
		{
			name:    "unmatched paren",
			input:   "(SELECT * FROM cpu_metrics /tmp/cpu.csv",
			wantErr: true,
		},
		{
			name:    "empty parens",
			input:   "() /tmp/cpu.csv",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, path, err := parseExportArgs(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("parseExportArgs(%q) = (%q, %q, nil), want error", tt.input, sql, path)
				}
				return
			}
			if err != nil {
				t.Errorf("parseExportArgs(%q) returned error: %v", tt.input, err)
				return
			}
			if sql != tt.wantSQL {
				t.Errorf("parseExportArgs(%q) sql = %q, want %q", tt.input, sql, tt.wantSQL)
			}
			if path != tt.wantPath {
				t.Errorf("parseExportArgs(%q) path = %q, want %q", tt.input, path, tt.wantPath)
			}
		})
	}
}
