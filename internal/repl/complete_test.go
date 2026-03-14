package repl

import (
	"fmt"
	"strings"
	"testing"

	"github.com/knz/bubbline/editline"
	"github.com/duggan/bewitch/internal/api"
)

func mockQueryFn(sql string) (*api.QueryResponse, error) {
	switch {
	case strings.Contains(sql, "information_schema.tables"):
		return &api.QueryResponse{
			Columns: []string{"table_name"},
			Rows: [][]any{
				{"alerts"},
				{"cpu_metrics"},
				{"disk_metrics"},
				{"memory_metrics"},
				{"network_metrics"},
				{"process_info"},
				{"process_metrics"},
			},
		}, nil
	case strings.Contains(sql, "sql_auto_complete"):
		// Extract the partial SQL from the query
		return mockAutoComplete(sql)
	}
	return &api.QueryResponse{}, nil
}

func mockAutoComplete(sql string) (*api.QueryResponse, error) {
	// Extract the argument: sql_auto_complete('...')
	start := strings.Index(sql, "sql_auto_complete('")
	if start < 0 {
		return &api.QueryResponse{}, nil
	}
	start += len("sql_auto_complete('")
	end := strings.LastIndex(sql, "')")
	if end < 0 || end <= start {
		return &api.QueryResponse{}, nil
	}
	partial := strings.ReplaceAll(sql[start:end], "''", "'")

	// Simulate DuckDB's sql_auto_complete behavior
	switch {
	case strings.EqualFold(partial, "SEL"):
		return &api.QueryResponse{
			Columns: []string{"suggestion", "suggestion_start"},
			Rows:    [][]any{{"SELECT", float64(0)}},
		}, nil
	case strings.EqualFold(partial, "SELECT * FROM cpu"):
		return &api.QueryResponse{
			Columns: []string{"suggestion", "suggestion_start"},
			Rows:    [][]any{{"cpu_metrics", float64(14)}},
		}, nil
	case strings.EqualFold(partial, "SELECT * FROM "):
		return &api.QueryResponse{
			Columns: []string{"suggestion", "suggestion_start"},
			Rows: [][]any{
				{"cpu_metrics", float64(14)},
				{"disk_metrics", float64(14)},
				{"memory_metrics", float64(14)},
			},
		}, nil
	case strings.HasPrefix(partial, "SELECT *\nFROM cpu"):
		return &api.QueryResponse{
			Columns: []string{"suggestion", "suggestion_start"},
			Rows:    [][]any{{"cpu_metrics", float64(14)}},
		}, nil
	}
	return &api.QueryResponse{Columns: []string{"suggestion", "suggestion_start"}}, nil
}

// completionEntries extracts completion strings from an editline.Completions.
func completionEntries(comp editline.Completions) []string {
	if comp == nil {
		return nil
	}
	var results []string
	numCats := comp.NumCategories()
	for catIdx := 0; catIdx < numCats; catIdx++ {
		numE := comp.NumEntries(catIdx)
		for eIdx := 0; eIdx < numE; eIdx++ {
			e := comp.Entry(catIdx, eIdx)
			results = append(results, e.Title())
		}
	}
	return results
}

