package image

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"strings"
)

// Service provides image manipulation utilities. Load (or LoadFile) must
// be called before Resize/Crop/Rotate/Bytes operate on image data.
type Service struct {
	img    image.Image
	format string
}

// New creates a new Image service
func New() *Service {
	return &Service{}
}

// ImageInfo describes basic image metadata
type ImageInfo struct {
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Format string `json:"format"`
	Size   int64  `json:"size"`
}

// Load decodes raw image bytes (PNG, JPEG, or GIF) into the service state
func (s *Service) Load(data []byte) error {
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to decode image: %w", err)
	}
	s.img = img
	s.format = format
	return nil
}

// LoadFile decodes an image from a filesystem path into the service state
func (s *Service) LoadFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read image file: %w", err)
	}
	return s.Load(data)
}

// Bytes encodes the current image state to the requested format
// (png, jpeg, or gif)
func (s *Service) Bytes(format string) ([]byte, error) {
	if s.img == nil {
		return nil, fmt.Errorf("no image loaded")
	}

	var buf bytes.Buffer
	switch strings.ToLower(format) {
	case "png":
		if err := png.Encode(&buf, s.img); err != nil {
			return nil, fmt.Errorf("failed to encode PNG: %w", err)
		}
	case "jpeg", "jpg":
		if err := jpeg.Encode(&buf, s.img, &jpeg.Options{Quality: 90}); err != nil {
			return nil, fmt.Errorf("failed to encode JPEG: %w", err)
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

// Resize scales the loaded image to width x height using nearest-neighbor
// sampling
func (s *Service) Resize(width, height int) error {
	if s.img == nil {
		return fmt.Errorf("no image loaded")
	}
	if width <= 0 || height <= 0 {
		return fmt.Errorf("width and height must be positive")
	}
	s.img = resizeNearest(s.img, width, height)
	return nil
}

// Crop extracts the width x height region starting at (x, y) from the
// loaded image
func (s *Service) Crop(x, y, width, height int) error {
	if s.img == nil {
		return fmt.Errorf("no image loaded")
	}
	if width <= 0 || height <= 0 {
		return fmt.Errorf("width and height must be positive")
	}
	cropped, err := cropImage(s.img, x, y, width, height)
	if err != nil {
		return err
	}
	s.img = cropped
	return nil
}

// Rotate rotates the loaded image clockwise by the given number of degrees
func (s *Service) Rotate(degrees int) error {
	if s.img == nil {
		return fmt.Errorf("no image loaded")
	}
	s.img = rotateImage(s.img, degrees)
	return nil
}

// GetInfo reads image dimensions, format, and file size from a filesystem
// path without decoding the full pixel data
func (s *Service) GetInfo(imagePath string) (*ImageInfo, error) {
	file, err := os.Open(imagePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open image file: %w", err)
	}
	defer file.Close()

	cfg, format, err := image.DecodeConfig(file)
	if err != nil {
		return nil, fmt.Errorf("failed to read image metadata: %w", err)
	}

	stat, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to stat image file: %w", err)
	}

	return &ImageInfo{
		Width:  cfg.Width,
		Height: cfg.Height,
		Format: format,
		Size:   stat.Size(),
	}, nil
}

// GeneratePlaceholder renders a solid-background placeholder image of the
// given dimensions in the requested output format (png, jpeg, or gif)
func (s *Service) GeneratePlaceholder(width, height int, format, bgColorHex string) ([]byte, error) {
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("width and height must be positive")
	}

	bg, err := parseHexColor(bgColorHex)
	if err != nil {
		bg = color.RGBA{R: 0xCC, G: 0xCC, B: 0xCC, A: 0xFF}
	}
	border := contrastColor(bg)

	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			canvas.Set(x, y, bg)
		}
	}

	drawBorder(canvas, border)
	drawDiagonalCross(canvas, border)

	s.img = canvas
	s.format = "png"
	return s.Bytes(orDefaultFormat(format))
}

func orDefaultFormat(format string) string {
	if strings.TrimSpace(format) == "" {
		return "png"
	}
	return format
}
