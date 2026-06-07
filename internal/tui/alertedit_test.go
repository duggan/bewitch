package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/duggan/bewitch/internal/api"
	"github.com/duggan/bewitch/internal/config"
)

// TestAlertFooterAlwaysVisible guards the regression where the shortcut help and the
// delete-confirmation prompt lived at the bottom of the scrollable viewport: once the
// fired-alerts table grew, they scrolled off-screen, so the help vanished and a delete
// could never be confirmed. They must now render in a fixed footer, always visible.
func TestAlertFooterAlwaysVisible(t *testing.T) {
	mc := newMockClient()
	mc.rules = []api.AlertRuleMetric{
		{ID: 1, Name: "disk_40", Type: "threshold", Severity: "warning", Enabled: true,
			Metric: "disk.used_pct", Operator: ">", Value: 40, Duration: "1m", Mount: "/"},
	}
	// Far more fired alerts than fit on screen, so a bottom-of-content footer would
	// scroll away.
	ts := time.Now()
	for i := 0; i < 80; i++ {
		mc.alerts = append(mc.alerts, api.AlertMetric{
			ID: i + 1, Timestamp: ts, RuleName: "disk_40", Severity: "warning",
			Message: "disk.used_pct 42.7 > 40.0 over 1m", Acknowledged: true,
		})
	}
	m := alertsModel(t, mc)

	if !strings.Contains(m.View(), "d:delete") {
		t.Error("help line missing from View() with a full fired-alerts table (must be a fixed footer)")
	}

	updated, _ := m.Update(key("d"))
	m = updated.(Model)
	if !m.alertConfirmDelete {
		t.Fatal("delete confirmation not armed after 'd'")
	}
	view := m.View()
	if !strings.Contains(view, "Delete rule") {
		t.Error("delete-confirm prompt missing from View() — it must render in the fixed footer, not scrollable content")
	}
	if strings.Contains(view, "d:delete") {
		t.Error("help line should yield to the confirm prompt while a delete is pending")
	}
}

// alertsModel builds a ready model on the alerts view with the given rules/alerts loaded.
func alertsModel(t *testing.T, mc *mockClient) Model {
	t.Helper()
	m := NewModel(mc, time.Second, config.DefaultHistoryRanges, DefaultCaptureSettings(), false)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = updated.(Model)
	m.current = viewAlerts
	m.refreshAlertRules()
	m.refreshAlertsData()
	return m
}

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

