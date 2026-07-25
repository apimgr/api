package convert

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// RGB is an 8-bit-per-channel color
type RGB struct {
	R int `json:"r"`
	G int `json:"g"`
	B int `json:"b"`
}

// HSL is a hue/saturation/lightness color (hue in degrees 0-360,
// saturation/lightness as percentages 0-100)
type HSL struct {
	H float64 `json:"h"`
	S float64 `json:"s"`
	L float64 `json:"l"`
}

// HexToRGB parses a "#rrggbb" or "rrggbb" hex color into RGB components
func (s *Service) HexToRGB(hex string) (RGB, error) {
	hex = strings.TrimPrefix(strings.TrimSpace(hex), "#")
	if len(hex) != 6 {
		return RGB{}, fmt.Errorf("hex color must be 6 hex digits, got %q", hex)
	}
	r, err := strconv.ParseInt(hex[0:2], 16, 32)
	if err != nil {
		return RGB{}, fmt.Errorf("invalid red component: %w", err)
	}
	g, err := strconv.ParseInt(hex[2:4], 16, 32)
	if err != nil {
		return RGB{}, fmt.Errorf("invalid green component: %w", err)
	}
	b, err := strconv.ParseInt(hex[4:6], 16, 32)
	if err != nil {
		return RGB{}, fmt.Errorf("invalid blue component: %w", err)
	}
	return RGB{R: int(r), G: int(g), B: int(b)}, nil
}

// RGBToHex formats RGB components as a "#rrggbb" hex color
func (s *Service) RGBToHex(c RGB) (string, error) {
	if c.R < 0 || c.R > 255 || c.G < 0 || c.G > 255 || c.B < 0 || c.B > 255 {
		return "", fmt.Errorf("rgb components must be 0-255, got r=%d g=%d b=%d", c.R, c.G, c.B)
	}
	return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B), nil
}

// RGBToHSL converts RGB components to HSL
func (s *Service) RGBToHSL(c RGB) (HSL, error) {
	if c.R < 0 || c.R > 255 || c.G < 0 || c.G > 255 || c.B < 0 || c.B > 255 {
		return HSL{}, fmt.Errorf("rgb components must be 0-255, got r=%d g=%d b=%d", c.R, c.G, c.B)
	}
	r := float64(c.R) / 255
	g := float64(c.G) / 255
	b := float64(c.B) / 255

	max := math.Max(r, math.Max(g, b))
	min := math.Min(r, math.Min(g, b))
	l := (max + min) / 2

	var h, sat float64
	if max == min {
		h, sat = 0, 0
	} else {
		d := max - min
		if l > 0.5 {
			sat = d / (2 - max - min)
		} else {
			sat = d / (max + min)
		}
		switch max {
		case r:
			h = (g - b) / d
			if g < b {
				h += 6
			}
		case g:
			h = (b-r)/d + 2
		case b:
			h = (r-g)/d + 4
		}
		h /= 6
	}

	return HSL{H: math.Round(h * 360), S: math.Round(sat * 100), L: math.Round(l * 100)}, nil
}

// HSLToRGB converts HSL components to RGB
func (s *Service) HSLToRGB(c HSL) (RGB, error) {
	if c.H < 0 || c.H > 360 || c.S < 0 || c.S > 100 || c.L < 0 || c.L > 100 {
		return RGB{}, fmt.Errorf("hsl components must be h:0-360 s:0-100 l:0-100, got h=%v s=%v l=%v", c.H, c.S, c.L)
	}
	h := c.H / 360
	sat := c.S / 100
	l := c.L / 100

	if sat == 0 {
		v := math.Round(l * 255)
		return RGB{R: int(v), G: int(v), B: int(v)}, nil
	}

	var q float64
	if l < 0.5 {
		q = l * (1 + sat)
	} else {
		q = l + sat - l*sat
	}
	p := 2*l - q

	hueToRGB := func(p, q, t float64) float64 {
		if t < 0 {
			t++
		}
		if t > 1 {
			t--
		}
		switch {
		case t < 1.0/6.0:
			return p + (q-p)*6*t
		case t < 1.0/2.0:
			return q
		case t < 2.0/3.0:
			return p + (q-p)*(2.0/3.0-t)*6
		default:
			return p
		}
	}

	r := hueToRGB(p, q, h+1.0/3.0)
	g := hueToRGB(p, q, h)
	b := hueToRGB(p, q, h-1.0/3.0)

	return RGB{
		R: int(math.Round(r * 255)),
		G: int(math.Round(g * 255)),
		B: int(math.Round(b * 255)),
	}, nil
}
