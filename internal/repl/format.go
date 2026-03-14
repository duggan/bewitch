package repl

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/duggan/bewitch/internal/api"
	"golang.org/x/term"
)

// formatQueryResponse formats an API query response as a psql-style table.
// Results containing multi-line cell values (e.g., EXPLAIN output) are rendered
// in expanded format instead of a tabular layout.
func formatQueryResponse(qr *api.QueryResponse) string {
	cols := qr.Columns
	if len(cols) == 0 {
		return "(0 rows)\n"
	}

	data := make([][]string, len(qr.Rows))
	multiline := false
	for i, row := range qr.Rows {
		data[i] = make([]string, len(row))
		for j, v := range row {
			data[i][j] = formatValue(v)
			if strings.Contains(data[i][j], "\n") {
				multiline = true
			}
		}
	}

	if multiline {
		// EXPLAIN results have columns (explain_key, explain_value) — just
		// print the values directly without labels.
		if isExplainResult(cols) {
			return formatExplain(data)
		}
		return formatExpanded(cols, data)
	}

	widths := computeWidths(cols, data, terminalWidth())

	var buf strings.Builder
	renderHeader(&buf, cols, widths)
	renderSeparator(&buf, widths)
	for _, row := range data {
		renderRow(&buf, row, widths)
	}
	fmt.Fprintf(&buf, "(%d rows)\n", len(data))
	return buf.String()
}

// isExplainResult returns true if the columns match DuckDB's EXPLAIN output
// format (explain_key, explain_value).
func isExplainResult(cols []string) bool {
	return len(cols) == 2 && cols[0] == "explain_key" && cols[1] == "explain_value"
}

// formatExplain renders EXPLAIN output by printing each plan section's value
// directly, without column labels.
func formatExplain(data [][]string) string {
	var buf strings.Builder
	for _, row := range data {
		buf.WriteString("\n")
		if len(row) >= 2 {
			buf.WriteString(row[1])
		}
		buf.WriteString("\n")
	}
	return buf.String()
}

// formatExpanded renders results in expanded format (one field per line),
// used when cell values contain newlines (e.g., EXPLAIN query plans).
func formatExpanded(cols []string, data [][]string) string {
	var buf strings.Builder
	buf.WriteString("\n")
	for i, row := range data {
		if len(data) > 1 {
			fmt.Fprintf(&buf, "-[ RECORD %d ]\n", i+1)
		}
		for j, col := range cols {
			val := ""
			if j < len(row) {
				val = row[j]
			}
			fmt.Fprintf(&buf, "%s: %s\n", col, val)
		}
	}
	return buf.String()
}

// formatValue converts a database value to its display string.
// Values arrive from JSON decoding: float64 for numbers, string for text, nil for NULL.
func formatValue(v any) string {
	if v == nil {
		return "NULL"
	}
	switch val := v.(type) {
	case time.Time:
		return val.Format("2006-01-02 15:04:05")
	case float64:
		if val == float64(int64(val)) {
			return fmt.Sprintf("%d", int64(val))
		}
		return fmt.Sprintf("%.2f", val)
	case float32:
		return formatValue(float64(val))
	case []byte:
		return string(val)
	default:
		return fmt.Sprintf("%v", val)
	}
}

// computeWidths determines column widths that fit within the terminal.
func computeWidths(headers []string, data [][]string, termWidth int) []int {
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h)
	}
	for _, row := range data {
		for i, cell := range row {
			if i < len(widths) && len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
	}

	// Separator overhead: " | " between columns (3 chars each) plus leading/trailing space
	overhead := 3*(len(widths)-1) + 2
	if len(widths) == 1 {
		overhead = 2
	}

	totalWidth := overhead
	for _, w := range widths {
		totalWidth += w
	}

	// Shrink columns proportionally if they exceed terminal width
	if termWidth > 0 && totalWidth > termWidth {
		available := termWidth - overhead
		if available < len(widths) {
			available = len(widths) // at least 1 char per column
		}
		// Shrink widest columns first
		for totalDataWidth(widths) > available {
			maxIdx := 0
			for i, w := range widths {
				if w > widths[maxIdx] {
					maxIdx = i
				}
			}
			if widths[maxIdx] <= 4 {
				break // don't shrink below 4
			}
			widths[maxIdx]--
		}
	}

	return widths
}

func totalDataWidth(widths []int) int {
	total := 0
	for _, w := range widths {
		total += w
	}
	return total
}

func renderHeader(buf *strings.Builder, headers []string, widths []int) {
	for i, h := range headers {
		if i > 0 {
			buf.WriteString(" | ")
		} else {
			buf.WriteString(" ")
		}
		buf.WriteString(padRight(h, widths[i]))
	}
	buf.WriteString("\n")
}

func renderSeparator(buf *strings.Builder, widths []int) {
	for i, w := range widths {
		if i > 0 {
			buf.WriteString("-+-")
		} else {
			buf.WriteString("-")
		}
		buf.WriteString(strings.Repeat("-", w))
	}
	buf.WriteString("\n")
}

func renderRow(buf *strings.Builder, row []string, widths []int) {
	for i := range widths {
		if i > 0 {
			buf.WriteString(" | ")
		} else {
			buf.WriteString(" ")
		}
		cell := ""
		if i < len(row) {
			cell = row[i]
		}
		if len(cell) > widths[i] {
			cell = cell[:widths[i]-1] + "~"
		}
		buf.WriteString(padRight(cell, widths[i]))
	}
	buf.WriteString("\n")
}

func padRight(s string, width int) string {
	if len(s) >= width {
		return s[:width]
	}
	return s + strings.Repeat(" ", width-len(s))
}

func terminalWidth() int {
	w, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || w <= 0 {
		return 120
	}
	return w
}
