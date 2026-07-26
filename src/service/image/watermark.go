package image

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"strings"
)

// watermarkFont is a minimal hand-authored 5x7 bitmap font used to render
// watermark text without depending on any font-rendering library - there is
// no golang.org/x/image/font (or any other font) dependency in go.mod,
// matching the same no-font constraint documented in
// service/generate/avatar.go. Each glyph is 7 rows of a 5-character
// string; '#' is a filled pixel, '.' is empty. Only A-Z, 0-9, and space
// are defined.
var watermarkFont = map[rune][7]string{
	'A': {".###.", "#...#", "#...#", "#####", "#...#", "#...#", "#...#"},
	'B': {"####.", "#...#", "#...#", "####.", "#...#", "#...#", "####."},
	'C': {".####", "#....", "#....", "#....", "#....", "#....", ".####"},
	'D': {"####.", "#...#", "#...#", "#...#", "#...#", "#...#", "####."},
	'E': {"#####", "#....", "#....", "####.", "#....", "#....", "#####"},
	'F': {"#####", "#....", "#....", "####.", "#....", "#....", "#...."},
	'G': {".####", "#....", "#....", "#.###", "#...#", "#...#", ".####"},
	'H': {"#...#", "#...#", "#...#", "#####", "#...#", "#...#", "#...#"},
	'I': {"#####", "..#..", "..#..", "..#..", "..#..", "..#..", "#####"},
	'J': {"..###", "...#.", "...#.", "...#.", "...#.", "#..#.", ".##.."},
	'K': {"#...#", "#..#.", "#.#..", "##...", "#.#..", "#..#.", "#...#"},
	'L': {"#....", "#....", "#....", "#....", "#....", "#....", "#####"},
	'M': {"#...#", "##.##", "#.#.#", "#...#", "#...#", "#...#", "#...#"},
	'N': {"#...#", "##..#", "#.#.#", "#..##", "#...#", "#...#", "#...#"},
	'O': {".###.", "#...#", "#...#", "#...#", "#...#", "#...#", ".###."},
	'P': {"####.", "#...#", "#...#", "####.", "#....", "#....", "#...."},
	'Q': {".###.", "#...#", "#...#", "#...#", "#.#.#", "#..#.", ".##.#"},
	'R': {"####.", "#...#", "#...#", "####.", "#.#..", "#..#.", "#...#"},
	'S': {".####", "#....", "#....", ".###.", "....#", "....#", "####."},
	'T': {"#####", "..#..", "..#..", "..#..", "..#..", "..#..", "..#.."},
	'U': {"#...#", "#...#", "#...#", "#...#", "#...#", "#...#", ".###."},
	'V': {"#...#", "#...#", "#...#", "#...#", "#...#", ".#.#.", "..#.."},
	'W': {"#...#", "#...#", "#...#", "#.#.#", "#.#.#", "#.#.#", ".#.#."},
	'X': {"#...#", "#...#", ".#.#.", "..#..", ".#.#.", "#...#", "#...#"},
	'Y': {"#...#", "#...#", ".#.#.", "..#..", "..#..", "..#..", "..#.."},
	'Z': {"#####", "....#", "...#.", "..#..", ".#...", "#....", "#####"},
	'0': {".###.", "#...#", "#..##", "#.#.#", "##..#", "#...#", ".###."},
	'1': {"..#..", ".##..", "..#..", "..#..", "..#..", "..#..", "#####"},
	'2': {".###.", "#...#", "....#", "...#.", "..#..", ".#...", "#####"},
	'3': {".###.", "#...#", "....#", "..##.", "....#", "#...#", ".###."},
	'4': {"...#.", "..##.", ".#.#.", "#..#.", "#####", "...#.", "...#."},
	'5': {"#####", "#....", "####.", "....#", "....#", "#...#", ".###."},
	'6': {"..##.", ".#...", "#....", "####.", "#...#", "#...#", ".###."},
	'7': {"#####", "....#", "...#.", "..#..", ".#...", ".#...", ".#..."},
	'8': {".###.", "#...#", "#...#", ".###.", "#...#", "#...#", ".###."},
	'9': {".###.", "#...#", "#...#", ".####", "....#", "...#.", ".##.."},
	' ': {".....", ".....", ".....", ".....", ".....", ".....", "....."},
}

