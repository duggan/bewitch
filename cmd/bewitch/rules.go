package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"

	"github.com/duggan/bewitch/internal/api"
)

// rulesFileVersion is the format version written by `bewitch rules export`.
const rulesFileVersion = 1

// rulesFile is the on-disk format of `bewitch rules export|import`: the API's
// rule objects, without ids (a rule is identified by its unique name).
type rulesFile struct {
	Version int            `json:"version"`
	Rules   []exportedRule `json:"rules"`
}

// exportedRule is an api.AlertRuleMetric whose id is dropped from the JSON: the
// outer, less-nested ID field shadows the embedded one and is always zero.
type exportedRule struct {
	api.AlertRuleMetric
	ID int `json:"id,omitempty"`
}

// ruleClient is the part of the daemon client that rules export/import uses.
type ruleClient interface {
	GetAlertRules() ([]api.AlertRuleMetric, error)
	CreateAlertRule(api.AlertRuleMetric) error
	UpdateAlertRule(api.AlertRuleMetric) error
	ToggleAlertRule(id int) error
}

func runRules(c ruleClient, args []string) {
	usage := func() {
		fmt.Fprintln(os.Stderr, "usage: bewitch rules export [file]\n       bewitch rules import [-dry-run] <file|->")
		os.Exit(1)
	}
	if len(args) == 0 {
		usage()
	}
	switch args[0] {
	case "export":
		if len(args) > 2 {
			usage()
		}
		out := io.Writer(os.Stdout)
		if len(args) == 2 && args[1] != "-" {
			f, err := os.Create(args[1])
			if err != nil {
				fmt.Fprintf(os.Stderr, "export failed: %v\n", err)
				os.Exit(1)
			}
			defer f.Close()
			out = f
		}
		if err := exportRules(c, out); err != nil {
			fmt.Fprintf(os.Stderr, "export failed: %v\n", err)
			os.Exit(1)
		}
	case "import":
		var dryRun bool
		var path string
		for _, a := range args[1:] {
			switch a {
			case "-dry-run", "--dry-run", "-n":
				dryRun = true
			default:
				if path != "" {
					usage()
				}
				path = a
			}
		}
		if path == "" {
			usage()
		}
		in := io.Reader(os.Stdin)
		if path != "-" {
			f, err := os.Open(path)
			if err != nil {
				fmt.Fprintf(os.Stderr, "import failed: %v\n", err)
				os.Exit(1)
			}
			defer f.Close()
			in = f
		}
		rules, err := readRulesFile(in)
		if err != nil {
			fmt.Fprintf(os.Stderr, "import failed: %v\n", err)
			os.Exit(1)
		}
		if err := importRules(c, rules, dryRun, os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "import failed: %v\n", err)
			os.Exit(1)
		}
	default:
		usage()
	}
}

func exportRules(c ruleClient, out io.Writer) error {
	rules, err := c.GetAlertRules()
	if err != nil {
		return err
	}
	f := rulesFile{Version: rulesFileVersion, Rules: make([]exportedRule, 0, len(rules))}
	for _, r := range rules {
		f.Rules = append(f.Rules, exportedRule{AlertRuleMetric: r})
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(f)
}

func readRulesFile(in io.Reader) ([]api.AlertRuleMetric, error) {
	var f rulesFile
	dec := json.NewDecoder(in)
	dec.DisallowUnknownFields() // a typo'd field would otherwise silently become a default
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("parsing rules file: %w", err)
	}
	if f.Version != rulesFileVersion {
		return nil, fmt.Errorf("unsupported rules file version %d (want %d)", f.Version, rulesFileVersion)
	}
	rules := make([]api.AlertRuleMetric, 0, len(f.Rules))
	seen := make(map[string]bool, len(f.Rules))
	for i, r := range f.Rules {
		if r.Name == "" {
			return nil, fmt.Errorf("rule %d has no name", i+1)
		}
		if seen[r.Name] {
			return nil, fmt.Errorf("rule %q appears twice", r.Name)
		}
		seen[r.Name] = true
		r.AlertRuleMetric.ID = 0
		rules = append(rules, r.AlertRuleMetric)
	}
	return rules, nil
}

