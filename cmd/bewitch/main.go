package main

import (
	"bufio"
	"crypto/sha256"
	"crypto/tls"
	"encoding/json"
	"crypto/x509"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/log"
	"github.com/duggan/bewitch/internal/config"
	"github.com/duggan/bewitch/internal/repl"
	"github.com/duggan/bewitch/internal/tui"
)

var version = "dev"

func main() {
	configPath := flag.String("config", "/etc/bewitch.toml", "path to config file")
	debug := flag.Bool("debug", false, "show debug console in TUI")
	addr := flag.String("addr", "", "TCP address of remote daemon (host:port)")
	useTLS := flag.Bool("tls", true, "use TLS for TCP connections")
	tlsSkipVerify := flag.Bool("tls-skip-verify", false, "skip TLS fingerprint verification")
	tlsResetFP := flag.Bool("tls-reset-fingerprint", false, "update stored fingerprint for this server")
	token := flag.String("token", "", "bearer token for TCP authentication")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion || (flag.NArg() > 0 && flag.Arg(0) == "version") {
		fmt.Println("bewitch", version)
		return
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "loading config: %v\n", err)
		os.Exit(1)
	}

	// Resolve TLS config for TCP connections (pre-flight fingerprint check)
	var tlsCfg *tls.Config
	if *addr != "" && *useTLS {
		var err error
		tlsCfg, err = resolveTLS(*addr, *tlsSkipVerify, *tlsResetFP)
		if err != nil {
			fmt.Fprintf(os.Stderr, "TLS: %v\n", err)
			os.Exit(1)
		}
	}

	// Resolve auth token: CLI flag takes precedence, then config file
	effectiveToken := *token
	if effectiveToken == "" && *addr != "" {
		effectiveToken = cfg.Daemon.AuthToken
	}

	// Handle subcommands
	if flag.NArg() > 0 {
		switch flag.Arg(0) {
		case "archive":
			runArchive(cfg, *addr, tlsCfg, effectiveToken)
			return
		case "unarchive":
			runUnarchive(cfg, *addr, tlsCfg, effectiveToken)
			return
		case "compact":
			runCompact(cfg, *addr, tlsCfg, effectiveToken)
			return
		case "repl":
			runSQL(cfg, *addr, tlsCfg, effectiveToken)
			return
		case "snapshot":
			runSnapshot(cfg, *addr, tlsCfg, effectiveToken)
			return
		case "capture-views":
			runCaptureViews(cfg, *addr, tlsCfg, effectiveToken)
			return
		case "capture-frames":
			runCaptureFrames(cfg, *addr, tlsCfg, effectiveToken)
			return
		default:
			fmt.Fprintf(os.Stderr, "unknown command: %s\n", flag.Arg(0))
			os.Exit(1)
		}
	}

	refreshInterval := 2 * time.Second
	if cfg.TUI.RefreshInterval != "" {
		if d, err := time.ParseDuration(cfg.TUI.RefreshInterval); err == nil {
			refreshInterval = d
		}
	}

	historyRanges, err := cfg.TUI.ParseHistoryRanges()
	if err != nil {
		fmt.Fprintf(os.Stderr, "parsing history ranges: %v\n", err)
		os.Exit(1)
	}

	captureSettings := tui.CaptureSettingsFromConfig(cfg.TUI.Capture)

	client := makeClient(cfg, *addr, tlsCfg, effectiveToken)
	model := tui.NewModel(client, refreshInterval, historyRanges, captureSettings, *debug)

	p := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		log.Fatalf("TUI error: %v", err)
	}
}

func makeClient(cfg *config.Config, addr string, tlsCfg *tls.Config, token string) *tui.DaemonClient {
	if addr != "" {
		return tui.NewDaemonClientTCP(addr, tlsCfg, token)
	}
	return tui.NewDaemonClient(cfg.Daemon.Socket)
}

