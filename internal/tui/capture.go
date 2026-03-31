package tui

import (
	"fmt"
	"image/color"
	"image/png"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/duggan/bewitch/internal/config"
)

// CaptureCell represents a single character cell with style information.
type CaptureCell struct {
	Char rune
	FG   color.RGBA
	BG   color.RGBA
	Bold bool
}

// CaptureGrid is a 2D grid of styled cells parsed from ANSI output.
type CaptureGrid struct {
	Cells  [][]CaptureCell
	Width  int // max columns
	Height int // number of rows
}

// Default terminal colors matching the bewitch palette.
var (
	defaultCaptureFG = color.RGBA{0xF8, 0xF8, 0xF2, 0xFF} // colorText #F8F8F2
	defaultCaptureBG = color.RGBA{0x1A, 0x1A, 0x2E, 0xFF} // colorDarkBg #1A1A2E
)

// CaptureSettings holds resolved capture configuration for rendering.
type CaptureSettings struct {
	Directory   string
	DPI         int
	Compression png.CompressionLevel
	Background  color.RGBA
	Foreground  color.RGBA
}

// DefaultCaptureSettings returns settings with sensible defaults.
func DefaultCaptureSettings() CaptureSettings {
	return CaptureSettings{
		DPI:         144,
		Compression: png.BestCompression,
		Background:  defaultCaptureBG,
		Foreground:  defaultCaptureFG,
	}
}

// CaptureSettingsFromConfig builds CaptureSettings from the config, falling back to defaults.
func CaptureSettingsFromConfig(cfg config.CaptureConfig) CaptureSettings {
	s := DefaultCaptureSettings()
	if cfg.Directory != "" {
		s.Directory = cfg.Directory
	}
	s.DPI = cfg.GetDPI()
	switch cfg.GetCompression() {
	case "none":
		s.Compression = png.NoCompression
	case "default":
		s.Compression = png.DefaultCompression
	default:
		s.Compression = png.BestCompression
	}
	if c, err := parseHexColor(cfg.Background); err == nil {
		s.Background = c
	}
	if c, err := parseHexColor(cfg.Foreground); err == nil {
		s.Foreground = c
	}
	return s
}

// parseHexColor parses a "#RRGGBB" hex color string.
func parseHexColor(s string) (color.RGBA, error) {
	s = strings.TrimPrefix(s, "#")
	if len(s) != 6 {
		return color.RGBA{}, fmt.Errorf("invalid hex color: %q", s)
	}
	var r, g, b uint8
	_, err := fmt.Sscanf(s, "%02x%02x%02x", &r, &g, &b)
	if err != nil {
		return color.RGBA{}, fmt.Errorf("invalid hex color: %q", s)
	}
	return color.RGBA{r, g, b, 0xFF}, nil
}

// ANSI 16-color palette (standard + bright).
var ansi16Colors = [16]color.RGBA{
	{0x00, 0x00, 0x00, 0xFF}, // 0 black
	{0xAA, 0x00, 0x00, 0xFF}, // 1 red
	{0x00, 0xAA, 0x00, 0xFF}, // 2 green
	{0xAA, 0x55, 0x00, 0xFF}, // 3 yellow
	{0x00, 0x00, 0xAA, 0xFF}, // 4 blue
	{0xAA, 0x00, 0xAA, 0xFF}, // 5 magenta
	{0x00, 0xAA, 0xAA, 0xFF}, // 6 cyan
	{0xAA, 0xAA, 0xAA, 0xFF}, // 7 white
	{0x55, 0x55, 0x55, 0xFF}, // 8 bright black
	{0xFF, 0x55, 0x55, 0xFF}, // 9 bright red
	{0x55, 0xFF, 0x55, 0xFF}, // 10 bright green
	{0xFF, 0xFF, 0x55, 0xFF}, // 11 bright yellow
	{0x55, 0x55, 0xFF, 0xFF}, // 12 bright blue
	{0xFF, 0x55, 0xFF, 0xFF}, // 13 bright magenta
	{0x55, 0xFF, 0xFF, 0xFF}, // 14 bright cyan
	{0xFF, 0xFF, 0xFF, 0xFF}, // 15 bright white
}

// ansi256Color converts a 256-color index to RGBA.
func ansi256Color(idx int) color.RGBA {
	if idx < 16 {
		return ansi16Colors[idx]
	}
	if idx < 232 {
		// 6x6x6 color cube: indices 16..231
		idx -= 16
		b := idx % 6
		idx /= 6
		g := idx % 6
		r := idx / 6
		return color.RGBA{
			R: uint8(r * 255 / 5),
			G: uint8(g * 255 / 5),
			B: uint8(b * 255 / 5),
			A: 0xFF,
		}
	}
	// Grayscale ramp: indices 232..255
	v := uint8(8 + (idx-232)*10)
	return color.RGBA{v, v, v, 0xFF}
}