// TestFormRoundTrip verifies fromAlertRuleMetric -> toAlertRuleMetric preserves every
// rule type's fields losslessly (the centerpiece guard for editing).
func TestFormRoundTrip(t *testing.T) {
	cases := []api.AlertRuleMetric{
		{ID: 1, Name: "cpu_hot", Type: "threshold", Severity: "warning", Enabled: true,
			Metric: "cpu.aggregate", Operator: ">", Value: 90, Duration: "5m"},
		{ID: 2, Name: "disk_40", Type: "threshold", Severity: "warning", Enabled: true,
			Metric: "disk.used_pct", Operator: ">", Value: 40.5, Duration: "1m", Mount: "/"},
		{ID: 3, Name: "net_rx", Type: "threshold", Severity: "critical", Enabled: false,
			Metric: "network.rx", Operator: ">", Value: 1000000, Duration: "30s", InterfaceName: "eth0"},
		{ID: 4, Name: "net_tx", Type: "threshold", Severity: "warning", Enabled: true,
			Metric: "network.tx", Operator: ">", Value: 500000, Duration: "30s", InterfaceName: "eth0"},
		{ID: 5, Name: "temp_hot", Type: "threshold", Severity: "critical", Enabled: true,
			Metric: "temperature.sensor", Operator: ">", Value: 80, Duration: "2m", Sensor: "coretemp/0"},
		{ID: 6, Name: "gpu_util", Type: "threshold", Severity: "warning", Enabled: true,
			Metric: "gpu.utilization", Operator: ">", Value: 95, Duration: "5m", Sensor: "Intel UHD"},
		// gpu.temperature can't be created in the form; edit must preserve it.
		{ID: 7, Name: "gpu_temp", Type: "threshold", Severity: "critical", Enabled: true,
			Metric: "gpu.temperature", Operator: ">", Value: 85, Duration: "1m", Sensor: "NVIDIA"},
		{ID: 8, Name: "mem_thrash", Type: "variance", Severity: "warning", Enabled: true,
			Metric: "memory.variance", DeltaThreshold: 5.5, MinCount: 10, Duration: "5m"},
		{ID: 9, Name: "disk_fill", Type: "predictive", Severity: "warning", Enabled: true,
			Metric: "disk.used_pct", Mount: "/data", PredictHours: 72, ThresholdPct: 95},
		{ID: 10, Name: "nginx_down", Type: "process_down", Severity: "critical", Enabled: true,
			ProcessName: "nginx", ProcessPattern: "*/nginx*", MinInstances: 2, CheckDuration: "30s"},
		{ID: 11, Name: "worker_thrash", Type: "process_thrashing", Severity: "warning", Enabled: true,
			ProcessName: "worker", RestartThreshold: 5, RestartWindow: "5m"},
		{ID: 12, Name: "smart_realloc", Type: "threshold", Severity: "critical", Enabled: true,
			Metric: "smart.reallocated", Operator: ">", Value: 0, Duration: "10m"},
	}

	for _, in := range cases {
		t.Run(in.Name, func(t *testing.T) {
			out := fromAlertRuleMetric(in).toAlertRuleMetric()
			if out.ID != in.ID || out.Name != in.Name || out.Type != in.Type ||
				out.Severity != in.Severity || out.Enabled != in.Enabled || out.Metric != in.Metric {
				t.Errorf("base mismatch:\n in=%+v\nout=%+v", in, out)
			}
			switch in.Type {
			case "threshold":
				if out.Operator != in.Operator || out.Value != in.Value || out.Duration != in.Duration ||
					out.Mount != in.Mount || out.InterfaceName != in.InterfaceName || out.Sensor != in.Sensor {
					t.Errorf("threshold fields mismatch:\n in=%+v\nout=%+v", in, out)
				}
			case "variance":
				if out.DeltaThreshold != in.DeltaThreshold || out.MinCount != in.MinCount || out.Duration != in.Duration {
					t.Errorf("variance fields mismatch:\n in=%+v\nout=%+v", in, out)
				}
			case "predictive":
				if out.Mount != in.Mount || out.PredictHours != in.PredictHours || out.ThresholdPct != in.ThresholdPct {
					t.Errorf("predictive fields mismatch:\n in=%+v\nout=%+v", in, out)
				}
			case "process_down":
				if out.ProcessName != in.ProcessName || out.ProcessPattern != in.ProcessPattern ||
					out.MinInstances != in.MinInstances || out.CheckDuration != in.CheckDuration {
					t.Errorf("process_down fields mismatch:\n in=%+v\nout=%+v", in, out)
				}
			case "process_thrashing":
				if out.ProcessName != in.ProcessName || out.RestartThreshold != in.RestartThreshold ||
					out.RestartWindow != in.RestartWindow {
					t.Errorf("process_thrashing fields mismatch:\n in=%+v\nout=%+v", in, out)
				}
			}
		})
	}
}

// TestFormRoundTripAggregate verifies the per-rule aggregate survives the
// form's edit round-trip, and that an old rule with no stored aggregate comes
// back as "avg" rather than empty.
func TestFormRoundTripAggregate(t *testing.T) {
	for _, agg := range []string{"avg", "max", "min"} {
		in := api.AlertRuleMetric{ID: 1, Name: "cpu", Type: "threshold", Severity: "warning", Enabled: true,
			Metric: "cpu.aggregate", Operator: ">", Value: 90, Duration: "5m", Aggregate: agg}
		if out := fromAlertRuleMetric(in).toAlertRuleMetric(); out.Aggregate != agg {
			t.Errorf("aggregate %q did not round-trip, got %q", agg, out.Aggregate)
		}
	}
	in := api.AlertRuleMetric{ID: 2, Name: "old", Type: "threshold", Severity: "warning", Enabled: true,
		Metric: "disk.used_pct", Operator: ">", Value: 80, Duration: "5m", Mount: "/"}
	if out := fromAlertRuleMetric(in).toAlertRuleMetric(); out.Aggregate != "avg" {
		t.Errorf("empty aggregate must default to avg, got %q", out.Aggregate)
	}
}

