package tui

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// palette holds a complete set of UI colors with ANSI fallbacks.
type palette struct {
	Pink, Purple, Magenta, Lavender, DeepPurple lipgloss.CompleteColor
	Muted, Text, DarkPurple, Orange, Red, Green lipgloss.CompleteColor
	DarkBg, Dim, Dimmer, DebugBdr, DebugText    lipgloss.CompleteColor
	GradientStart, GradientEnd                   string // raw hex for progress bar
}

// Default palette — warm pink/purple, tuned for P3 displays (Terminal.app).
var defaultPalette = palette{
	Pink:       lipgloss.CompleteColor{TrueColor: "#FF6EC7", ANSI256: "206", ANSI: "13"},
	Purple:     lipgloss.CompleteColor{TrueColor: "#BB86FC", ANSI256: "141", ANSI: "5"},
	Magenta:    lipgloss.CompleteColor{TrueColor: "#E040FB", ANSI256: "170", ANSI: "13"},
	Lavender:   lipgloss.CompleteColor{TrueColor: "#CF6EFF", ANSI256: "177", ANSI: "5"},
	DeepPurple: lipgloss.CompleteColor{TrueColor: "#7C4DFF", ANSI256: "99", ANSI: "4"},
	Muted:      lipgloss.CompleteColor{TrueColor: "#9E8CBA", ANSI256: "139", ANSI: "8"},
	Text:       lipgloss.CompleteColor{TrueColor: "#F8F8F2", ANSI256: "15", ANSI: "15"},
	DarkPurple: lipgloss.CompleteColor{TrueColor: "#3D2B56", ANSI256: "53", ANSI: "0"},
	Orange:     lipgloss.CompleteColor{TrueColor: "#FFB86C", ANSI256: "215", ANSI: "11"},
	Red:        lipgloss.CompleteColor{TrueColor: "#FF5555", ANSI256: "203", ANSI: "9"},
	Green:      lipgloss.CompleteColor{TrueColor: "#50FA7B", ANSI256: "84", ANSI: "10"},
	DarkBg:     lipgloss.CompleteColor{TrueColor: "#1A1A2E", ANSI256: "234", ANSI: "0"},
	Dim:        lipgloss.CompleteColor{TrueColor: "#666666", ANSI256: "242", ANSI: "8"},
	Dimmer:     lipgloss.CompleteColor{TrueColor: "#444444", ANSI256: "238", ANSI: "0"},
	DebugBdr:   lipgloss.CompleteColor{TrueColor: "#555555", ANSI256: "240", ANSI: "8"},
	DebugText:  lipgloss.CompleteColor{TrueColor: "#888888", ANSI256: "245", ANSI: "8"},

	GradientStart: "#7C4DFF",
	GradientEnd:   "#FF6EC7",
}

// Ghostty palette — pre-warmed for sRGB to match P3 appearance.
// P3 shifts purples warm/pink and saturates pinks; these compensate
// by pushing hues toward magenta and boosting saturation.
var ghosttyPalette = palette{
	Pink:       lipgloss.CompleteColor{TrueColor: "#FF7AD4", ANSI256: "206", ANSI: "13"}, // warmer, more magenta
	Purple:     lipgloss.CompleteColor{TrueColor: "#C48DFF", ANSI256: "141", ANSI: "5"},  // redder purple
	Magenta:    lipgloss.CompleteColor{TrueColor: "#E94FFF", ANSI256: "170", ANSI: "13"}, // pushed toward pink
	Lavender:   lipgloss.CompleteColor{TrueColor: "#D87EFF", ANSI256: "177", ANSI: "5"},  // warmer lavender
	DeepPurple: lipgloss.CompleteColor{TrueColor: "#8B5CFF", ANSI256: "99", ANSI: "4"},   // slightly warmer
	Muted:      lipgloss.CompleteColor{TrueColor: "#A893C4", ANSI256: "139", ANSI: "8"},  // warmer muted
	Text:       lipgloss.CompleteColor{TrueColor: "#F8F8F2", ANSI256: "15", ANSI: "15"},  // unchanged
	DarkPurple: lipgloss.CompleteColor{TrueColor: "#3D2B56", ANSI256: "53", ANSI: "0"},   // unchanged
	Orange:     lipgloss.CompleteColor{TrueColor: "#FFB86C", ANSI256: "215", ANSI: "11"}, // unchanged
	Red:        lipgloss.CompleteColor{TrueColor: "#FF5555", ANSI256: "203", ANSI: "9"},  // unchanged
	Green:      lipgloss.CompleteColor{TrueColor: "#50FA7B", ANSI256: "84", ANSI: "10"},  // unchanged
	DarkBg:     lipgloss.CompleteColor{TrueColor: "#1A1A2E", ANSI256: "234", ANSI: "0"},  // unchanged
	Dim:        lipgloss.CompleteColor{TrueColor: "#666666", ANSI256: "242", ANSI: "8"},  // unchanged
	Dimmer:     lipgloss.CompleteColor{TrueColor: "#444444", ANSI256: "238", ANSI: "0"},  // unchanged
	DebugBdr:   lipgloss.CompleteColor{TrueColor: "#555555", ANSI256: "240", ANSI: "8"},  // unchanged
	DebugText:  lipgloss.CompleteColor{TrueColor: "#888888", ANSI256: "245", ANSI: "8"},  // unchanged

	GradientStart: "#8B5CFF",
	GradientEnd:   "#FF7AD4",
}