// importRules makes the daemon's rules match the file, by name: missing rules
// are created, differing ones updated in place (keeping their fired alerts),
// and the enabled flag is applied. Rules on the daemon that aren't in the file
// are left alone. A rule whose type differs from the daemon's same-named rule
// is skipped, since a rule's type can't change. Every rule is attempted; the
// error reports how many failed.
func importRules(c ruleClient, rules []api.AlertRuleMetric, dryRun bool, out io.Writer) error {
	current, err := c.GetAlertRules()
	if err != nil {
		return err
	}
	byName := make(map[string]api.AlertRuleMetric, len(current))
	for _, r := range current {
		byName[r.Name] = r
	}

	prefix := ""
	if dryRun {
		prefix = "would "
	}
	var failed, changed int
	for _, r := range rules {
		cur, exists := byName[r.Name]
		var action string
		var apply func() error
		switch {
		case !exists:
			action, apply = "create", func() error { return c.CreateAlertRule(r) }
		case cur.Type != r.Type:
			fmt.Fprintf(out, "skip    %s: it is a %s rule on this host, not %s (delete it first to change the type)\n", r.Name, cur.Type, r.Type)
			failed++
			continue
		case sameRule(cur, r):
			fmt.Fprintf(out, "ok      %s\n", r.Name)
			continue
		default:
			upd := r
			upd.ID = cur.ID
			action, apply = "update", func() error { return c.UpdateAlertRule(upd) }
		}
		changed++
		if dryRun {
			fmt.Fprintf(out, "%s%s %s\n", prefix, action, r.Name)
			continue
		}
		if err := apply(); err != nil {
			fmt.Fprintf(out, "failed  %s: %s: %v\n", r.Name, action, err)
			failed++
			continue
		}
		fmt.Fprintf(out, "%-7s %s\n", action+"d", r.Name)
	}

	// Create and update don't set the enabled flag (it's a separate toggle), so
	// reconcile it against the daemon's state after the writes.
	if !dryRun && changed > 0 {
		if current, err = c.GetAlertRules(); err != nil {
			return err
		}
	}
	state := make(map[string]api.AlertRuleMetric, len(current))
	for _, r := range current {
		state[r.Name] = r
	}
	for _, r := range rules {
		cur, ok := state[r.Name]
		if !ok {
			if !dryRun {
				continue // its create failed (already reported)
			}
			cur = api.AlertRuleMetric{Type: r.Type, Enabled: true} // would be created, enabled
		}
		if cur.Type != r.Type || cur.Enabled == r.Enabled {
			continue
		}
		verb := "disable"
		if r.Enabled {
			verb = "enable"
		}
		if dryRun {
			fmt.Fprintf(out, "%s%s %s\n", prefix, verb, r.Name)
			continue
		}
		if err := c.ToggleAlertRule(cur.ID); err != nil {
			fmt.Fprintf(out, "failed  %s: %s: %v\n", r.Name, verb, err)
			failed++
			continue
		}
		fmt.Fprintf(out, "%-7s %s\n", verb+"d", r.Name)
	}

	if failed > 0 {
		return fmt.Errorf("%d of %d rules could not be applied", failed, len(rules))
	}
	return nil
}

// sameRule reports whether a daemon rule already matches a file rule, ignoring
// the id and the enabled flag (reconciled separately). An unset aggregate
// means avg.
func sameRule(cur, want api.AlertRuleMetric) bool {
	norm := func(r api.AlertRuleMetric) api.AlertRuleMetric {
		r.ID, r.Enabled = 0, false
		if r.Type == "threshold" && r.Aggregate == "" {
			r.Aggregate = "avg"
		}
		return r
	}
	return reflect.DeepEqual(norm(cur), norm(want))
}
