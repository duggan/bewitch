package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/duggan/bewitch/internal/config"

	"github.com/charmbracelet/lipgloss"
)

type datePickerPhase int

const (
	phasePickStart datePickerPhase = iota
	phasePickEnd
)

type datePickerModel struct {
	cursor    time.Time // currently highlighted date
	startDate time.Time // selected start date
	endDate   time.Time // selected end date
	phase     datePickerPhase
	confirmed bool
	cancelled bool
	presetIdx int // >=0 means a preset was selected, -1 means custom range
	ranges    []config.HistoryRange
}

func newDatePicker(ranges []config.HistoryRange) datePickerModel {
	now := time.Now()
	weekAgo := now.AddDate(0, 0, -7)
	return datePickerModel{
		cursor:    weekAgo,
		startDate: weekAgo,
		endDate:   now,
		phase:     phasePickStart,
		presetIdx: -1,
		ranges:    ranges,
	}
}

func (d datePickerModel) update(key string) datePickerModel {
	switch key {
	case "left":
		d.cursor = d.cursor.AddDate(0, 0, -1)
	case "right":
		d.cursor = d.cursor.AddDate(0, 0, 1)
	case "up":
		d.cursor = d.cursor.AddDate(0, 0, -7)
	case "down":
		d.cursor = d.cursor.AddDate(0, 0, 7)
	case "<", ",":
		d.cursor = d.cursor.AddDate(0, -1, 0)
	case ">", ".":
		d.cursor = d.cursor.AddDate(0, 1, 0)
	case "enter":
		if d.phase == phasePickStart {
			d.startDate = d.cursor
			d.phase = phasePickEnd
			// Move cursor to today for end date picking
			d.cursor = time.Now()
		} else {
			end := d.cursor
			start := d.startDate
			// Ensure start <= end
			if end.Before(start) {
				start, end = end, start
			}
			d.startDate = start
			// Set end to end-of-day
			d.endDate = time.Date(end.Year(), end.Month(), end.Day(), 23, 59, 59, 0, end.Location())
			// Set start to start-of-day
			d.startDate = time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, start.Location())
			d.confirmed = true
		}
	case "esc":
		d.cancelled = true
	default:
		// Preset shortcuts: keys 1-9 map to range indices 0-8
		if len(key) == 1 && key[0] >= '1' && key[0] <= '9' {
			idx := int(key[0] - '1')
			if idx < len(d.ranges) {
				d.applyPreset(idx)
			}
		}
	}

	// Don't allow cursor past today
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	cursorDay := time.Date(d.cursor.Year(), d.cursor.Month(), d.cursor.Day(), 0, 0, 0, 0, d.cursor.Location())
	if cursorDay.After(today) {
		d.cursor = today
	}

	return d
}

func (d *datePickerModel) applyPreset(idx int) {
	d.presetIdx = idx
	d.confirmed = true
}

func (d datePickerModel) view(width int) string {
	var b strings.Builder

	// Phase indicator
	if d.phase == phasePickStart {
		b.WriteString(headerStyle.Render("Select start date") + "\n\n")
	} else {
		startStr := d.startDate.Format("Jan 02, 2006")
		b.WriteString(headerStyle.Render("Start: "+startStr+"  →  Select end date") + "\n\n")
	}

	// Render calendar for cursor's month
	b.WriteString(d.renderCalendar())
	b.WriteString("\n\n")

	// Presets
	presetStyle := lipgloss.NewStyle().Foreground(colorMuted)
	b.WriteString(presetStyle.Render("Presets: "))
	for i, r := range d.ranges {
		if i > 0 {
			b.WriteString(presetStyle.Render("  "))
		}
		label := fmt.Sprintf("%d:%s", i+1, r.Label)
		b.WriteString(lipgloss.NewStyle().Foreground(colorLavender).Render(label))
	}
	b.WriteString("\n")

	// Help
	b.WriteString(helpStyle.Render("←→:day  ↑↓:week  </>:month  enter:select  esc:cancel"))

	return b.String()
}

func (d datePickerModel) renderCalendar() string {
	var b strings.Builder

	cursor := d.cursor
	year, month, _ := cursor.Date()
	loc := cursor.Location()

	// Month/year header
	monthHeader := fmt.Sprintf("%s %d", month.String(), year)
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(colorPink).Render(monthHeader) + "\n")

	// Day-of-week header
	dayHeaders := "Su Mo Tu We Th Fr Sa"
	b.WriteString(lipgloss.NewStyle().Foreground(colorPurple).Render(dayHeaders) + "\n")

	// First day of month
	firstDay := time.Date(year, month, 1, 0, 0, 0, 0, loc)
	startWeekday := int(firstDay.Weekday()) // 0=Sunday
	daysInMonth := time.Date(year, month+1, 0, 0, 0, 0, 0, loc).Day()

	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	cursorDay := time.Date(cursor.Year(), cursor.Month(), cursor.Day(), 0, 0, 0, 0, loc)

	// Leading spaces
	b.WriteString(strings.Repeat("   ", startWeekday))

	for day := 1; day <= daysInMonth; day++ {
		thisDate := time.Date(year, month, day, 0, 0, 0, 0, loc)
		dayStr := fmt.Sprintf("%2d", day)

		style := lipgloss.NewStyle().Foreground(colorText)

		// Highlight if in selected range (when picking end date)
		if d.phase == phasePickEnd {
			startDay := time.Date(d.startDate.Year(), d.startDate.Month(), d.startDate.Day(), 0, 0, 0, 0, loc)
			if !thisDate.Before(startDay) && !thisDate.After(cursorDay) ||
				!thisDate.Before(cursorDay) && !thisDate.After(startDay) {
				style = style.Background(colorDarkPurple)
			}
		}

		// Cursor highlight
		if thisDate.Equal(cursorDay) {
			style = style.Bold(true).Foreground(colorDarkBg).Background(colorPink)
		}

		// Today marker
		if thisDate.Equal(today) && !thisDate.Equal(cursorDay) {
			style = style.Underline(true).Foreground(colorLavender)
		}

		// Future dates dimmed
		if thisDate.After(today) {
			style = lipgloss.NewStyle().Foreground(colorDimmer)
		}

		b.WriteString(style.Render(dayStr))

		weekday := (startWeekday + day) % 7
		if weekday == 0 && day < daysInMonth {
			b.WriteString("\n")
		} else {
			b.WriteString(" ")
		}
	}

	return b.String()
}