// resolveTLS performs the pre-flight TLS fingerprint check (TOFU).
// It connects to the server, fetches its certificate, and verifies the fingerprint
// against the known_hosts file. Returns a tls.Config suitable for the connection.
func resolveTLS(addr string, skipVerify, resetFP bool) (*tls.Config, error) {
	if skipVerify {
		return &tls.Config{InsecureSkipVerify: true}, nil
	}

	// Fetch the server's certificate
	conn, err := tls.Dial("tcp", addr, &tls.Config{InsecureSkipVerify: true})
	if err != nil {
		return nil, fmt.Errorf("connecting to %s: %w", addr, err)
	}
	conn.Close()

	certs := conn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return nil, fmt.Errorf("server at %s presented no certificates", addr)
	}
	leaf := certs[0]
	fingerprint := certFingerprint(leaf)

	// Load known hosts
	hosts, err := tui.LoadKnownHosts()
	if err != nil {
		return nil, fmt.Errorf("reading known_hosts: %w", err)
	}

	stored, known := hosts[addr]
	switch {
	case known && stored == fingerprint && !resetFP:
		// Fingerprint matches — proceed silently

	case known && stored != fingerprint && !resetFP:
		// Fingerprint mismatch — refuse connection
		return nil, fmt.Errorf("server fingerprint changed!\n"+
			"  Expected: %s\n"+
			"  Got:      %s\n"+
			"This could indicate a MITM attack or the daemon was restarted with a new certificate.\n"+
			"If this is expected, reconnect with -tls-reset-fingerprint to update.", stored, fingerprint)

	case resetFP:
		// User explicitly wants to update the fingerprint
		fmt.Printf("TLS fingerprint for %s:\n  %s\n", addr, fingerprint)
		if err := tui.SaveKnownHost(addr, fingerprint); err != nil {
			return nil, fmt.Errorf("saving fingerprint: %w", err)
		}
		fmt.Println("Fingerprint updated.")

	default:
		// New server — prompt the user (TOFU)
		fmt.Printf("TLS fingerprint for %s:\n  %s\nTrust this server? [y/N]: ", addr, fingerprint)
		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer != "y" && answer != "yes" {
			return nil, fmt.Errorf("connection refused by user")
		}
		if err := tui.SaveKnownHost(addr, fingerprint); err != nil {
			return nil, fmt.Errorf("saving fingerprint: %w", err)
		}
	}

	// Build a tls.Config that pins to this fingerprint
	pinnedFP := fingerprint
	return &tls.Config{
		InsecureSkipVerify: true,
		VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			if len(rawCerts) == 0 {
				return fmt.Errorf("no certificates presented")
			}
			sum := sha256.Sum256(rawCerts[0])
			got := "sha256:" + hex.EncodeToString(sum[:])
			if got != pinnedFP {
				return fmt.Errorf("certificate fingerprint mismatch: expected %s, got %s", pinnedFP, got)
			}
			return nil
		},
	}, nil
}

