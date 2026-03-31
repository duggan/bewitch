package tui

import (
	"bytes"
	"image/color"
	"image/png"
	"testing"
)

func TestParseANSI_PlainText(t *testing.T) {
	grid := ParseANSI("Hello", defaultCaptureFG, defaultCaptureBG)
	if grid.Height != 1 {
		t.Fatalf("expected 1 row, got %d", grid.Height)
	}
	if grid.Width != 5 {
		t.Fatalf("expected 5 cols, got %d", grid.Width)
	}
	for i, ch := range "Hello" {
		if grid.Cells[0][i].Char != ch {
			t.Errorf("cell[0][%d]: expected %q, got %q", i, ch, grid.Cells[0][i].Char)
		}
		if grid.Cells[0][i].FG != defaultCaptureFG {
			t.Errorf("cell[0][%d]: expected default FG", i)
		}
		if grid.Cells[0][i].BG != defaultCaptureBG {
			t.Errorf("cell[0][%d]: expected default BG", i)
		}
	}
}

func TestParseANSI_Multiline(t *testing.T) {
	grid := ParseANSI("ab\ncd\nef", defaultCaptureFG, defaultCaptureBG)
	if grid.Height != 3 {
		t.Fatalf("expected 3 rows, got %d", grid.Height)
	}
	if grid.Width != 2 {
		t.Fatalf("expected 2 cols, got %d", grid.Width)
	}
	if grid.Cells[1][0].Char != 'c' {
		t.Errorf("expected 'c', got %q", grid.Cells[1][0].Char)
	}
}

func TestParseANSI_TrueColor(t *testing.T) {
	// Red foreground, blue background via 24-bit SGR
	input := "\x1b[38;2;255;0;0;48;2;0;0;255mX\x1b[0m"
	grid := ParseANSI(input, defaultCaptureFG, defaultCaptureBG)
	if grid.Width != 1 || grid.Height != 1 {
		t.Fatalf("expected 1x1, got %dx%d", grid.Width, grid.Height)
	}
	cell := grid.Cells[0][0]
	if cell.Char != 'X' {
		t.Errorf("expected 'X', got %q", cell.Char)
	}
	expectFG := color.RGBA{255, 0, 0, 255}
	if cell.FG != expectFG {
		t.Errorf("FG: expected %v, got %v", expectFG, cell.FG)
	}
	expectBG := color.RGBA{0, 0, 255, 255}
	if cell.BG != expectBG {
		t.Errorf("BG: expected %v, got %v", expectBG, cell.BG)
	}
}

func TestParseANSI_256Color(t *testing.T) {
	// 256-color index 196 = rgb(255,0,0)
	input := "\x1b[38;5;196mR\x1b[0m"
	grid := ParseANSI(input, defaultCaptureFG, defaultCaptureBG)
	cell := grid.Cells[0][0]
	if cell.Char != 'R' {
		t.Errorf("expected 'R', got %q", cell.Char)
	}
	// Index 196 = color cube (196-16=180), r=180/36=5, g=(180%36)/6=0, b=0
	expect := color.RGBA{255, 0, 0, 255}
	if cell.FG != expect {
		t.Errorf("FG: expected %v, got %v", expect, cell.FG)
	}
}

func TestParseANSI_StandardColors(t *testing.T) {
	// Green foreground (SGR 32)
	input := "\x1b[32mG\x1b[0m"
	grid := ParseANSI(input, defaultCaptureFG, defaultCaptureBG)
	cell := grid.Cells[0][0]
	if cell.FG != ansi16Colors[2] {
		t.Errorf("FG: expected %v, got %v", ansi16Colors[2], cell.FG)
	}
}

func TestParseANSI_BrightColors(t *testing.T) {
	// Bright red foreground (SGR 91)
	input := "\x1b[91mR\x1b[0m"
	grid := ParseANSI(input, defaultCaptureFG, defaultCaptureBG)
	cell := grid.Cells[0][0]
	if cell.FG != ansi16Colors[9] {
		t.Errorf("FG: expected %v, got %v", ansi16Colors[9], cell.FG)
	}
}

func TestParseANSI_Bold(t *testing.T) {
	input := "\x1b[1mB\x1b[0mN"
	grid := ParseANSI(input, defaultCaptureFG, defaultCaptureBG)
	if !grid.Cells[0][0].Bold {
		t.Error("expected first cell to be bold")
	}
	if grid.Cells[0][1].Bold {
		t.Error("expected second cell to not be bold")
	}
}

func TestParseANSI_Reset(t *testing.T) {
	input := "\x1b[31mR\x1b[0mD"
	grid := ParseANSI(input, defaultCaptureFG, defaultCaptureBG)
	// After reset, second char should have default FG
	if grid.Cells[0][1].FG != defaultCaptureFG {
		t.Errorf("expected default FG after reset, got %v", grid.Cells[0][1].FG)
	}
}

func TestParseANSI_NonSGRSequencesStripped(t *testing.T) {
	// Cursor movement sequence should be silently consumed
	input := "A\x1b[2;3HB"
	grid := ParseANSI(input, defaultCaptureFG, defaultCaptureBG)
	if grid.Width != 2 {
		t.Fatalf("expected 2 cols (non-SGR stripped), got %d", grid.Width)
	}
	if grid.Cells[0][0].Char != 'A' || grid.Cells[0][1].Char != 'B' {
		t.Errorf("expected 'A' and 'B', got %q and %q", grid.Cells[0][0].Char, grid.Cells[0][1].Char)
	}
}