// TestViewFooterAlwaysVisible verifies the per-view shortcut help renders in the
// fixed footer (outside the scrollable viewport) for the interactive views, so it
// stays visible even when a long table overflows the screen — the same class of bug
// as the alerts view, generalized to Process / Network / Hardware.
func TestViewFooterAlwaysVisible(t *testing.T) {
	mc := newMockClient()
	// Enough processes to overflow the viewport (a bottom-of-content footer would
	// scroll off).
	var procs []api.ProcessMetric
	for i := 0; i < 80; i++ {
		procs = append(procs, api.ProcessMetric{PID: int32(i + 1), Name: "proc", State: "S", CPUUserPct: 1})
	}
	mc.procs = &api.ProcessResponse{Processes: procs, TotalProcs: int32(len(procs))}
	mc.net = []api.NetworkMetric{{Interface: "eth0", RxBytesSec: 100}, {Interface: "eth1"}}

	build := func(v view) Model {
		m := NewModel(mc, time.Second, config.DefaultHistoryRanges, DefaultCaptureSettings(), false)
		updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 28})
		m = updated.(Model)
		m.current = v
		return m
	}

	t.Run("process", func(t *testing.T) {
		m := build(viewProcess)
		m.refreshProcessData()
		out := m.View()
		for _, want := range []string{"/:search", "c:cpu", "f:fds"} {
			if !strings.Contains(out, want) {
				t.Errorf("process footer missing %q from View() (must be a fixed footer)", want)
			}
		}
	})

	t.Run("network", func(t *testing.T) {
		m := build(viewNetwork)
		m.refreshNetData()
		out := m.View()
		for _, want := range []string{"↑↓:navigate", "space:toggle", "a:all"} {
			if !strings.Contains(out, want) {
				t.Errorf("network footer missing %q from View()", want)
			}
		}
	})

	t.Run("hardware temperature section with data", func(t *testing.T) {
		m := build(viewHardware)
		m.hardwareSection = hwSectionTemp
		m.tempData = []api.TemperatureMetric{{Sensor: "coretemp/0", TempCelsius: 50}}
		if !strings.Contains(m.View(), "space:toggle") {
			t.Error("hardware selection help missing from View() when the section has data")
		}
	})

	t.Run("hardware ECC section has no selection footer", func(t *testing.T) {
		m := build(viewHardware)
		m.hardwareSection = hwSectionECC
		if strings.Contains(m.View(), "space:toggle") {
			t.Error("ECC section must not show per-item selection help")
		}
	})
}

// TestFromAlertRuleMetricCategory checks the derived category/direction used to render
// the locked parameter group.
func TestFromAlertRuleMetricCategory(t *testing.T) {
	cases := []struct {
		metric, ruleType string
		wantCat, wantDir string
	}{
		{"cpu.aggregate", "threshold", "cpu", ""},
		{"disk.used_pct", "threshold", "disk", ""},
		{"network.rx", "threshold", "network", "rx"},
		{"network.tx", "threshold", "network", "tx"},
		{"temperature.sensor", "threshold", "temperature", ""},
		{"gpu.utilization", "threshold", "gpu", ""},
		{"smart.reallocated", "threshold", "smart", ""},
		{"smart.unhealthy", "threshold", "smart", ""},
		{"memory.variance", "variance", "memory", ""},
		{"disk.used_pct", "predictive", "disk", ""},
	}
	for _, c := range cases {
		s := fromAlertRuleMetric(api.AlertRuleMetric{Type: c.ruleType, Metric: c.metric})
		if s.category != c.wantCat {
			t.Errorf("%s/%s: category=%q want %q", c.ruleType, c.metric, s.category, c.wantCat)
		}
		if s.direction != c.wantDir {
			t.Errorf("%s/%s: direction=%q want %q", c.ruleType, c.metric, s.direction, c.wantDir)
		}
	}
}

