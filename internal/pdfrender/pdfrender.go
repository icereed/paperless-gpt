// Package pdfrender opens PDFs and renders their pages to images.
//
// It uses PDFium (Apache-2.0) through go-pdfium's WebAssembly runtime, so it
// needs neither CGO nor a system library, and it keeps copyleft code out of
// the paperless-gpt binary.
package pdfrender

import (
	"fmt"
	"image"
	"image/draw"
	"math"
	"sync"
	"time"

	"github.com/klippa-app/go-pdfium"
	"github.com/klippa-app/go-pdfium/enums"
	"github.com/klippa-app/go-pdfium/references"
	"github.com/klippa-app/go-pdfium/requests"
	"github.com/klippa-app/go-pdfium/webassembly"
)

// maxInstances bounds how many documents are open at the same time; each
// PDFium instance has its own WebAssembly memory.
const maxInstances = 4

// instanceTimeout is how long Open waits for a free PDFium instance.
const instanceTimeout = 2 * time.Minute

var (
	poolOnce sync.Once
	pool     pdfium.Pool
	poolErr  error
)

func getPool() (pdfium.Pool, error) {
	poolOnce.Do(func() {
		pool, poolErr = webassembly.Init(webassembly.Config{MinIdle: 0, MaxIdle: 1, MaxTotal: maxInstances})
	})
	return pool, poolErr
}

// Document is an open PDF. It is not safe for concurrent use.
type Document struct {
	instance pdfium.Pdfium
	doc      references.FPDF_DOCUMENT
	pages    int
}

// Open parses a PDF held in memory.
func Open(data []byte) (*Document, error) {
	p, err := getPool()
	if err != nil {
		return nil, fmt.Errorf("initializing PDFium: %w", err)
	}
	instance, err := p.GetInstance(instanceTimeout)
	if err != nil {
		return nil, fmt.Errorf("getting a PDFium instance: %w", err)
	}
	opened, err := instance.OpenDocument(&requests.OpenDocument{File: &data})
	if err != nil {
		instance.Close()
		return nil, fmt.Errorf("opening PDF: %w", err)
	}
	count, err := instance.FPDF_GetPageCount(&requests.FPDF_GetPageCount{Document: opened.Document})
	if err != nil {
		instance.FPDF_CloseDocument(&requests.FPDF_CloseDocument{Document: opened.Document})
		instance.Close()
		return nil, fmt.Errorf("counting pages: %w", err)
	}
	return &Document{instance: instance, doc: opened.Document, pages: count.PageCount}, nil
}

// NumPages returns the number of pages.
func (d *Document) NumPages() int { return d.pages }

func (d *Document) page(index int) (requests.Page, error) {
	if index < 0 || index >= d.pages {
		return requests.Page{}, fmt.Errorf("page %d out of range: document has %d pages", index+1, d.pages)
	}
	return requests.Page{ByIndex: &requests.PageByIndex{Document: d.doc, Index: index}}, nil
}

// PageSize returns the size of a page (0-based) in points (1/72 inch).
func (d *Document) PageSize(index int) (width, height float64, err error) {
	page, err := d.page(index)
	if err != nil {
		return 0, 0, err
	}
	size, err := d.instance.GetPageSize(&requests.GetPageSize{Page: page})
	if err != nil {
		return 0, 0, err
	}
	return size.Width, size.Height, nil
}

// RenderDPI renders a page (0-based) with annotations at the given
// resolution. Transparent areas are rendered on white, like a PDF viewer.
func (d *Document) RenderDPI(index int, dpi float64) (*image.RGBA, error) {
	page, err := d.page(index)
	if err != nil {
		return nil, err
	}
	res, err := d.instance.RenderPageInDPI(&requests.RenderPageInDPI{
		Page:        page,
		DPI:         int(math.Round(dpi)),
		RenderFlags: enums.FPDF_RENDER_FLAG_ANNOT,
	})
	if err != nil {
		return nil, err
	}
	// The pixel buffer belongs to the WebAssembly memory and is only valid
	// until Cleanup, so copy it out first.
	defer res.Cleanup()
	src := res.Result.RenderedImage
	out := image.NewRGBA(src.Bounds())
	if res.Result.HasTransparency {
		draw.Draw(out, out.Bounds(), image.White, image.Point{}, draw.Src)
		draw.Draw(out, out.Bounds(), src, src.Bounds().Min, draw.Over)
	} else {
		draw.Draw(out, out.Bounds(), src, src.Bounds().Min, draw.Src)
	}
	return out, nil
}

// Close releases the document and its PDFium instance.
func (d *Document) Close() error {
	_, err := d.instance.FPDF_CloseDocument(&requests.FPDF_CloseDocument{Document: d.doc})
	if cerr := d.instance.Close(); err == nil {
		err = cerr
	}
	return err
}
