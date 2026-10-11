package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

func encryptedFixture(t *testing.T, user, owner string) string {
	t.Helper()
	src := filepath.Join("tests", "pdf", "sample.pdf")
	out := filepath.Join(t.TempDir(), "fixture.pdf")
	if err := api.EncryptFile(src, out, model.NewAESConfiguration(user, owner, 256)); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestOpenPDFForOCREmptyUserPassword(t *testing.T) {
	path := encryptedFixture(t, "", "owner-password")
	doc, cleanup, err := openPDFForOCR(context.Background(), path, "")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	defer doc.Close()
	if got := doc.NumPages(); got == 0 {
		t.Fatal("empty-user-password PDF opened with zero pages")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("original fixture was changed or removed: %v", err)
	}
}

func TestOpenPDFForOCRRejectsNonEmptyUserPassword(t *testing.T) {
	path := encryptedFixture(t, "required-user-password", "owner-password")
	_, cleanup, err := openPDFForOCR(context.Background(), path, "")
	cleanup()
	if err == nil {
		t.Fatal("expected non-empty user-password PDF to be rejected")
	}
	if !errors.Is(err, ErrPDFPasswordRequired) {
		t.Fatalf("expected ErrPDFPasswordRequired, got %v", err)
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatalf("original fixture was changed or removed: %v", statErr)
	}
}

func TestOpenPDFForOCRWithPassword(t *testing.T) {
	path := encryptedFixture(t, "required-user-password", "owner-password")
	doc, cleanup, err := openPDFForOCR(context.Background(), path, "required-user-password")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	defer doc.Close()
	if doc.NumPages() == 0 {
		t.Fatal("password-protected PDF opened with zero pages")
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatalf("original fixture was changed or removed: %v", statErr)
	}
}

func TestOCRErrorForStorageClassifiesPasswordFailures(t *testing.T) {
	if got := ocrErrorForStorage(ErrPDFPasswordRequired); got != "pdf_password_required: document requires a non-empty password" {
		t.Fatalf("unexpected classified error: %q", got)
	}
	if got := ocrErrorForStorage(errors.New("unexpected provider failure")); got != "unexpected provider failure" {
		t.Fatalf("unexpected generic error rewrite: %q", got)
	}
}

func TestOpenPDFForOCRHonorsCancellation(t *testing.T) {
	path := encryptedFixture(t, "", "owner-password")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, cleanup, err := openPDFForOCR(ctx, path, "")
	cleanup()
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
}

func TestOpenPDFForOCRDoesNotClassifyMalformedPDFAsPasswordFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "malformed.pdf")
	if err := os.WriteFile(path, []byte("not a PDF"), 0600); err != nil {
		t.Fatal(err)
	}
	_, cleanup, err := openPDFForOCR(context.Background(), path, "supplied-password")
	cleanup()
	if err == nil {
		t.Fatal("expected malformed PDF failure")
	}
	if errors.Is(err, ErrPDFPasswordRequired) {
		t.Fatalf("malformed PDF was mislabeled as password failure: %v", err)
	}
}

func TestJobStoreClearsPDFPassword(t *testing.T) {
	store := &JobStore{jobs: map[string]*Job{
		"job-1": {ID: "job-1", Options: OCROptions{PDFPassword: "transient-secret"}},
	}}
	store.clearPDFPassword("job-1")
	store.RLock()
	defer store.RUnlock()
	if got := store.jobs["job-1"].Options.PDFPassword; got != "" {
		t.Fatalf("PDF password retained after terminal cleanup: %q", got)
	}
}
