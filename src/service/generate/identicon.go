package generate

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"image"
	"image/color"
	"image/png"
)

// Identicon renders a classic GitHub-style identicon for the given seed
// string as PNG bytes: a sha256 hash of the seed drives both a foreground
// color and a 5x5 grid pattern that is horizontally mirrored (columns 0/4
// and 1/3 always match, column 2 is its own center), the same construction
// GitHub's original identicon generator uses.
func (s *Service) Identicon(seed string, size int) ([]byte, error) {
	if seed == "" {
		return nil, fmt.Errorf("seed must not be empty")
	}
	if size <= 0 {
		size = 256
	}

	sum := sha256.Sum256([]byte(seed))

	fg := color.RGBA{
		R: sum[0],
		G: sum[1],
		B: sum[2],
		A: 255,
	}
	bg := color.RGBA{R: 240, G: 240, B: 240, A: 255}

	const grid = 5
	const halfCols = 3 // columns 0,1,2 are hash-driven; 3,4 mirror 1,0
	cell := size / grid

	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			img.Set(x, y, bg)
		}
	}

	bitPos := 0
	for row := 0; row < grid; row++ {
		for col := 0; col < halfCols; col++ {
			byteIndex := bitPos % len(sum)
			bit := sum[byteIndex]&(1<<uint(bitPos%8)) != 0
			bitPos++
			if !bit {
				continue
			}
			drawBlock(img, col*cell, row*cell, cell, cell, fg)
			if col < 2 {
				mirrorCol := grid - 1 - col
				drawBlock(img, mirrorCol*cell, row*cell, cell, cell, fg)
			}
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("failed to encode identicon PNG: %w", err)
	}
	return buf.Bytes(), nil
}
