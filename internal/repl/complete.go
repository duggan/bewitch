package repl

import (
	"fmt"
	"strings"

	"github.com/knz/bubbline/editline"
	"github.com/duggan/bewitch/internal/api"
)

var dotCommands = []string{
	".tables", ".schema", ".columns", ".count",
	".metrics", ".export", ".dimensions", ".help",
	".quit", ".exit",
}

var dotCommandsWithTableArg = map[string]bool{
	".schema": true, ".columns": true, ".count": true, ".export": true,
}

// newAutoCompleteFn returns a bubbline AutoCompleteFn that provides
// dot-command and SQL completion via DuckDB's sql_auto_complete().
func newAutoCompleteFn(queryFn func(string) (*api.QueryResponse, error)) editline.AutoCompleteFn {
	var tables []string
	var tablesLoaded bool

	loadTables := func() []string {
		if tablesLoaded {
			return tables
		}
		tablesLoaded = true
		qr, err := queryFn(`SELECT table_name FROM information_schema.tables WHERE table_schema = 'main' ORDER BY table_name`)
		if err != nil || qr.Error != "" {
			return nil
		}
		for _, row := range qr.Rows {
			if len(row) > 0 {
				tables = append(tables, fmt.Sprintf("%v", row[0]))
			}
		}
		return tables
	}

	return func(entireInput [][]rune, line, col int) (string, editline.Completions) {
		if line >= len(entireInput) {
			return "", nil
		}

		currentLine := string(entireInput[line][:col])

		// Dot-command completion: only on first line
		if line == 0 && strings.HasPrefix(strings.TrimSpace(currentLine), ".") {
			return completeDot(currentLine, col, loadTables)
		}

		// SQL completion: build full text up to cursor position
		var fullText strings.Builder
		for i, l := range entireInput {
			if i > 0 {
				fullText.WriteRune('\n')
			}
			if i < line {
				fullText.WriteString(string(l))
			} else if i == line {
				fullText.WriteString(string(l[:col]))
			}
		}

		return completeSQL(fullText.String(), col, queryFn)
	}
}

// completeDot handles dot-command and dot-command argument completion.
func completeDot(lineStr string, col int, loadTables func() []string) (string, editline.Completions) {
	parts := strings.Fields(lineStr)
	if len(parts) == 0 {
		return "", nil
	}

	if len(parts) == 1 && !strings.HasSuffix(lineStr, " ") {
		// Completing the command name itself
		prefix := parts[0]
		var matches []string
		for _, cmd := range dotCommands {
			if strings.HasPrefix(cmd, strings.ToLower(prefix)) {
				matches = append(matches, cmd)
			}
		}
		if len(matches) == 0 {
			return "", nil
		}
		start := col - len(prefix)
		return "", editline.SimpleWordsCompletion(matches, "command", col, start, col)
	}

	// Completing an argument (table name)
	cmd := strings.ToLower(parts[0])
	if !dotCommandsWithTableArg[cmd] {
		return "", nil
	}

	prefix := ""
	argStart := col
	if len(parts) > 1 && !strings.HasSuffix(lineStr, " ") {
		prefix = parts[len(parts)-1]
		argStart = col - len(prefix)
	}

	tbls := loadTables()
	var matches []string
	for _, t := range tbls {
		if strings.HasPrefix(t, strings.ToLower(prefix)) {
			matches = append(matches, t)
		}
	}
	if len(matches) == 0 {
		return "", nil
	}
	return "", editline.SimpleWordsCompletion(matches, "table", col, argStart, col)
}

// completeSQL uses DuckDB's sql_auto_complete() for context-aware SQL completion.
func completeSQL(fullText string, col int, queryFn func(string) (*api.QueryResponse, error)) (string, editline.Completions) {
	if strings.TrimSpace(fullText) == "" {
		return "", nil
	}

	escaped := strings.ReplaceAll(fullText, "'", "''")
	sql := fmt.Sprintf("SELECT suggestion, suggestion_start FROM sql_auto_complete('%s')", escaped)

	qr, err := queryFn(sql)
	if err != nil || qr.Error != "" || len(qr.Rows) == 0 {
		return "", nil
	}

	var suggestions []string
	var startOffset int
	for i, row := range qr.Rows {
		if len(row) < 2 {
			continue
		}
		suggestion := fmt.Sprintf("%v", row[0])
		if i == 0 {
			startOffset = toInt(row[1])
		}
		suggestions = append(suggestions, suggestion)
	}

	if len(suggestions) == 0 {
		return "", nil
	}

	// The prefix being replaced spans from startOffset to end of fullText.
	// Translate to a column offset on the current line.
	prefixLen := len(fullText) - startOffset
	if prefixLen < 0 {
		prefixLen = 0
	}
	wordStartCol := col - prefixLen
	if wordStartCol < 0 {
		wordStartCol = 0
	}

	return "", editline.SimpleWordsCompletion(suggestions, "sql", col, wordStartCol, col)
}

// toInt converts a value from a query response to an int.
func toInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	default:
		return 0
	}
}
