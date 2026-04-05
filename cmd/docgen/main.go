// cmd/docgen extracts API response types from Go source using go/ast and outputs
// a JSON schema consumed by the documentation site. No project imports — avoids
// pulling in CGO/DuckDB.
//
// Usage: go run cmd/docgen/main.go > site/src/generated/api-schema.json
package main

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// --- output schema ---

type APISchema struct {
	Endpoints []Endpoint         `json:"endpoints"`
	Types     map[string]TypeDef `json:"types"`
}

type Endpoint struct {
	Method      string  `json:"method"`
	Path        string  `json:"path"`
	Description string  `json:"description"`
	Category    string  `json:"category"`
	Response    string  `json:"response,omitempty"`
	Request     string  `json:"request,omitempty"`
	QueryParams []Param `json:"query_params,omitempty"`
	Notes       []string `json:"notes,omitempty"`
}

type Param struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
}

type TypeDef struct {
	Name        string  `json:"name"`
	Description string  `json:"description,omitempty"`
	Fields      []Field `json:"fields"`
}

type Field struct {
	Name     string `json:"name"`
	JSONName string `json:"json_name"`
	Type     string `json:"type"`
	Optional bool   `json:"optional,omitempty"`
}

// --- AST extraction ---

// parseStructTypes extracts all exported struct type definitions from Go source files.
func parseStructTypes(paths []string) map[string]*ast.StructType {
	fset := token.NewFileSet()
	structs := make(map[string]*ast.StructType)

	for _, p := range paths {
		files, err := filepath.Glob(p)
		if err != nil {
			continue
		}
		for _, f := range files {
			node, err := parser.ParseFile(fset, f, nil, parser.ParseComments)
			if err != nil {
				fmt.Fprintf(os.Stderr, "parse %s: %v\n", f, err)
				continue
			}
			for _, decl := range node.Decls {
				gd, ok := decl.(*ast.GenDecl)
				if !ok || gd.Tok != token.TYPE {
					continue
				}
				for _, spec := range gd.Specs {
					ts, ok := spec.(*ast.TypeSpec)
					if !ok || !ts.Name.IsExported() {
						continue
					}
					st, ok := ts.Type.(*ast.StructType)
					if !ok {
						continue
					}
					structs[ts.Name.Name] = st
				}
			}
		}
	}
	return structs
}

// goTypeToJSON converts a Go AST type expression to a JSON-friendly type string.
func goTypeToJSON(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		switch t.Name {
		case "string":
			return "string"
		case "bool":
			return "boolean"
		case "int", "int8", "int16", "int32", "int64",
			"uint", "uint8", "uint16", "uint32", "uint64",
			"float32", "float64":
			return "number"
		case "any":
			return "any"
		default:
			// Named type reference (e.g., CPUCoreMetric)
			return t.Name
		}
	case *ast.ArrayType:
		elem := goTypeToJSON(t.Elt)
		return elem + "[]"
	case *ast.StarExpr:
		return goTypeToJSON(t.X)
	case *ast.MapType:
		key := goTypeToJSON(t.Key)
		val := goTypeToJSON(t.Value)
		return fmt.Sprintf("map<%s, %s>", key, val)
	case *ast.SelectorExpr:
		// e.g., alert.NotifyResult or time.Time or time.Duration
		pkg := ""
		if ident, ok := t.X.(*ast.Ident); ok {
			pkg = ident.Name
		}
		name := t.Sel.Name
		if pkg == "time" && name == "Time" {
			return "string" // ISO 8601
		}
		if pkg == "time" && name == "Duration" {
			return "number" // nanoseconds
		}
		return name
	case *ast.InterfaceType:
		return "any"
	default:
		return "unknown"
	}
}

// parseJSONTag extracts the JSON field name and omitempty from a struct tag.
func parseJSONTag(tag string) (name string, omitempty bool) {
	// Strip backticks
	tag = strings.Trim(tag, "`")
	// Find json:"..."
	idx := strings.Index(tag, `json:"`)
	if idx < 0 {
		return "", false
	}
	rest := tag[idx+6:]
	end := strings.Index(rest, `"`)
	if end < 0 {
		return "", false
	}
	val := rest[:end]
	if val == "-" {
		return "-", false
	}
	parts := strings.Split(val, ",")
	name = parts[0]
	for _, p := range parts[1:] {
		if p == "omitempty" {
			omitempty = true
		}
	}
	return name, omitempty
}

// buildTypeDef converts an AST struct into our TypeDef.
func buildTypeDef(name string, st *ast.StructType) TypeDef {
	td := TypeDef{Name: name}
	if st.Fields == nil {
		return td
	}
	for _, f := range st.Fields.List {
		if len(f.Names) == 0 {
			continue // embedded
		}
		fieldName := f.Names[0].Name
		if !ast.IsExported(fieldName) {
			continue
		}
		jsonName := ""
		omit := false
		if f.Tag != nil {
			jsonName, omit = parseJSONTag(f.Tag.Value)
		}
		if jsonName == "-" {
			continue
		}
		if jsonName == "" {
			jsonName = fieldName
		}
		td.Fields = append(td.Fields, Field{
			Name:     fieldName,
			JSONName: jsonName,
			Type:     goTypeToJSON(f.Type),
			Optional: omit,
		})
	}
	return td
}

