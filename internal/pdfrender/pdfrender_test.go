package pdfrender

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func open(t *testing.T, name string) *Document {
	t.Helper()
	data, err := os.ReadFile("../../tests/pdf/" + name)
	require.NoError(t, err)
	doc, err := Open(data)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, doc.Close()) })
	return doc
}

func TestPagesSizeAndRender(t *testing.T) {
	doc := open(t, "five-pager.pdf")
	assert.Equal(t, 5, doc.NumPages())

	w, h, err := doc.PageSize(0)
	require.NoError(t, err)
	assert.Greater(t, w, 100.0)
	assert.Greater(t, h, 100.0)

	img, err := doc.RenderDPI(0, 72)
	require.NoError(t, err)
	assert.InDelta(t, w, img.Bounds().Dx(), 1, "72 DPI renders one pixel per point")
	assert.InDelta(t, h, img.Bounds().Dy(), 1)

	img2, err := doc.RenderDPI(0, 144)
	require.NoError(t, err)
	assert.InDelta(t, 2*img.Bounds().Dx(), img2.Bounds().Dx(), 2)

	// The corner of a normal document page is white, not transparent black.
	r, g, b, a := img.At(1, 1).RGBA()
	assert.Equal(t, [4]uint32{0xffff, 0xffff, 0xffff, 0xffff}, [4]uint32{r, g, b, a})
}

func TestOutOfRangeAndInvalid(t *testing.T) {
	doc := open(t, "sample.pdf")
	_, err := doc.RenderDPI(doc.NumPages(), 72)
	assert.ErrorContains(t, err, "out of range")
	_, _, err = doc.PageSize(-1)
	assert.Error(t, err)

	_, err = Open([]byte("not a pdf"))
	assert.Error(t, err)
}
