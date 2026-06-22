package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gorm.io/gorm"
)

func TestFireflyConfigNormalizesInstanceURL(t *testing.T) {
	settingsMutex.Lock()
	previous := settings
	settings = Settings{
		FireflyEnabled:     true,
		FireflyInstanceURL: " https://firefly.example.com/ ",
	}
	settingsMutex.Unlock()
	defer func() {
		settingsMutex.Lock()
		settings = previous
		settingsMutex.Unlock()
	}()

	secretKeyMaterialOverride = func() ([]byte, error) {
		return []byte("12345678901234567890123456789012"), nil
	}
	defer func() { secretKeyMaterialOverride = nil }()

	encrypted, err := EncryptSecret("pat")
	if err != nil {
		t.Fatalf("failed to encrypt token: %v", err)
	}
	settingsMutex.Lock()
	settings.FireflyAPIToken = encrypted
	settingsMutex.Unlock()

	cfg, configured, reason := fireflyConfigFromSettings()
	if !configured || reason != "" {
		t.Fatalf("expected configured firefly settings, got configured=%v reason=%q", configured, reason)
	}
	if cfg.InstanceURL != "https://firefly.example.com" {
		t.Fatalf("expected normalized instance URL, got %q", cfg.InstanceURL)
	}
}

func TestFireflyConfigRejectsMissingToken(t *testing.T) {
	settingsMutex.Lock()
	previous := settings
	settings = Settings{FireflyEnabled: true, FireflyInstanceURL: "https://firefly.example.com"}
	settingsMutex.Unlock()
	defer func() {
		settingsMutex.Lock()
		settings = previous
		settingsMutex.Unlock()
	}()

	cfg, configured, reason := fireflyConfigFromSettings()
	if configured {
		t.Fatalf("expected unconfigured without token, got cfg=%#v reason=%q", cfg, reason)
	}
	if !strings.Contains(reason, "API token is required") {
		t.Fatalf("expected token error, got %q", reason)
	}
}

func TestProbeFireflyHealthReportsHTTPFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"unauthorized"}`))
	}))
	defer server.Close()

	cfg := FireflyConfig{InstanceURL: server.URL, Token: "pat"}
	err := probeFireflyHealth(context.Background(), cfg)
	if err == nil || !strings.Contains(err.Error(), "firefly health check failed") {
		t.Fatalf("expected health probe failure, got %v", err)
	}
}

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

func TestResolveFireflyFieldValueUsesSharedDocumentHelpers(t *testing.T) {
	suggestion := DocumentSuggestion{
		SuggestedTitle:       "Firefly receipt",
		SuggestedCreatedDate: "2026-04-20",
		OriginalDocument: Document{
			Title:       "Original title",
			CreatedDate: "2026-01-01",
		},
	}

	value, ok := resolveSuggestionFieldValue(suggestion, paperlessFieldRefDocumentTitle)
	if !ok || value != "Firefly receipt" {
		t.Fatalf("expected suggested title, got %#v (ok=%v)", value, ok)
	}

	value, ok = resolveSuggestionFieldValue(suggestion, paperlessFieldRefDocumentCreatedDate)
	if !ok || value != "2026-04-20" {
		t.Fatalf("expected suggested created date, got %#v (ok=%v)", value, ok)
	}
}

func TestRankFireflyCandidatesNearDateRanksBelowExact(t *testing.T) {
	derived := fireflyDerivedTransaction{
		Description:  "Vendor receipt",
		Date:         "2026-04-20",
		Amount:       42.15,
		CurrencyCode: "USD",
	}
	candidates := []FireflyTransactionCandidate{
		{ID: "near", Description: "Vendor receipt", Date: "2026-04-23", Amount: "42.15", CurrencyCode: "USD"},
		{ID: "exact", Description: "Vendor receipt", Date: "2026-04-20", Amount: "42.15", CurrencyCode: "USD"},
	}

	ranked, auto := rankFireflyCandidates(derived, DocumentSuggestion{}, candidates)

	if auto != "exact" {
		t.Fatalf("expected exact to auto-select, got %q", auto)
	}
	if len(ranked) < 2 || ranked[0].ID != "exact" || ranked[1].ID != "near" {
		t.Fatalf("expected exact before near match, got %#v", ranked)
	}
}

func TestRankFireflyCandidatesNearDateOnlyDoesNotAutoSelect(t *testing.T) {
	derived := fireflyDerivedTransaction{
		Description:  "Vendor receipt",
		Date:         "2026-04-20",
		Amount:       42.15,
		CurrencyCode: "USD",
	}

	ranked, auto := rankFireflyCandidates(derived, DocumentSuggestion{}, []FireflyTransactionCandidate{
		{ID: "near", Description: "Vendor receipt", Date: "2026-04-21", Amount: "42.15", CurrencyCode: "USD"},
	})

	if auto != "" {
		t.Fatalf("near-date match must not auto-select, got %q", auto)
	}
	if len(ranked) != 1 || ranked[0].ID != "near" {
		t.Fatalf("expected near-date candidate to remain ranked, got %#v", ranked)
	}
}

func TestRankFireflyCandidatesWrongCurrencyDoesNotAutoSelect(t *testing.T) {
	derived := fireflyDerivedTransaction{
		Description:  "Vendor receipt",
		Date:         "2026-04-20",
		Amount:       42.15,
		CurrencyCode: "USD",
	}

	ranked, auto := rankFireflyCandidates(derived, DocumentSuggestion{}, []FireflyTransactionCandidate{
		{ID: "wrong-currency", Description: "Vendor receipt", Date: "2026-04-20", Amount: "42.15", CurrencyCode: "EUR"},
	})

	if auto != "" {
		t.Fatalf("wrong-currency match must not auto-select, got %q", auto)
	}
	if len(ranked) != 1 || ranked[0].ID != "wrong-currency" {
		t.Fatalf("expected wrong-currency candidate to remain ranked for manual review, got %#v", ranked)
	}
}

func TestRankFireflyCandidatesKeepsZeroScoreCandidatesForManualReview(t *testing.T) {
	derived := fireflyDerivedTransaction{
		Description:  "Vendor receipt",
		Date:         "2026-04-20",
		Amount:       42.15,
		CurrencyCode: "USD",
	}

	ranked, auto := rankFireflyCandidates(derived, DocumentSuggestion{}, []FireflyTransactionCandidate{
		{ID: "manual-review", Description: "Different merchant", Date: "not-a-date", Amount: "99.99", CurrencyCode: "EUR"},
	})

	if auto != "" {
		t.Fatalf("zero-score candidate must not auto-select, got %q", auto)
	}
	if len(ranked) != 1 || ranked[0].ID != "manual-review" {
		t.Fatalf("expected zero-score candidate to remain available for manual review, got %#v", ranked)
	}
	if !strings.Contains(ranked[0].MatchReason, "manual review") {
		t.Fatalf("expected manual-review fallback reason, got %q", ranked[0].MatchReason)
	}
}

func TestRankFireflyCandidatesDescriptionOnlyDoesNotAutoSelect(t *testing.T) {
	derived := fireflyDerivedTransaction{
		Description:  "Vendor receipt",
		Date:         "2026-04-20",
		Amount:       42.15,
		CurrencyCode: "USD",
	}

	_, auto := rankFireflyCandidates(derived, DocumentSuggestion{}, []FireflyTransactionCandidate{
		{ID: "description-only", Description: "Vendor receipt", Date: "2026-04-12", Amount: "99.99", CurrencyCode: "USD"},
	})

	if auto != "" {
		t.Fatalf("description-only match must not auto-select, got %q", auto)
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

func TestDeriveFireflyTransactionRequiresAmount(t *testing.T) {
	_, err := deriveFireflyTransaction(DocumentSuggestion{
		ID:                   123,
		SuggestedTitle:       "Vendor receipt",
		SuggestedCreatedDate: "2026-04-20",
	}, FireflyConfig{DefaultCurrency: "USD"})

	if err == nil || !strings.Contains(err.Error(), "requires an amount") {
		t.Fatalf("expected missing amount error, got %v", err)
	}
}

func TestDeriveFireflyTransactionRejectsInvalidDate(t *testing.T) {
	_, err := deriveFireflyTransaction(DocumentSuggestion{
		ID:                   123,
		SuggestedTitle:       "Vendor receipt",
		SuggestedCreatedDate: "04/20/2026",
		SuggestedCustomFields: []CustomFieldSuggestion{
			{Name: "Grand Total", Value: "$42.15"},
		},
	}, FireflyConfig{DefaultCurrency: "USD"})

	if err == nil || !strings.Contains(err.Error(), "YYYY-MM-DD or RFC3339") {
		t.Fatalf("expected invalid date error, got %v", err)
	}
}

func TestApplyFireflyNoMatchCheckboxFalseSkipsWithoutSideEffects(t *testing.T) {
	db, err := InitializeTestDB()
	if err != nil {
		t.Fatalf("failed to initialize test db: %v", err)
	}
	service := NewIntegrationsService(db)
	client := &fireflyTestClient{}
	suggestion := fireflySuggestion()
	suggestion.ApplyFirefly = true
	suggestion.CreateFireflyTransaction = false
	withFireflySettings(t, FireflyConfig{Enabled: true, InstanceURL: "http://firefly.test", Token: "pat", DefaultCurrency: "USD"})

	result, err := service.ApplyFirefly(context.Background(), client, suggestion, 1)
	if err != nil {
		t.Fatalf("ApplyFirefly returned error: %v", err)
	}
	if result != nil {
		t.Fatalf("expected no result when no transaction selected and create disabled, got %#v", result)
	}
	if client.downloadPDFCalls != 0 {
		t.Fatalf("expected no PDF download, got %d", client.downloadPDFCalls)
	}
}

func TestApplyFireflySelectedExistingAttachesOnly(t *testing.T) {
	db, err := InitializeTestDB()
	if err != nil {
		t.Fatalf("failed to initialize test db: %v", err)
	}
	service := NewIntegrationsService(db)
	var createdTransactions int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/attachments":
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Fatalf("failed to parse attachment multipart: %v", err)
			}
			if got := r.FormValue("attachable_id"); got != "tx-existing" {
				t.Fatalf("expected selected transaction id, got %q", got)
			}
			_, _ = w.Write([]byte(`{"data":{"id":"att-1"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/transactions":
			createdTransactions++
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"data":{"id":"created"}}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	withFireflySettings(t, FireflyConfig{Enabled: true, InstanceURL: server.URL, Token: "pat", DefaultCurrency: "USD"})
	client := &fireflyTestClient{document: Document{ID: 42, Title: "Vendor", ArchivedFileName: "vendor.pdf"}, pdf: []byte("%PDF-1.4")}
	suggestion := DocumentSuggestion{ID: 42, ApplyFirefly: true, SelectedFireflyTransactionID: "tx-existing"}

	result, err := service.ApplyFirefly(context.Background(), client, suggestion, 7)
	if err != nil {
		t.Fatalf("ApplyFirefly returned error: %v", err)
	}
	if result == nil || !result.Matched || result.Created || !result.AttachmentUploaded {
		t.Fatalf("unexpected result: %#v", result)
	}
	if createdTransactions != 0 {
		t.Fatalf("selected transaction path must not create transactions, created %d", createdTransactions)
	}
}