// --- route table ---

func endpoints() []Endpoint {
	historyParams := []Param{
		{Name: "start", Type: "number", Description: "Start time (Unix seconds)"},
		{Name: "end", Type: "number", Description: "End time (Unix seconds)"},
	}

	return []Endpoint{
		// Status & Config
		{Method: "GET", Path: "/api/status", Description: "Daemon status, uptime, and collector intervals", Category: "Status & Config", Response: "StatusResponse"},
		{Method: "GET", Path: "/api/config", Description: "Full daemon, alerts, and TUI configuration", Category: "Status & Config", Response: "ConfigResponse"},

		// Live Metrics
		{Method: "GET", Path: "/api/metrics/cpu", Description: "Per-core CPU usage percentages", Category: "Live Metrics", Response: "CPUResponse", Notes: []string{"ETag caching"}},
		{Method: "GET", Path: "/api/metrics/memory", Description: "Memory and swap usage", Category: "Live Metrics", Response: "MemoryMetric", Notes: []string{"ETag caching"}},
		{Method: "GET", Path: "/api/metrics/disk", Description: "Per-mount disk space, I/O rates, and SMART health", Category: "Live Metrics", Response: "DiskResponse", Notes: []string{"ETag caching"}},
		{Method: "GET", Path: "/api/metrics/network", Description: "Per-interface throughput and error counters", Category: "Live Metrics", Response: "NetworkResponse", Notes: []string{"ETag caching"}},
		{Method: "GET", Path: "/api/metrics/temperature", Description: "Hardware sensor temperatures", Category: "Live Metrics", Response: "TemperatureResponse", Notes: []string{"ETag caching"}},
		{Method: "GET", Path: "/api/metrics/power", Description: "Power consumption per RAPL zone", Category: "Live Metrics", Response: "PowerResponse", Notes: []string{"ETag caching"}},
		{Method: "GET", Path: "/api/metrics/ecc", Description: "ECC memory error counts", Category: "Live Metrics", Response: "ECCResponse", Notes: []string{"ETag caching"}},
		{Method: "GET", Path: "/api/metrics/gpu", Description: "GPU utilization, frequency, power, and memory", Category: "Live Metrics", Response: "GPUResponse", Notes: []string{"ETag caching"}},
		{Method: "GET", Path: "/api/metrics/process", Description: "All processes (live in-memory snapshot)", Category: "Live Metrics", Response: "ProcessResponse", Notes: []string{"ETag caching"}},
		{Method: "GET", Path: "/api/metrics/dashboard", Description: "Combined overview of all metric types", Category: "Live Metrics", Response: "DashboardData", Notes: []string{"ETag caching"}},

		// History
		{Method: "GET", Path: "/api/history/cpu", Description: "CPU usage over time", Category: "History", Response: "HistoryResponse", QueryParams: historyParams, Notes: []string{"Bucket size auto-scales by range"}},
		{Method: "GET", Path: "/api/history/memory", Description: "Memory usage over time", Category: "History", Response: "HistoryResponse", QueryParams: historyParams},
		{Method: "GET", Path: "/api/history/disk", Description: "Disk space and I/O over time", Category: "History", Response: "HistoryResponse", QueryParams: historyParams},
		{Method: "GET", Path: "/api/history/network", Description: "Network throughput over time", Category: "History", Response: "HistoryResponse", QueryParams: historyParams},
		{Method: "GET", Path: "/api/history/temperature", Description: "Temperature over time", Category: "History", Response: "HistoryResponse", QueryParams: historyParams},
		{Method: "GET", Path: "/api/history/power", Description: "Power consumption over time", Category: "History", Response: "HistoryResponse", QueryParams: historyParams},
		{Method: "GET", Path: "/api/history/gpu", Description: "GPU metrics over time", Category: "History", Response: "HistoryResponse", QueryParams: historyParams},
		{Method: "GET", Path: "/api/history/process", Description: "Top processes by CPU over time", Category: "History", Response: "HistoryResponse", QueryParams: historyParams},

		// Alerts
		{Method: "GET", Path: "/api/alerts", Description: "List fired alerts", Category: "Alerts", Response: "AlertsResponse",
			QueryParams: []Param{{Name: "ack", Type: "boolean", Description: "Filter by acknowledged state (e.g. ?ack=false)"}}},
		{Method: "POST", Path: "/api/alerts/{id}/ack", Description: "Acknowledge a fired alert", Category: "Alerts", Response: "GenericResponse"},
		{Method: "GET", Path: "/api/alert-rules", Description: "List all alert rules", Category: "Alerts", Response: "AlertRulesResponse"},
		{Method: "POST", Path: "/api/alert-rules", Description: "Create a new alert rule", Category: "Alerts", Request: "AlertRuleMetric", Response: "GenericResponse"},
		{Method: "DELETE", Path: "/api/alert-rules/{id}", Description: "Delete an alert rule", Category: "Alerts", Response: "GenericResponse"},
		{Method: "PUT", Path: "/api/alert-rules/{id}/toggle", Description: "Toggle a rule enabled/disabled", Category: "Alerts", Response: "GenericResponse"},
		{Method: "POST", Path: "/api/test-notifications", Description: "Send a test alert to all notification channels", Category: "Alerts", Response: "NotifyTestResponse"},

		// Query & Export
		{Method: "POST", Path: "/api/query", Description: "Execute a read-only SQL query", Category: "Query & Export", Request: "QueryRequest", Response: "QueryResponse", Notes: []string{"Read-only enforcement via statement parser"}},
		{Method: "POST", Path: "/api/export", Description: "Export query results to a file (CSV, Parquet, or JSON)", Category: "Query & Export", Request: "ExportRequest", Response: "ExportResponse"},

		// Data Management
		{Method: "POST", Path: "/api/compact", Description: "Trigger database compaction", Category: "Data Management", Response: "GenericResponse"},
		{Method: "POST", Path: "/api/snapshot", Description: "Create a standalone database snapshot", Category: "Data Management", Request: "SnapshotRequest", Response: "SnapshotResponse"},
		{Method: "POST", Path: "/api/archive", Description: "Trigger Parquet archival of old data", Category: "Data Management", Response: "GenericResponse"},
		{Method: "POST", Path: "/api/unarchive", Description: "Reload Parquet data into the database", Category: "Data Management", Response: "GenericResponse"},
		{Method: "GET", Path: "/api/archive/status", Description: "Archive state and directory statistics", Category: "Data Management", Response: "ArchiveStatusResponse"},

		// Preferences
		{Method: "GET", Path: "/api/preferences", Description: "Get all saved preferences", Category: "Preferences", Response: "PreferencesResponse"},
		{Method: "POST", Path: "/api/preferences", Description: "Set a preference key/value pair", Category: "Preferences", Request: "PreferenceRequest", Response: "GenericResponse"},
	}
}

