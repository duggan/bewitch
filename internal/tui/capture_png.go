package tui

import (
	_ "embed"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

//go:embed fonts/NotoSansMono-Regular.ttf
var notoMonoRegularTTF []byte

//go:embed fonts/NotoSansMono-Bold.ttf
var notoMonoBoldTTF []byte

//go:embed fonts/NotoSansSymbols2-Subset.ttf
var notoSymbols2TTF []byte

//go:embed fonts/NotoSansSymbols-Moon.ttf
var notoSymbolsMoonTTF []byte

const pngFontSize = 14.0

var (
	pngFontOnce sync.Once
	pngFontDPI  float64
	pngFonts    struct {
		regular  font.Face
		bold     font.Face
		fallback []font.Face
		cellW    int
		cellH    int
	}
)

// initPNGFont lazily initializes the embedded Noto Sans Mono font faces and cell metrics.
// The DPI from the first call is used for all subsequent renders.
func initPNGFont(dpi float64) {
	pngFontOnce.Do(func() {
		pngFontDPI = dpi
		opts := &opentype.FaceOptions{
			Size:    pngFontSize,
			DPI:     dpi,
			Hinting: font.HintingFull,
		}

		fRegular, err := opentype.Parse(notoMonoRegularTTF)
		if err != nil {
			panic("capture: failed to parse Noto Sans Mono Regular: " + err.Error())
		}
		pngFonts.regular, err = opentype.NewFace(fRegular, opts)
		if err != nil {
			panic("capture: failed to create regular font face: " + err.Error())
		}

		fBold, err := opentype.Parse(notoMonoBoldTTF)
		if err != nil {
			panic("capture: failed to parse Noto Sans Mono Bold: " + err.Error())
		}
		pngFonts.bold, err = opentype.NewFace(fBold, opts)
		if err != nil {
			panic("capture: failed to create bold font face: " + err.Error())
		}

		// Load fallback fonts for glyphs missing from Noto Sans Mono
		for _, fb := range []struct {
			name string
			data []byte
		}{
			{"NotoSansSymbols2", notoSymbols2TTF}, // ✧ ✦ + braille
			{"NotoSansSymbols", notoSymbolsMoonTTF}, // ☽
		} {
			f, err := opentype.Parse(fb.data)
			if err != nil {
				panic("capture: failed to parse " + fb.name + ": " + err.Error())
			}
			face, err := opentype.NewFace(f, opts)
			if err != nil {
				panic("capture: failed to create " + fb.name + " face: " + err.Error())
			}
			pngFonts.fallback = append(pngFonts.fallback, face)
		}

		// Measure cell dimensions from the regular face metrics
		metrics := pngFonts.regular.Metrics()
		pngFonts.cellH = (metrics.Ascent + metrics.Descent).Ceil()
		adv, ok := pngFonts.regular.GlyphAdvance('M')
		if !ok {
			adv = fixed.I(8)
		}
		pngFonts.cellW = adv.Ceil()
	})
}

// RenderPNG writes the CaptureGrid as a PNG image to w using the given settings.
func RenderPNG(grid *CaptureGrid, w io.Writer, settings CaptureSettings) error {
	initPNGFont(float64(settings.DPI))

	padding := settings.DPI / 9 // scales with DPI: 72→8, 144→16, 216→24
	cellW := pngFonts.cellW
	cellH := pngFonts.cellH

	imgW := grid.Width*cellW + padding*2
	imgH := grid.Height*cellH + padding*2

	img := image.NewRGBA(image.Rect(0, 0, imgW, imgH))

	// Fill background
	bg := settings.Background
	draw.Draw(img, img.Bounds(), image.NewUniform(bg), image.Point{}, draw.Src)

	metrics := pngFonts.regular.Metrics()

	for row, cells := range grid.Cells {
		y := padding + row*cellH

		// Draw background rects for non-default BG cells
		for col, cell := range cells {
			if cell.BG != bg {
				x := padding + col*cellW
				rect := image.Rect(x, y, x+cellW, y+cellH)
				draw.Draw(img, rect, image.NewUniform(cell.BG), image.Point{}, draw.Src)
			}
		}

		// Draw text
		baseline := y + metrics.Ascent.Ceil()
		for col, cell := range cells {
			ch := cell.Char
			if ch == 0 || ch < ' ' {
				continue
			}

			x := padding + col*cellW

			face := pngFonts.regular
			if cell.Bold {
				face = pngFonts.bold
			}

			// Resolve font: primary → fallback chain → skip
			_, hasGlyph := face.GlyphAdvance(ch)
			if !hasGlyph {
				for _, fb := range pngFonts.fallback {
					if _, ok := fb.GlyphAdvance(ch); ok {
						face = fb
						hasGlyph = true
						break
					}
				}
			}

			if !hasGlyph {
				continue
			}

			d := &font.Drawer{
				Dst:  img,
				Src:  image.NewUniform(cell.FG),
				Face: face,
				Dot:  fixed.P(x, baseline),
			}
			d.DrawString(string(ch))
		}
	}

	enc := &png.Encoder{CompressionLevel: settings.Compression}
	return enc.Encode(w, img)
}

// pngCellDimensions returns cell width and height for the current font configuration.
// Used by tests. Initializes fonts at the given DPI if not already initialized.
func pngCellDimensions(dpi int) (cellW, cellH int) {
	initPNGFont(float64(dpi))
	return pngFonts.cellW, pngFonts.cellH
}

// pngBGColor is a test helper that returns the background from settings.
func pngBGColor(settings CaptureSettings) color.RGBA {
	return settings.Background
}