// sgrState tracks the current SGR (Select Graphic Rendition) style.
type sgrState struct {
	fg      color.RGBA
	bg      color.RGBA
	bold    bool
	resetFG color.RGBA // color to restore on SGR 0/39
	resetBG color.RGBA // color to restore on SGR 0/49
}

// ParseANSI parses an ANSI-styled string into a CaptureGrid.
// It handles SGR sequences for foreground/background colors and bold.
// Non-SGR escape sequences are silently consumed.
func ParseANSI(s string, fg, bg color.RGBA) *CaptureGrid {
	lines := strings.Split(s, "\n")
	grid := &CaptureGrid{
		Cells:  make([][]CaptureCell, len(lines)),
		Height: len(lines),
	}

	for i, line := range lines {
		row := parseANSILine(line, fg, bg)
		grid.Cells[i] = row
		if len(row) > grid.Width {
			grid.Width = len(row)
		}
	}

	return grid
}

// parseANSILine parses a single line of ANSI text into cells.
func parseANSILine(line string, fg, bg color.RGBA) []CaptureCell {
	var cells []CaptureCell
	state := sgrState{fg: fg, bg: bg, resetFG: fg, resetBG: bg}
	p := ansi.NewParser()

	var parserState byte
	input := []byte(line)

	for len(input) > 0 {
		seq, width, n, newState := ansi.DecodeSequence(input, parserState, p)
		parserState = newState

		if n == 0 {
			// Should not happen, but avoid infinite loop
			input = input[1:]
			continue
		}

		if width > 0 {
			// Printable character(s) — decode the rune
			r := firstRune(string(seq))
			cells = append(cells, CaptureCell{
				Char: r,
				FG:   state.fg,
				BG:   state.bg,
				Bold: state.bold,
			})
			// Wide characters occupy extra cells
			for j := 1; j < width; j++ {
				cells = append(cells, CaptureCell{
					Char: 0,
					FG:   state.fg,
					BG:   state.bg,
					Bold: state.bold,
				})
			}
		} else if len(seq) > 1 && seq[0] == 0x1b && seq[1] == '[' {
			// CSI sequence — check if it's SGR (final byte 'm')
			cmd := ansi.Cmd(p.Command())
			if cmd.Final() == 'm' {
				applySGR(&state, p.Params())
			}
			// All other CSI sequences are silently consumed
		}
		// ESC, OSC, DCS, and other sequences are silently consumed

		input = input[n:]
	}

	return cells
}

// applySGR applies SGR parameters to the current state.
func applySGR(state *sgrState, params ansi.Params) {
	if len(params) == 0 {
		// SGR with no params = reset
		state.fg = state.resetFG
		state.bg = state.resetBG
		state.bold = false
		return
	}

	i := 0
	for i < len(params) {
		p := params[i].Param(0)

		switch {
		case p == 0:
			state.fg = state.resetFG
			state.bg = state.resetBG
			state.bold = false
		case p == 1:
			state.bold = true
		case p == 22:
			state.bold = false
		case p >= 30 && p <= 37:
			state.fg = ansi16Colors[p-30]
		case p >= 40 && p <= 47:
			state.bg = ansi16Colors[p-40]
		case p >= 90 && p <= 97:
			state.fg = ansi16Colors[p-90+8]
		case p >= 100 && p <= 107:
			state.bg = ansi16Colors[p-100+8]
		case p == 39:
			state.fg = state.resetFG
		case p == 49:
			state.bg = state.resetBG
		case p == 38 || p == 48:
			// Extended color: 38;5;N (256-color) or 38;2;R;G;B (truecolor)
			isFG := p == 38
			i++
			if i >= len(params) {
				break
			}
			mode := params[i].Param(0)
			switch mode {
			case 5: // 256-color
				i++
				if i >= len(params) {
					break
				}
				idx := params[i].Param(0)
				c := ansi256Color(idx)
				if isFG {
					state.fg = c
				} else {
					state.bg = c
				}
			case 2: // truecolor
				if i+3 >= len(params) {
					i = len(params) - 1
					break
				}
				r := params[i+1].Param(0)
				g := params[i+2].Param(0)
				b := params[i+3].Param(0)
				c := color.RGBA{uint8(r), uint8(g), uint8(b), 0xFF}
				if isFG {
					state.fg = c
				} else {
					state.bg = c
				}
				i += 3
			}
		}

		i++
	}
}

// firstRune returns the first rune from a string.
func firstRune(s string) rune {
	for _, r := range s {
		return r
	}
	return ' '
}
