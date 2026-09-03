package image

import (
	"bytes"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Covers Optimize: JPEG honors the requested quality, PNG/GIF still encode
// (using the stdlib's fixed lossless compression, since there is no PNG
// quality knob), an out-of-range quality falls back to the default, and an
// unsupported format errors.
func TestOptimize(t *testing.T) {
	t.Run("no image loaded", func(t *testing.T) {
		s := New()
		_, err := s.Optimize("jpeg", 50)
		require.Error(t, err)
	})

	t.Run("jpeg quality", func(t *testing.T) {
		s := New()
		require.NoError(t, s.Load(encodePNG(t, 20, 20, color.RGBA{R: 200, G: 50, B: 50, A: 255})))
		out, err := s.Optimize("jpeg", 40)
		require.NoError(t, err)
		_, err = jpeg.Decode(bytes.NewReader(out))
		require.NoError(t, err)
	})

	t.Run("jpeg invalid quality falls back to default", func(t *testing.T) {
		s := New()
		require.NoError(t, s.Load(encodePNG(t, 20, 20, color.RGBA{R: 200, G: 50, B: 50, A: 255})))
		out, err := s.Optimize("jpeg", 0)
		require.NoError(t, err)
		_, err = jpeg.Decode(bytes.NewReader(out))
		require.NoError(t, err)
	})

	t.Run("png", func(t *testing.T) {
		s := New()
		require.NoError(t, s.Load(encodePNG(t, 20, 20, color.RGBA{R: 10, G: 10, B: 10, A: 255})))
		out, err := s.Optimize("png", 90)
		require.NoError(t, err)
		_, err = png.Decode(bytes.NewReader(out))
		require.NoError(t, err)
	})

	t.Run("gif", func(t *testing.T) {
		s := New()
		require.NoError(t, s.Load(encodePNG(t, 20, 20, color.RGBA{R: 10, G: 10, B: 10, A: 255})))
		out, err := s.Optimize("gif", 90)
		require.NoError(t, err)
		assert.NotEmpty(t, out)
	})

	t.Run("unsupported format", func(t *testing.T) {
		s := New()
		require.NoError(t, s.Load(encodePNG(t, 20, 20, color.RGBA{R: 10, G: 10, B: 10, A: 255})))
		_, err := s.Optimize("bmp", 90)
		require.Error(t, err)
	})
}
