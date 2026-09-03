package image

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"strings"
)

// ApplyFilter mutates the loaded image in place using one of the supported
// named filters: grayscale, sepia, invert, blur, brighten, darken. amount is
// interpreted per-filter (ignored by grayscale/sepia/invert, blur radius in
// pixels for blur, a -1..1 scale factor for brighten/darken). An unknown
// filter name is an error, never a silent no-op.
func (s *Service) ApplyFilter(name string, amount float64) error {
	if s.img == nil {
		return fmt.Errorf("no image loaded")
	}

	switch strings.ToLower(strings.TrimSpace(name)) {
	case "grayscale", "greyscale":
		s.img = filterGrayscale(s.img)
	case "sepia":
		s.img = filterSepia(s.img)
	case "invert":
		s.img = filterInvert(s.img)
	case "blur":
		radius := int(math.Round(amount))
		if radius <= 0 {
			radius = 2
		}
		s.img = filterBoxBlur(s.img, radius)
	case "brighten":
		if amount <= 0 {
			amount = 0.2
		}
		s.img = filterBrightness(s.img, amount)
	case "darken":
		if amount <= 0 {
			amount = 0.2
		}
		s.img = filterBrightness(s.img, -amount)
	default:
		return fmt.Errorf("unsupported filter: %s", name)
	}
	return nil
}

// filterGrayscale converts every pixel to its luminance-weighted gray value.
func filterGrayscale(src image.Image) image.Image {
	bounds := src.Bounds()
	dst := image.NewRGBA(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, a := src.At(x, y).RGBA()
			gray := uint8((0.299*float64(r) + 0.587*float64(g) + 0.114*float64(b)) / 257)
			dst.Set(x, y, color.RGBA{R: gray, G: gray, B: gray, A: uint8(a / 257)})
		}
	}
	return dst
}

// filterSepia applies the standard sepia color transform matrix, clamping
// each output channel to the valid 0-255 range.
func filterSepia(src image.Image) image.Image {
	bounds := src.Bounds()
	dst := image.NewRGBA(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, a := src.At(x, y).RGBA()
			fr, fg, fb := float64(r/257), float64(g/257), float64(b/257)
			sr := clampChannel(0.393*fr + 0.769*fg + 0.189*fb)
			sg := clampChannel(0.349*fr + 0.686*fg + 0.168*fb)
			sb := clampChannel(0.272*fr + 0.534*fg + 0.131*fb)
			dst.Set(x, y, color.RGBA{R: sr, G: sg, B: sb, A: uint8(a / 257)})
		}
	}
	return dst
}

// filterInvert flips each color channel (255 - value), leaving alpha intact.
func filterInvert(src image.Image) image.Image {
	bounds := src.Bounds()
	dst := image.NewRGBA(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, a := src.At(x, y).RGBA()
			dst.Set(x, y, color.RGBA{
				R: 255 - uint8(r/257),
				G: 255 - uint8(g/257),
				B: 255 - uint8(b/257),
				A: uint8(a / 257),
			})
		}
	}
	return dst
}

// filterBoxBlur applies a simple (2*radius+1)^2 box blur, sampling only
// pixels that fall inside the image bounds.
func filterBoxBlur(src image.Image, radius int) image.Image {
	bounds := src.Bounds()
	dst := image.NewRGBA(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			var sumR, sumG, sumB, sumA, count float64
			for dy := -radius; dy <= radius; dy++ {
				for dx := -radius; dx <= radius; dx++ {
					sx, sy := x+dx, y+dy
					if sx < bounds.Min.X || sx >= bounds.Max.X || sy < bounds.Min.Y || sy >= bounds.Max.Y {
						continue
					}
					r, g, b, a := src.At(sx, sy).RGBA()
					sumR += float64(r)
					sumG += float64(g)
					sumB += float64(b)
					sumA += float64(a)
					count++
				}
			}
			if count == 0 {
				count = 1
			}
			dst.Set(x, y, color.RGBA{
				R: uint8(sumR / count / 257),
				G: uint8(sumG / count / 257),
				B: uint8(sumB / count / 257),
				A: uint8(sumA / count / 257),
			})
		}
	}
	return dst
}

// filterBrightness shifts every RGB channel by amount * 255, where amount is
// a -1..1 scale factor (positive brightens, negative darkens), clamping to
// the valid 0-255 range.
func filterBrightness(src image.Image, amount float64) image.Image {
	bounds := src.Bounds()
	dst := image.NewRGBA(bounds)
	shift := amount * 255
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, a := src.At(x, y).RGBA()
			dst.Set(x, y, color.RGBA{
				R: clampChannel(float64(r/257) + shift),
				G: clampChannel(float64(g/257) + shift),
				B: clampChannel(float64(b/257) + shift),
				A: uint8(a / 257),
			})
		}
	}
	return dst
}

// clampChannel clamps a floating point channel value to the valid 0-255
// range and rounds it to the nearest uint8.
func clampChannel(v float64) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(math.Round(v))
}
