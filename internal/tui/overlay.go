package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// placeOverlay composites fg centered on top of bg.
// Both are ANSI-styled strings. The fg replaces bg content where it overlaps.
func placeOverlay(bgStr, fgStr string, bgWidth, bgHeight int) string {
	bgLines := strings.Split(bgStr, "\n")
	fgLines := strings.Split(fgStr, "\n")

	fgW := 0
	for _, line := range fgLines {
		if w := ansi.StringWidth(line); w > fgW {
			fgW = w
		}
	}
	fgH := len(fgLines)

	// Center the overlay
	startY := (bgHeight - fgH) / 2
	startX := (bgWidth - fgW) / 2
	if startY < 0 {
		startY = 0
	}
	if startX < 0 {
		startX = 0
	}

	// Ensure bg has enough lines
	for len(bgLines) < bgHeight {
		bgLines = append(bgLines, "")
	}

	// Splice overlay lines into background
	for i, fgLine := range fgLines {
		row := startY + i
		if row >= len(bgLines) {
			break
		}
		bgLines[row] = spliceANSILine(bgLines[row], fgLine, startX, bgWidth)
	}

	return strings.Join(bgLines, "\n")
}

// spliceANSILine replaces a region of a background line with a foreground string
// starting at cell position startX. Handles ANSI escape sequences.
func spliceANSILine(bg, fg string, startX, totalWidth int) string {
	// Truncate the bg line into: left (0..startX) + fg + right (startX+fgWidth..)
	fgWidth := ansi.StringWidth(fg)

	left := ansi.Truncate(bg, startX, "")
	// Pad left if bg was shorter than startX
	leftW := ansi.StringWidth(left)
	if leftW < startX {
		left += strings.Repeat(" ", startX-leftW)
	}

	// For the right side, we need to skip startX+fgWidth cells from bg
	rightStart := startX + fgWidth
	right := ""
	if rightStart < totalWidth {
		// TruncateLeft cuts from the left, leaving content from rightStart onward
		right = ansi.TruncateLeft(bg, rightStart, "")
	}

	return left + fg + right
}
