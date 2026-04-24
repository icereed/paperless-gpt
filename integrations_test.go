package main

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func newIntegrationTestContext(req *http.Request) *gin.Context {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	return c
}

func TestJobberMatchCandidateDisplayLabel(t *testing.T) {
	candidate := JobberMatchCandidate{
		JobNumber:  "1109",
		ClientName: "Paul Remy",
		JobName:    "Untitled job",
	}

	got := candidate.DisplayLabel()
	want := "#1109 - Paul Remy - Untitled job"
	if got != want {
		t.Fatalf("DisplayLabel() = %q, want %q", got, want)
	}
}

func TestRankJobberCandidatesPrefersJobNumberClientAndTitleMatches(t *testing.T) {
	document := Document{
		Title:            "Paint receipt for job 1107",
		Content:          "Materials purchased for Imene Benaissa and peinture retouche.",
		Correspondent:    "Benjamin Moore",
		DocumentTypeName: "Receipt",
	}

	candidates := []JobberMatchCandidate{
		{
			ID:         "job-1",
			JobNumber:  "1103",
			ClientName: "Monica Bialobrzeski",
			JobName:    "Hardwood floor restauration estimate",
		},
		{
			ID:         "job-2",
			JobNumber:  "1107",
			ClientName: "Imene Benaissa",
			JobName:    "Retouche de peinture et travaux divers",
		},
	}

	ranked := rankJobberCandidates(document, candidates)
	if len(ranked) != 2 {
		t.Fatalf("expected 2 ranked candidates, got %d", len(ranked))
	}

	if ranked[0].ID != "job-2" {
		t.Fatalf("expected best ranked candidate to be job-2, got %s", ranked[0].ID)
	}
}

func TestIssueAndConsumeReceiptAccessToken(t *testing.T) {
	db, err := InitializeTestDB()
	if err != nil {
		t.Fatalf("InitializeTestDB() error = %v", err)
	}

	service := NewIntegrationsService(db)
	token, err := service.IssueReceiptAccessToken(t.Context(), 42, time.Minute)
	if err != nil {
		t.Fatalf("IssueReceiptAccessToken() error = %v", err)
	}
	if token.Token == "" {
		t.Fatal("expected non-empty token")
	}

	record, err := service.ConsumeReceiptAccessToken(t.Context(), token.Token)
	if err != nil {
		t.Fatalf("ConsumeReceiptAccessToken() error = %v", err)
	}
	if record.DocumentID != 42 {
		t.Fatalf("expected document ID 42, got %d", record.DocumentID)
	}

	// Tokens are single-use: second consume must fail because the row was deleted.
	_, err = service.ConsumeReceiptAccessToken(t.Context(), token.Token)
	if err == nil {
		t.Fatal("expected error on second ConsumeReceiptAccessToken(), got nil")
	}
}

func TestGetIntegrationProviderJobberUsesCurrentEnv(t *testing.T) {
	t.Setenv("JOBBER_CLIENT_ID", "jobber-client-id")
	t.Setenv("JOBBER_CLIENT_SECRET", "jobber-client-secret")

	provider := getIntegrationProvider(integrationProviderJobber)
	if provider == nil {
		t.Fatal("expected jobber provider")
	}

	configured, reason := provider.Configured()
	if !configured {
		t.Fatalf("expected jobber provider to be configured, got reason %q", reason)
	}
}

func TestGetIntegrationProviderJobberRequiresBothEnvVars(t *testing.T) {
	originalID, hadID := os.LookupEnv("JOBBER_CLIENT_ID")
	originalSecret, hadSecret := os.LookupEnv("JOBBER_CLIENT_SECRET")
	defer func() {
		if hadID {
			_ = os.Setenv("JOBBER_CLIENT_ID", originalID)
		} else {
			_ = os.Unsetenv("JOBBER_CLIENT_ID")
		}
		if hadSecret {
			_ = os.Setenv("JOBBER_CLIENT_SECRET", originalSecret)
		} else {
			_ = os.Unsetenv("JOBBER_CLIENT_SECRET")
		}
	}()

	if err := os.Unsetenv("JOBBER_CLIENT_ID"); err != nil {
		t.Fatalf("Unsetenv(JOBBER_CLIENT_ID) error = %v", err)
	}
	if err := os.Unsetenv("JOBBER_CLIENT_SECRET"); err != nil {
		t.Fatalf("Unsetenv(JOBBER_CLIENT_SECRET) error = %v", err)
	}

	provider := getIntegrationProvider(integrationProviderJobber)
	if provider == nil {
		t.Fatal("expected jobber provider")
	}

	configured, reason := provider.Configured()
	if configured {
		t.Fatal("expected jobber provider to be unconfigured without env vars")
	}
	if reason == "" {
		t.Fatal("expected non-empty reason when env vars are missing")
	}
}

func TestConfiguredPublicBaseURLPrefersLegacyOverride(t *testing.T) {
	t.Setenv("APP_PUBLIC_URL", "https://paperless-gpt.thomasrich.ca")
	t.Setenv("PAPERLESS_GPT_PUBLIC_URL", "https://legacy.paperless-gpt.example.com/")

	got := configuredPublicBaseURL()
	want := "https://legacy.paperless-gpt.example.com"
	if got != want {
		t.Fatalf("configuredPublicBaseURL() = %q, want %q", got, want)
	}
}

