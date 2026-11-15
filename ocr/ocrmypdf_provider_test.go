package ocr

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOCRmyPDFProviderReturnsSearchablePDFAndExtractedText(t *testing.T) {
	commands := newOCRmyPDFTestCommands(t, "Pages: 2\n", "recognized text\n")
	provider, err := NewProvider(Config{
		Provider:         "ocrmypdf",
		OCRMyPDFCommand:  commands.ocr,
		PDFInfoCommand:   commands.pdfinfo,
		PDFToTextCommand: commands.pdftotext,
		OCRMyPDFEngine:   "paddleocr",
		OCRMyPDFLanguage: "eng+hin",
	})
	require.NoError(t, err)

	input := []byte("%PDF-1.7\noriginal")
	result, err := provider.ProcessImage(context.Background(), input, 0)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, strings.HasPrefix(string(result.PDF), "%PDF-"))
	assert.Equal(t, "recognized text", result.Text)
	assert.Equal(t, "ocrmypdf", result.Metadata["provider"])
	assert.Equal(t, "paddleocr", result.Metadata["engine"])
}

func TestOCRmyPDFProviderUsesEngineSpecificCLISelection(t *testing.T) {
	for _, tc := range []struct {
		engine string
		want   []string
		avoid  []string
	}{
		{engine: "paddleocr", want: []string{"--ocr-engine\nauto", "--plugin\nocrmypdf_paddleocr"}, avoid: []string{"--ocr-engine\nrapidocr"}},
		{engine: "rapidocr", want: []string{"--ocr-engine\nrapidocr"}, avoid: []string{"--plugin\n", "ocrmypdf_paddleocr"}},
	} {
		t.Run(tc.engine, func(t *testing.T) {
			commands := newOCRmyPDFTestCommands(t, "Pages: 2\n", "recognized text\n")
			argsPath := filepath.Join(t.TempDir(), "ocr-args")
			quotedArgsPath := "'" + strings.ReplaceAll(argsPath, "'", "'\\''") + "'"
			commands.ocr = writeOCRmyPDFScript(t,
				"prev=\"\"\nlast=\"\"\nfor arg do prev=\"$last\"; last=\"$arg\"; done\n"+
					"printf '%s\\n' \"$@\" > "+quotedArgsPath+"\ncp \"$prev\" \"$last\"\n")
			provider, err := NewProvider(Config{
				Provider:         "ocrmypdf",
				OCRMyPDFCommand:  commands.ocr,
				PDFInfoCommand:   commands.pdfinfo,
				PDFToTextCommand: commands.pdftotext,
				OCRMyPDFEngine:   tc.engine,
				OCRMyPDFLanguage: "eng+hin",
			})
			require.NoError(t, err)
			_, err = provider.ProcessImage(context.Background(), []byte("%PDF-1.7\noriginal"), 0)
			require.NoError(t, err)
			args, err := os.ReadFile(argsPath)
			require.NoError(t, err)
			for _, expected := range tc.want {
				assert.Contains(t, string(args), expected)
			}
			for _, unexpected := range tc.avoid {
				assert.NotContains(t, string(args), unexpected)
			}
			assert.Contains(t, string(args), "--language\neng+hin")
			assert.Contains(t, string(args), "--mode\nredo")
			assert.Contains(t, string(args), "--jobs\n1")
			assert.Contains(t, string(args), "--output-type\npdf")
		})
	}
}

