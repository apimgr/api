package generate

import (
	"bytes"
	"fmt"
	"image/png"
	"strings"

	"github.com/boombuler/barcode"
	"github.com/boombuler/barcode/code128"
	"github.com/boombuler/barcode/code39"
	"github.com/boombuler/barcode/ean"
)

// Barcode renders a 1D barcode as PNG bytes for the given format and data.
// Supported formats: code128, code39, ean13, upca (encoded as EAN-13 with a
// leading zero digit, per the standard UPC-A/EAN-13 relationship). width and
// height set the final rendered pixel size after scaling.
func (s *Service) Barcode(format, data string, width, height int) ([]byte, error) {
	if width <= 0 {
		width = 300
	}
	if height <= 0 {
		height = 100
	}

	var bc barcode.Barcode
	var err error

	switch strings.ToLower(format) {
	case "code128":
		bc, err = code128.Encode(data)
	case "code39":
		bc, err = code39.Encode(data, true, false)
	case "ean13":
		bc, err = ean.Encode(data)
	case "upca":
		bc, err = encodeUPCA(data)
	default:
		return nil, fmt.Errorf("unsupported barcode format %q: supported formats are code128, code39, ean13, upca", format)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to encode %s barcode: %w", format, err)
	}

	scaled, err := barcode.Scale(bc, width, height)
	if err != nil {
		return nil, fmt.Errorf("failed to scale barcode: %w", err)
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, scaled); err != nil {
		return nil, fmt.Errorf("failed to encode barcode PNG: %w", err)
	}
	return buf.Bytes(), nil
}

// encodeUPCA encodes a UPC-A code by delegating to the EAN-13 encoder with a
// leading zero digit prepended, which is the standard way UPC-A codes are
// represented within the EAN-13 numbering system.
func encodeUPCA(data string) (barcode.Barcode, error) {
	code := data
	switch len(code) {
	case 11, 12:
		code = "0" + code
	case 13:
		// already EAN-13 form (leading zero expected for a true UPC-A value)
	default:
		return nil, fmt.Errorf("upca data must be 11-12 digits (or 13 with the leading zero already present), got %d", len(code))
	}
	return ean.Encode(code)
}
