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
	suggestion := fireflySuggestion()
	suggestion.ApplyFirefly = true
	suggestion.SelectedFireflyTransactionID = "tx-existing"

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

func TestQuickBooksRealmIDFallsBackToMetadataAndScope(t *testing.T) {
	if got := quickBooksRealmID(&IntegrationConnection{AccountID: "account-realm"}); got != "account-realm" {
		t.Fatalf("expected account id realm, got %q", got)
	}
	if got := quickBooksRealmID(&IntegrationConnection{MetadataJSON: `{"realm_id":"metadata-realm"}`}); got != "metadata-realm" {
		t.Fatalf("expected metadata realm, got %q", got)
	}
	if got := quickBooksRealmID(&IntegrationConnection{Scopes: "com.intuit.quickbooks.accounting realm:scope-realm"}); got != "scope-realm" {
		t.Fatalf("expected scope realm, got %q", got)
	}
}

func TestQuickBooksFetchIdentityPersistsRealmFromScope(t *testing.T) {
	identity, err := (quickBooksProvider{}).FetchIdentity(context.Background(), &IntegrationConnection{
		Scopes: "com.intuit.quickbooks.accounting realm:12345",
	})
	if err != nil {
		t.Fatalf("FetchIdentity returned error: %v", err)
	}
	if identity.AccountID != "12345" {
		t.Fatalf("expected account id 12345, got %q", identity.AccountID)
	}
	if identity.Metadata["realm_id"] != "12345" {
		t.Fatalf("expected realm_id metadata, got %#v", identity.Metadata)
	}
}

func TestQuickBooksAPIBaseURLUsesSandboxWhenConfigured(t *testing.T) {
	withQuickBooksEnvironment(t, quickBooksEnvironmentSandbox)

	if got := quickBooksAPIBaseURL(); got != quickBooksSandboxAPIBaseURL {
		t.Fatalf("expected sandbox base URL, got %q", got)
	}
}

func TestNormalizeQuickBooksEnvironment(t *testing.T) {
	if got := normalizeQuickBooksEnvironment(quickBooksEnvironmentSandbox); got != quickBooksEnvironmentSandbox {
		t.Fatalf("expected sandbox, got %q", got)
	}
	for _, value := range []string{"", "bad-value", quickBooksEnvironmentProduction} {
		if got := normalizeQuickBooksEnvironment(value); got != quickBooksEnvironmentProduction {
			t.Fatalf("expected production for %q, got %q", value, got)
		}
	}
}

func TestQuickBooksAPIBaseURLDefaultsToProduction(t *testing.T) {
	withQuickBooksEnvironment(t, "")

	if got := quickBooksAPIBaseURL(); got != quickBooksProductionAPIBaseURL {
		t.Fatalf("expected production base URL, got %q", got)
	}
}

func TestDefaultSettingsUseQuickBooksProduction(t *testing.T) {
	if got := defaultSettings().QuickBooksEnvironment; got != quickBooksEnvironmentProduction {
		t.Fatalf("expected default QuickBooks environment %q, got %q", quickBooksEnvironmentProduction, got)
	}
}

func TestParseQuickBooksAttachableID(t *testing.T) {
	raw := []byte(`{"AttachableResponse":[{"Attachable":{"Id":"987"}}]}`)
	if got := parseQuickBooksAttachableID(raw); got != "987" {
		t.Fatalf("expected attachable id 987, got %q", got)
	}
}

func TestFormatQuickBooksUploadErrorMapsApplicationAuthorizationFailed(t *testing.T) {
	raw := []byte(`{"fault":{"error":[{"message":"message=ApplicationAuthorizationFailed; errorCode=003100; statusCode=403","code":"3100"}]}}`)
	err := quickBooksReceiptUploadError(http.StatusForbidden, raw)

	if !isQuickBooksAuthorizationFailed(raw) {
		t.Fatalf("expected authorization failure classification")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "reconnect quickbooks") {
		t.Fatalf("expected reconnect guidance, got %v", err)
	}
	if strings.Contains(err.Error(), `"fault"`) {
		t.Fatalf("expected user-facing message without raw payload, got %v", err)
	}
}

func TestFormatQuickBooksUploadErrorKeepsUnexpectedPayload(t *testing.T) {
	raw := []byte(`{"fault":{"error":[{"message":"other failure","code":"9999"}]}}`)
	err := quickBooksReceiptUploadError(http.StatusBadRequest, raw)

	if isQuickBooksAuthorizationFailed(raw) {
		t.Fatalf("unexpected authorization classification for %s", raw)
	}
	if !strings.Contains(err.Error(), "other failure") {
		t.Fatalf("expected original payload for unexpected error, got %v", err)
	}
}

func withQuickBooksEnvironment(t *testing.T, environment string) {
	t.Helper()
	t.Cleanup(func() {
		settingsMutex.Lock()
		settings = Settings{}
		settingsMutex.Unlock()
	})
	settingsMutex.Lock()
	settings = Settings{QuickBooksEnvironment: environment}
	settingsMutex.Unlock()
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
	encrypted, err := EncryptSecret(cfg.Token)
	if err != nil {
		t.Fatalf("failed to encrypt test token: %v", err)
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
