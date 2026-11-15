package ocr

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/pdfcpu/pdfcpu/pkg/api"
)

const (
	defaultOCRMyPDFCommand  = "/opt/ocr/bin/ocrmypdf"
	defaultPDFToTextCommand = "/usr/bin/pdftotext"
	defaultPDFInfoCommand   = "/usr/bin/pdfinfo"
)

var pdfPagesPattern = regexp.MustCompile(`(?m)^Pages:\s+(\d+)\s*$`)

type OCRMyPDFProvider struct {
	command   string
	pdfToText string
	pdfInfo   string
	engine    string
	language  string
}

func newOCRMyPDFProvider(config Config) (Provider, error) {
	engine := strings.ToLower(strings.TrimSpace(config.OCRMyPDFEngine))
	if engine == "" {
		engine = "paddleocr"
	}
	if engine != "paddleocr" && engine != "rapidocr" {
		return nil, fmt.Errorf("unsupported OCRMY_PDF_ENGINE %q (supported: paddleocr, rapidocr)", engine)
	}
	language := strings.TrimSpace(config.OCRMyPDFLanguage)
	if language == "" {
		language = "eng"
	}
	if language != "eng" && language != "eng+hin" {
		return nil, fmt.Errorf("unsupported OCRMY_PDF_LANGUAGE %q (supported: eng, eng+hin)", language)
	}

	provider := &OCRMyPDFProvider{
		command:   config.OCRMyPDFCommand,
		pdfToText: config.PDFToTextCommand,
		pdfInfo:   config.PDFInfoCommand,
		engine:    engine,
		language:  language,
	}
	if provider.command == "" {
		provider.command = defaultOCRMyPDFCommand
	}
	if provider.pdfToText == "" {
		provider.pdfToText = defaultPDFToTextCommand
	}
	if provider.pdfInfo == "" {
		provider.pdfInfo = defaultPDFInfoCommand
	}
	return provider, nil
}

func (p *OCRMyPDFProvider) ProducesSearchablePDF() bool { return true }

// ProcessImage accepts a whole PDF or single image original (pageNumber zero).
// Images are embedded in a PDF before OCR; PDF inputs pass through unchanged.
func (p *OCRMyPDFProvider) ProcessImage(ctx context.Context, input []byte, pageNumber int) (*OCRResult, error) {
	if pageNumber != 0 {
		return nil, fmt.Errorf("ocrmypdf requires whole_pdf mode (page number 0), got %d", pageNumber)
	}
	if !bytes.HasPrefix(input, []byte("%PDF-")) {
		switch http.DetectContentType(input) {
		case "image/jpeg", "image/png":
			var pdf bytes.Buffer
			if err := api.ImportImages(nil, &pdf, []io.Reader{bytes.NewReader(input)}, nil, nil); err != nil {
				return nil, fmt.Errorf("convert image original to PDF: %w", err)
			}
			input = pdf.Bytes()
		default:
			return nil, fmt.Errorf("ocrmypdf requires valid whole-document PDF or JPEG/PNG input")
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	dir, err := os.MkdirTemp("", "paperless-gpt-ocrmypdf-")
	if err != nil {
		return nil, fmt.Errorf("create private OCR temporary directory: %w", err)
	}
	defer os.RemoveAll(dir)

	inputPath := filepath.Join(dir, "input.pdf")
	outputPath := filepath.Join(dir, "output.pdf")
	if err := os.WriteFile(inputPath, input, 0o600); err != nil {
		return nil, fmt.Errorf("write OCR input PDF: %w", err)
	}

	inputPages, err := p.pageCount(ctx, inputPath)
	if err != nil {
		return nil, fmt.Errorf("read input PDF page count: %w", err)
	}
	if inputPages < 1 {
		return nil, fmt.Errorf("input PDF has no pages")
	}

	args := []string{
		"--output-type", "pdf",
		"--jobs", "1",
		"--mode", "redo",
		"--language", p.language,
	}
	if p.engine == "paddleocr" {
		args = append(args, "--ocr-engine", "auto", "--plugin", "ocrmypdf_paddleocr")
	} else {
		args = append(args, "--ocr-engine", "rapidocr")
	}
	args = append(args, inputPath, outputPath)
	if _, err := runOCRCommand(ctx, p.command, args...); err != nil {
		return nil, fmt.Errorf("ocrmypdf command failed: %w", err)
	}

	pdf, err := os.ReadFile(outputPath)
	if err != nil {
		return nil, fmt.Errorf("read OCR output PDF: %w", err)
	}
	if len(pdf) < 5 || !bytes.HasPrefix(pdf, []byte("%PDF-")) {
		return nil, fmt.Errorf("ocrmypdf produced invalid or empty PDF output")
	}
	outputPages, err := p.pageCount(ctx, outputPath)
	if err != nil {
		return nil, fmt.Errorf("read output PDF page count: %w", err)
	}
	if outputPages != inputPages {
		return nil, fmt.Errorf("ocrmypdf output page count %d does not match input page count %d", outputPages, inputPages)
	}

	text, err := runOCRCommand(ctx, p.pdfToText, "-layout", outputPath, "-")
	if err != nil {
		return nil, fmt.Errorf("pdftotext command failed: %w", err)
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, fmt.Errorf("pdftotext returned no text from OCR output PDF")
	}

	return &OCRResult{
		Text: text,
		PDF:  pdf,
		Metadata: map[string]string{
			"provider":   "ocrmypdf",
			"engine":     p.engine,
			"page_count": strconv.Itoa(outputPages),
		},
	}, nil
}

func (p *OCRMyPDFProvider) pageCount(ctx context.Context, path string) (int, error) {
	output, err := runOCRCommand(ctx, p.pdfInfo, path)
	if err != nil {
		return 0, err
	}
	matches := pdfPagesPattern.FindStringSubmatch(output)
	if len(matches) != 2 {
		return 0, fmt.Errorf("pdfinfo did not report a page count")
	}
	pages, err := strconv.Atoi(matches[1])
	if err != nil {
		return 0, fmt.Errorf("invalid PDF page count")
	}
	return pages, nil
}

func runOCRCommand(ctx context.Context, command string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if err == syscall.ESRCH {
			return nil
		}
		return err
	}
	cmd.WaitDelay = time.Second
	var stdout boundedCommandOutput
	cmd.Stdout = &stdout
	cmd.Stderr = &discardCommandOutput{}
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", err
	}
	if stdout.truncated {
		return "", fmt.Errorf("command output exceeded %d bytes", maxCommandOutputBytes)
	}
	return stdout.String(), nil
}

const maxCommandOutputBytes = 64 << 20

type boundedCommandOutput struct {
	bytes.Buffer
	truncated bool
}

func (b *boundedCommandOutput) Write(data []byte) (int, error) {
	original := len(data)
	remaining := maxCommandOutputBytes - b.Len()
	if len(data) > remaining {
		data = data[:remaining]
		b.truncated = true
	}
	_, _ = b.Buffer.Write(data)
	return original, nil
}

type discardCommandOutput struct{}

func (*discardCommandOutput) Write(data []byte) (int, error) { return len(data), nil }
