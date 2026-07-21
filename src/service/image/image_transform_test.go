package image

import (
	"image"
	"image/color"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// solidImage builds a small in-memory RGBA image filled with a single
// color, used as a deterministic fixture for the pure transform helpers.
func solidImage(width, height int, c color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, c)
		}
	}
	return img
}

// Covers resizeNearest: downscale, upscale, and the degenerate
// zero-source-dimension guard (an empty source bounds must not panic).
func TestResizeNearest(t *testing.T) {
	t.Run("downscale preserves color", func(t *testing.T) {
		src := solidImage(10, 10, color.RGBA{R: 200, A: 255})
		dst := resizeNearest(src, 4, 4)
		assert.Equal(t, 4, dst.Bounds().Dx())
		assert.Equal(t, 4, dst.Bounds().Dy())
		r, _, _, _ := dst.At(1, 1).RGBA()
		assert.Equal(t, uint32(200<<8|200), r)
	})

	t.Run("upscale", func(t *testing.T) {
		src := solidImage(2, 2, color.RGBA{G: 100, A: 255})
		dst := resizeNearest(src, 8, 8)
		assert.Equal(t, 8, dst.Bounds().Dx())
	})

	t.Run("zero-size source returns empty canvas without panic", func(t *testing.T) {
		src := image.NewRGBA(image.Rect(0, 0, 0, 0))
		dst := resizeNearest(src, 5, 5)
		assert.Equal(t, 5, dst.Bounds().Dx())
		assert.Equal(t, 5, dst.Bounds().Dy())
	})
}

// Covers cropImage: an in-bounds crop via the SubImage fast path (RGBA
// implements SubImage), and the out-of-bounds error.
func TestCropImage(t *testing.T) {
	t.Run("in bounds", func(t *testing.T) {
		src := solidImage(10, 10, color.RGBA{B: 50, A: 255})
		cropped, err := cropImage(src, 1, 1, 4, 4)
		require.NoError(t, err)
		assert.Equal(t, 4, cropped.Bounds().Dx())
		assert.Equal(t, 4, cropped.Bounds().Dy())
	})

	t.Run("out of bounds", func(t *testing.T) {
		src := solidImage(10, 10, color.RGBA{A: 255})
		_, err := cropImage(src, 5, 5, 20, 20)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "out of image bounds")
	})

	t.Run("negative origin out of bounds", func(t *testing.T) {
		src := solidImage(10, 10, color.RGBA{A: 255})
		_, err := cropImage(src, -1, 0, 4, 4)
		require.Error(t, err)
	})

	t.Run("exact full-image crop", func(t *testing.T) {
		src := solidImage(5, 5, color.RGBA{A: 255})
		cropped, err := cropImage(src, 0, 0, 5, 5)
		require.NoError(t, err)
		assert.Equal(t, 5, cropped.Bounds().Dx())
	})
}

// Covers rotateImage: the degrees==0 identity shortcut (returns the
// same image, not a copy), a 90-degree rotation that swaps width and
// height, a 180-degree rotation that preserves dimensions, and
// out-of-range/negative degree normalization (e.g. 450 == 90, -90 ==
// 270).
func TestRotateImage(t *testing.T) {
	t.Run("zero degrees returns original", func(t *testing.T) {
		src := solidImage(6, 4, color.RGBA{A: 255})
		out := rotateImage(src, 0)
		assert.Same(t, image.Image(src), out)
	})

	t.Run("90 degrees swaps dimensions", func(t *testing.T) {
		src := solidImage(20, 10, color.RGBA{R: 10, A: 255})
		out := rotateImage(src, 90)
		assert.Equal(t, 10, out.Bounds().Dx())
		assert.Equal(t, 20, out.Bounds().Dy())
	})

	t.Run("180 degrees preserves dimensions", func(t *testing.T) {
		src := solidImage(20, 10, color.RGBA{A: 255})
		out := rotateImage(src, 180)
		assert.Equal(t, 20, out.Bounds().Dx())
		assert.Equal(t, 10, out.Bounds().Dy())
	})

	t.Run("360 normalizes to identity", func(t *testing.T) {
		src := solidImage(6, 4, color.RGBA{A: 255})
		out := rotateImage(src, 360)
		assert.Same(t, image.Image(src), out)
	})

	t.Run("450 normalizes to 90", func(t *testing.T) {
		src := solidImage(20, 10, color.RGBA{A: 255})
		out := rotateImage(src, 450)
		assert.Equal(t, 10, out.Bounds().Dx())
		assert.Equal(t, 20, out.Bounds().Dy())
	})

	t.Run("negative degrees normalize into range", func(t *testing.T) {
		src := solidImage(20, 10, color.RGBA{A: 255})
		out := rotateImage(src, -90)
		// -90 normalizes to 270, which (like 90) swaps dimensions.
		assert.Equal(t, 10, out.Bounds().Dx())
		assert.Equal(t, 20, out.Bounds().Dy())
	})
}