// iTerm2 palette — same as default for now, easy to tune later.
var itermPalette = defaultPalette

// Color variables — assigned in init() based on detected terminal.
var (
	colorPink       lipgloss.CompleteColor
	colorPurple     lipgloss.CompleteColor
	colorMagenta    lipgloss.CompleteColor
	colorLavender   lipgloss.CompleteColor
	colorDeepPurple lipgloss.CompleteColor
	colorMuted      lipgloss.CompleteColor
	colorText       lipgloss.CompleteColor
	colorDarkPurple lipgloss.CompleteColor
	colorOrange     lipgloss.CompleteColor
	colorRed        lipgloss.CompleteColor
	colorGreen      lipgloss.CompleteColor
	colorDarkBg     lipgloss.CompleteColor
	colorDim        lipgloss.CompleteColor
	colorDimmer     lipgloss.CompleteColor
	colorDebugBdr   lipgloss.CompleteColor
	colorDebugText  lipgloss.CompleteColor

	gradientStart string
	gradientEnd   string
)

// Style variables — assigned in init() after colors are set.
var (
	titleStyle          lipgloss.Style
	headerStyle         lipgloss.Style
	labelStyle          lipgloss.Style
	valueStyle          lipgloss.Style
	barFillStyle        lipgloss.Style
	barWarnStyle        lipgloss.Style
	barCritStyle        lipgloss.Style
	barEmptyStyle       lipgloss.Style
	helpStyle           lipgloss.Style
	sectionStyle        lipgloss.Style
	alertWarnStyle      lipgloss.Style
	alertCritStyle      lipgloss.Style
	panelBorder         lipgloss.Border
	panelStyle          lipgloss.Style
	activePanelStyle    lipgloss.Style
	activeTabStyle      lipgloss.Style
	inactiveTabStyle    lipgloss.Style
	highlightLabelStyle lipgloss.Style
	summaryStyle        lipgloss.Style
	rxArrowStyle        lipgloss.Style
	txArrowStyle        lipgloss.Style
	dimStyle            lipgloss.Style
	selectedRowStyle    lipgloss.Style
)

