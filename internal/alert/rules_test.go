package alert

import (
	"math"
	"strings"
	"testing"
	"time"
)

func TestLinearRegression(t *testing.T) {
	tests := []struct {
		name          string
		xs, ys        []float64
		wantSlope     float64
		wantIntercept float64
		tolerance     float64
	}{
		{
			"perfect positive line",
			[]float64{1, 2, 3}, []float64{2, 4, 6},
			2.0, 0.0, 1e-9,
		},
		{
			"flat line",
			[]float64{1, 2, 3}, []float64{5, 5, 5},
			0.0, 5.0, 1e-9,
		},
		{
			"negative slope",
			[]float64{1, 2, 3}, []float64{6, 4, 2},
			-2.0, 8.0, 1e-9,
		},
		{
			"two points",
			[]float64{0, 10}, []float64{0, 10},
			1.0, 0.0, 1e-9,
		},
		{
			"near zero denominator (identical x)",
			[]float64{1, 1, 1}, []float64{1, 2, 3},
			0.0, 0.0, 1e-9,
		},
		{
			"large unix timestamps",
			[]float64{1700000000, 1700003600}, []float64{50, 60},
			10.0 / 3600.0, 0, 1.0, // intercept will be large; just check slope
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			slope, intercept := linearRegression(tt.xs, tt.ys)
			if math.Abs(slope-tt.wantSlope) > tt.tolerance {
				t.Errorf("slope = %f, want %f", slope, tt.wantSlope)
			}
			// Skip intercept check for large timestamp case (intercept is huge)
			if tt.name != "large unix timestamps" {
				if math.Abs(intercept-tt.wantIntercept) > tt.tolerance {
					t.Errorf("intercept = %f, want %f", intercept, tt.wantIntercept)
				}
			}
		})
	}
}

func TestGlobToSQL(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"nginx*", "nginx%"},
		{"post?res", "post_res"},
		{"*", "%"},
		{"literal", "literal"},
		{"100%", "100\\%"},
		{"some_thing", "some\\_thing"},
		{"nginx*worker?", "nginx%worker_"},
		{"*foo*bar*", "%foo%bar%"},
		{"", ""},
		{"no-wildcards", "no-wildcards"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := globToSQL(tt.input)
			if got != tt.want {
				t.Errorf("globToSQL(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestThresholdRuleCompare(t *testing.T) {
	tests := []struct {
		op   string
		val  float64
		in   float64
		want bool
	}{
		{">", 90.0, 91.0, true},
		{">", 90.0, 90.0, false},
		{">", 90.0, 89.0, false},
		{">=", 90.0, 90.0, true},
		{">=", 90.0, 91.0, true},
		{">=", 90.0, 89.0, false},
		{"<", 10.0, 9.0, true},
		{"<", 10.0, 10.0, false},
		{"<=", 10.0, 10.0, true},
		{"<=", 10.0, 11.0, false},
		{"invalid", 10.0, 5.0, false},
		{"", 10.0, 5.0, false},
	}
	for _, tt := range tests {
		t.Run(tt.op, func(t *testing.T) {
			r := &ThresholdRule{
				cfg: ThresholdConfig{Operator: tt.op, Value: tt.val},
			}
			if got := r.compare(tt.in); got != tt.want {
				t.Errorf("compare(%f) with op=%q val=%f = %v, want %v", tt.in, tt.op, tt.val, got, tt.want)
			}
		})
	}
}

func TestThresholdRuleBuildQuery(t *testing.T) {
	cutoff := time.Now()

	validMetrics := []struct {
		metric   string
		wantArgs int
	}{
		{"cpu.aggregate", 1},
		{"cpu.steal", 1},
		{"memory.used_pct", 1},
		{"disk.used_pct", 2},
		{"network.rx", 2},
		{"network.tx", 2},
		{"temperature.sensor", 2},
		{"smart.reallocated", 1},
		{"smart.pending", 1},
		{"smart.uncorrectable", 1},
		{"smart.percent_used", 1},
		{"smart.unhealthy", 1},
	}

	for _, tt := range validMetrics {
		t.Run(tt.metric, func(t *testing.T) {
			r := &ThresholdRule{
				cfg: ThresholdConfig{
					Metric:        tt.metric,
					Mount:         "/",
					InterfaceName: "eth0",
					Sensor:        "coretemp",
				},
			}
			query, args, agg, err := r.buildQuery(cutoff)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if query == "" {
				t.Error("expected non-empty query")
			}
			if len(args) != tt.wantArgs {
				t.Errorf("args count = %d, want %d", len(args), tt.wantArgs)
			}
			// Aggregate label must match the actual SQL aggregate so the fired
			// message is truthful: SMART branches use MAX/COUNT, others AVG.
			wantAgg := "avg"
			switch tt.metric {
			case "smart.reallocated", "smart.pending", "smart.uncorrectable", "smart.percent_used":
				wantAgg = "max"
			case "smart.unhealthy":
				wantAgg = "count"
			}
			if agg != wantAgg {
				t.Errorf("agg label = %q, want %q", agg, wantAgg)
			}
		})
	}

	t.Run("unknown metric returns error", func(t *testing.T) {
		r := &ThresholdRule{cfg: ThresholdConfig{Metric: "unknown.metric"}}
		_, _, _, err := r.buildQuery(cutoff)
		if err == nil {
			t.Error("expected error for unknown metric")
		}
	})
}

// TestThresholdAggregateChoice verifies the per-rule aggregate threads into the
// SQL function and the message label for value metrics, defaults to avg on
// empty/unknown, and is IGNORED by the SMART metrics (intrinsic MAX/COUNT).
func TestThresholdAggregateChoice(t *testing.T) {
	cutoff := time.Now()
	cases := []struct {
		name, metric, aggregate, wantLabel, wantFn string
	}{
		{"cpu avg", "cpu.aggregate", "avg", "avg", "AVG("},
		{"cpu max", "cpu.aggregate", "max", "max", "MAX("},
		{"cpu min", "cpu.aggregate", "min", "min", "MIN("},
		{"cpu.steal max", "cpu.steal", "max", "max", "MAX("},
		{"cpu empty defaults avg", "cpu.aggregate", "", "avg", "AVG("},
		{"cpu unknown defaults avg", "cpu.aggregate", "bogus", "avg", "AVG("},
		{"memory min", "memory.used_pct", "min", "min", "MIN("},
		{"disk max", "disk.used_pct", "max", "max", "MAX("},
		{"network.rx max", "network.rx", "max", "max", "MAX("},
		{"temperature max", "temperature.sensor", "max", "max", "MAX("},
		{"gpu.utilization max", "gpu.utilization", "max", "max", "MAX("},
		// SMART ignores the user's choice and keeps its intrinsic aggregate.
		{"smart.reallocated ignores avg", "smart.reallocated", "avg", "max", "MAX("},
		{"smart.percent_used ignores min", "smart.percent_used", "min", "max", "MAX("},
		{"smart.unhealthy ignores max", "smart.unhealthy", "max", "count", "COUNT("},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := &ThresholdRule{cfg: ThresholdConfig{
				Metric: c.metric, Mount: "/", InterfaceName: "eth0", Sensor: "s", Aggregate: c.aggregate,
			}}
			query, _, agg, err := r.buildQuery(cutoff)
			if err != nil {
				t.Fatalf("buildQuery: %v", err)
			}
			if agg != c.wantLabel {
				t.Errorf("agg label = %q, want %q", agg, c.wantLabel)
			}
			if !strings.Contains(query, c.wantFn) {
				t.Errorf("query missing %q:\n%s", c.wantFn, query)
			}
		})
	}
}

