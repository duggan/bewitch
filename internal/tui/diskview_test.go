package tui

import "testing"

func TestHumanCount(t *testing.T) {
	tests := []struct {
		input uint64
		want  string
	}{
		{0, "0"},
		{1, "1"},
		{10, "10"},
		{100, "100"},
		{999, "999"},
		{1000, "1,000"},
		{12345, "12,345"},
		{100000, "100,000"},
		{1234567, "1,234,567"},
		{1000000000, "1,000,000,000"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := humanCount(tt.input)
			if got != tt.want {
				t.Errorf("humanCount(%d) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
