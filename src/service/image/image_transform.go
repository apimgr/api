package image

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"strconv"
	"strings"

	// Blank imports register PNG/JPEG/GIF decoders with image.Decode
	// and image.DecodeConfig
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
)

// resizeNearest scales img to width x height using nearest-neighbor
// sampling (pure standard library, no external dependency)
func resizeNearest(img image.Image, width, height int) image.Image {
	srcBounds := img.Bounds()
	sw, sh := srcBounds.Dx(), srcBounds.Dy()

	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	if sw == 0 || sh == 0 {
		return dst
	}

	xRatio := float64(sw) / float64(width)
	yRatio := float64(sh) / float64(height)

	for y := 0; y < height; y++ {
		srcY := srcBounds.Min.Y + int(float64(y)*yRatio)
		for x := 0; x < width; x++ {
			srcX := srcBounds.Min.X + int(float64(x)*xRatio)
			dst.Set(x, y, img.At(srcX, srcY))
		}
	}
	return dst
}

// cropImage extracts the width x height region starting at (x, y)
func cropImage(img image.Image, x, y, width, height int) (image.Image, error) {
	b := img.Bounds()
	rect := image.Rect(b.Min.X+x, b.Min.Y+y, b.Min.X+x+width, b.Min.Y+y+height)
	if !rect.In(b) {
		return nil, fmt.Errorf("crop rectangle is out of image bounds")
	}

	if sub, ok := img.(interface {
		SubImage(r image.Rectangle) image.Image
	}); ok {
		return sub.SubImage(rect), nil
	}

	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(dst, dst.Bounds(), img, rect.Min, draw.Src)
	return dst, nil
}

// rotateImage rotates img clockwise by degrees using nearest-neighbor
// sampling; the output canvas grows to fit the rotated bounds
func rotateImage(img image.Image, degrees int) image.Image {
	deg := ((degrees % 360) + 360) % 360
	if deg == 0 {
		return img
	}

	b := img.Bounds()
	w, h := b.Dx(), b.Dy()

	angle := float64(deg) * math.Pi / 180
	cos, sin := math.Cos(angle), math.Sin(angle)

	newW := int(math.Ceil(math.Abs(float64(w)*cos) + math.Abs(float64(h)*sin)))
	newH := int(math.Ceil(math.Abs(float64(w)*sin) + math.Abs(float64(h)*cos)))
	if newW <= 0 {
		newW = 1
	}
	if newH <= 0 {
		newH = 1
	}

	cx, cy := float64(w)/2, float64(h)/2
	ncx, ncy := float64(newW)/2, float64(newH)/2

	dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
	for y := 0; y < newH; y++ {
		for x := 0; x < newW; x++ {
			dx := float64(x) - ncx
			dy := float64(y) - ncy

			srcX := dx*cos + dy*sin + cx
			srcY := -dx*sin + dy*cos + cy

			sx := int(math.Round(srcX)) + b.Min.X
			sy := int(math.Round(srcY)) + b.Min.Y

			if sx >= b.Min.X && sx < b.Max.X && sy >= b.Min.Y && sy < b.Max.Y {
				dst.Set(x, y, img.At(sx, sy))
			}
		}
	}
	return dst
}

// parseHexColor parses a "#RRGGBB" or "RRGGBB" string into a color.RGBA
func parseHexColor(s string) (color.RGBA, error) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(s) != 6 {
		return color.RGBA{}, fmt.Errorf("invalid hex color: %s", s)
	}

	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return color.RGBA{}, fmt.Errorf("invalid hex color: %s", s)
	}

	return color.RGBA{
		R: uint8(v >> 16),
		G: uint8(v >> 8),
		B: uint8(v),
		A: 0xFF,
	}, nil
}

// contrastColor picks black or white depending on the perceived luminance
// of bg, so borders/markers stay visible against any background
func contrastColor(bg color.RGBA) color.RGBA {
	luminance := 0.299*float64(bg.R) + 0.587*float64(bg.G) + 0.114*float64(bg.B)
	if luminance > 140 {
		return color.RGBA{A: 0xFF}
	}
	return color.RGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}
}

// drawBorder draws a one-pixel border around the canvas
func drawBorder(canvas *image.RGBA, c color.RGBA) {
	b := canvas.Bounds()
	for x := b.Min.X; x < b.Max.X; x++ {
		canvas.Set(x, b.Min.Y, c)
		canvas.Set(x, b.Max.Y-1, c)
	}
	for y := b.Min.Y; y < b.Max.Y; y++ {
		canvas.Set(b.Min.X, y, c)
		canvas.Set(b.Max.X-1, y, c)
	}
}

// drawDiagonalCross draws an "X" across the canvas, the conventional
// visual marker for a placeholder/missing-image asset
func drawDiagonalCross(canvas *image.RGBA, c color.RGBA) {
	b := canvas.Bounds()
	w, h := b.Dx(), b.Dy()
	if w == 0 || h == 0 {
		return
	}

	for x := 0; x < w; x++ {
		y1 := x * h / w
		canvas.Set(b.Min.X+x, b.Min.Y+y1, c)

		y2 := h - 1 - y1
		canvas.Set(b.Min.X+x, b.Min.Y+y2, c)
	}
}