func init() {
	p := defaultPalette
	switch os.Getenv("TERM_PROGRAM") {
	case "ghostty":
		p = ghosttyPalette
	case "iTerm.app":
		p = itermPalette
	}

	// Assign colors from selected palette
	colorPink = p.Pink
	colorPurple = p.Purple
	colorMagenta = p.Magenta
	colorLavender = p.Lavender
	colorDeepPurple = p.DeepPurple
	colorMuted = p.Muted
	colorText = p.Text
	colorDarkPurple = p.DarkPurple
	colorOrange = p.Orange
	colorRed = p.Red
	colorGreen = p.Green
	colorDarkBg = p.DarkBg
	colorDim = p.Dim
	colorDimmer = p.Dimmer
	colorDebugBdr = p.DebugBdr
	colorDebugText = p.DebugText
	gradientStart = p.GradientStart
	gradientEnd = p.GradientEnd

	// Create styles using the selected colors
	titleStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(colorPink).
		MarginBottom(1)

	headerStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(colorPurple)

	labelStyle = lipgloss.NewStyle().
		Foreground(colorMagenta).
		Width(14)

	valueStyle = lipgloss.NewStyle().
		Foreground(colorText)

	barFillStyle = lipgloss.NewStyle().
		Foreground(colorLavender)

	barWarnStyle = lipgloss.NewStyle().
		Foreground(colorOrange)

	barCritStyle = lipgloss.NewStyle().
		Foreground(colorRed)

	barEmptyStyle = lipgloss.NewStyle().
		Foreground(colorDarkPurple)

	helpStyle = lipgloss.NewStyle().
		Foreground(colorMuted).
		MarginTop(1)

	sectionStyle = lipgloss.NewStyle().
		MarginBottom(1)

	alertWarnStyle = lipgloss.NewStyle().
		Foreground(colorOrange)

	alertCritStyle = lipgloss.NewStyle().
		Foreground(colorRed)

	// Panel style: rounded border in deep purple
	panelBorder = lipgloss.RoundedBorder()

	panelStyle = lipgloss.NewStyle().
		Border(panelBorder).
		BorderForeground(colorDeepPurple).
		Padding(0, 1)

	// Active/highlighted panel
	activePanelStyle = lipgloss.NewStyle().
		Border(panelBorder).
		BorderForeground(colorPink).
		Padding(0, 1)

	// Tab bar styles
	activeTabStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(colorDarkBg).
		Background(colorPink).
		Padding(0, 1)

	inactiveTabStyle = lipgloss.NewStyle().
		Foreground(colorMuted).
		Padding(0, 1)

	// Aggregate/highlight row
	highlightLabelStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(colorPink).
		Width(14)

	// Summary line style
	summaryStyle = lipgloss.NewStyle().
		Foreground(colorMuted).
		Italic(true)

	// Arrow styles for network
	rxArrowStyle = lipgloss.NewStyle().Foreground(colorPurple)
	txArrowStyle = lipgloss.NewStyle().Foreground(colorPink)
	dimStyle = lipgloss.NewStyle().Foreground(colorDim)

	// Selected row style (hot pink background, dark text)
	selectedRowStyle = lipgloss.NewStyle().
		Foreground(colorDarkBg).
		Background(colorPink)
}

// tabInfo maps view constants to their display names
var tabInfo = map[view]struct {
	name      string
	shortName string
}{
	viewDashboard:   {"Dashboard", "Dash"},
	viewCPU:         {"CPU", "CPU"},
	viewMemory:      {"Memory", "Mem"},
	viewDisk:        {"Disk", "Disk"},
	viewNetwork:     {"Network", "Net"},
	viewTemperature: {"Temp", "Temp"},
	viewPower:       {"Power", "Pwr"},
	viewProcess:     {"Procs", "Proc"},
	viewAlerts:      {"Alerts", "Alert"},
}

func renderTabBar(active view, width int, visibleTabs []view) string {
	var prefix string
	if width >= 100 {
		sparkL := lipgloss.NewStyle().Foreground(colorLavender).Render("✧ ☽ ")
		title := lipgloss.NewStyle().Bold(true).Foreground(colorPink).Render("bewitch")
		sparkR := lipgloss.NewStyle().Foreground(colorLavender).Render(" ✦˚  ")
		prefix = sparkL + title + sparkR
	} else {
		prefix = lipgloss.NewStyle().Bold(true).Foreground(colorPink).Render("bewitch") + " "
	}

	var tabs []string
	for i, v := range visibleTabs {
		info := tabInfo[v]
		name := info.name
		if width < 100 {
			name = info.shortName
		}
		// Keys are 1-indexed based on position in visibleTabs
		key := fmt.Sprintf("%d", i+1)
		label := key + ":" + name
		if v == active {
			tabs = append(tabs, activeTabStyle.Render(label))
		} else {
			tabs = append(tabs, inactiveTabStyle.Render(label))
		}
	}
	bar := lipgloss.JoinHorizontal(lipgloss.Top, prefix, lipgloss.JoinHorizontal(lipgloss.Top, tabs...))
	line := lipgloss.NewStyle().
		Foreground(colorDeepPurple).
		Render(strings.Repeat("─", width))
	return bar + "\n" + line + "\n"
}

func renderPanel(title string, content string, width int) string {
	innerWidth := width - 2 // Width() includes padding but not border
	if innerWidth < 10 {
		innerWidth = 10
	}
	style := panelStyle.Width(innerWidth)
	if title != "" {
		titleStr := lipgloss.NewStyle().
			Bold(true).
			Foreground(colorPink).
			Render(" " + title + " ")
		return titleStr + "\n" + style.Render(content)
	}
	return style.Render(content)
}

// renderPanelRaw renders a panel with a pre-styled title (no additional formatting applied).
func renderPanelRaw(title string, content string, width int) string {
	innerWidth := width - 2 // Width() includes padding but not border
	if innerWidth < 10 {
		innerWidth = 10
	}
	style := panelStyle.Width(innerWidth)
	if title != "" {
		return " " + title + " \n" + style.Render(content)
	}
	return style.Render(content)
}

