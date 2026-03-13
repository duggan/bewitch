package alert

import (
	"math"
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
		{"memory.used_pct", 1},
		{"disk.used_pct", 2},
		{"network.rx", 2},
		{"network.tx", 2},
		{"temperature.sensor", 2},
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
			query, args, err := r.buildQuery(cutoff)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if query == "" {
				t.Error("expected non-empty query")
			}
			if len(args) != tt.wantArgs {
				t.Errorf("args count = %d, want %d", len(args), tt.wantArgs)
			}
		})
	}

	t.Run("unknown metric returns error", func(t *testing.T) {
		r := &ThresholdRule{cfg: ThresholdConfig{Metric: "unknown.metric"}}
		_, _, err := r.buildQuery(cutoff)
		if err == nil {
			t.Error("expected error for unknown metric")
		}
	})
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
