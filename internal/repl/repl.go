package repl

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/knz/bubbline"
	"github.com/duggan/bewitch/internal/api"
)

// Config holds the configuration for the REPL.
type Config struct {
	Socket      string
	Addr        string      // TCP address; takes precedence over Socket when non-empty
	ArchivePath string
	TLSConfig   *tls.Config // TLS configuration for TCP connections; nil = plain HTTP
	Token       string      // bearer token for TCP authentication; empty = no auth
}

// REPL is an interactive DuckDB SQL console that queries via the daemon API.
type REPL struct {
	http        *http.Client
	editor      *bubbline.Editor
	baseURL     string
	target      string // display string: socket path or TCP address
	archivePath string
	interactive bool
}

// New creates a new REPL. Returns an error if the daemon is not reachable.
func New(cfg Config) (*REPL, error) {
	var client *http.Client
	var baseURL string
	var target string // for error messages

	if cfg.Addr != "" {
		transport := api.NewTCPTransport(cfg.TLSConfig)
		scheme := "http"
		if cfg.TLSConfig != nil {
			scheme = "https"
		}
		client = &http.Client{
			Timeout:   5 * time.Second,
			Transport: api.WrapTransport(transport, cfg.Token),
		}
		baseURL = scheme + "://" + cfg.Addr
		target = cfg.Addr
	} else {
		client = &http.Client{
			Timeout:   5 * time.Second,
			Transport: api.NewUnixTransport(cfg.Socket),
		}
		baseURL = "http://localhost"
		target = cfg.Socket
	}

	// Verify the daemon is reachable
	resp, err := client.Get(baseURL + "/api/status")
	if err != nil {
		return nil, fmt.Errorf("cannot connect to daemon at %s: %w\nIs bewitchd running?", target, err)
	}
	resp.Body.Close()

	r := &REPL{
		http:        client,
		baseURL:     baseURL,
		target:      target,
		archivePath: cfg.ArchivePath,
		interactive: isTerminal(),
	}

	if r.interactive {
		ed := bubbline.New()
		ed.Prompt = "bewitch> "
		ed.NextPrompt = "    ...> "

		// Multi-line: input is complete when it's a dot-command or ends with ";"
		ed.CheckInputComplete = checkInputComplete

		// Tab completion
		ed.AutoComplete = newAutoCompleteFn(r.query)

		// History
		historyFile := os.ExpandEnv("$HOME/.bewitch_sql_history")
		_ = ed.LoadHistory(historyFile)
		ed.SetAutoSaveHistory(historyFile, true)

		r.editor = ed
	}

	return r, nil
}

// Close releases REPL resources.
func (r *REPL) Close() {
	if r.editor != nil {
		r.editor.Close()
	}
}

// out returns the writer for REPL output.
func (r *REPL) out() io.Writer {
	return os.Stdout
}

// query sends SQL to the daemon and returns the response.
func (r *REPL) query(sql string) (*api.QueryResponse, error) {
	body, err := json.Marshal(struct {
		SQL string `json:"sql"`
	}{SQL: sql})
	if err != nil {
		return nil, err
	}

	resp, err := r.http.Post(r.baseURL+"/api/query", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	var qr api.QueryResponse
	if err := json.NewDecoder(resp.Body).Decode(&qr); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &qr, nil
}

// Run starts the REPL loop.
func (r *REPL) Run() error {
	if r.interactive {
		return r.runInteractive()
	}
	return r.runPiped()
}

// checkInputComplete determines whether the user's input is ready to execute.
// Returns true for dot-commands (single line starting with ".") and for
// SQL statements terminated with ";".
func checkInputComplete(entireInput [][]rune, line, col int) bool {
	if len(entireInput) == 0 {
		return true
	}

	// Dot-commands are single-line and always complete
	firstLine := strings.TrimSpace(string(entireInput[0]))
	if strings.HasPrefix(firstLine, ".") {
		return true
	}

	// SQL: complete when the trimmed full text ends with ";"
	var full strings.Builder
	for i, l := range entireInput {
		if i > 0 {
			full.WriteRune('\n')
		}
		full.WriteString(string(l))
	}
	return strings.HasSuffix(strings.TrimSpace(full.String()), ";")
}

func (r *REPL) runInteractive() error {
	r.printBanner()

	for {
		line, err := r.editor.GetLine()
		if err != nil {
			if errors.Is(err, bubbline.ErrInterrupted) {
				continue
			}
			if err == io.EOF {
				fmt.Fprintln(r.out(), "Bye!")
				return nil
			}
			return err
		}

		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		r.editor.AddHistory(line)

		// Dot-commands
		if strings.HasPrefix(trimmed, ".") {
			r.handleDotCommand(trimmed)
			continue
		}

		// SQL query (strip trailing semicolons)
		query := strings.TrimRight(trimmed, ";")
		query = strings.TrimSpace(query)
		if query != "" {
			r.executeQuery(query)
		}
	}
}

func (r *REPL) runPiped() error {
	scanner := bufio.NewScanner(os.Stdin)
	var buf strings.Builder

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		if trimmed == "" && buf.Len() == 0 {
			continue
		}

		if buf.Len() == 0 && strings.HasPrefix(trimmed, ".") {
			r.handleDotCommand(trimmed)
			continue
		}

		buf.WriteString(line)
		buf.WriteString("\n")

		if !strings.HasSuffix(strings.TrimSpace(buf.String()), ";") {
			continue
		}

		query := strings.TrimSpace(buf.String())
		query = strings.TrimRight(query, ";")
		buf.Reset()

		r.executeQuery(query)
	}

	// Execute any remaining buffered input
	if buf.Len() > 0 {
		query := strings.TrimSpace(buf.String())
		query = strings.TrimRight(query, ";")
		if query != "" {
			r.executeQuery(query)
		}
	}

	return scanner.Err()
}

func (r *REPL) executeQuery(sql string) {
	qr, err := r.query(sql)
	if err != nil {
		fmt.Fprintf(r.out(), "Error: %v\n", err)
		return
	}
	if qr.Error != "" {
		fmt.Fprintf(r.out(), "Error: %s\n", qr.Error)
		return
	}

	output := formatQueryResponse(qr)
	fmt.Fprint(r.out(), output)
}

// export sends an export request to the daemon and returns the response.
func (r *REPL) export(req api.ExportRequest) (*api.ExportResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	resp, err := r.http.Post(r.baseURL+"/api/export", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	var er api.ExportResponse
	if err := json.NewDecoder(resp.Body).Decode(&er); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &er, nil
}

func isTerminal() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return true
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
