package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/ross/bewitch/internal/api"

	"github.com/NimbleMarkets/ntcharts/linechart/timeserieslinechart"
	"github.com/charmbracelet/lipgloss"
)

var chartColors = []string{
	"84",  // green
	"215", // orange
	"123", // cyan
	"141", // soft purple
	"228", // yellow
	"203", // red
	"75",  // sky blue
	"177", // lavender
	"43",  // mint
	"117", // pale cyan
	"183", // light purple
	"209", // light salmon
	"114", // seafoam
	"220", // golden
	"110", // light sky blue
}

// chartConfig holds all parameters for rendering a braille time-series chart.
type chartConfig struct {
	series     []api.TimeSeries
	width      int
	height     int
	start, end time.Time
	yMin, yMax float64
	yFormatter func(int, float64) string

	// labelTransform transforms series labels for the legend (e.g., "_rx" -> " rx").
	// If nil, labels are used as-is.
	labelTransform func(string) string

	// pinnedMap marks series with a pin indicator (*) in the legend.
	// If nil, no pin indicators are shown.
	pinnedMap map[string]bool

	// staleDropoff, if > 0, causes series whose last data point is older than
	// this duration before the chart end to drop to zero instead of forward-filling.
	// Used for process charts where stopped processes should not forward-fill.
	staleDropoff time.Duration
}

// renderBrailleChart renders a braille time-series line chart with legend.
func renderBrailleChart(cfg chartConfig) string {
	chartWidth := cfg.width
	if chartWidth < 20 {
		chartWidth = 20
	}
	chartHeight := cfg.height
	if chartHeight < 4 {
		chartHeight = 4
	}

	opts := []timeserieslinechart.Option{
		timeserieslinechart.WithTimeRange(cfg.start, cfg.end),
		timeserieslinechart.WithYRange(cfg.yMin, cfg.yMax),
		timeserieslinechart.WithXLabelFormatter(xLabelFormatter(cfg.end.Sub(cfg.start))),
		timeserieslinechart.WithYLabelFormatter(cfg.yFormatter),
	}

	for i, s := range cfg.series {
		if len(s.Points) == 0 {
			continue
		}
		color := chartColors[i%len(chartColors)]
		style := lipgloss.NewStyle().Foreground(lipgloss.Color(color))
		points := make([]timeserieslinechart.TimePoint, len(s.Points))
		for j, p := range s.Points {
			points[j] = timeserieslinechart.TimePoint{
				Time:  time.Unix(0, p.TimestampNS),
				Value: p.Value,
			}
		}
		if cfg.staleDropoff > 0 {
			lastTime := points[len(points)-1].Time
			if cfg.end.Sub(lastTime) > cfg.staleDropoff {
				// Process stopped — drop to zero one bucket after last real data.
				points = append(points, timeserieslinechart.TimePoint{
					Time:  lastTime.Add(cfg.staleDropoff / 2),
					Value: 0,
				})
			} else {
				points = forwardFillPoints(points, cfg.end)
			}
		} else {
			points = forwardFillPoints(points, cfg.end)
		}
		opts = append(opts,
			timeserieslinechart.WithDataSetStyle(s.Label, style),
			timeserieslinechart.WithDataSetTimeSeries(s.Label, points),
		)
	}

	chart := timeserieslinechart.New(chartWidth, chartHeight, opts...)
	chart.DrawBrailleAll()

	// Build legend
	var legend strings.Builder
	for i, s := range cfg.series {
		if len(s.Points) == 0 {
			continue
		}
		color := chartColors[i%len(chartColors)]
		style := lipgloss.NewStyle().Foreground(lipgloss.Color(color))
		if i > 0 {
			legend.WriteString("  ")
		}
		label := s.Label
		if cfg.labelTransform != nil {
			label = cfg.labelTransform(label)
		}
		if cfg.pinnedMap != nil && cfg.pinnedMap[s.Label] {
			label = "* " + label
		}
		legend.WriteString(style.Render("━ " + label))
	}

	return chart.View() + "\n" + legend.String()
}

// forwardFillPoints extends the last point's value to the given end time,
// preventing a "drop to zero" artifact when the chart time range extends
// beyond the last data point.
func forwardFillPoints(points []timeserieslinechart.TimePoint, end time.Time) []timeserieslinechart.TimePoint {
	if len(points) > 0 && points[len(points)-1].Time.Before(end) {
		points = append(points, timeserieslinechart.TimePoint{
			Time:  end,
			Value: points[len(points)-1].Value,
		})
	}
	return points
}

// chartHeightForTerminal computes chart height based on terminal height.
func chartHeightForTerminal(termHeight int) int {
	h := termHeight / 3
	if h < 8 {
		h = 8
	}
	if h > 24 {
		h = 24
	}
	return h
}

// Percentage Y-axis formatter (0%, 50%, 100%).
func yFmtPercent(_ int, v float64) string {
	return fmt.Sprintf("%.0f%%", v)
}

// Temperature Y-axis formatter.
func yFmtCelsius(_ int, v float64) string {
	return fmt.Sprintf("%.0f°C", v)
}

// Power Y-axis formatter (W / kW).
func yFmtWatts(_ int, v float64) string {
	if v >= 1000 {
		return fmt.Sprintf("%.1fkW", v/1000)
	}
	return fmt.Sprintf("%.0fW", v)
}

// Network Y-axis formatter.
func yFmtNetBytes(_ int, v float64) string {
	return humanBytes(uint64(v)) + "/s"
}

func yFmtNetBits(_ int, v float64) string {
	return humanBits(uint64(v))
}

// netLabelTransform converts "iface_rx" -> "iface rx", "iface_tx" -> "iface tx".
func netLabelTransform(label string) string {
	label = strings.Replace(label, "_rx", " rx", 1)
	label = strings.Replace(label, "_tx", " tx", 1)
	return label
}

// autoMaxY finds the maximum value across all series, applies a floor and rounding.
func autoMaxY(series []api.TimeSeries, floor float64, roundTo float64) float64 {
	maxVal := 0.0
	for _, s := range series {
		for _, p := range s.Points {
			if p.Value > maxVal {
				maxVal = p.Value
			}
		}
	}
	if maxVal < floor {
		maxVal = floor
	}
	if roundTo > 0 {
		maxVal = float64(int(maxVal/roundTo)+1) * roundTo
	} else {
		maxVal = maxVal * 1.1
	}
	return maxVal
}

// renderPercentChart renders a 0-100% braille chart (used by CPU, Memory, Disk views).
func renderPercentChart(series []api.TimeSeries, width, height int, start, end time.Time) string {
	return renderBrailleChart(chartConfig{
		series:     series,
		width:      width,
		height:     height,
		start:      start,
		end:        end,
		yMin:       0,
		yMax:       100,
		yFormatter: yFmtPercent,
	})
}

func historyHelp(rangeLabel string) string {
	return helpStyle.Render(fmt.Sprintf("< >: range [%s]  r: pick dates", rangeLabel))
}

// historyHelpInline returns help text styled for inclusion inside a panel (with spacing above).
func historyHelpInline(rangeLabel string) string {
	return "\n\n" + lipgloss.NewStyle().Foreground(colorDeepPurple).Render(
		fmt.Sprintf("< >:range [%s]  r:pick dates", rangeLabel))
}