// TestEditKeyOpensPrePopulatedForm verifies 'e' opens the form in edit mode pre-filled
// from the selected rule, with the type-selection steps skipped.
func TestEditKeyOpensPrePopulatedForm(t *testing.T) {
	mc := newMockClient()
	mc.rules = []api.AlertRuleMetric{
		{ID: 42, Name: "disk_40", Type: "threshold", Severity: "warning", Enabled: true,
			Metric: "disk.used_pct", Operator: ">", Value: 40.5, Duration: "1m", Mount: "/"},
	}
	m := alertsModel(t, mc)
	if len(m.alertRules) != 1 {
		t.Fatalf("expected 1 rule loaded, got %d", len(m.alertRules))
	}

	updated, _ := m.Update(key("e"))
	m = updated.(Model)

	if !m.alertFormActive || m.alertFormState == nil {
		t.Fatal("expected edit form to open")
	}
	if m.alertFormState.editID != 42 {
		t.Errorf("editID = %d, want 42", m.alertFormState.editID)
	}
	if m.alertFormState.category != "disk" || m.alertFormState.valueStr != "40.5" {
		t.Errorf("form not pre-populated: cat=%q value=%q", m.alertFormState.category, m.alertFormState.valueStr)
	}
}

// TestDeleteConfirmation verifies 'd' arms confirmation (no immediate delete), a non-'y'
// key cancels, and 'd' then 'y' confirms.
func TestDeleteConfirmation(t *testing.T) {
	mkModel := func() (Model, *mockClient) {
		mc := newMockClient()
		mc.rules = []api.AlertRuleMetric{
			{ID: 7, Name: "disk_40", Type: "threshold", Severity: "warning", Enabled: true, Metric: "disk.used_pct"},
		}
		return alertsModel(t, mc), mc
	}

	// 'd' arms but does not delete.
	m, _ := mkModel()
	updated, _ := m.Update(key("d"))
	m = updated.(Model)
	if !m.alertConfirmDelete || m.alertConfirmName != "disk_40" {
		t.Fatalf("expected confirmation armed for disk_40, got armed=%v name=%q", m.alertConfirmDelete, m.alertConfirmName)
	}

	// A non-'y' key cancels.
	updated, _ = m.Update(key("x"))
	m = updated.(Model)
	if m.alertConfirmDelete {
		t.Error("expected confirmation cancelled by non-'y' key")
	}

	// 'd' then 'y' confirms and clears the prompt.
	m, mc := mkModel()
	updated, _ = m.Update(key("d"))
	m = updated.(Model)
	updated, _ = m.Update(key("y"))
	m = updated.(Model)
	if m.alertConfirmDelete {
		t.Error("expected confirmation cleared after 'y'")
	}
	// DeleteAlertRule runs in a goroutine; give it a moment, then check it was called.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		mc.mu.Lock()
		n := len(mc.deletedRuleIDs)
		mc.mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	mc.mu.Lock()
	defer mc.mu.Unlock()
	if len(mc.deletedRuleIDs) != 1 || mc.deletedRuleIDs[0] != 7 {
		t.Errorf("expected DeleteAlertRule(7), got %v", mc.deletedRuleIDs)
	}
}

// TestAckUsesCachedRow verifies acknowledging acks the row the user is looking at (the
// table cursor indexing the cached m.alertsData), not a freshly re-fetched, possibly
// reordered slice. To actually exercise that hazard, the mock reorders its alerts after
// the initial render: the fixed code must still ack ID 101 (row 0 as rendered), whereas
// the old re-fetch-and-reindex code would ack the reordered element (102).
func TestAckUsesCachedRow(t *testing.T) {
	mc := newMockClient()
	mc.alerts = []api.AlertMetric{
		{ID: 101, RuleName: "disk_40", Severity: "warning", Message: "high", Timestamp: time.Now()},
		{ID: 102, RuleName: "cpu_hot", Severity: "critical", Message: "hot", Timestamp: time.Now()},
	}
	m := alertsModel(t, mc) // refreshes m.alertsData = [101, 102]
	m.alertFocus = 1        // focus the fired-alerts table

	// Build the table rows so the table has content and a cursor at row 0 (=> ID 101).
	m.viewport.SetContent(m.renderCurrentContent())

	// Simulate the data reordering between render and keypress: any re-fetch now sees
	// [102, 101]. The cached m.alertsData the user is looking at is still [101, 102].
	mc.mu.Lock()
	mc.alerts = []api.AlertMetric{mc.alerts[1], mc.alerts[0]}
	mc.mu.Unlock()

	updated, _ := m.Update(key("enter"))
	_ = updated.(Model)

	mc.mu.Lock()
	defer mc.mu.Unlock()
	if len(mc.ackedIDs) != 1 || mc.ackedIDs[0] != 101 {
		t.Errorf("expected AckAlert(101) for the rendered row 0, got %v (re-fetch/re-index bug?)", mc.ackedIDs)
	}
}
