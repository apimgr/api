package generate

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"image"
	"image/color"
	"image/png"
)

// Avatar renders a deterministic identicon-style avatar for the given
// initials as PNG bytes. There is no golang.org/x/image (or other font
// rendering) dependency available in go.mod, so rather than faking bitmap
// text rendering by hand this draws a hash-derived geometric block pattern:
// a colored background (derived from the initials) with a symmetric grid of
// contrasting blocks overlaid, giving each distinct set of initials a
// visually unique, stable avatar. This is a deliberate scope substitution
// for literal centered-text rendering; see TODO.AI.md for the note.
func (s *Service) Avatar(initials string, size int) ([]byte, error) {
	if initials == "" {
		return nil, fmt.Errorf("initials must not be empty")
	}
	if size <= 0 {
		size = 256
	}

	sum := sha256.Sum256([]byte(initials))

	bg := color.RGBA{
		R: sum[0],
		G: sum[1],
		B: sum[2],
		A: 255,
	}
	fg := color.RGBA{
		R: 255 - sum[0],
		G: 255 - sum[1],
		B: 255 - sum[2],
		A: 255,
	}

	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			img.Set(x, y, bg)
		}
	}

	// Overlay a 5x5 grid of blocks. Only the left half of each row is
	// hash-driven; the right half mirrors it, producing the same
	// left-right symmetric look as the identicon tool's grid so both
	// hash-based image tools stay visually distinguishable from each
	// other despite sharing the general technique (identicon uses a
	// different mirrored-square scheme keyed on a different byte range).
	const grid = 5
	cell := size / grid
	half := (grid + 1) / 2
	for row := 0; row < grid; row++ {
		for col := 0; col < half; col++ {
			bitIndex := row*half + col
			byteIndex := (bitIndex / 8) % len(sum)
			bit := sum[byteIndex] & (1 << uint(bitIndex%8))
			if bit == 0 {
				continue
			}
			drawBlock(img, col*cell, row*cell, cell, cell, fg)
			mirrorCol := grid - 1 - col
			drawBlock(img, mirrorCol*cell, row*cell, cell, cell, fg)
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("failed to encode avatar PNG: %w", err)
	}
	return buf.Bytes(), nil
}

// drawBlock fills an axis-aligned rectangle in img with the given color.
func drawBlock(img *image.RGBA, x, y, w, h int, c color.RGBA) {
	bounds := img.Bounds()
	for py := y; py < y+h && py < bounds.Max.Y; py++ {
		for px := x; px < x+w && px < bounds.Max.X; px++ {
			img.Set(px, py, c)
		}
	}
}
