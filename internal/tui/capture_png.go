package tui

import (
	_ "embed"
	"image"
	"image/draw"
	"image/png"
	"io"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

//go:embed fonts/JetBrainsMono-Regular.ttf
var jbMonoRegularTTF []byte

//go:embed fonts/JetBrainsMono-Bold.ttf
var jbMonoBoldTTF []byte

//go:embed fonts/NotoSansSymbols2-Subset.ttf
var notoSymbols2TTF []byte

//go:embed fonts/NotoSansSymbols-Moon.ttf
var notoSymbolsMoonTTF []byte

const (
	pngFontSize = 14.0
	pngDPI      = 72.0
	pngPadding  = 8
)

var (
	pngFontOnce     sync.Once
	pngFaceRegular font.Face
	pngFaceBold    font.Face
	pngFallbacks   []font.Face // fallback fonts tried in order for missing glyphs
	pngCellW       int
	pngCellH       int
)

// initPNGFont lazily initializes the embedded JetBrains Mono font faces and cell metrics.
func initPNGFont() {
	pngFontOnce.Do(func() {
		opts := &opentype.FaceOptions{
			Size:    pngFontSize,
			DPI:     pngDPI,
			Hinting: font.HintingFull,
		}

		fRegular, err := opentype.Parse(jbMonoRegularTTF)
		if err != nil {
			panic("capture: failed to parse JetBrains Mono Regular: " + err.Error())
		}
		pngFaceRegular, err = opentype.NewFace(fRegular, opts)
		if err != nil {
			panic("capture: failed to create regular font face: " + err.Error())
		}

		fBold, err := opentype.Parse(jbMonoBoldTTF)
		if err != nil {
			panic("capture: failed to parse JetBrains Mono Bold: " + err.Error())
		}
		pngFaceBold, err = opentype.NewFace(fBold, opts)
		if err != nil {
			panic("capture: failed to create bold font face: " + err.Error())
		}

		// Load fallback fonts for glyphs missing from JetBrains Mono
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
			pngFallbacks = append(pngFallbacks, face)
		}

		// Measure cell dimensions from the regular face metrics
		metrics := pngFaceRegular.Metrics()
		pngCellH = (metrics.Ascent + metrics.Descent).Ceil()
		adv, ok := pngFaceRegular.GlyphAdvance('M')
		if !ok {
			adv = fixed.I(8)
		}
		pngCellW = adv.Ceil()
	})
}

// RenderPNG writes the CaptureGrid as a PNG image to w.
func RenderPNG(grid *CaptureGrid, w io.Writer) error {
	initPNGFont()

	imgW := grid.Width*pngCellW + pngPadding*2
	imgH := grid.Height*pngCellH + pngPadding*2

	img := image.NewRGBA(image.Rect(0, 0, imgW, imgH))

	// Fill background
	draw.Draw(img, img.Bounds(), image.NewUniform(defaultCaptureBG), image.Point{}, draw.Src)

	metrics := pngFaceRegular.Metrics()

	for row, cells := range grid.Cells {
		y := pngPadding + row*pngCellH

		// Draw background rects for non-default BG cells
		for col, cell := range cells {
			if cell.BG != defaultCaptureBG {
				x := pngPadding + col*pngCellW
				rect := image.Rect(x, y, x+pngCellW, y+pngCellH)
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

			x := pngPadding + col*pngCellW

			face := pngFaceRegular
			if cell.Bold {
				face = pngFaceBold
			}

			// Resolve font: primary → fallback chain → skip
			_, hasGlyph := face.GlyphAdvance(ch)
			if !hasGlyph {
				for _, fb := range pngFallbacks {
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

	return png.Encode(w, img)
}