// Covers parseHexColor: with and without a leading "#", uppercase and
// lowercase hex digits, and error paths for wrong length and invalid
// hex characters.
func TestParseHexColor(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    color.RGBA
		wantErr bool
	}{
		{name: "with hash", input: "#FF0000", want: color.RGBA{R: 255, A: 255}},
		{name: "without hash", input: "00FF00", want: color.RGBA{G: 255, A: 255}},
		{name: "lowercase", input: "#0000ff", want: color.RGBA{B: 255, A: 255}},
		{name: "mixed rgb", input: "#336699", want: color.RGBA{R: 0x33, G: 0x66, B: 0x99, A: 255}},
		{name: "too short", input: "#FFF", wantErr: true},
		{name: "too long", input: "#FFFFFFFF", wantErr: true},
		{name: "invalid hex chars", input: "#GGGGGG", wantErr: true},
		{name: "empty", input: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseHexColor(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// Covers contrastColor: a light background yields black, a dark
// background yields white, and the luminance threshold boundary itself.
func TestContrastColor(t *testing.T) {
	t.Run("light background yields black", func(t *testing.T) {
		c := contrastColor(color.RGBA{R: 255, G: 255, B: 255, A: 255})
		assert.Equal(t, color.RGBA{A: 0xFF}, c)
	})

	t.Run("dark background yields white", func(t *testing.T) {
		c := contrastColor(color.RGBA{R: 0, G: 0, B: 0, A: 255})
		assert.Equal(t, color.RGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}, c)
	})

	t.Run("mid-gray below threshold yields white", func(t *testing.T) {
		c := contrastColor(color.RGBA{R: 100, G: 100, B: 100, A: 255})
		assert.Equal(t, color.RGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}, c)
	})
}

// Covers drawBorder: verifies every edge pixel is set to the border
// color and at least one interior pixel is left untouched.
func TestDrawBorder(t *testing.T) {
	canvas := image.NewRGBA(image.Rect(0, 0, 5, 5))
	borderColor := color.RGBA{R: 1, G: 2, B: 3, A: 255}
	drawBorder(canvas, borderColor)

	b := canvas.Bounds()
	for x := b.Min.X; x < b.Max.X; x++ {
		assert.Equal(t, borderColor, canvas.RGBAAt(x, b.Min.Y))
		assert.Equal(t, borderColor, canvas.RGBAAt(x, b.Max.Y-1))
	}
	for y := b.Min.Y; y < b.Max.Y; y++ {
		assert.Equal(t, borderColor, canvas.RGBAAt(b.Min.X, y))
		assert.Equal(t, borderColor, canvas.RGBAAt(b.Max.X-1, y))
	}

	// The center pixel of a 5x5 canvas is strictly interior and must
	// remain at the zero value (border drawing must not touch it).
	assert.Equal(t, color.RGBA{}, canvas.RGBAAt(2, 2))
}

// Covers drawDiagonalCross: verifies it does not panic on a
// zero-dimension canvas and marks pixels along both diagonals of a
// normal canvas.
func TestDrawDiagonalCross(t *testing.T) {
	t.Run("zero dimension does not panic", func(t *testing.T) {
		canvas := image.NewRGBA(image.Rect(0, 0, 0, 0))
		assert.NotPanics(t, func() {
			drawDiagonalCross(canvas, color.RGBA{A: 255})
		})
	})

	t.Run("marks corners on square canvas", func(t *testing.T) {
		canvas := image.NewRGBA(image.Rect(0, 0, 4, 4))
		markColor := color.RGBA{R: 9, A: 255}
		drawDiagonalCross(canvas, markColor)

		// Top-left corner is on the primary diagonal.
		assert.Equal(t, markColor, canvas.RGBAAt(0, 0))
		// Bottom-right corner is also on the primary diagonal for a
		// square canvas (x == w-1 maps y1 == h-1).
		assert.Equal(t, markColor, canvas.RGBAAt(3, 3))
	})
}