func certFingerprint(cert *x509.Certificate) string {
	sum := sha256.Sum256(cert.Raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func runSQL(cfg *config.Config, addr string, tlsCfg *tls.Config, token string) {
	r, err := repl.New(repl.Config{
		Socket:      cfg.Daemon.Socket,
		Addr:        addr,
		ArchivePath: cfg.Daemon.ArchivePath,
		TLSConfig:   tlsCfg,
		Token:       token,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer r.Close()

	if err := r.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runArchive(cfg *config.Config, addr string, tlsCfg *tls.Config, token string) {
	client := makeClient(cfg, addr, tlsCfg, token)
	fmt.Println("requesting archival...")
	if err := client.Archive(); err != nil {
		fmt.Fprintf(os.Stderr, "archive failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("archive complete")
}

func runUnarchive(cfg *config.Config, addr string, tlsCfg *tls.Config, token string) {
	client := makeClient(cfg, addr, tlsCfg, token)
	fmt.Println("requesting unarchive (reloading Parquet data into DuckDB)...")
	if err := client.Unarchive(); err != nil {
		fmt.Fprintf(os.Stderr, "unarchive failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("unarchive complete")
}

func runCompact(cfg *config.Config, addr string, tlsCfg *tls.Config, token string) {
	client := makeClient(cfg, addr, tlsCfg, token)
	fmt.Println("requesting database compaction...")
	if err := client.Compact(); err != nil {
		fmt.Fprintf(os.Stderr, "compaction failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("compaction complete")
}

func runSnapshot(cfg *config.Config, addr string, tlsCfg *tls.Config, token string) {
	// Parse snapshot-specific flags from remaining args
	args := flag.Args()[1:] // skip "snapshot"
	var withSystemTables bool
	var path string
	for _, arg := range args {
		switch arg {
		case "-with-system-tables", "--with-system-tables":
			withSystemTables = true
		default:
			if path != "" {
				fmt.Fprintf(os.Stderr, "unexpected argument: %s\n", arg)
				os.Exit(1)
			}
			path = arg
		}
	}

	if path == "" {
		fmt.Fprintf(os.Stderr, "usage: bewitch snapshot [-with-system-tables] <path>\n")
		os.Exit(1)
	}
	if !filepath.IsAbs(path) {
		abs, err := filepath.Abs(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: resolving path: %v\n", err)
			os.Exit(1)
		}
		path = abs
	}

	client := makeClient(cfg, addr, tlsCfg, token)
	if withSystemTables {
		fmt.Println("creating snapshot (with system tables)...")
	} else {
		fmt.Println("creating snapshot...")
	}
	resp, err := client.Snapshot(path, withSystemTables)
	if err != nil {
		fmt.Fprintf(os.Stderr, "snapshot failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("snapshot created: %s (%.1f MB)\n", resp.Path, float64(resp.SizeBytes)/(1024*1024))
}

func runCaptureViews(cfg *config.Config, addr string, tlsCfg *tls.Config, token string) {
	args := flag.Args()[1:] // skip "capture-views"
	cols, rows := 120, 32
	var dir string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--cols", "-cols":
			i++
			if i >= len(args) {
				fmt.Fprintf(os.Stderr, "missing value for --cols\n")
				os.Exit(1)
			}
			v, err := strconv.Atoi(args[i])
			if err != nil {
				fmt.Fprintf(os.Stderr, "invalid --cols value: %s\n", args[i])
				os.Exit(1)
			}
			cols = v
		case "--rows", "-rows":
			i++
			if i >= len(args) {
				fmt.Fprintf(os.Stderr, "missing value for --rows\n")
				os.Exit(1)
			}
			v, err := strconv.Atoi(args[i])
			if err != nil {
				fmt.Fprintf(os.Stderr, "invalid --rows value: %s\n", args[i])
				os.Exit(1)
			}
			rows = v
		default:
			if dir != "" {
				fmt.Fprintf(os.Stderr, "unexpected argument: %s\n", args[i])
				os.Exit(1)
			}
			dir = args[i]
		}
	}

	if dir == "" {
		fmt.Fprintf(os.Stderr, "usage: bewitch capture-views [--cols N] [--rows N] <dir>\n")
		os.Exit(1)
	}
	if !filepath.IsAbs(dir) {
		abs, err := filepath.Abs(dir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: resolving path: %v\n", err)
			os.Exit(1)
		}
		dir = abs
	}

	captureSettings := tui.CaptureSettingsFromConfig(cfg.TUI.Capture)
	client := makeClient(cfg, addr, tlsCfg, token)
	historyRanges, _ := cfg.TUI.ParseHistoryRanges()
	if historyRanges == nil {
		historyRanges = config.DefaultHistoryRanges
	}
	model := tui.NewModel(client, 2*time.Second, historyRanges, captureSettings, false)
	model.SetSize(cols, rows)

	fmt.Printf("capturing all views (%dx%d) to %s ...\n", cols, rows, dir)
	imgW, imgH, files, err := model.CaptureAllViews(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "capture failed: %v\n", err)
		os.Exit(1)
	}
	for _, f := range files {
		fmt.Printf("  %s\n", filepath.Base(f))
	}
	fmt.Printf("done: %d files, %dx%d pixels\n", len(files), imgW, imgH)
}

func runCaptureFrames(cfg *config.Config, addr string, tlsCfg *tls.Config, token string) {
	args := flag.Args()[1:] // skip "capture-frames"
	cols, rows := 120, 32
	frames := 5
	delay := 400 * time.Millisecond
	var outPath string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--cols", "-cols":
			i++
			if i >= len(args) {
				fmt.Fprintf(os.Stderr, "missing value for --cols\n")
				os.Exit(1)
			}
			v, err := strconv.Atoi(args[i])
			if err != nil {
				fmt.Fprintf(os.Stderr, "invalid --cols value: %s\n", args[i])
				os.Exit(1)
			}
			cols = v
		case "--rows", "-rows":
			i++
			if i >= len(args) {
				fmt.Fprintf(os.Stderr, "missing value for --rows\n")
				os.Exit(1)
			}
			v, err := strconv.Atoi(args[i])
			if err != nil {
				fmt.Fprintf(os.Stderr, "invalid --rows value: %s\n", args[i])
				os.Exit(1)
			}
			rows = v
		case "--frames", "-frames":
			i++
			if i >= len(args) {
				fmt.Fprintf(os.Stderr, "missing value for --frames\n")
				os.Exit(1)
			}
			v, err := strconv.Atoi(args[i])
			if err != nil {
				fmt.Fprintf(os.Stderr, "invalid --frames value: %s\n", args[i])
				os.Exit(1)
			}
			frames = v
		case "--delay", "-delay":
			i++
			if i >= len(args) {
				fmt.Fprintf(os.Stderr, "missing value for --delay\n")
				os.Exit(1)
			}
			v, err := time.ParseDuration(args[i])
			if err != nil {
				fmt.Fprintf(os.Stderr, "invalid --delay value: %s\n", args[i])
				os.Exit(1)
			}
			delay = v
		default:
			if outPath != "" {
				fmt.Fprintf(os.Stderr, "unexpected argument: %s\n", args[i])
				os.Exit(1)
			}
			outPath = args[i]
		}
	}

	if outPath == "" {
		fmt.Fprintf(os.Stderr, "usage: bewitch capture-frames [--cols N] [--rows N] [--frames N] [--delay duration] <output.json>\n")
		os.Exit(1)
	}
	if !filepath.IsAbs(outPath) {
		abs, err := filepath.Abs(outPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: resolving path: %v\n", err)
			os.Exit(1)
		}
		outPath = abs
	}

	captureSettings := tui.CaptureSettingsFromConfig(cfg.TUI.Capture)
	client := makeClient(cfg, addr, tlsCfg, token)
	historyRanges, _ := cfg.TUI.ParseHistoryRanges()
	if historyRanges == nil {
		historyRanges = config.DefaultHistoryRanges
	}
	model := tui.NewModel(client, 2*time.Second, historyRanges, captureSettings, false)
	model.SetSize(cols, rows)

	fmt.Printf("capturing ANSI frames (%dx%d, %d frames/view, %s delay) ...\n", cols, rows, frames, delay)
	frameSet, err := model.CaptureAllViewsANSI(frames, delay)
	if err != nil {
		fmt.Fprintf(os.Stderr, "capture failed: %v\n", err)
		os.Exit(1)
	}

	f, err := os.Create(outPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error creating output file: %v\n", err)
		os.Exit(1)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	if err := enc.Encode(frameSet); err != nil {
		fmt.Fprintf(os.Stderr, "error writing JSON: %v\n", err)
		os.Exit(1)
	}

	totalFrames := 0
	for _, viewFrames := range frameSet.Views {
		totalFrames += len(viewFrames)
	}
	fi, _ := f.Stat()
	fmt.Printf("done: %d views, %d total frames, %s\n", len(frameSet.Views), totalFrames, formatBytes(fi.Size()))
}

func formatBytes(b int64) string {
	switch {
	case b >= 1024*1024:
		return fmt.Sprintf("%.1f MB", float64(b)/(1024*1024))
	case b >= 1024:
		return fmt.Sprintf("%.1f KB", float64(b)/1024)
	default:
		return fmt.Sprintf("%d B", b)
	}
}
