package main

import (
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
	doc, cleanup, err := openPDFForOCR(path)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	defer doc.Close()
	if got := doc.NumPage(); got == 0 {
		t.Fatal("empty-user-password PDF opened with zero pages")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("original fixture was changed or removed: %v", err)
	}
}

func TestOpenPDFForOCRRejectsNonEmptyUserPassword(t *testing.T) {
	path := encryptedFixture(t, "required-user-password", "owner-password")
	_, cleanup, err := openPDFForOCR(path)
	cleanup()
	if err == nil {
		t.Fatal("expected non-empty user-password PDF to be rejected")
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatalf("original fixture was changed or removed: %v", statErr)
	}
}