// syntheticTypes defines request types that aren't in the Go source
// (they're just inline JSON in handlers, not named structs).
func syntheticTypes() map[string]TypeDef {
	return map[string]TypeDef{
		"QueryRequest": {
			Name: "QueryRequest",
			Fields: []Field{
				{Name: "SQL", JSONName: "sql", Type: "string"},
			},
		},
		"PreferenceRequest": {
			Name: "PreferenceRequest",
			Fields: []Field{
				{Name: "Key", JSONName: "key", Type: "string"},
				{Name: "Value", JSONName: "value", Type: "string"},
			},
		},
	}
}

func main() {
	root := "."
	if len(os.Args) > 1 {
		root = os.Args[1]
	}

	// Source files to parse for struct types.
	globs := []string{
		filepath.Join(root, "internal/api/*.go"),
		filepath.Join(root, "internal/alert/notifier.go"),
	}

	astStructs := parseStructTypes(globs)

	// Build type definitions from AST.
	types := syntheticTypes()
	for name, st := range astStructs {
		types[name] = buildTypeDef(name, st)
	}

	// Collect only types that are reachable from endpoints.
	eps := endpoints()
	reachable := reachableTypes(eps, types)

	schema := APISchema{
		Endpoints: eps,
		Types:     reachable,
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(schema); err != nil {
		fmt.Fprintf(os.Stderr, "json encode: %v\n", err)
		os.Exit(1)
	}
}

// reachableTypes walks from endpoint response/request types and collects
// all transitively referenced types.
func reachableTypes(eps []Endpoint, allTypes map[string]TypeDef) map[string]TypeDef {
	needed := make(map[string]bool)
	var walk func(name string)
	walk = func(name string) {
		// Strip array suffix
		name = strings.TrimSuffix(name, "[]")
		if needed[name] || isPrimitive(name) {
			return
		}
		td, ok := allTypes[name]
		if !ok {
			return
		}
		needed[name] = true
		for _, f := range td.Fields {
			walk(f.Type)
		}
	}

	for _, ep := range eps {
		if ep.Response != "" {
			walk(ep.Response)
		}
		if ep.Request != "" {
			walk(ep.Request)
		}
	}

	result := make(map[string]TypeDef)
	// Sort for deterministic output
	var names []string
	for n := range needed {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		result[n] = allTypes[n]
	}
	return result
}

func isPrimitive(t string) bool {
	switch t {
	case "string", "number", "boolean", "any", "unknown":
		return true
	}
	return false
}