func TestOAuthCallbackURLUsesAppPublicURL(t *testing.T) {
	t.Setenv("APP_PUBLIC_URL", "https://paperless-gpt.thomasrich.ca/")
	t.Setenv("PAPERLESS_GPT_PUBLIC_URL", "")

	req := httptest.NewRequest(http.MethodGet, "http://192.168.1.20:8036/api/integrations/jobber/connect/start", nil)
	req.Host = "192.168.1.20:8036"

	got := oauthCallbackURL(newIntegrationTestContext(req), integrationProviderJobber)
	want := "https://paperless-gpt.thomasrich.ca/api/integrations/jobber/oauth/callback"
	if got != want {
		t.Fatalf("oauthCallbackURL() = %q, want %q", got, want)
	}
}

func TestGetExternalBaseURLFallsBackToForwardedHeaders(t *testing.T) {
	t.Setenv("APP_PUBLIC_URL", "")
	t.Setenv("PAPERLESS_GPT_PUBLIC_URL", "")

	req := httptest.NewRequest(http.MethodGet, "http://192.168.1.20:8036/api/integrations/jobber/connect/start", nil)
	req.Host = "192.168.1.20:8036"
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "paperless-gpt.thomasrich.ca")

	got := getExternalBaseURL(newIntegrationTestContext(req))
	want := "https://paperless-gpt.thomasrich.ca"
	if got != want {
		t.Fatalf("getExternalBaseURL() = %q, want %q", got, want)
	}
}

func TestResolveJobberExpenseFieldValuePrefersSuggestedBuiltInFields(t *testing.T) {
	suggestion := DocumentSuggestion{
		SuggestedTitle:         "Approved receipt title",
		SuggestedCorrespondent: "Approved vendor",
		SuggestedDocumentType:  "Receipt",
		OriginalDocument: Document{
			Title:            "Original title",
			Correspondent:    "Original vendor",
			DocumentTypeName: "Invoice",
		},
	}

	title, ok := resolveJobberExpenseFieldValue(suggestion, paperlessFieldRefDocumentTitle)
	if !ok || title != "Approved receipt title" {
		t.Fatalf("expected suggested title, got %#v (ok=%v)", title, ok)
	}

	correspondent, ok := resolveJobberExpenseFieldValue(suggestion, paperlessFieldRefDocumentCorrespondent)
	if !ok || correspondent != "Approved vendor" {
		t.Fatalf("expected suggested correspondent, got %#v (ok=%v)", correspondent, ok)
	}

	documentType, ok := resolveJobberExpenseFieldValue(suggestion, paperlessFieldRefDocumentType)
	if !ok || documentType != "Receipt" {
		t.Fatalf("expected suggested document type, got %#v (ok=%v)", documentType, ok)
	}
}

func TestResolveJobberExpenseFieldValueSupportsCustomFieldReferences(t *testing.T) {
	suggestion := DocumentSuggestion{
		SuggestedCustomFields: []CustomFieldSuggestion{
			{ID: 17, Name: "Total", Value: "123.45"},
		},
		OriginalDocument: Document{
			CustomFields: []CustomFieldResponse{
				{Field: 19, Value: "fallback"},
			},
		},
	}

	value, ok := resolveJobberExpenseFieldValue(suggestion, customFieldReference(17))
	if !ok || value != "123.45" {
		t.Fatalf("expected suggested custom field value, got %#v (ok=%v)", value, ok)
	}

	value, ok = resolveJobberExpenseFieldValue(suggestion, customFieldReference(19))
	if !ok || value != "fallback" {
		t.Fatalf("expected original custom field fallback, got %#v (ok=%v)", value, ok)
	}
}

func TestDeriveJobberExpenseDateUsesMappedField(t *testing.T) {
	suggestion := DocumentSuggestion{
		SuggestedCreatedDate: "2026-04-15",
	}

	got, err := deriveJobberExpenseDate(suggestion, paperlessFieldRefDocumentCreatedDate)
	if err != nil {
		t.Fatalf("deriveJobberExpenseDate() error = %v", err)
	}
	if got != "2026-04-15T00:00:00Z" {
		t.Fatalf("deriveJobberExpenseDate() = %q, want %q", got, "2026-04-15T00:00:00Z")
	}
}

func TestDeriveJobberExpenseTotalUsesMappedField(t *testing.T) {
	suggestion := DocumentSuggestion{
		SuggestedCustomFields: []CustomFieldSuggestion{
			{ID: 21, Name: "Amount", Value: "$456.78"},
		},
	}

	got, ok := deriveJobberExpenseTotal(suggestion, customFieldReference(21))
	if !ok {
		t.Fatal("expected mapped total to be detected")
	}
	if got != 456.78 {
		t.Fatalf("deriveJobberExpenseTotal() = %v, want %v", got, 456.78)
	}
}

