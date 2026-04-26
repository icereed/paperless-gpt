package main

import (
	"io"
	"strings"
	"testing"
)

func TestRankFireflyCandidatesAutoSelectsUniqueExactMatch(t *testing.T) {
	derived := fireflyDerivedTransaction{
		Description:  "Vendor receipt",
		Date:         "2026-04-20",
		Amount:       42.15,
		CurrencyCode: "USD",
	}
	candidates := []FireflyTransactionCandidate{
		{ID: "tx-1", Description: "Vendor receipt", Date: "2026-04-20", Amount: "42.15", CurrencyCode: "USD"},
		{ID: "tx-2", Description: "Other", Date: "2026-04-23", Amount: "42.15", CurrencyCode: "USD"},
	}

	ranked, auto := rankFireflyCandidates(derived, DocumentSuggestion{}, candidates)

	if auto != "tx-1" {
		t.Fatalf("expected tx-1 to auto-select, got %q", auto)
	}
	if len(ranked) == 0 || ranked[0].ID != "tx-1" {
		t.Fatalf("expected tx-1 first, got %#v", ranked)
	}
	if !strings.Contains(ranked[0].MatchReason, "same date") || !strings.Contains(ranked[0].MatchReason, "same amount") {
		t.Fatalf("expected match reason to mention date and amount, got %q", ranked[0].MatchReason)
	}
}

func TestRankFireflyCandidatesDoesNotAutoSelectAmbiguousExactMatches(t *testing.T) {
	derived := fireflyDerivedTransaction{
		Description:  "Vendor receipt",
		Date:         "2026-04-20",
		Amount:       42.15,
		CurrencyCode: "USD",
	}
	candidates := []FireflyTransactionCandidate{
		{ID: "tx-1", Description: "Vendor receipt", Date: "2026-04-20", Amount: "42.15", CurrencyCode: "USD"},
		{ID: "tx-2", Description: "Vendor receipt", Date: "2026-04-20", Amount: "42.15", CurrencyCode: "USD"},
	}

	_, auto := rankFireflyCandidates(derived, DocumentSuggestion{}, candidates)

	if auto != "" {
		t.Fatalf("expected no auto-selection for ambiguous exact matches, got %q", auto)
	}
}

func TestDeriveFireflyAmountFallsBackToSuggestedTotalAmountOrPrice(t *testing.T) {
	suggestion := DocumentSuggestion{
		SuggestedCustomFields: []CustomFieldSuggestion{
			{Name: "Memo", Value: "10.00"},
			{Name: "Grand Total", Value: "$42.15"},
		},
	}

	amount, amountString, ok := deriveFireflyAmount(suggestion, "")

	if !ok || amount != 42.15 || amountString != "42.15" {
		t.Fatalf("expected total amount 42.15, got amount=%v string=%q ok=%v", amount, amountString, ok)
	}
}

func TestBuildQuickBooksReceiptUploadContainsReceiptMetadataAndPDF(t *testing.T) {
	reader, contentType, err := buildQuickBooksReceiptUpload("receipt.pdf", []byte("%PDF-1.4"), "Receipt note")
	if err != nil {
		t.Fatalf("buildQuickBooksReceiptUpload returned error: %v", err)
	}
	body := readAllString(t, reader)

	if !strings.Contains(contentType, "multipart/form-data") {
		t.Fatalf("expected multipart content type, got %q", contentType)
	}
	for _, want := range []string{`"Category":"Receipt"`, `"ContentType":"application/pdf"`, `name="file_content_01"; filename="receipt.pdf"`, "%PDF-1.4"} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected multipart body to contain %q, got %s", want, body)
		}
	}
}

func readAllString(t *testing.T, reader io.Reader) string {
	t.Helper()
	body, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("failed to read body: %v", err)
	}
	return string(body)
}