func TestApplyFireflyCreateIfNoMatchCreatesAndAttaches(t *testing.T) {
	db, err := InitializeTestDB()
	if err != nil {
		t.Fatalf("failed to initialize test db: %v", err)
	}
	service := NewIntegrationsService(db)
	var createdTransactions int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/transactions":
			_, _ = w.Write([]byte(`{"data":[]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/transactions":
			createdTransactions++
			var payload map[string][]map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("invalid transaction payload: %v", err)
			}
			if got := payload["transactions"][0]["external_id"]; got != "paperless-gpt-document-42" {
				t.Fatalf("expected document marker external_id, got %#v", got)
			}
			_, _ = w.Write([]byte(`{"data":{"id":"tx-new"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/attachments":
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Fatalf("failed to parse attachment multipart: %v", err)
			}
			if got := r.FormValue("attachable_id"); got != "tx-new" {
				t.Fatalf("expected new transaction id, got %q", got)
			}
			_, _ = w.Write([]byte(`{"data":{"id":"att-1"}}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	withFireflySettings(t, FireflyConfig{Enabled: true, InstanceURL: server.URL, Token: "pat", DefaultCurrency: "USD"})
	client := &fireflyTestClient{document: Document{ID: 42, Title: "Vendor", ArchivedFileName: "vendor.pdf"}, pdf: []byte("%PDF-1.4")}
	suggestion := fireflySuggestion()
	suggestion.ApplyFirefly = true
	suggestion.CreateFireflyTransaction = true

	result, err := service.ApplyFirefly(context.Background(), client, suggestion, 7)
	if err != nil {
		t.Fatalf("ApplyFirefly returned error: %v", err)
	}
	if result == nil || !result.Created || result.TransactionID != "tx-new" || !result.AttachmentUploaded {
		t.Fatalf("unexpected result: %#v", result)
	}
	if createdTransactions != 1 {
		t.Fatalf("expected exactly one transaction create, got %d", createdTransactions)
	}
}

func TestApplyFireflyDuplicateCandidatePreventsSilentCreate(t *testing.T) {
	db, err := InitializeTestDB()
	if err != nil {
		t.Fatalf("failed to initialize test db: %v", err)
	}
	service := NewIntegrationsService(db)
	var createdTransactions int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/transactions":
			_, _ = w.Write([]byte(`{"data":[{"id":"tx-existing","attributes":{"transactions":[{"description":"Vendor receipt","date":"2026-04-20T00:00:00+00:00","amount":"42.15","currency_code":"USD"}]}}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/transactions":
			createdTransactions++
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"data":{"id":"created"}}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	withFireflySettings(t, FireflyConfig{Enabled: true, InstanceURL: server.URL, Token: "pat", DefaultCurrency: "USD"})
	suggestion := fireflySuggestion()
	suggestion.ApplyFirefly = true
	suggestion.CreateFireflyTransaction = true

	_, err = service.ApplyFirefly(context.Background(), &fireflyTestClient{}, suggestion, 7)
	if err == nil || !strings.Contains(err.Error(), "possible Firefly duplicate") {
		t.Fatalf("expected duplicate error, got %v", err)
	}
	if createdTransactions != 0 {
		t.Fatalf("duplicate protection must block create, created %d", createdTransactions)
	}
}

func TestApplyFireflyNearDateDuplicateCandidatePreventsCreate(t *testing.T) {
	db, err := InitializeTestDB()
	if err != nil {
		t.Fatalf("failed to initialize test db: %v", err)
	}
	service := NewIntegrationsService(db)
	var createdTransactions int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/transactions":
			_, _ = w.Write([]byte(`{"data":[{"id":"tx-existing","attributes":{"transactions":[{"description":"Vendor receipt","date":"2026-04-21T00:00:00+00:00","amount":"42.15","currency_code":"USD"}]}}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/transactions":
			createdTransactions++
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"data":{"id":"created"}}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	withFireflySettings(t, FireflyConfig{Enabled: true, InstanceURL: server.URL, Token: "pat", DefaultCurrency: "USD"})
	suggestion := fireflySuggestion()
	suggestion.ApplyFirefly = true
	suggestion.CreateFireflyTransaction = true

	_, err = service.ApplyFirefly(context.Background(), &fireflyTestClient{}, suggestion, 7)
	if err == nil || !strings.Contains(err.Error(), "possible Firefly duplicate") {
		t.Fatalf("expected near-date duplicate error, got %v", err)
	}
	if createdTransactions != 0 {
		t.Fatalf("near-date duplicate protection must block create, created %d", createdTransactions)
	}
}

func TestApplyFireflyWeakCandidateDoesNotBlockCreate(t *testing.T) {
	db, err := InitializeTestDB()
	if err != nil {
		t.Fatalf("failed to initialize test db: %v", err)
	}
	service := NewIntegrationsService(db)
	var createdTransactions int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/transactions":
			_, _ = w.Write([]byte(`{"data":[{"id":"tx-existing","attributes":{"transactions":[{"description":"Vendor receipt","date":"2026-04-20T00:00:00+00:00","amount":"42.15","currency_code":"EUR"}]}}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/transactions":
			createdTransactions++
			_, _ = w.Write([]byte(`{"data":{"id":"tx-new"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/attachments":
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Fatalf("failed to parse attachment multipart: %v", err)
			}
			if got := r.FormValue("attachable_id"); got != "tx-new" {
				t.Fatalf("expected created transaction id, got %q", got)
			}
			_, _ = w.Write([]byte(`{"data":{"id":"att-1"}}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	withFireflySettings(t, FireflyConfig{Enabled: true, InstanceURL: server.URL, Token: "pat", DefaultCurrency: "USD"})
	suggestion := fireflySuggestion()
	suggestion.ApplyFirefly = true
	suggestion.CreateFireflyTransaction = true

	result, err := service.ApplyFirefly(context.Background(), &fireflyTestClient{}, suggestion, 7)
	if err != nil {
		t.Fatalf("expected create to proceed when only weak candidates exist, got %v", err)
	}
	if result == nil || !result.Created || result.TransactionID != "tx-new" || !result.AttachmentUploaded {
		t.Fatalf("unexpected result: %#v", result)
	}
	if createdTransactions != 1 {
		t.Fatalf("expected weak candidate path to create exactly one transaction, got %d", createdTransactions)
	}
}

func TestApplyFireflyDuplicateCheckFailureBlocksCreate(t *testing.T) {
	db, err := InitializeTestDB()
	if err != nil {
		t.Fatalf("failed to initialize test db: %v", err)
	}
	service := NewIntegrationsService(db)
	var createdTransactions int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/transactions":
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`{"message":"temporary outage"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/transactions":
			createdTransactions++
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"data":{"id":"created"}}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	withFireflySettings(t, FireflyConfig{Enabled: true, InstanceURL: server.URL, Token: "pat", DefaultCurrency: "USD"})
	suggestion := fireflySuggestion()
	suggestion.ApplyFirefly = true
	suggestion.CreateFireflyTransaction = true

	_, err = service.ApplyFirefly(context.Background(), &fireflyTestClient{}, suggestion, 7)
	if err == nil || !strings.Contains(err.Error(), "duplicate check failed before create") {
		t.Fatalf("expected duplicate check failure, got %v", err)
	}
	if createdTransactions != 0 {
		t.Fatalf("create must not run when duplicate check fails, created %d", createdTransactions)
	}
}

func TestApplyFireflyAttachmentFailureReturnsDocumentContext(t *testing.T) {
	db, err := InitializeTestDB()
	if err != nil {
		t.Fatalf("failed to initialize test db: %v", err)
	}
	service := NewIntegrationsService(db)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/transactions":
			_, _ = w.Write([]byte(`{"data":[]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/transactions":
			_, _ = w.Write([]byte(`{"data":{"id":"tx-new"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/attachments":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"message":"attachment failed"}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	withFireflySettings(t, FireflyConfig{Enabled: true, InstanceURL: server.URL, Token: "pat", DefaultCurrency: "USD"})
	suggestion := fireflySuggestion()
	suggestion.ApplyFirefly = true
	suggestion.CreateFireflyTransaction = true

	result, err := service.ApplyFirefly(context.Background(), &fireflyTestClient{}, suggestion, 7)
	if err == nil || !strings.Contains(err.Error(), "firefly receipt attachment failed for document 42") {
		t.Fatalf("expected attachment error with document context, got result=%#v err=%v", result, err)
	}
	if result == nil || !result.Created || result.TransactionID != "tx-new" {
		t.Fatalf("expected created transaction result preserved, got %#v", result)
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

func fireflySuggestion() DocumentSuggestion {
	return DocumentSuggestion{
		ID:                   42,
		OriginalDocument:     Document{ID: 42, Title: "Vendor receipt", CreatedDate: "2026-04-20"},
		SuggestedTitle:       "Vendor receipt",
		SuggestedCreatedDate: "2026-04-20",
		SuggestedCustomFields: []CustomFieldSuggestion{
			{Name: "Grand Total", Value: "$42.15"},
		},
	}
}

func withFireflySettings(t *testing.T, cfg FireflyConfig) {
	t.Helper()
	secretKeyMaterialOverride = func() ([]byte, error) {
		return []byte("12345678901234567890123456789012"), nil
	}
	t.Cleanup(func() {
		secretKeyMaterialOverride = nil
		settingsMutex.Lock()
		settings = Settings{}
		settingsMutex.Unlock()
	})
	var encrypted string
	if strings.TrimSpace(cfg.Token) != "" {
		enc, err := EncryptSecret(cfg.Token)
		if err != nil {
			t.Fatalf("failed to encrypt test token: %v", err)
		}
		encrypted = enc
	}
	settingsMutex.Lock()
	settings = Settings{
		FireflyEnabled:                   cfg.Enabled,
		FireflyInstanceURL:               cfg.InstanceURL,
		FireflyAPIToken:                  encrypted,
		FireflyDefaultSourceAccount:      cfg.DefaultSourceAccount,
		FireflyDefaultDestinationAccount: cfg.DefaultDestinationAccount,
		FireflyDefaultCurrency:           cfg.DefaultCurrency,
	}
	settingsMutex.Unlock()
}

type fireflyTestClient struct {
	document         Document
	pdf              []byte
	downloadPDFCalls int
}

func (c *fireflyTestClient) GetDocumentsByTag(ctx context.Context, tag string, pageSize int) ([]Document, error) {
	return nil, nil
}

func (c *fireflyTestClient) GetDocumentCountByTag(ctx context.Context, tag string) (int, error) {
	return 0, nil
}

func (c *fireflyTestClient) UpdateDocuments(ctx context.Context, documents []DocumentSuggestion, db *gorm.DB, isUndo bool, batchID ...uint) error {
	return nil
}

func (c *fireflyTestClient) GetDocument(ctx context.Context, documentID int) (Document, error) {
	if c.document.ID == 0 {
		return Document{ID: documentID, Title: "Vendor receipt", ArchivedFileName: "receipt.pdf"}, nil
	}
	return c.document, nil
}

func (c *fireflyTestClient) GetAllTags(ctx context.Context) (map[string]int, error) {
	return nil, nil
}

func (c *fireflyTestClient) GetAllCorrespondents(ctx context.Context) (map[string]int, error) {
	return nil, nil
}

func (c *fireflyTestClient) GetAllDocumentTypes(ctx context.Context) ([]DocumentType, error) {
	return nil, nil
}

func (c *fireflyTestClient) GetCustomFields(ctx context.Context) ([]CustomField, error) {
	return nil, nil
}

func (c *fireflyTestClient) CreateTag(ctx context.Context, tagName string) (int, error) {
	return 0, nil
}

func (c *fireflyTestClient) DownloadPDF(ctx context.Context, document Document) ([]byte, error) {
	c.downloadPDFCalls++
	if len(c.pdf) == 0 {
		return []byte("%PDF-1.4"), nil
	}
	return c.pdf, nil
}

func (c *fireflyTestClient) DownloadDocumentAsImages(ctx context.Context, documentID int, pageLimit int) ([]string, int, error) {
	return nil, 0, nil
}

func (c *fireflyTestClient) DownloadDocumentAsPDF(ctx context.Context, documentID int, limitPages int, split bool) ([]string, []byte, int, error) {
	return nil, nil, 0, nil
}

func (c *fireflyTestClient) UploadDocument(ctx context.Context, data []byte, filename string, metadata map[string]interface{}) (string, error) {
	return "", nil
}

func (c *fireflyTestClient) UpsertDocumentCustomFields(ctx context.Context, documentID int, fieldValues map[int]interface{}, db *gorm.DB) error {
	return nil
}

func (c *fireflyTestClient) UpsertDocumentCustomFieldsWithBatch(ctx context.Context, documentID int, fieldValues map[int]interface{}, db *gorm.DB, batchID *uint) error {
	return nil
}

func (c *fireflyTestClient) GetTaskStatus(ctx context.Context, taskID string) (map[string]interface{}, error) {
	return nil, nil
}

func (c *fireflyTestClient) DeleteDocument(ctx context.Context, documentID int) error {
	return nil
}