func TestParseANSI_CustomColors(t *testing.T) {
	customFG := color.RGBA{0x00, 0xFF, 0x00, 0xFF}
	customBG := color.RGBA{0xFF, 0x00, 0x00, 0xFF}
	// Plain text should use custom defaults
	grid := ParseANSI("A", customFG, customBG)
	cell := grid.Cells[0][0]
	if cell.FG != customFG {
		t.Errorf("FG: expected %v, got %v", customFG, cell.FG)
	}
	if cell.BG != customBG {
		t.Errorf("BG: expected %v, got %v", customBG, cell.BG)
	}
	// After SGR reset, should return to custom defaults
	grid = ParseANSI("\x1b[31mR\x1b[0mD", customFG, customBG)
	if grid.Cells[0][1].FG != customFG {
		t.Errorf("FG after reset: expected %v, got %v", customFG, grid.Cells[0][1].FG)
	}
}

func TestRenderPNG_Dimensions(t *testing.T) {
	settings := DefaultCaptureSettings()
	grid := ParseANSI("ABCD\nEFGH", settings.Foreground, settings.Background)
	var buf bytes.Buffer
	if err := RenderPNG(grid, &buf, settings); err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(&buf)
	if err != nil {
		t.Fatal(err)
	}
	bounds := img.Bounds()
	cellW, cellH := pngCellDimensions(settings.DPI)
	padding := settings.DPI / 9
	expectedW := 4*cellW + padding*2
	expectedH := 2*cellH + padding*2
	if bounds.Dx() != expectedW {
		t.Errorf("width: expected %d, got %d", expectedW, bounds.Dx())
	}
	if bounds.Dy() != expectedH {
		t.Errorf("height: expected %d, got %d", expectedH, bounds.Dy())
	}
}

func TestRenderPNG_BackgroundColor(t *testing.T) {
	settings := DefaultCaptureSettings()
	grid := ParseANSI("A", settings.Foreground, settings.Background)
	var buf bytes.Buffer
	if err := RenderPNG(grid, &buf, settings); err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(&buf)
	if err != nil {
		t.Fatal(err)
	}
	// Check a corner pixel is the configured background color
	bg := settings.Background
	r, g, b, a := img.At(0, 0).RGBA()
	if uint8(r>>8) != bg.R || uint8(g>>8) != bg.G || uint8(b>>8) != bg.B || uint8(a>>8) != bg.A {
		t.Errorf("corner pixel: expected %v, got (%d,%d,%d,%d)", bg, r>>8, g>>8, b>>8, a>>8)
	}
}

func TestRenderPNG_NonDefaultBG(t *testing.T) {
	// Blue background cell
	settings := DefaultCaptureSettings()
	grid := ParseANSI("\x1b[44mX\x1b[0m", settings.Foreground, settings.Background)
	var buf bytes.Buffer
	if err := RenderPNG(grid, &buf, settings); err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(&buf)
	if err != nil {
		t.Fatal(err)
	}
	cellW, cellH := pngCellDimensions(settings.DPI)
	padding := settings.DPI / 9
	// Check center of the first cell has blue-ish background
	cx := padding + cellW/2
	cy := padding + cellH/2
	r, g, b, _ := img.At(cx, cy).RGBA()
	// Blue BG is ansi16Colors[4] = {0,0,0xAA,0xFF}
	// The pixel might be the BG color or have text drawn on top,
	// but the blue channel should be significant
	if uint8(b>>8) < 0x80 {
		t.Errorf("expected blue-ish BG at cell center, got RGB(%d,%d,%d)", r>>8, g>>8, b>>8)
	}
}

func TestAnsi256Color(t *testing.T) {
	// Standard 16 colors
	for i := 0; i < 16; i++ {
		if ansi256Color(i) != ansi16Colors[i] {
			t.Errorf("ansi256Color(%d) != ansi16Colors[%d]", i, i)
		}
	}
	// Grayscale: index 232 should be dark gray (8)
	c := ansi256Color(232)
	if c.R != 8 || c.G != 8 || c.B != 8 {
		t.Errorf("ansi256Color(232) = (%d,%d,%d), want (8,8,8)", c.R, c.G, c.B)
	}
	// Color cube: index 196 = 5,0,0 → (255,0,0)
	c = ansi256Color(196)
	if c.R != 255 || c.G != 0 || c.B != 0 {
		t.Errorf("ansi256Color(196) = (%d,%d,%d), want (255,0,0)", c.R, c.G, c.B)
	}
}

func TestParseHexColor(t *testing.T) {
	tests := []struct {
		input   string
		want    color.RGBA
		wantErr bool
	}{
		{"#FF0000", color.RGBA{255, 0, 0, 255}, false},
		{"#1A1A2E", color.RGBA{0x1A, 0x1A, 0x2E, 0xFF}, false},
		{"1A1A2E", color.RGBA{0x1A, 0x1A, 0x2E, 0xFF}, false},  // without #
		{"#ff00ff", color.RGBA{0xFF, 0x00, 0xFF, 0xFF}, false},  // lowercase
		{"", color.RGBA{}, true},
		{"#FFF", color.RGBA{}, true},     // too short
		{"#ZZZZZZ", color.RGBA{}, true},  // invalid hex
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseHexColor(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseHexColor(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("parseHexColor(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestCaptureSettingsDefaults(t *testing.T) {
	s := DefaultCaptureSettings()
	if s.DPI != 144 {
		t.Errorf("DPI: expected 144, got %d", s.DPI)
	}
	if s.Background != defaultCaptureBG {
		t.Errorf("Background: expected %v, got %v", defaultCaptureBG, s.Background)
	}
	if s.Foreground != defaultCaptureFG {
		t.Errorf("Foreground: expected %v, got %v", defaultCaptureFG, s.Foreground)
	}
}