func TestOCRmyPDFProviderRejectsUnsupportedLanguage(t *testing.T) {
	commands := newOCRmyPDFTestCommands(t, "Pages: 2\n", "recognized text\n")
	_, err := NewProvider(Config{
		Provider:         "ocrmypdf",
		OCRMyPDFCommand:  commands.ocr,
		PDFInfoCommand:   commands.pdfinfo,
		PDFToTextCommand: commands.pdftotext,
		OCRMyPDFEngine:   "paddleocr",
		OCRMyPDFLanguage: "eng+deu",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "OCRMY_PDF_LANGUAGE")
}

func TestOCRmyPDFProviderRejectsIncompleteOutputPDF(t *testing.T) {
	commands := newOCRmyPDFTestCommands(t, "Pages: 2\n", "recognized text\n")
	commands.pdfinfo = writeOCRmyPDFScript(t, "case \"$*\" in *output.pdf*) echo 'Pages: 1';; *) echo 'Pages: 2';; esac\n")
	provider := mustOCRmyPDFProvider(t, commands)

	_, err := provider.ProcessImage(context.Background(), []byte("%PDF-1.7\noriginal"), 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "page count")
}

func TestOCRmyPDFProviderReportsCLIError(t *testing.T) {
	commands := newOCRmyPDFTestCommands(t, "Pages: 2\n", "recognized text\n")
	commands.ocr = writeOCRmyPDFScript(t, "echo failed >&2\nexit 17\n")
	provider := mustOCRmyPDFProvider(t, commands)

	_, err := provider.ProcessImage(context.Background(), []byte("%PDF-1.7\noriginal"), 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ocrmypdf")
}

func TestRunOCRCommandRejectsOutputOverflow(t *testing.T) {
	command := writeOCRmyPDFScript(t, "head -c 67108865 /dev/zero\n")

	_, err := runOCRCommand(context.Background(), command)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "command output exceeded")
}

func TestOCRmyPDFProviderCancellationKillsProcessGroup(t *testing.T) {
	commands := newOCRmyPDFTestCommands(t, "Pages: 2\n", "recognized text\n")
	started := filepath.Join(t.TempDir(), "child-started")
	childReady := filepath.Join(t.TempDir(), "child-ready")
	childMarker := filepath.Join(t.TempDir(), "child-survived")
	released := filepath.Join(t.TempDir(), "release-child")
	t.Cleanup(func() { _ = os.WriteFile(released, nil, 0o600) })
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }
	commands.ocr = writeOCRmyPDFScript(t, fmt.Sprintf(
		"(printf ready > %s; while [ ! -e %s ]; do sleep 0.01; done; printf alive > %s) &\nprintf ready > %s\nwait\n",
		quote(childReady), quote(released), quote(childMarker), quote(started),
	))
	provider := mustOCRmyPDFProvider(t, commands)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := provider.ProcessImage(ctx, []byte("%PDF-1.7\noriginal"), 0)
		done <- err
	}()

	waitForFile := func(path string, timeout time.Duration) bool {
		deadline := time.Now().Add(timeout)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(path); err == nil {
				return true
			}
			time.Sleep(10 * time.Millisecond)
		}
		return false
	}
	if !waitForFile(started, 5*time.Second) || !waitForFile(childReady, 5*time.Second) {
		cancel()
		t.Fatal("OCR CLI child did not acknowledge startup")
	}
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(3 * time.Second):
		t.Fatal("OCR cancellation did not stop the CLI process group")
	}
	require.NoError(t, os.WriteFile(released, nil, 0o600))
	assert.False(t, waitForFile(childMarker, time.Second), "OCR worker child survived CLI cancellation")
}

func TestOCRmyPDFProviderRejectsNonWholePDFInput(t *testing.T) {
	provider := mustOCRmyPDFProvider(t, newOCRmyPDFTestCommands(t, "Pages: 2\n", "recognized text\n"))
	_, err := provider.ProcessImage(context.Background(), []byte("image"), 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "whole_pdf")
}

func TestOCRmyPDFProviderConvertsImageOriginalToWholePDF(t *testing.T) {
	var input bytes.Buffer
	require.NoError(t, jpeg.Encode(&input, image.NewRGBA(image.Rect(0, 0, 40, 60)), nil))
	provider := mustOCRmyPDFProvider(t, newOCRmyPDFTestCommands(t, "Pages: 1\n", "recognized image text\n"))
	result, err := provider.ProcessImage(context.Background(), input.Bytes(), 0)
	require.NoError(t, err)
	require.True(t, bytes.HasPrefix(result.PDF, []byte("%PDF-")), "OCR command must receive PDF, not JPEG bytes")
	assert.Equal(t, "recognized image text", result.Text)
	assert.Equal(t, "1", result.Metadata["page_count"])
}

type ocrmyPDFTestCommands struct {
	ocr, pdfinfo, pdftotext string
}

func newOCRmyPDFTestCommands(t *testing.T, pageInfo, text string) ocrmyPDFTestCommands {
	t.Helper()
	return ocrmyPDFTestCommands{
		ocr:       writeOCRmyPDFScript(t, "prev=\"\"\nlast=\"\"\nfor arg do prev=\"$last\"; last=\"$arg\"; done\ncp \"$prev\" \"$last\"\n"),
		pdfinfo:   writeOCRmyPDFScript(t, "printf '%s' '"+strings.ReplaceAll(pageInfo, "'", "'\\''")+"'\n"),
		pdftotext: writeOCRmyPDFScript(t, "printf '%s' '"+strings.ReplaceAll(text, "'", "'\\''")+"'\n"),
	}
}

func writeOCRmyPDFScript(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "command")
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\nset -eu\n"+body), 0o700))
	return path
}

func mustOCRmyPDFProvider(t *testing.T, commands ocrmyPDFTestCommands) Provider {
	t.Helper()
	provider, err := NewProvider(Config{
		Provider:         "ocrmypdf",
		OCRMyPDFCommand:  commands.ocr,
		PDFInfoCommand:   commands.pdfinfo,
		PDFToTextCommand: commands.pdftotext,
		OCRMyPDFEngine:   "rapidocr",
		OCRMyPDFLanguage: "eng",
	})
	require.NoError(t, err)
	return provider
}