// renderBar renders a horizontal bar chart.
func renderBar(pct float64, width int) string {
	if width < 2 {
		width = 20
	}
	filled := int(pct / 100 * float64(width))
	if filled > width {
		filled = width
	}
	if filled < 0 {
		filled = 0
	}

	style := barFillStyle
	if pct > 90 {
		style = barCritStyle
	} else if pct > 70 {
		style = barWarnStyle
	}

	bar := style.Render(strings.Repeat("█", filled)) + barEmptyStyle.Render(strings.Repeat("▒", width-filled))
	return bar
}

func humanBits(b uint64) string {
	bits := b * 8
	const (
		kbit = 1000
		mbit = kbit * 1000
		gbit = mbit * 1000
		tbit = gbit * 1000
	)
	switch {
	case bits >= tbit:
		return fmt.Sprintf("%.1fTbps", float64(bits)/float64(tbit))
	case bits >= gbit:
		return fmt.Sprintf("%.1fGbps", float64(bits)/float64(gbit))
	case bits >= mbit:
		return fmt.Sprintf("%.1fMbps", float64(bits)/float64(mbit))
	case bits >= kbit:
		return fmt.Sprintf("%.1fKbps", float64(bits)/float64(kbit))
	default:
		return fmt.Sprintf("%dbps", bits)
	}
}

func humanBytes(b uint64) string {
	const (
		kb = 1024
		mb = kb * 1024
		gb = mb * 1024
		tb = gb * 1024
	)
	switch {
	case b >= tb:
		return fmt.Sprintf("%.1fT", float64(b)/float64(tb))
	case b >= gb:
		return fmt.Sprintf("%.1fG", float64(b)/float64(gb))
	case b >= mb:
		return fmt.Sprintf("%.1fM", float64(b)/float64(mb))
	case b >= kb:
		return fmt.Sprintf("%.1fK", float64(b)/float64(kb))
	default:
		return fmt.Sprintf("%dB", b)
	}
}

// collectorsForView returns the collector names relevant to each tab view.
var collectorsForView = map[view][]string{
	viewDashboard:   {"cpu", "memory", "disk", "network", "ecc", "temperature", "power", "process"},
	viewCPU:         {"cpu"},
	viewMemory:      {"memory", "ecc"},
	viewDisk:        {"disk"},
	viewNetwork:     {"network"},
	viewTemperature: {"temperature"},
	viewPower:       {"power"},
	viewProcess:     {"process"},
}

var collectorDisplayNames = map[string]string{
	"cpu": "CPU", "memory": "Memory", "disk": "Disk", "network": "Network",
	"ecc": "ECC", "temperature": "Temperature", "power": "Power", "process": "Process",
}

func buildStatusBar(status map[string]any, current view, lastChange time.Time) string {
	names, ok := collectorsForView[current]
	if !ok {
		return ""
	}
	intervals, ok := status["collector_intervals"].(map[string]string)
	if !ok || len(intervals) == 0 {
		return ""
	}
	var text string
	if len(names) == 1 {
		if v, ok := intervals[names[0]]; ok {
			text = "Collection interval: " + v
		}
	} else {
		var parts []string
		for _, name := range names {
			if v, ok := intervals[name]; ok {
				display := collectorDisplayNames[name]
				parts = append(parts, display+" "+v)
			}
		}
		if len(parts) > 0 {
			text = "Collection intervals: " + strings.Join(parts, ", ")
		}
	}
	if text == "" {
		return ""
	}

	// Append staleness indicator when data is older than 3× the longest collector interval
	if !lastChange.IsZero() {
		var maxInterval time.Duration
		for _, name := range names {
			if v, ok := intervals[name]; ok {
				if d, err := time.ParseDuration(v); err == nil && d > maxInterval {
					maxInterval = d
				}
			}
		}
		if maxInterval > 0 {
			age := time.Since(lastChange)
			if age > 3*maxInterval {
				text += fmt.Sprintf(" · stale (%ds ago)", int(age.Seconds()))
			}
		}
	}

	return text
}

func renderStatusBar(text string, width int) string {
	if text == "" {
		return ""
	}
	return lipgloss.NewStyle().
		Background(colorDeepPurple).
		Foreground(colorDarkBg).
		Width(width).
		Render(" " + text)
}

func xLabelFormatter(duration time.Duration) func(int, float64) string {
	return func(_ int, v float64) string {
		t := time.Unix(int64(v), 0)
		switch {
		case duration <= 24*time.Hour:
			return t.Format("15:04")
		case duration <= 7*24*time.Hour:
			return t.Format("Mon 15:04")
		default:
			return t.Format("01/02")
		}
	}
}
