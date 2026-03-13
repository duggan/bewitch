package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

const (
	debugMinHeight     = 5  // minimum panel height (3 content + 2 border)
	debugMaxHeight     = 20 // maximum panel height
	debugDefaultHeight = 5  // default panel height
)

// debugLog is a ring buffer of recent debug messages displayed in the TUI.
type debugLog struct {
	lines  []string
	max    int
	height int // current panel height (including 2 border lines)
	scroll int // offset from bottom (0 = latest)
}

func newDebugLog(max int) *debugLog {
	return &debugLog{max: max, height: debugDefaultHeight}
}

// Printf appends a timestamped message to the debug log.
func (d *debugLog) Printf(format string, args ...any) {
	ts := time.Now().Format("15:04:05")
	msg := fmt.Sprintf(format, args...)
	line := ts + " " + msg
	d.lines = append(d.lines, line)
	if len(d.lines) > d.max {
		d.lines = d.lines[len(d.lines)-d.max:]
	}
	// Auto-scroll to bottom on new message (unless user scrolled up)
	if d.scroll > 0 {
		d.scroll++
	}
}

// Grow increases panel height by 1 line, up to debugMaxHeight.
func (d *debugLog) Grow() {
	if d.height < debugMaxHeight {
		d.height++
	}
}

// Shrink decreases panel height by 1 line, down to debugMinHeight.
func (d *debugLog) Shrink() {
	if d.height > debugMinHeight {
		d.height--
		// Clamp scroll offset
		d.clampScroll()
	}
}

// ScrollUp scrolls the debug log up (toward older messages).
func (d *debugLog) ScrollUp() {
	visibleLines := d.height - 2
	maxScroll := len(d.lines) - visibleLines
	if maxScroll < 0 {
		maxScroll = 0
	}
	if d.scroll < maxScroll {
		d.scroll++
	}
}

// ScrollDown scrolls the debug log down (toward newer messages).
func (d *debugLog) ScrollDown() {
	if d.scroll > 0 {
		d.scroll--
	}
}

// ScrollToBottom resets scroll to show the latest messages.
func (d *debugLog) ScrollToBottom() {
	d.scroll = 0
}

func (d *debugLog) clampScroll() {
	visibleLines := d.height - 2
	maxScroll := len(d.lines) - visibleLines
	if maxScroll < 0 {
		maxScroll = 0
	}
	if d.scroll > maxScroll {
		d.scroll = maxScroll
	}
}

// Height returns the total rendered height (title line + bordered panel).
func (d *debugLog) Height() int {
	return d.height + 1 // +1 for title line above border
}

// Render returns a bordered panel showing debug log lines.
func (d *debugLog) Render(width int) string {
	visibleLines := d.height - 2 // subtract top+bottom border

	end := len(d.lines) - d.scroll
	if end < 0 {
		end = 0
	}
	start := end - visibleLines
	if start < 0 {
		start = 0
	}

	var content string
	if end > start {
		content = strings.Join(d.lines[start:end], "\n")
	}
	// Pad to fixed height so the panel doesn't jump
	shown := end - start
	for shown < visibleLines {
		content += "\n"
		shown++
	}

	// Show scroll position indicator in border title
	title := "Debug"
	if d.scroll > 0 {
		title = fmt.Sprintf("Debug [+%d↑]", d.scroll)
	}
	totalLines := len(d.lines)
	if totalLines > 0 {
		title += fmt.Sprintf(" (%d lines)", totalLines)
	}

	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorDebugBdr).
		Foreground(colorDebugText).
		Width(width - 2). // subtract border
		MaxHeight(d.height)

	titleStyle := lipgloss.NewStyle().
		Foreground(colorPurple).
		Bold(true)

	return titleStyle.Render(title) + "\n" + style.Render(content)
}
