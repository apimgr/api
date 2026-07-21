package image

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// encodePNG builds a small solid-color PNG for use as test fixture
// input, so tests don't depend on files outside the repo.
func encodePNG(t *testing.T, width, height int, c color.Color) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

// Covers New: returns a non-nil, usable Service.
func TestNew(t *testing.T) {
	s := New()
	require.NotNil(t, s)
}

// Covers Load: valid PNG bytes decode successfully and record the
// detected format, while garbage bytes produce a decode error.
func TestLoad(t *testing.T) {
	t.Run("valid PNG", func(t *testing.T) {
		s := New()
		data := encodePNG(t, 4, 4, color.RGBA{R: 255, A: 255})
		require.NoError(t, s.Load(data))
		assert.Equal(t, "png", s.format)
		assert.NotNil(t, s.img)
	})

	t.Run("garbage bytes", func(t *testing.T) {
		s := New()
		err := s.Load([]byte("not an image"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to decode image")
	})

	t.Run("empty bytes", func(t *testing.T) {
		s := New()
		err := s.Load([]byte{})
		require.Error(t, err)
	})
}

// Covers LoadFile: a real file on disk decodes successfully, and a
// missing path produces a read error.
func TestLoadFile(t *testing.T) {
	t.Run("valid file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "test.png")
		require.NoError(t, os.WriteFile(path, encodePNG(t, 3, 3, color.RGBA{G: 255, A: 255}), 0644))

		s := New()
		require.NoError(t, s.LoadFile(path))
		assert.NotNil(t, s.img)
	})

	t.Run("missing file", func(t *testing.T) {
		s := New()
		err := s.LoadFile(filepath.Join(t.TempDir(), "does-not-exist.png"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to read image file")
	})
}

// Covers Bytes: encoding to each supported format when an image is
// loaded, the no-image-loaded error, and the unsupported-format error.
func TestBytes(t *testing.T) {
	s := New()
	require.NoError(t, s.Load(encodePNG(t, 5, 5, color.RGBA{B: 255, A: 255})))

	t.Run("png", func(t *testing.T) {
		out, err := s.Bytes("png")
		require.NoError(t, err)
		_, err = png.Decode(bytes.NewReader(out))
		require.NoError(t, err)
	})

	t.Run("jpeg", func(t *testing.T) {
		out, err := s.Bytes("jpeg")
		require.NoError(t, err)
		_, err = jpeg.Decode(bytes.NewReader(out))
		require.NoError(t, err)
	})

	t.Run("jpg alias", func(t *testing.T) {
		out, err := s.Bytes("JPG")
		require.NoError(t, err)
		_, err = jpeg.Decode(bytes.NewReader(out))
		require.NoError(t, err)
	})

	t.Run("gif", func(t *testing.T) {
		out, err := s.Bytes("gif")
		require.NoError(t, err)
		_, err = gif.Decode(bytes.NewReader(out))
		require.NoError(t, err)
	})

	t.Run("unsupported format", func(t *testing.T) {
		out, err := s.Bytes("bmp")
		require.Error(t, err)
		assert.Nil(t, out)
		assert.Contains(t, err.Error(), "unsupported output format")
	})

	t.Run("no image loaded", func(t *testing.T) {
		empty := New()
		out, err := empty.Bytes("png")
		require.Error(t, err)
		assert.Nil(t, out)
		assert.Contains(t, err.Error(), "no image loaded")
	})
}

// Covers Resize: successful resize (both upscale and downscale),
// no-image-loaded error, and non-positive dimension errors.
func TestResize(t *testing.T) {
	t.Run("no image loaded", func(t *testing.T) {
		s := New()
		err := s.Resize(10, 10)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no image loaded")
	})

	s := New()
	require.NoError(t, s.Load(encodePNG(t, 10, 10, color.RGBA{R: 128, A: 255})))

	t.Run("downscale", func(t *testing.T) {
		require.NoError(t, s.Resize(5, 5))
		assert.Equal(t, 5, s.img.Bounds().Dx())
		assert.Equal(t, 5, s.img.Bounds().Dy())
	})

	t.Run("upscale", func(t *testing.T) {
		require.NoError(t, s.Resize(20, 20))
		assert.Equal(t, 20, s.img.Bounds().Dx())
	})

	t.Run("zero width", func(t *testing.T) {
		err := s.Resize(0, 10)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must be positive")
	})

	t.Run("negative height", func(t *testing.T) {
		err := s.Resize(10, -1)
		require.Error(t, err)
	})
}

// Covers Crop: a valid in-bounds crop, no-image-loaded error,
// non-positive dimension errors, and an out-of-bounds crop rectangle.
func TestCrop(t *testing.T) {
	t.Run("no image loaded", func(t *testing.T) {
		s := New()
		err := s.Crop(0, 0, 5, 5)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no image loaded")
	})

	s := New()
	require.NoError(t, s.Load(encodePNG(t, 10, 10, color.RGBA{G: 200, A: 255})))

	t.Run("valid crop", func(t *testing.T) {
		require.NoError(t, s.Crop(2, 2, 4, 4))
		assert.Equal(t, 4, s.img.Bounds().Dx())
		assert.Equal(t, 4, s.img.Bounds().Dy())
	})

	t.Run("zero dimension", func(t *testing.T) {
		err := s.Crop(0, 0, 0, 5)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must be positive")
	})

	t.Run("out of bounds", func(t *testing.T) {
		fresh := New()
		require.NoError(t, fresh.Load(encodePNG(t, 10, 10, color.RGBA{A: 255})))
		err := fresh.Crop(5, 5, 100, 100)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "out of image bounds")
	})
}

// Covers Rotate: no-image-loaded error, a zero-degree no-op rotation,
// and a 90-degree rotation that swaps width/height for a non-square
// image.
func TestRotate(t *testing.T) {
	t.Run("no image loaded", func(t *testing.T) {
		s := New()
		err := s.Rotate(90)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no image loaded")
	})

	t.Run("zero degrees no-op", func(t *testing.T) {
		s := New()
		require.NoError(t, s.Load(encodePNG(t, 6, 6, color.RGBA{A: 255})))
		require.NoError(t, s.Rotate(0))
		assert.Equal(t, 6, s.img.Bounds().Dx())
	})

	t.Run("90 degrees swaps dimensions", func(t *testing.T) {
		s := New()
		require.NoError(t, s.Load(encodePNG(t, 20, 10, color.RGBA{A: 255})))
		require.NoError(t, s.Rotate(90))
		assert.Equal(t, 10, s.img.Bounds().Dx())
		assert.Equal(t, 20, s.img.Bounds().Dy())
	})
}

// Covers GetInfo: a valid file returns correct metadata, a missing
// path errors, and a non-image file errors on the metadata decode.
func TestGetInfo(t *testing.T) {
	t.Run("valid image", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "info.png")
		data := encodePNG(t, 12, 8, color.RGBA{A: 255})
		require.NoError(t, os.WriteFile(path, data, 0644))

		s := New()
		info, err := s.GetInfo(path)
		require.NoError(t, err)
		assert.Equal(t, 12, info.Width)
		assert.Equal(t, 8, info.Height)
		assert.Equal(t, "png", info.Format)
		assert.Equal(t, int64(len(data)), info.Size)
	})

	t.Run("missing file", func(t *testing.T) {
		s := New()
		info, err := s.GetInfo(filepath.Join(t.TempDir(), "missing.png"))
		require.Error(t, err)
		assert.Nil(t, info)
		assert.Contains(t, err.Error(), "failed to open image file")
	})

	t.Run("not an image", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "text.txt")
		require.NoError(t, os.WriteFile(path, []byte("hello world"), 0644))

		s := New()
		info, err := s.GetInfo(path)
		require.Error(t, err)
		assert.Nil(t, info)
		assert.Contains(t, err.Error(), "failed to read image metadata")
	})
}

