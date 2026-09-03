package image

import (
	"image/color"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Covers Watermark: valid text draws without error and leaves image
// dimensions unchanged, empty text errors, and an out-of-range opacity
// falls back to the default rather than erroring.
func TestWatermark(t *testing.T) {
	t.Run("no image loaded", func(t *testing.T) {
		s := New()
		err := s.Watermark("hello", 0.5)
		require.Error(t, err)
	})

	t.Run("empty text", func(t *testing.T) {
		s := New()
		require.NoError(t, s.Load(encodePNG(t, 50, 50, color.RGBA{R: 10, G: 10, B: 10, A: 255})))
		err := s.Watermark("   ", 0.5)
		require.Error(t, err)
	})

	t.Run("valid text", func(t *testing.T) {
		s := New()
		require.NoError(t, s.Load(encodePNG(t, 50, 50, color.RGBA{R: 10, G: 10, B: 10, A: 255})))
		require.NoError(t, s.Watermark("hello", 0.5))
		assert.Equal(t, 50, s.Bounds().Dx())
		assert.Equal(t, 50, s.Bounds().Dy())
	})

	t.Run("out of range opacity falls back to default", func(t *testing.T) {
		s := New()
		require.NoError(t, s.Load(encodePNG(t, 50, 50, color.RGBA{R: 10, G: 10, B: 10, A: 255})))
		require.NoError(t, s.Watermark("hi", 5))
	})

	t.Run("unknown character falls back to solid block", func(t *testing.T) {
		s := New()
		require.NoError(t, s.Load(encodePNG(t, 50, 50, color.RGBA{R: 10, G: 10, B: 10, A: 255})))
		require.NoError(t, s.Watermark("hi!", 0.5))
	})
}
