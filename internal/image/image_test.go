package image

import (
	"bytes"
	stdimage "image"
	"image/png"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"veloria/assets"
)

func newTestGenerator(t *testing.T) *Generator {
	t.Helper()
	gen, err := New(assets.FS)
	require.NoError(t, err)
	return gen
}

func TestNew(t *testing.T) {
	gen := newTestGenerator(t)
	assert.NotNil(t, gen.baseImage)
	assert.NotNil(t, gen.boldFont)
	assert.NotNil(t, gen.regularFont)
}

func TestRenderSearch_ValidPNG(t *testing.T) {
	gen := newTestGenerator(t)

	data, err := gen.RenderSearch("test_pattern", 42, 5, "plugins")
	require.NoError(t, err)
	require.NotEmpty(t, data)

	// Verify it's a valid PNG with the right dimensions.
	img, err := png.Decode(bytes.NewReader(data))
	require.NoError(t, err)

	bounds := img.Bounds()
	assert.Equal(t, 1200, bounds.Dx())
	assert.Equal(t, 630, bounds.Dy())
}

func TestRenderSearch_LongTerm(t *testing.T) {
	gen := newTestGenerator(t)

	longTerm := "a]very[long+regex{pattern}(that|exceeds)the.sixty+character+limit+for+display+purposes"
	data, err := gen.RenderSearch(longTerm, 100, 20, "themes")
	require.NoError(t, err)

	// Should still produce a valid image, no panic.
	_, err = png.Decode(bytes.NewReader(data))
	require.NoError(t, err)
}

func TestRenderSearch_ZeroMatches(t *testing.T) {
	gen := newTestGenerator(t)

	data, err := gen.RenderSearch("no_results", 0, 0, "cores")
	require.NoError(t, err)

	_, err = png.Decode(bytes.NewReader(data))
	require.NoError(t, err)
}

func TestRenderSearch_Unicode(t *testing.T) {
	gen := newTestGenerator(t)

	data, err := gen.RenderSearch("日本語テスト", 10, 3, "plugins")
	require.NoError(t, err)

	_, err = png.Decode(bytes.NewReader(data))
	require.NoError(t, err)
}

func TestRenderSearch_Dimensions(t *testing.T) {
	gen := newTestGenerator(t)

	data, err := gen.RenderSearch("test", 1, 1, "plugins")
	require.NoError(t, err)

	img, _, err := stdimage.Decode(bytes.NewReader(data))
	require.NoError(t, err)

	bounds := img.Bounds()
	assert.Equal(t, 1200, bounds.Max.X-bounds.Min.X)
	assert.Equal(t, 630, bounds.Max.Y-bounds.Min.Y)
}

func BenchmarkRenderSearch(b *testing.B) {
	gen, err := New(assets.FS)
	require.NoError(b, err)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := gen.RenderSearch("eval\\(.*base64_decode", 1234, 56, "plugins")
		if err != nil {
			b.Fatal(err)
		}
	}
}