// Covers GeneratePlaceholder: default format, an explicit format, an
// invalid hex color falling back to the default gray, and non-positive
// dimension errors.
func TestGeneratePlaceholder(t *testing.T) {
	s := New()

	t.Run("default format from empty string", func(t *testing.T) {
		out, err := s.GeneratePlaceholder(16, 16, "", "#336699")
		require.NoError(t, err)
		img, err := png.Decode(bytes.NewReader(out))
		require.NoError(t, err)
		assert.Equal(t, 16, img.Bounds().Dx())
	})

	t.Run("explicit jpeg format", func(t *testing.T) {
		out, err := s.GeneratePlaceholder(10, 10, "jpeg", "#000000")
		require.NoError(t, err)
		_, err = jpeg.Decode(bytes.NewReader(out))
		require.NoError(t, err)
	})

	t.Run("invalid hex color falls back to default gray", func(t *testing.T) {
		out, err := s.GeneratePlaceholder(4, 4, "png", "not-a-color")
		require.NoError(t, err)
		img, err := png.Decode(bytes.NewReader(out))
		require.NoError(t, err)
		r, g, b, _ := img.At(1, 1).RGBA()
		// Default fallback color is 0xCCCCCC.
		assert.Equal(t, r, g)
		assert.Equal(t, g, b)
	})

	t.Run("zero width", func(t *testing.T) {
		out, err := s.GeneratePlaceholder(0, 10, "png", "#ffffff")
		require.Error(t, err)
		assert.Nil(t, out)
	})

	t.Run("negative height", func(t *testing.T) {
		out, err := s.GeneratePlaceholder(10, -5, "png", "#ffffff")
		require.Error(t, err)
		assert.Nil(t, out)
	})
}

// Covers orDefaultFormat: blank/whitespace input defaults to "png",
// and a non-empty value passes through unchanged.
func TestOrDefaultFormat(t *testing.T) {
	assert.Equal(t, "png", orDefaultFormat(""))
	assert.Equal(t, "png", orDefaultFormat("   "))
	assert.Equal(t, "jpeg", orDefaultFormat("jpeg"))
}