func TestDeriveJobberExpenseDateFallsBackToToday(t *testing.T) {
	// A suggestion with no date at all — should not error, should return today.
	suggestion := DocumentSuggestion{ID: 99}
	got, err := deriveJobberExpenseDate(suggestion, "")
	if err != nil {
		t.Fatalf("deriveJobberExpenseDate() unexpected error: %v", err)
	}
	today := time.Now().Format("2006-01-02")
	expected := today + "T00:00:00Z"
	if got != expected {
		t.Fatalf("deriveJobberExpenseDate() = %q, want %q (today)", got, expected)
	}
}

func TestDeriveJobberExpenseDateUsesOriginalDocumentDate(t *testing.T) {
	suggestion := DocumentSuggestion{
		OriginalDocument: Document{CreatedDate: "2025-03-10"},
	}
	got, err := deriveJobberExpenseDate(suggestion, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "2025-03-10T00:00:00Z" {
		t.Fatalf("got %q, want %q", got, "2025-03-10T00:00:00Z")
	}
}

func TestIsJobberAuthError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		{"random error", fmt.Errorf("network timeout"), false},
		{"401 status", fmt.Errorf("graphql request failed: 401, unauthorized"), true},
		{"upper-case unauthorized", fmt.Errorf("Unauthorized request"), true},
		{"invalid_token body", fmt.Errorf("oauth: invalid_token"), true},
		{"wrapped invalid_grant", fmt.Errorf("refresh: %w", fmt.Errorf("oauth2: invalid_grant: expired")), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isJobberAuthError(tc.err); got != tc.want {
				t.Fatalf("isJobberAuthError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// resetJobberConnectionRow wipes the shared in-memory sqlite row used by all
// integration tests so each test starts from a clean state. The tests in this
// file share the same `file::memory:?cache=shared` database via
// InitializeTestDB().
func resetJobberConnectionRow(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.Where("provider = ?", integrationProviderJobber).Delete(&IntegrationConnection{}).Error; err != nil {
		t.Fatalf("reset jobber row: %v", err)
	}
}

func TestFetchAllJobberCandidatesReturnsNotConnectedWhenNoRow(t *testing.T) {
	db, err := InitializeTestDB()
	if err != nil {
		t.Fatalf("InitializeTestDB() error = %v", err)
	}
	resetJobberConnectionRow(t, db)

	service := NewIntegrationsService(db)
	candidates, err := service.FetchAllJobberCandidates(t.Context())
	if !errors.Is(err, errJobberNotConnected) {
		t.Fatalf("expected errJobberNotConnected, got candidates=%v, err=%v", candidates, err)
	}
	if candidates != nil {
		t.Fatalf("expected nil candidates on not-connected, got %v", candidates)
	}
}

func TestFetchAllJobberCandidatesReturnsNotConnectedWhenDisconnected(t *testing.T) {
	db, err := InitializeTestDB()
	if err != nil {
		t.Fatalf("InitializeTestDB() error = %v", err)
	}
	resetJobberConnectionRow(t, db)

	now := time.Now()
	if err := db.Create(&IntegrationConnection{
		Provider:       integrationProviderJobber,
		Status:         integrationStatusDisconnected,
		DisconnectedAt: &now,
	}).Error; err != nil {
		t.Fatalf("seed disconnect row: %v", err)
	}

	service := NewIntegrationsService(db)
	_, err = service.FetchAllJobberCandidates(t.Context())
	if !errors.Is(err, errJobberNotConnected) {
		t.Fatalf("expected errJobberNotConnected when row is disconnected, got %v", err)
	}
}

func TestEnsureFreshTokenRefreshesWhenExpiryIsNil(t *testing.T) {
	db, err := InitializeTestDB()
	if err != nil {
		t.Fatalf("InitializeTestDB() error = %v", err)
	}
	resetJobberConnectionRow(t, db)

	// Seed a connection with no AccessTokenExpiresAt AND no refresh token.
	// We expect ensureFreshToken to attempt a refresh (previously it was a no-op)
	// and fail loudly so the caller surfaces the problem instead of sending a
	// request with a stale token that produces an opaque 401.
	if err := db.Create(&IntegrationConnection{
		Provider:    integrationProviderJobber,
		Status:      integrationStatusConnected,
		AccessToken: "stale",
	}).Error; err != nil {
		t.Fatalf("seed conn: %v", err)
	}

	conn, err := getConnectionByProvider(db, integrationProviderJobber)
	if err != nil {
		t.Fatalf("get conn: %v", err)
	}

	provider := jobberProvider{}
	_, err = provider.ensureFreshToken(t.Context(), db, conn)
	if err == nil {
		t.Fatal("expected ensureFreshToken to return an error when expiry is nil and refresh token is missing")
	}
}

func TestFetchAllJobberCandidatesIntegrationSchema(t *testing.T) {
	// Smoke-check that the sentinel errors are defined with stable messages.
	if errJobberNotConnected == nil || errJobberAuthFailed == nil {
		t.Fatal("sentinel errors must be defined")
	}
	if errJobberNotConnected.Error() == errJobberAuthFailed.Error() {
		t.Fatal("sentinel errors must be distinguishable")
	}
}