// glyphFor returns the bitmap rows for r, falling back to a solid filled
// block for any rune not defined in watermarkFont so unsupported
// characters remain visible rather than being silently skipped.
func glyphFor(r rune) [7]string {
	if g, ok := watermarkFont[r]; ok {
		return g
	}
	return [7]string{"#####", "#####", "#####", "#####", "#####", "#####", "#####"}
}

// Watermark tiles text diagonally across the loaded image using the
// watermarkFont bitmap glyphs, alpha-blended at opacity (0..1, default
// 0.35 when out of range) so the underlying image stays visible. text is
// upper-cased since the font only defines uppercase glyphs.
func (s *Service) Watermark(text string, opacity float64) error {
	if s.img == nil {
		return fmt.Errorf("no image loaded")
	}
	text = strings.ToUpper(strings.TrimSpace(text))
	if text == "" {
		return fmt.Errorf("watermark text is required")
	}
	if opacity <= 0 || opacity > 1 {
		opacity = 0.35
	}

	bounds := s.img.Bounds()
	dst := image.NewRGBA(bounds)
	draw.Draw(dst, bounds, s.img, bounds.Min, draw.Src)

	scale := bounds.Dx() / 200
	if scale < 1 {
		scale = 1
	}
	glyphWidth := 6 * scale
	glyphHeight := 8 * scale
	textWidth := glyphWidth * len(text)
	watermarkColor := color.RGBA{R: 255, G: 255, B: 255, A: 255}

	// Tile the watermark diagonally across the whole image so it cannot
	// be cropped out by taking a sub-region.
	rowStride := glyphHeight * 3
	colStride := textWidth + glyphWidth*4
	for oy := -glyphHeight; oy < bounds.Dy()+glyphHeight; oy += rowStride {
		rowOffset := (oy / rowStride) * (colStride / 2)
		for ox := -textWidth - rowOffset; ox < bounds.Dx()+textWidth; ox += colStride {
			drawWatermarkText(dst, bounds, text, ox, oy, scale, watermarkColor, opacity)
		}
	}

	s.img = dst
	return nil
}

// drawWatermarkText renders text at (originX, originY) into dst using the
// watermarkFont glyph table, alpha-blending each filled pixel over the
// existing content at the given opacity.
func drawWatermarkText(dst *image.RGBA, bounds image.Rectangle, text string, originX, originY, scale int, c color.RGBA, opacity float64) {
	cursorX := originX
	for _, r := range text {
		glyph := glyphFor(r)
		for row := 0; row < 7; row++ {
			for col := 0; col < 5; col++ {
				if glyph[row][col] != '#' {
					continue
				}
				for sy := 0; sy < scale; sy++ {
					for sx := 0; sx < scale; sx++ {
						px := cursorX + col*scale + sx
						py := originY + row*scale + sy
						if px < bounds.Min.X || px >= bounds.Max.X || py < bounds.Min.Y || py >= bounds.Max.Y {
							continue
						}
						blendWatermarkPixel(dst, px, py, c, opacity)
					}
				}
			}
		}
		cursorX += glyphWidthFor(scale)
	}
}

// glyphWidthFor returns the horizontal advance (in pixels) between two
// glyphs at the given scale: 5 pixel columns plus 1 column of spacing.
func glyphWidthFor(scale int) int {
	return 6 * scale
}

// blendWatermarkPixel alpha-blends c over the existing pixel at (x, y)
// using the standard blend = fg*a + bg*(1-a) formula per channel.
func blendWatermarkPixel(dst *image.RGBA, x, y int, c color.RGBA, opacity float64) {
	bg := dst.RGBAAt(x, y)
	blend := func(fg, bg uint8) uint8 {
		return uint8(float64(fg)*opacity + float64(bg)*(1-opacity))
	}
	dst.SetRGBA(x, y, color.RGBA{
		R: blend(c.R, bg.R),
		G: blend(c.G, bg.G),
		B: blend(c.B, bg.B),
		A: 255,
	})
}