func TestRuleConstructorsAndName(t *testing.T) {
	base := AlertRuleBase{ID: 1, Name: "test-rule", Type: "threshold", Severity: "warning", Enabled: true}

	threshold := NewThresholdRule(base, ThresholdConfig{})
	if threshold.Name() != "test-rule" {
		t.Errorf("ThresholdRule.Name() = %q", threshold.Name())
	}

	predictive := NewPredictiveRule(base, PredictiveConfig{})
	if predictive.Name() != "test-rule" {
		t.Errorf("PredictiveRule.Name() = %q", predictive.Name())
	}

	variance := NewVarianceRule(base, VarianceConfig{})
	if variance.Name() != "test-rule" {
		t.Errorf("VarianceRule.Name() = %q", variance.Name())
	}

	procDown := NewProcessDownRule(base, ProcessDownConfig{})
	if procDown.Name() != "test-rule" {
		t.Errorf("ProcessDownRule.Name() = %q", procDown.Name())
	}

	procThrash := NewProcessThrashingRule(base, ProcessThrashingConfig{})
	if procThrash.Name() != "test-rule" {
		t.Errorf("ProcessThrashingRule.Name() = %q", procThrash.Name())
	}
}

func TestVarianceMetricSupported(t *testing.T) {
	// Variance is memory-only; non-memory metrics must be rejected so a
	// hand-edited rule errors loudly instead of silently running memory variance.
	for _, m := range []string{"memory.variance", "memory.used_pct", ""} {
		if !varianceMetricSupported(m) {
			t.Errorf("varianceMetricSupported(%q) = false, want true", m)
		}
	}
	for _, m := range []string{"cpu.aggregate", "disk.used_pct", "network.rx", "smart.reallocated"} {
		if varianceMetricSupported(m) {
			t.Errorf("varianceMetricSupported(%q) = true, want false", m)
		}
	}
}

func TestVarianceRuleRejectsNonMemoryMetric(t *testing.T) {
	// The guard runs before any DB access, so a nil db is never dereferenced.
	r := &VarianceRule{cfg: VarianceConfig{Metric: "cpu.aggregate", Duration: "5m", MinCount: 1}}
	alert, err := r.Evaluate(nil)
	if err == nil {
		t.Fatal("expected an error for a non-memory variance metric, got nil")
	}
	if alert != nil {
		t.Errorf("expected nil alert on error, got %+v", alert)
	}
	if !strings.Contains(err.Error(), "variance only supports memory") {
		t.Errorf("error = %q, want it to mention memory-only", err.Error())
	}
}
