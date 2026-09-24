package pdfrender

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func open(t *testing.T, name string) *Document {
	t.Helper()
	data, err := os.ReadFile("../../tests/pdf/" + name)
	require.NoError(t, err)
	return openBytes(t, data)
}

func openBytes(t *testing.T, data []byte) *Document {
	t.Helper()
	doc, err := Open(context.Background(), data)
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

	_, err = Open(context.Background(), []byte("not a pdf"))
	assert.Error(t, err)
}

// filledFormPDF is a one-page PDF with a text field whose value has no
// appearance stream (NeedAppearances), as many form tools produce. Only form
// rendering draws such a value.
func filledFormPDF() []byte {
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R] /NeedAppearances true /DA (/Helv 24 Tf 0 g) /DR << /Font << /Helv 5 0 R >> >> >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Annots [4 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (name) /V (FILLED VALUE) /Rect [20 80 280 120] /P 3 0 R /F 4 /DA (/Helv 24 Tf 0 g) >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	var b bytes.Buffer
	b.WriteString("%PDF-1.7\n")
	offsets := make([]int, len(objects))
	for i, o := range objects {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return b.Bytes()
}

// darkPixels counts clearly non-white pixels inside r.
func darkPixels(img *image.RGBA, r image.Rectangle) int {
	n := 0
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			c := img.RGBAAt(x, y)
			if int(c.R)+int(c.G)+int(c.B) < 3*128 {
				n++
			}
		}
	}
	return n
}

func TestRendersFormFieldValues(t *testing.T) {
	doc := openBytes(t, filledFormPDF())
	img, err := doc.RenderDPI(0, 72)
	require.NoError(t, err)
	// The field sits at y=80..120 in PDF space (origin bottom-left) on a
	// 200pt page, i.e. rows 80..120 from the top at 72 DPI.
	field := image.Rect(20, 80, 280, 120)
	assert.Greater(t, darkPixels(img, field), 50, "the field value must be drawn")
}

func TestOpenStopsWhenContextIsDone(t *testing.T) {
	data, err := os.ReadFile("../../tests/pdf/sample.pdf")
	require.NoError(t, err)

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = Open(canceled, data)
	assert.ErrorIs(t, err, context.Canceled)

	// With every instance in use, waiting for one ends with the context.
	for i := 0; i < maxInstances; i++ {
		openBytes(t, data)
	}
	short, cancelShort := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancelShort()
	start := time.Now()
	_, err = Open(short, data)
	assert.Error(t, err)
	assert.Less(t, time.Since(start), 5*time.Second, "waiting stops with the context, not after instanceTimeout")
}