func TestDotCommandCompletion(t *testing.T) {
	fn := newAutoCompleteFn(mockQueryFn)

	tests := []struct {
		name    string
		line    string
		want    []string
		wantNil bool
	}{
		{
			name: "complete .ta to .tables",
			line: ".ta",
			want: []string{".tables"},
		},
		{
			name: "complete .sch to .schema",
			line: ".sch",
			want: []string{".schema"},
		},
		{
			name: "complete .e shows .export and .exit",
			line: ".e",
			want: []string{".export", ".exit"},
		},
		{
			name: "complete .q to .quit",
			line: ".q",
			want: []string{".quit"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := [][]rune{[]rune(tt.line)}
			_, comp := fn(input, 0, len([]rune(tt.line)))
			got := completionEntries(comp)
			if tt.wantNil {
				if comp != nil {
					t.Fatalf("expected nil completions, got %v", got)
				}
				return
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %d results %v, want %d %v", len(got), got, len(tt.want), tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("result[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestDotCommandArgCompletion(t *testing.T) {
	fn := newAutoCompleteFn(mockQueryFn)

	tests := []struct {
		name    string
		line    string
		want    []string
		wantNil bool
	}{
		{
			name: "complete .schema cpu to cpu_metrics",
			line: ".schema cpu",
			want: []string{"cpu_metrics"},
		},
		{
			name: "complete .count with no prefix shows all tables",
			line: ".count ",
			want: []string{"alerts", "cpu_metrics", "disk_metrics", "memory_metrics", "network_metrics", "process_info", "process_metrics"},
		},
		{
			name:    "no completion for .help args",
			line:    ".help foo",
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := [][]rune{[]rune(tt.line)}
			_, comp := fn(input, 0, len([]rune(tt.line)))
			got := completionEntries(comp)
			if tt.wantNil {
				if comp != nil {
					t.Fatalf("expected nil completions, got %v", got)
				}
				return
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %d results %v, want %d %v", len(got), got, len(tt.want), tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("result[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestSQLCompletion(t *testing.T) {
	fn := newAutoCompleteFn(mockQueryFn)

	tests := []struct {
		name    string
		line    string
		want    []string
		wantNil bool
	}{
		{
			name: "complete SEL to SELECT",
			line: "SEL",
			want: []string{"SELECT"},
		},
		{
			name: "complete table name after FROM",
			line: "SELECT * FROM cpu",
			want: []string{"cpu_metrics"},
		},
		{
			name:    "empty input returns nothing",
			line:    "",
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := [][]rune{[]rune(tt.line)}
			_, comp := fn(input, 0, len([]rune(tt.line)))
			got := completionEntries(comp)
			if tt.wantNil {
				if comp != nil {
					t.Fatalf("expected nil completions, got %v", got)
				}
				return
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %d results %v, want %d %v", len(got), got, len(tt.want), tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("result[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestMultiLineCompletion(t *testing.T) {
	fn := newAutoCompleteFn(mockQueryFn)

	// Two-line input: "SELECT *" on line 0, "FROM cpu" on line 1, cursor at end of line 1
	input := [][]rune{
		[]rune("SELECT *"),
		[]rune("FROM cpu"),
	}
	_, comp := fn(input, 1, 8) // line 1, col 8 (end of "FROM cpu")

	got := completionEntries(comp)
	if len(got) != 1 || got[0] != "cpu_metrics" {
		t.Errorf("got %v, want [\"cpu_metrics\"]", got)
	}
}

func TestSQLCompletionError(t *testing.T) {
	errFn := func(sql string) (*api.QueryResponse, error) {
		return nil, fmt.Errorf("connection refused")
	}
	fn := newAutoCompleteFn(errFn)

	input := [][]rune{[]rune("SEL")}
	_, comp := fn(input, 0, 3)
	if comp != nil {
		t.Errorf("expected nil completions on error, got %v", completionEntries(comp))
	}
}

func TestCheckInputComplete(t *testing.T) {
	tests := []struct {
		name  string
		input [][]rune
		want  bool
	}{
		{
			name:  "empty input",
			input: [][]rune{},
			want:  true,
		},
		{
			name:  "dot-command is always complete",
			input: [][]rune{[]rune(".tables")},
			want:  true,
		},
		{
			name:  "semicolon-terminated SQL",
			input: [][]rune{[]rune("SELECT 1;")},
			want:  true,
		},
		{
			name:  "unterminated SQL",
			input: [][]rune{[]rune("SELECT")},
			want:  false,
		},
		{
			name: "multi-line SQL terminated",
			input: [][]rune{
				[]rune("SELECT *"),
				[]rune("FROM cpu_metrics;"),
			},
			want: true,
		},
		{
			name: "multi-line SQL not terminated",
			input: [][]rune{
				[]rune("SELECT *"),
				[]rune("FROM cpu_metrics"),
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// line and col don't matter for our implementation
			got := checkInputComplete(tt.input, 0, 0)
			if got != tt.want {
				t.Errorf("checkInputComplete() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestToInt(t *testing.T) {
	tests := []struct {
		val  any
		want int
	}{
		{float64(14), 14},
		{int(7), 7},
		{int64(42), 42},
		{"unknown", 0},
		{nil, 0},
	}
	for _, tt := range tests {
		got := toInt(tt.val)
		if got != tt.want {
			t.Errorf("toInt(%v) = %d, want %d", tt.val, got, tt.want)
		}
	}
}
