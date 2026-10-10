package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/duggan/bewitch/internal/api"
)

// The top Y label must not be clipped: ntcharts reserved the label column from
// the tick positions it stepped through, which could skip the top tick, so
// "100%" rendered as "00%".
func TestChartTopYLabelNotClipped(t *testing.T) {
	end := time.Unix(1_800_000_000, 0)
	start := end.Add(-time.Hour)
	var pts []api.TimeSeriesPoint
	for ts := start; !ts.After(end); ts = ts.Add(time.Minute) {
		pts = append(pts, api.TimeSeriesPoint{TimestampNS: ts.UnixNano(), Value: 50})
	}
	for _, h := range []int{8, 10, 12, 13, 17, 20} {
		out := renderBrailleChart(chartConfig{
			series: []api.TimeSeries{{Label: "cpu", Points: pts}},
			start:  start, end: end, width: 100, height: h,
			yMin: 0, yMax: 100, yFormatter: yFmtPercent,
		})
		if !strings.Contains(out, "100%") {
			t.Errorf("height %d: top label clipped:\n%s", h, out)
		}
	}
}
