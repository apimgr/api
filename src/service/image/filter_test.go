package image

import (
	"image/color"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Covers ApplyFilter: every supported filter mutates the loaded image
// without error, and an unknown filter name errors instead of silently
// no-op-ing.
func TestApplyFilter(t *testing.T) {
	t.Run("no image loaded", func(t *testing.T) {
		s := New()
		err := s.ApplyFilter("grayscale", 0)
		require.Error(t, err)
	})

	filters := []string{"grayscale", "greyscale", "sepia", "invert", "blur", "brighten", "darken"}
	for _, name := range filters {
		t.Run(name, func(t *testing.T) {
			s := New()
			require.NoError(t, s.Load(encodePNG(t, 10, 10, color.RGBA{R: 100, G: 150, B: 200, A: 255})))
			require.NoError(t, s.ApplyFilter(name, 0.3))
			assert.Equal(t, 10, s.Bounds().Dx())
			assert.Equal(t, 10, s.Bounds().Dy())
		})
	}

	t.Run("unknown filter", func(t *testing.T) {
		s := New()
		require.NoError(t, s.Load(encodePNG(t, 10, 10, color.RGBA{R: 100, G: 150, B: 200, A: 255})))
		err := s.ApplyFilter("does-not-exist", 0)
		require.Error(t, err)
	})

	t.Run("invert actually inverts", func(t *testing.T) {
		s := New()
		require.NoError(t, s.Load(encodePNG(t, 2, 2, color.RGBA{R: 10, G: 20, B: 30, A: 255})))
		require.NoError(t, s.ApplyFilter("invert", 0))
		r, g, b, _ := s.img.At(0, 0).RGBA()
		assert.Equal(t, uint8(245), uint8(r/257))
		assert.Equal(t, uint8(235), uint8(g/257))
		assert.Equal(t, uint8(225), uint8(b/257))
	})
}
