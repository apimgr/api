package image

import (
	"bytes"
	"fmt"
	"image/gif"
	"image/jpeg"
	"image/png"
	"strings"
)

// Optimize re-encodes the loaded image in the requested format, returning
// the encoded bytes. quality only affects JPEG output (1-100, clamped,
// default 75 when out of range); it is otherwise ignored because Go's
// standard library has no lossy quality knob for PNG or GIF - PNG output
// always uses png.BestCompression (lossless, size varies with image
// content) and GIF output is unchanged from Bytes("gif"). Callers should
// not expect a fixed compression ratio for PNG/GIF; only JPEG quality is a
// real, honest trade-off.
func (s *Service) Optimize(format string, quality int) ([]byte, error) {
	if s.img == nil {
		return nil, fmt.Errorf("no image loaded")
	}

	if quality <= 0 || quality > 100 {
		quality = 75
	}

	var buf bytes.Buffer
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "jpeg", "jpg":
		if err := jpeg.Encode(&buf, s.img, &jpeg.Options{Quality: quality}); err != nil {
			return nil, fmt.Errorf("failed to encode JPEG: %w", err)
		}
	case "png":
		encoder := png.Encoder{CompressionLevel: png.BestCompression}
		if err := encoder.Encode(&buf, s.img); err != nil {
			return nil, fmt.Errorf("failed to encode PNG: %w", err)
		}
	case "gif":
		if err := gif.Encode(&buf, s.img, nil); err != nil {
			return nil, fmt.Errorf("failed to encode GIF: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported output format: %s", format)
	}
	return buf.Bytes(), nil
}
