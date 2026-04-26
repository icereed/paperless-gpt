package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/oauth2"
	"gorm.io/gorm"
)

const (
	integrationProviderJobber      = "jobber"
	integrationProviderGoogleDrive = "google_drive"
	integrationProviderQuickBooks  = "quickbooks"
	integrationProviderFirefly     = "firefly"

	integrationStatusConnected    = "connected"
	integrationStatusDisconnected = "disconnected"

	// jobberGraphQLVersion is the Jobber GraphQL API version sent in every
	// request via the X-JOBBER-GRAPHQL-VERSION header, which is required by
	// Jobber for all apps.
	jobberGraphQLVersion = "2025-04-16"
)

const (
	paperlessFieldRefCustomPrefix          = "custom_field:"
	paperlessFieldRefDocumentTitle         = "document.title"
	paperlessFieldRefDocumentContent       = "document.content"
	paperlessFieldRefDocumentCorrespondent = "document.correspondent"
	paperlessFieldRefDocumentCreatedDate   = "document.created_date"
	paperlessFieldRefDocumentType          = "document.document_type"
	paperlessFieldRefOriginalFileName      = "document.original_file_name"
	paperlessFieldRefArchivedFileName      = "document.archived_file_name"
)

type IntegrationConnection struct {
	ID                   uint   `gorm:"primaryKey"`
	Provider             string `gorm:"uniqueIndex;size:64;not null"`
	Status               string `gorm:"size:32;not null"`
	AccountID            string `gorm:"size:255"`
	AccountName          string `gorm:"size:255"`
	AccessToken          string `gorm:"type:TEXT"`
	RefreshToken         string `gorm:"type:TEXT"`
	AccessTokenExpiresAt *time.Time
	Scopes               string `gorm:"type:TEXT"`
	MetadataJSON         string `gorm:"type:TEXT"`
	DisconnectedAt       *time.Time
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

type OAuthStateRecord struct {
	ID         uint   `gorm:"primaryKey"`
	Provider   string `gorm:"size:64;index;not null"`
	State      string `gorm:"uniqueIndex;size:255;not null"`
	ReturnPath string `gorm:"size:1024"`
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type IntegrationActionLog struct {
	ID              uint   `gorm:"primaryKey"`
	DocumentID      int    `gorm:"index;not null"`
	BatchID         *uint  `gorm:"index"`
	Provider        string `gorm:"size:64;index;not null"`
	ActionType      string `gorm:"size:64;not null"`
	Status          string `gorm:"size:32;not null"`
	ExternalID      string `gorm:"size:255"`
	ExternalURL     string `gorm:"size:2048"`
	RequestSummary  string `gorm:"type:TEXT"`
	ResponseSummary string `gorm:"type:TEXT"`
	ErrorMessage    string `gorm:"type:TEXT"`
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type integrationOAuthStartRequest struct {
	ReturnPath string `json:"return_path"`
}

type integrationOAuthStartResponse struct {
	URL string `json:"url"`
}

type providerToken struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    *time.Time
	Scopes       []string
}

type providerIdentity struct {
	AccountID   string
	AccountName string
	Metadata    map[string]string
}

type integrationProvider interface {
	Name() string
	Configured() (bool, string)
	AuthorizationURL(c *gin.Context, state string) (string, error)
	ExchangeCode(ctx context.Context, c *gin.Context, code string) (*providerToken, error)
	RefreshToken(ctx context.Context, conn *IntegrationConnection) (*providerToken, error)
	FetchIdentity(ctx context.Context, conn *IntegrationConnection) (*providerIdentity, error)
}

type providerNotConfiguredError struct {
	Reason string
}

func (e providerNotConfiguredError) Error() string {
	return e.Reason
}

func getIntegrationProvider(provider string) integrationProvider {
	switch provider {
	case integrationProviderJobber:
		return newJobberProvider()
	case integrationProviderGoogleDrive:
		return newGoogleDriveProvider()
	case integrationProviderQuickBooks:
		return newQuickBooksProvider()
	default:
		return nil
	}
}

func generateOAuthStateToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func configuredPublicBaseURL() string {
	if configured := strings.TrimSpace(os.Getenv("PAPERLESS_GPT_PUBLIC_URL")); configured != "" {
		return strings.TrimRight(configured, "/")
	}
	if configured := strings.TrimSpace(os.Getenv("APP_PUBLIC_URL")); configured != "" {
		return strings.TrimRight(configured, "/")
	}
	return ""
}

func getExternalBaseURL(c *gin.Context) string {
	if configured := configuredPublicBaseURL(); configured != "" {
		return configured
	}

	scheme := c.Request.Header.Get("X-Forwarded-Proto")
	if scheme == "" {
		if c.Request.TLS != nil {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}

	host := c.Request.Header.Get("X-Forwarded-Host")
	if host == "" {
		host = c.Request.Host
	}

	return fmt.Sprintf("%s://%s", scheme, host)
}

func oauthCallbackURL(c *gin.Context, provider string) string {
	return fmt.Sprintf("%s/api/integrations/%s/oauth/callback", getExternalBaseURL(c), provider)
}

func getConnectionByProvider(db *gorm.DB, provider string) (*IntegrationConnection, error) {
	var conn IntegrationConnection
	err := db.Where("provider = ?", provider).First(&conn).Error
	if err != nil {
		return nil, err
	}
	return &conn, nil
}

func getOptionalConnectionByProvider(db *gorm.DB, provider string) (*IntegrationConnection, error) {
	conn, err := getConnectionByProvider(db, provider)
	if err != nil && err != gorm.ErrRecordNotFound {
		return nil, err
	}
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return conn, nil
}

func upsertIntegrationConnection(db *gorm.DB, provider string, token *providerToken, identity *providerIdentity) (*IntegrationConnection, error) {
	conn, err := getOptionalConnectionByProvider(db, provider)
	if err != nil {
		return nil, err
	}
	if conn == nil {
		conn = &IntegrationConnection{Provider: provider}
	}

	conn.Status = integrationStatusConnected
	conn.AccessToken = token.AccessToken
	if token.RefreshToken != "" {
		conn.RefreshToken = token.RefreshToken
	}
	conn.AccessTokenExpiresAt = token.ExpiresAt
	if len(token.Scopes) > 0 {
		conn.Scopes = strings.Join(token.Scopes, " ")
	}
	conn.DisconnectedAt = nil

	if identity != nil {
		conn.AccountID = identity.AccountID
		conn.AccountName = identity.AccountName
		if len(identity.Metadata) > 0 {
			metadataJSON, err := json.Marshal(identity.Metadata)
			if err != nil {
				return nil, err
			}
			conn.MetadataJSON = string(metadataJSON)
		}
	}

	if conn.ID == 0 {
		if err := db.Create(conn).Error; err != nil {
			return nil, err
		}
	} else {
		if err := db.Save(conn).Error; err != nil {
			return nil, err
		}
	}

	return conn, nil
}

func disconnectIntegrationConnection(db *gorm.DB, provider string) error {
	conn, err := getOptionalConnectionByProvider(db, provider)
	if err != nil {
		return err
	}
	if conn == nil {
		return nil
	}
	now := time.Now()
	conn.Status = integrationStatusDisconnected
	conn.AccessToken = ""
	conn.RefreshToken = ""
	conn.AccessTokenExpiresAt = nil
	conn.DisconnectedAt = &now
	return db.Save(conn).Error
}

func saveOAuthState(db *gorm.DB, provider, state, returnPath string) error {
	record := OAuthStateRecord{
		Provider:   provider,
		State:      state,
		ReturnPath: returnPath,
	}
	return db.Create(&record).Error
}

func consumeOAuthState(db *gorm.DB, provider, state string) (*OAuthStateRecord, error) {
	var record OAuthStateRecord
	if err := db.Where("provider = ? AND state = ?", provider, state).First(&record).Error; err != nil {
		return nil, err
	}
	if err := db.Delete(&record).Error; err != nil {
		return nil, err
	}
	return &record, nil
}

func metadataMap(conn *IntegrationConnection) map[string]string {
	if conn == nil || strings.TrimSpace(conn.MetadataJSON) == "" {
		return map[string]string{}
	}
	var result map[string]string
	if err := json.Unmarshal([]byte(conn.MetadataJSON), &result); err != nil {
		log.WithError(err).Warn("Failed to parse integration metadata")
		return map[string]string{}
	}
	return result
}

func summarizeIntegrationStatus(provider string, impl integrationProvider, conn *IntegrationConnection) IntegrationConnectionStatus {
	configured, reason := impl.Configured()
	status := IntegrationConnectionStatus{
		Provider:   provider,
		Configured: configured,
		Reason:     reason,
	}
	if conn != nil && conn.Status == integrationStatusConnected {
		status.Connected = true
		status.AccountName = conn.AccountName
		status.AccountID = conn.AccountID
	}
	return status
}

func (app *App) getIntegrationStatuses(ctx context.Context) ([]IntegrationConnectionStatus, error) {
	providers := []string{
		integrationProviderJobber,
		integrationProviderGoogleDrive,
		integrationProviderQuickBooks,
	}

	statuses := make([]IntegrationConnectionStatus, 0, len(providers)+1)
	for _, providerName := range providers {
		impl := getIntegrationProvider(providerName)
		if impl == nil {
			continue
		}
		conn, err := getOptionalConnectionByProvider(app.Database.WithContext(ctx), providerName)
		if err != nil {
			return nil, err
		}
		statuses = append(statuses, summarizeIntegrationStatus(providerName, impl, conn))
	}
	if app.Integrations != nil {
		statuses = append(statuses, app.Integrations.FireflyStatus(ctx))
	}

	return statuses, nil
}

func (app *App) getIntegrationsHandler(c *gin.Context) {
	statuses, err := app.getIntegrationStatuses(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to load integrations: %v", err)})
		return
	}
	c.JSON(http.StatusOK, gin.H{"integrations": statuses})
}

func buildIntegrationRedirectURL(returnPath, provider, status, result string) string {
	path := strings.TrimSpace(returnPath)
	if path == "" {
		path = "/settings"
	}
	separator := "?"
	if strings.Contains(path, "?") {
		separator = "&"
	}
	return fmt.Sprintf("%s%sintegration=%s&status=%s&result=%s", path, separator, provider, status, result)
}

func insertIntegrationActionLog(db *gorm.DB, entry *IntegrationActionLog) {
	if err := db.Create(entry).Error; err != nil {
		log.WithError(err).Warn("Failed to persist integration action log")
	}
}

type IntegrationsService struct {
	DB             *gorm.DB
	CurrentBatchID *uint
}

type GoogleDriveUploadResult struct {
	FileID  string
	FileURL string
}

type JobberExpenseCreateResult struct {
	ExpenseID string
	WebURL    string
}

type ReceiptAccessToken struct {
	ID         uint      `gorm:"primaryKey"`
	Token      string    `gorm:"uniqueIndex;size:255;not null"`
	DocumentID int       `gorm:"index;not null"`
	ExpiresAt  time.Time `gorm:"index;not null"`
	CreatedAt  time.Time
}

func NewIntegrationsService(db *gorm.DB) *IntegrationsService {
	return &IntegrationsService{DB: db}
}

func (s *IntegrationsService) Status(provider string) IntegrationConnectionStatus {
	impl := getIntegrationProvider(provider)
	if impl == nil {
		return IntegrationConnectionStatus{}
	}
	conn, err := getOptionalConnectionByProvider(s.DB, provider)
	if err != nil {
		log.WithError(err).Warnf("failed to fetch connection for provider %s", provider)
	}
	return summarizeIntegrationStatus(provider, impl, conn)
}

func (s *IntegrationsService) Disconnect(ctx context.Context, provider string) error {
	return disconnectIntegrationConnection(s.DB.WithContext(ctx), provider)
}

func (s *IntegrationsService) IssueReceiptAccessToken(ctx context.Context, documentID int, ttl time.Duration) (*ReceiptAccessToken, error) {
	token, err := generateOAuthStateToken()
	if err != nil {
		return nil, err
	}
	receiptToken := &ReceiptAccessToken{
		Token:      token,
		DocumentID: documentID,
		ExpiresAt:  time.Now().Add(ttl),
	}
	if err := s.DB.WithContext(ctx).Create(receiptToken).Error; err != nil {
		return nil, err
	}
	return receiptToken, nil
}

func (s *IntegrationsService) ConsumeReceiptAccessToken(ctx context.Context, token string) (*ReceiptAccessToken, error) {
	var record ReceiptAccessToken
	if err := s.DB.WithContext(ctx).Where("token = ?", token).First(&record).Error; err != nil {
		return nil, err
	}
	if record.ExpiresAt.Before(time.Now()) {
		_ = s.DB.WithContext(ctx).Delete(&record).Error
		return nil, fmt.Errorf("receipt token expired")
	}
	// Delete the token after a successful read so it can only be used once.
	_ = s.DB.WithContext(ctx).Delete(&record).Error
	return &record, nil
}

// jobberCandidatePageSize is the number of jobs fetched per Jobber API page.
const jobberCandidatePageSize = 100

// jobberCandidateMaxJobs is the upper bound on total Jobber jobs fetched to avoid
// very large accounts causing excessive API calls (100 jobs/page × 5 pages = 500 max).
const jobberCandidateMaxJobs = 500

// Sentinel errors surfaced by the Jobber integration so HTTP handlers can map
// them to an appropriate status code and the UI can render a targeted message.
var (
	// errJobberNotConnected means the user has not connected Jobber (or has
	// disconnected it). The UI should prompt the user to connect.
	errJobberNotConnected = fmt.Errorf("jobber is not connected")
	// errJobberAuthFailed means Jobber rejected our token (401/unauthorized) and
	// we could not refresh it. The connection is likely stale and should be
	// re-authorized from the Integrations settings page.
	errJobberAuthFailed = fmt.Errorf("jobber authentication failed; please reconnect Jobber from Settings → Integrations")
)

// isAuthError reports whether an error returned by the Jobber GraphQL API
// represents an authentication failure that should trigger a token refresh
// or prompt the user to reconnect.
func isJobberAuthError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "401") ||
		strings.Contains(msg, "unauthorized") ||
		strings.Contains(msg, "invalid_token") ||
		strings.Contains(msg, "invalid_grant")
}

// FetchAllJobberCandidates fetches the full job list from Jobber using cursor-based
// pagination and returns it as an unranked slice of JobberMatchCandidate.
// It is intended to be called once per batch request, not once per document.
//
// If Jobber is not connected it returns errJobberNotConnected so callers can
// surface a useful message to the user instead of silently pretending the
// account has zero jobs.
func (s *IntegrationsService) FetchAllJobberCandidates(ctx context.Context) ([]JobberMatchCandidate, error) {
	conn, err := getOptionalConnectionByProvider(s.DB.WithContext(ctx), integrationProviderJobber)
	if err != nil {
		return nil, fmt.Errorf("loading jobber connection: %w", err)
	}
	if conn == nil || conn.Status != integrationStatusConnected {
		log.Debug("FetchAllJobberCandidates: Jobber is not connected")
		return nil, errJobberNotConnected
	}

	impl := newJobberProvider()
	validConn, err := impl.ensureFreshToken(ctx, s.DB.WithContext(ctx), conn)
	if err != nil {
		log.WithError(err).Warn("FetchAllJobberCandidates: failed to ensure fresh Jobber token")
		if isJobberAuthError(err) {
			_ = disconnectIntegrationConnection(s.DB.WithContext(ctx), integrationProviderJobber)
			return nil, errJobberAuthFailed
		}
		return nil, err
	}

	type jobNode struct {
		ID          string `json:"id"`
		JobNumber   int    `json:"jobNumber"`
		Title       string `json:"title"`
		StartAt     string `json:"startAt"`
		EndAt       string `json:"endAt"`
		CompletedAt string `json:"completedAt"`
		CreatedAt   string `json:"createdAt"`
		Client      struct {
			Name        string `json:"name"`
			CompanyName string `json:"companyName"`
		} `json:"client"`
	}

	const query = `query JobCandidates($first: Int!, $after: String) {
  jobs(first: $first, after: $after) {
    edges {
      cursor
      node {
        id
        jobNumber
        title
        startAt
        endAt
        completedAt
        createdAt
        client {
          name
          companyName
        }
      }
    }
    pageInfo {
      hasNextPage
      endCursor
    }
  }
}`

	var allNodes []jobNode
	cursor := ""

	// Try once, refresh-and-retry once on auth failure so a rotated token that
	// we haven't noticed expiring yet doesn't nuke a whole batch.
	retriedAuth := false

	for len(allNodes) < jobberCandidateMaxJobs {
		variables := map[string]interface{}{
			"first": jobberCandidatePageSize,
		}
		if cursor != "" {
			variables["after"] = cursor
		}

		var response struct {
			Data struct {
				Jobs struct {
					Edges []struct {
						Cursor string  `json:"cursor"`
						Node   jobNode `json:"node"`
					} `json:"edges"`
					PageInfo struct {
						HasNextPage bool   `json:"hasNextPage"`
						EndCursor   string `json:"endCursor"`
					} `json:"pageInfo"`
				} `json:"jobs"`
			} `json:"data"`
			Errors []struct {
				Message    string `json:"message"`
				Extensions struct {
					Code string `json:"code"`
				} `json:"extensions"`
			} `json:"errors"`
		}

		if err := executeJSONGraphQL(ctx, "https://api.getjobber.com/api/graphql", validConn.AccessToken, query, variables, &response); err != nil {
			if isJobberAuthError(err) && !retriedAuth {
				retriedAuth = true
				log.WithError(err).Info("FetchAllJobberCandidates: auth error, attempting forced token refresh")
				refreshed, refreshErr := impl.forceRefreshToken(ctx, s.DB.WithContext(ctx), validConn)
				if refreshErr != nil {
					log.WithError(refreshErr).Warn("FetchAllJobberCandidates: forced token refresh failed; marking Jobber disconnected")
					_ = disconnectIntegrationConnection(s.DB.WithContext(ctx), integrationProviderJobber)
					return nil, errJobberAuthFailed
				}
				validConn = refreshed
				continue
			}
			return nil, fmt.Errorf("jobber graphql request failed: %w", err)
		}
		if len(response.Errors) > 0 {
			firstErr := response.Errors[0]
			code := strings.ToUpper(strings.TrimSpace(firstErr.Extensions.Code))
			if code == "UNAUTHENTICATED" || code == "UNAUTHORIZED" {
				_ = disconnectIntegrationConnection(s.DB.WithContext(ctx), integrationProviderJobber)
				return nil, errJobberAuthFailed
			}
			return nil, fmt.Errorf("jobber graphql error: %s", firstErr.Message)
		}

		for _, edge := range response.Data.Jobs.Edges {
			allNodes = append(allNodes, edge.Node)
		}

		if !response.Data.Jobs.PageInfo.HasNextPage || response.Data.Jobs.PageInfo.EndCursor == "" {
			break
		}
		cursor = response.Data.Jobs.PageInfo.EndCursor
	}

	log.Debugf("FetchAllJobberCandidates: fetched %d job(s) from Jobber", len(allNodes))

	candidates := make([]JobberMatchCandidate, 0, len(allNodes))
	for _, node := range allNodes {
		clientName := strings.TrimSpace(node.Client.CompanyName)
		if clientName == "" {
			clientName = strings.TrimSpace(node.Client.Name)
		}
		jobName := strings.TrimSpace(node.Title)
		if jobName == "" {
			jobName = "Untitled job"
		}
		candidates = append(candidates, JobberMatchCandidate{
			ID:          node.ID,
			JobNumber:   fmt.Sprintf("%d", node.JobNumber),
			ClientName:  clientName,
			JobName:     jobName,
			StartAt:     node.StartAt,
			EndAt:       node.EndAt,
			CompletedAt: node.CompletedAt,
			CreatedAt:   node.CreatedAt,
		})
	}

	return candidates, nil
}

// GetJobberCandidates returns the full Jobber job list ranked by relevance to the
// given document. Use FetchAllJobberCandidates when processing multiple documents
// to avoid redundant API calls.
func (s *IntegrationsService) GetJobberCandidates(ctx context.Context, document Document) ([]JobberMatchCandidate, error) {
	candidates, err := s.FetchAllJobberCandidates(ctx)
	if err != nil {
		return nil, err
	}
	return rankJobberCandidates(document, candidates), nil
}

func (s *IntegrationsService) UploadDocumentToGoogleDrive(ctx context.Context, client ClientInterface, documentID int, folderID string, batchID ...uint) (*GoogleDriveUploadResult, error) {
	conn, err := getOptionalConnectionByProvider(s.DB.WithContext(ctx), integrationProviderGoogleDrive)
	if err != nil {
		return nil, err
	}
	if conn == nil || conn.Status != integrationStatusConnected {
		return nil, fmt.Errorf("google drive is not connected")
	}

	impl := googleDriveProvider{}
	validConn, err := impl.ensureFreshToken(ctx, s.DB.WithContext(ctx), conn)
	if err != nil {
		return nil, err
	}

	document, err := client.GetDocument(ctx, documentID)
	if err != nil {
		return nil, err
	}
	filename := strings.TrimSpace(document.ArchivedFileName)
	if filename == "" {
		filename = strings.TrimSpace(document.OriginalFileName)
	}
	if filename == "" {
		filename = fmt.Sprintf("document-%d.pdf", documentID)
	}

	fileContent, err := client.DownloadPDF(ctx, document)
	if err != nil {
		return nil, err
	}

	metadata := map[string]interface{}{
		"name": filename,
	}
	if strings.TrimSpace(folderID) != "" {
		metadata["parents"] = []string{folderID}
	}
	metaJSON, err := json.Marshal(metadata)
	if err != nil {
		return nil, err
	}

	bodyReader, contentType, err := buildMultipartDriveUpload(metaJSON, fileContent, filename)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://www.googleapis.com/upload/drive/v3/files?uploadType=multipart&fields=id,webViewLink", bodyReader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+validConn.AccessToken)
	req.Header.Set("Content-Type", contentType)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("google drive upload failed: %d, %s", resp.StatusCode, string(bodyBytes))
	}

	var uploadResp struct {
		ID      string `json:"id"`
		WebView string `json:"webViewLink"`
	}
	if err := json.Unmarshal(bodyBytes, &uploadResp); err != nil {
		return nil, err
	}

	entry := &IntegrationActionLog{
		DocumentID:      documentID,
		Provider:        integrationProviderGoogleDrive,
		ActionType:      "upload_document",
		Status:          "success",
		ExternalID:      uploadResp.ID,
		ExternalURL:     uploadResp.WebView,
		ResponseSummary: string(bodyBytes),
	}
	if len(batchID) > 0 && batchID[0] > 0 {
		entry.BatchID = &batchID[0]
	}
	insertIntegrationActionLog(s.DB.WithContext(ctx), entry)

	return &GoogleDriveUploadResult{
		FileID:  uploadResp.ID,
		FileURL: uploadResp.WebView,
	}, nil
}

func (s *IntegrationsService) CreateJobberExpense(ctx context.Context, client ClientInterface, suggestion DocumentSuggestion, candidate JobberMatchCandidate, batchID ...uint) (*JobberExpenseCreateResult, error) {
	if !suggestion.ApplyJobber {
		return nil, fmt.Errorf("jobber apply is disabled for this document")
	}
	var appliedBatchID *uint
	if len(batchID) > 0 && batchID[0] > 0 {
		appliedBatchID = &batchID[0]
	}

	conn, err := getOptionalConnectionByProvider(s.DB.WithContext(ctx), integrationProviderJobber)
	if err != nil {
		return nil, err
	}
	if conn == nil || conn.Status != integrationStatusConnected {
		return nil, fmt.Errorf("jobber is not connected")
	}

	impl := newJobberProvider()
	validConn, err := impl.ensureFreshToken(ctx, s.DB.WithContext(ctx), conn)
	if err != nil {
		return nil, err
	}

	settingsMutex.RLock()
	titleFieldRef := strings.TrimSpace(settings.JobberExpenseTitleFieldRef)
	descriptionFieldRef := strings.TrimSpace(settings.JobberExpenseDescriptionFieldRef)
	dateFieldRef := strings.TrimSpace(settings.JobberExpenseDateFieldRef)
	totalFieldRef := strings.TrimSpace(settings.JobberExpenseTotalFieldRef)
	settingsMutex.RUnlock()

	title := resolveJobberExpenseString(suggestion, titleFieldRef)
	if title == "" {
		title = strings.TrimSpace(suggestion.SuggestedTitle)
	}
	if title == "" {
		title = strings.TrimSpace(suggestion.OriginalDocument.Title)
	}
	if title == "" {
		title = candidate.DisplayLabel()
	}

	description := resolveJobberExpenseString(suggestion, descriptionFieldRef)
	if description == "" {
		description = buildJobberExpenseDescription(suggestion)
	}
	dateValue, err := deriveJobberExpenseDate(suggestion, dateFieldRef)
	if err != nil {
		return nil, err
	}

	totalValue, hasTotal := deriveJobberExpenseTotal(suggestion, totalFieldRef)

	receiptURL := ""
	baseURL := configuredPublicBaseURL()
	if baseURL != "" {
		receiptToken, err := s.IssueReceiptAccessToken(ctx, suggestion.ID, 30*time.Minute)
		if err != nil {
			log.WithError(err).Warnf("failed to issue receipt access token for document %d; expense will be created without receipt attachment", suggestion.ID)
		} else {
			receiptURL = baseURL + "/api/integrations/jobber/receipt/" + url.PathEscape(receiptToken.Token)
		}
	} else {
		log.Warnf("APP_PUBLIC_URL / PAPERLESS_GPT_PUBLIC_URL is not set; Jobber expense for document %d will be created without receipt attachment", suggestion.ID)
	}

	input := map[string]interface{}{
		"title":       title,
		"date":        dateValue,
		"linkedJobId": candidate.ID,
	}
	if description != "" {
		input["description"] = description
	}
	if hasTotal {
		input["total"] = totalValue
	}
	if receiptURL != "" {
		input["receiptUrl"] = receiptURL
	}

	mutation := `mutation ExpenseCreate($input: ExpenseCreateInput!) {
  expenseCreate(input: $input) {
    expense {
      id
      linkedJob {
        id
      }
    }
    userErrors {
      message
      path
    }
  }
}`

	var response struct {
		Data struct {
			ExpenseCreate struct {
				Expense *struct {
					ID        string `json:"id"`
					LinkedJob *struct {
						ID string `json:"id"`
					} `json:"linkedJob"`
				} `json:"expense"`
				UserErrors []struct {
					Message string   `json:"message"`
					Path    []string `json:"path"`
				} `json:"userErrors"`
			} `json:"expenseCreate"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	requestSummary := fmt.Sprintf("job=%s title=%s", candidate.ID, title)

	if err := executeJSONGraphQL(ctx, "https://api.getjobber.com/api/graphql", validConn.AccessToken, mutation, map[string]interface{}{"input": input}, &response); err != nil {
		entry := &IntegrationActionLog{
			DocumentID:     suggestion.ID,
			Provider:       integrationProviderJobber,
			ActionType:     "expense_create",
			Status:         "error",
			RequestSummary: requestSummary,
			ErrorMessage:   err.Error(),
		}
		entry.BatchID = appliedBatchID
		insertIntegrationActionLog(s.DB.WithContext(ctx), entry)
		return nil, err
	}
	if len(response.Errors) > 0 {
		errMsg := fmt.Sprintf("jobber graphql error: %s", response.Errors[0].Message)
		entry := &IntegrationActionLog{
			DocumentID:     suggestion.ID,
			Provider:       integrationProviderJobber,
			ActionType:     "expense_create",
			Status:         "error",
			RequestSummary: requestSummary,
			ErrorMessage:   errMsg,
		}
		entry.BatchID = appliedBatchID
		insertIntegrationActionLog(s.DB.WithContext(ctx), entry)
		return nil, fmt.Errorf("%s", errMsg)
	}
	if len(response.Data.ExpenseCreate.UserErrors) > 0 {
		errMsg := fmt.Sprintf("jobber expense create error: %s", response.Data.ExpenseCreate.UserErrors[0].Message)
		entry := &IntegrationActionLog{
			DocumentID:     suggestion.ID,
			Provider:       integrationProviderJobber,
			ActionType:     "expense_create",
			Status:         "error",
			RequestSummary: requestSummary,
			ErrorMessage:   errMsg,
		}
		entry.BatchID = appliedBatchID
		insertIntegrationActionLog(s.DB.WithContext(ctx), entry)
		return nil, fmt.Errorf("%s", errMsg)
	}
	if response.Data.ExpenseCreate.Expense == nil {
		errMsg := "jobber expense create returned no expense"
		entry := &IntegrationActionLog{
			DocumentID:     suggestion.ID,
			Provider:       integrationProviderJobber,
			ActionType:     "expense_create",
			Status:         "error",
			RequestSummary: requestSummary,
			ErrorMessage:   errMsg,
		}
		entry.BatchID = appliedBatchID
		insertIntegrationActionLog(s.DB.WithContext(ctx), entry)
		return nil, fmt.Errorf("%s", errMsg)
	}

	expenseID := response.Data.ExpenseCreate.Expense.ID
	webURL := ""
	if strings.TrimSpace(validConn.AccountName) != "" {
		// URL is not directly exposed by the mutation payload we query; keep empty for now.
		webURL = ""
	}

	entry := &IntegrationActionLog{
		DocumentID:      suggestion.ID,
		Provider:        integrationProviderJobber,
		ActionType:      "expense_create",
		Status:          "success",
		ExternalID:      expenseID,
		RequestSummary:  requestSummary,
		ResponseSummary: expenseID,
	}
	entry.BatchID = appliedBatchID
	insertIntegrationActionLog(s.DB.WithContext(ctx), entry)

	return &JobberExpenseCreateResult{
		ExpenseID: expenseID,
		WebURL:    webURL,
	}, nil
}

func buildMultipartDriveUpload(metadataJSON []byte, fileContent []byte, filename string) (io.Reader, string, error) {
	boundary := "paperless-gpt-drive-upload"
	// Strip characters that would break MIME headers before embedding in Content-Disposition.
	safeFilename := strings.Map(func(r rune) rune {
		if r == '\r' || r == '\n' || r == 0 || r == '"' {
			return -1
		}
		return r
	}, filename)
	if safeFilename == "" {
		safeFilename = "document.pdf"
	}
	var header bytes.Buffer
	header.WriteString("--" + boundary + "\r\n")
	header.WriteString("Content-Type: application/json; charset=UTF-8\r\n\r\n")
	header.Write(metadataJSON)
	header.WriteString("\r\n--" + boundary + "\r\n")
	header.WriteString("Content-Type: application/octet-stream\r\n")
	header.WriteString(fmt.Sprintf("Content-Disposition: form-data; name=\"file\"; filename=\"%s\"\r\n\r\n", safeFilename))
	footer := []byte("\r\n--" + boundary + "--\r\n")
	reader := io.MultiReader(&header, bytes.NewReader(fileContent), bytes.NewReader(footer))
	return reader, "multipart/related; boundary=" + boundary, nil
}

func buildJobberExpenseDescription(suggestion DocumentSuggestion) string {
	parts := []string{}
	if value := strings.TrimSpace(suggestion.SuggestedCorrespondent); value != "" {
		parts = append(parts, "Vendor: "+value)
	} else if value := strings.TrimSpace(suggestion.OriginalDocument.Correspondent); value != "" {
		parts = append(parts, "Vendor: "+value)
	}
	if value := strings.TrimSpace(suggestion.OriginalDocument.DocumentTypeName); value != "" {
		parts = append(parts, "Type: "+value)
	}
	return strings.Join(parts, " | ")
}

func deriveJobberExpenseDate(suggestion DocumentSuggestion, fieldRef string) (string, error) {
	raw := resolveJobberExpenseString(suggestion, fieldRef)
	if raw == "" {
		raw = strings.TrimSpace(suggestion.SuggestedCreatedDate)
	}
	if raw == "" {
		raw = strings.TrimSpace(suggestion.OriginalDocument.CreatedDate)
	}
	if raw == "" {
		// Fall back to today's date so expense creation is never blocked by a missing date.
		// Jobber requires a date; if the document has none we use today rather than failing.
		raw = time.Now().Format("2006-01-02")
		log.WithField("document_id", suggestion.ID).
			Warnf("No date found for document; using today (%s) as the Jobber expense date", raw)
	}
	if len(raw) == len("2006-01-02") {
		return raw + "T00:00:00Z", nil
	}
	return raw, nil
}

func deriveJobberExpenseTotal(suggestion DocumentSuggestion, fieldRef string) (float64, bool) {
	if value, ok := resolveJobberExpenseFieldValue(suggestion, fieldRef); ok {
		if parsed, ok := parseNumericValue(value); ok {
			return parsed, true
		}
	}
	for _, field := range suggestion.SuggestedCustomFields {
		name := strings.ToLower(strings.TrimSpace(field.Name))
		if strings.Contains(name, "total") || strings.Contains(name, "amount") {
			if parsed, ok := parseNumericValue(field.Value); ok {
				return parsed, true
			}
		}
	}
	return 0, false
}

func customFieldReference(fieldID int) string {
	if fieldID <= 0 {
		return ""
	}
	return fmt.Sprintf("%s%d", paperlessFieldRefCustomPrefix, fieldID)
}

func resolveJobberExpenseString(suggestion DocumentSuggestion, fieldRef string) string {
	value, ok := resolveJobberExpenseFieldValue(suggestion, fieldRef)
	if !ok {
		return ""
	}
	return stringifyExpenseFieldValue(value)
}

func resolveJobberExpenseFieldValue(suggestion DocumentSuggestion, fieldRef string) (interface{}, bool) {
	switch strings.TrimSpace(fieldRef) {
	case "":
		return nil, false
	case paperlessFieldRefDocumentTitle:
		if value := strings.TrimSpace(suggestion.SuggestedTitle); value != "" {
			return value, true
		}
		if value := strings.TrimSpace(suggestion.OriginalDocument.Title); value != "" {
			return value, true
		}
	case paperlessFieldRefDocumentContent:
		if value := strings.TrimSpace(suggestion.SuggestedContent); value != "" {
			return value, true
		}
		if value := strings.TrimSpace(suggestion.OriginalDocument.Content); value != "" {
			return value, true
		}
	case paperlessFieldRefDocumentCorrespondent:
		if value := strings.TrimSpace(suggestion.SuggestedCorrespondent); value != "" {
			return value, true
		}
		if value := strings.TrimSpace(suggestion.OriginalDocument.Correspondent); value != "" {
			return value, true
		}
	case paperlessFieldRefDocumentCreatedDate:
		if value := strings.TrimSpace(suggestion.SuggestedCreatedDate); value != "" {
			return value, true
		}
		if value := strings.TrimSpace(suggestion.OriginalDocument.CreatedDate); value != "" {
			return value, true
		}
	case paperlessFieldRefDocumentType:
		if value := strings.TrimSpace(suggestion.SuggestedDocumentType); value != "" {
			return value, true
		}
		if value := strings.TrimSpace(suggestion.OriginalDocument.DocumentTypeName); value != "" {
			return value, true
		}
	case paperlessFieldRefOriginalFileName:
		if value := strings.TrimSpace(suggestion.OriginalDocument.OriginalFileName); value != "" {
			return value, true
		}
	case paperlessFieldRefArchivedFileName:
		if value := strings.TrimSpace(suggestion.OriginalDocument.ArchivedFileName); value != "" {
			return value, true
		}
	default:
		if !strings.HasPrefix(strings.TrimSpace(fieldRef), paperlessFieldRefCustomPrefix) {
			return nil, false
		}
		fieldID, err := strconv.Atoi(strings.TrimPrefix(strings.TrimSpace(fieldRef), paperlessFieldRefCustomPrefix))
		if err != nil || fieldID <= 0 {
			return nil, false
		}
		for _, field := range suggestion.SuggestedCustomFields {
			if field.ID == fieldID && field.Value != nil {
				return field.Value, true
			}
		}
		for _, field := range suggestion.OriginalDocument.CustomFields {
			if field.Field == fieldID && field.Value != nil {
				return field.Value, true
			}
		}
	}

	return nil, false
}

func stringifyExpenseFieldValue(value interface{}) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(v)
	case fmt.Stringer:
		return strings.TrimSpace(v.String())
	case float64:
		return strings.TrimSpace(strconv.FormatFloat(v, 'f', -1, 64))
	case float32:
		return strings.TrimSpace(strconv.FormatFloat(float64(v), 'f', -1, 32))
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case int32:
		return strconv.FormatInt(int64(v), 10)
	case uint:
		return strconv.FormatUint(uint64(v), 10)
	case uint64:
		return strconv.FormatUint(v, 10)
	case uint32:
		return strconv.FormatUint(uint64(v), 10)
	default:
		return strings.TrimSpace(fmt.Sprint(value))
	}
}

func parseNumericValue(value interface{}) (float64, bool) {
	switch v := value.(type) {
	case float64:
		if !math.IsNaN(v) && !math.IsInf(v, 0) {
			return v, true
		}
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		if parsed, err := v.Float64(); err == nil {
			return parsed, true
		}
	case string:
		cleaned := strings.TrimSpace(v)
		cleaned = strings.ReplaceAll(cleaned, ",", "")
		cleaned = strings.TrimPrefix(cleaned, "$")
		if parsed, err := strconv.ParseFloat(cleaned, 64); err == nil {
			return parsed, true
		}
	}
	return 0, false
}

// jobberMatchNearWindowDays is how far outside a job's [start, end] window the
// document date can fall and still be considered a match (with a reduced
// score). Materials are often bought the day before a visit and receipts can
// be written up the day after, so a small one-week buffer captures the common
// real-world cases without being so wide that it creates constant false
// positives.
const jobberMatchNearWindowDays = 7

// jobberLongRunningThresholdDays caps how many days a job's [start, end]
// window can span before we consider it "long-running" and slightly
// de-prioritize it relative to a tighter-window job that also matches. A
// year-long recurring job whose window happens to contain the doc date is a
// weaker signal than a single-day visit on the same date.
const jobberLongRunningThresholdDays = 365

const (
	jobberScoreInWindow      = 100
	jobberScoreInLongWindow  = 80
	jobberScorePerDayPenalty = 5
)

// jobberRankResult bundles the ranked list with the per-document candidate ID
// to auto-select, when one stands out. AutoSelectedID is empty when no
// candidate is unambiguously best (no candidate is in-window, or several are
// tied for the top score).
type jobberRankResult struct {
	Candidates     []JobberMatchCandidate
	AutoSelectedID string
}

// parseJobberDate parses an ISO-8601 timestamp from Jobber and returns the
// calendar day in UTC. Jobber returns full timestamps for date fields
// (e.g. "2024-03-12T13:00:00Z"); for matching purposes we only care about the
// day, so we truncate the time component.
func parseJobberDate(value string) (time.Time, bool) {
	if value == "" {
		return time.Time{}, false
	}
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC), true
	}
	if t, err := time.Parse("2006-01-02", value); err == nil {
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC), true
	}
	return time.Time{}, false
}

// jobberCandidateWindow returns the effective [start, end] window for a
// candidate using Jobber's available date fields, falling back forward when
// some are missing. Returns ok=false when no usable date is available.
func jobberCandidateWindow(c JobberMatchCandidate) (start, end time.Time, ok bool) {
	startParsed, hasStart := parseJobberDate(c.StartAt)
	endParsed, hasEnd := parseJobberDate(c.EndAt)
	completedParsed, hasCompleted := parseJobberDate(c.CompletedAt)

	switch {
	case hasStart && hasEnd:
		return startParsed, endParsed, true
	case hasStart && hasCompleted:
		return startParsed, completedParsed, true
	case hasStart:
		return startParsed, startParsed, true
	case hasEnd:
		return endParsed, endParsed, true
	case hasCompleted:
		return completedParsed, completedParsed, true
	}
	return time.Time{}, time.Time{}, false
}

// scoreJobberCandidate rates a single candidate against a parsed document
// date. The returned score follows the constants documented at the top of
// this section (in-window > long-running in-window > near-window decaying by
// distance > 0). reason is a short human-readable label explaining the
// score and is surfaced in the UI when set.
func scoreJobberCandidate(docDate time.Time, c JobberMatchCandidate) (int, string) {
	start, end, ok := jobberCandidateWindow(c)
	if !ok {
		return 0, ""
	}

	windowDays := int(end.Sub(start).Hours()/24) + 1
	if windowDays < 1 {
		windowDays = 1
	}

	if !docDate.Before(start) && !docDate.After(end) {
		if windowDays > jobberLongRunningThresholdDays {
			return jobberScoreInLongWindow, fmt.Sprintf("Document date is within this job's window (%s – %s)", start.Format("2006-01-02"), end.Format("2006-01-02"))
		}
		return jobberScoreInWindow, fmt.Sprintf("Document date is within this job's window (%s – %s)", start.Format("2006-01-02"), end.Format("2006-01-02"))
	}

	var distance int
	if docDate.Before(start) {
		distance = int(start.Sub(docDate).Hours() / 24)
	} else {
		distance = int(docDate.Sub(end).Hours() / 24)
	}
	if distance <= jobberMatchNearWindowDays {
		score := jobberScoreInWindow - distance*jobberScorePerDayPenalty
		if score < 1 {
			score = 1
		}
		return score, fmt.Sprintf("Document date is %d day(s) outside this job's window (%s – %s)", distance, start.Format("2006-01-02"), end.Format("2006-01-02"))
	}
	return 0, ""
}

// rankJobberCandidates orders candidates by relevance to the supplied
// document, using only the document's date and each Jobber job's date fields.
// Candidates without dates fall to the bottom and are ordered by job
// recency.
//
// This deliberately avoids substring matching against OCR'd document text:
// receipts vary wildly in how (and whether) they reference jobs by number or
// client, and free-text matches were prone to false positives. The document's
// date, by contrast, is structured and almost always present.
func rankJobberCandidates(document Document, candidates []JobberMatchCandidate) []JobberMatchCandidate {
	result, _ := rankJobberCandidatesWithSelection(document, "", candidates)
	return result.Candidates
}

// rankJobberCandidatesWithSelection ranks candidates and additionally returns
// the ID of the candidate to auto-select, when one is unambiguously best.
//
// preferredDocDate, when non-empty, takes precedence over document.CreatedDate
// — it lets callers feed in the LLM-suggested date that the user is about to
// approve, instead of the (often wrong) date stored in Paperless.
func rankJobberCandidatesWithSelection(document Document, preferredDocDate string, candidates []JobberMatchCandidate) (jobberRankResult, bool) {
	type scored struct {
		candidate  JobberMatchCandidate
		score      int
		reason     string
		recencyKey time.Time
	}

	docDateStr := strings.TrimSpace(preferredDocDate)
	if docDateStr == "" {
		docDateStr = strings.TrimSpace(document.CreatedDate)
	}
	docDate, hasDocDate := parseJobberDate(docDateStr)

	scoredCandidates := make([]scored, 0, len(candidates))
	for _, candidate := range candidates {
		var score int
		var reason string
		if hasDocDate {
			score, reason = scoreJobberCandidate(docDate, candidate)
		}
		recencyKey, _ := parseJobberDate(candidate.StartAt)
		if recencyKey.IsZero() {
			recencyKey, _ = parseJobberDate(candidate.CreatedAt)
		}

		c := candidate
		c.MatchReason = reason
		scoredCandidates = append(scoredCandidates, scored{
			candidate:  c,
			score:      score,
			reason:     reason,
			recencyKey: recencyKey,
		})
	}

	sort.SliceStable(scoredCandidates, func(i, j int) bool {
		if scoredCandidates[i].score != scoredCandidates[j].score {
			return scoredCandidates[i].score > scoredCandidates[j].score
		}
		// Secondary: more recent first (later date wins).
		return scoredCandidates[i].recencyKey.After(scoredCandidates[j].recencyKey)
	})

	result := make([]JobberMatchCandidate, 0, len(scoredCandidates))
	for _, item := range scoredCandidates {
		result = append(result, item.candidate)
	}

	autoID := ""
	// Only auto-select when there's a clear winner: the top candidate must be
	// a strong match (in-window) and strictly better than the runner-up.
	// Otherwise we'd be picking arbitrarily from a tie.
	if len(scoredCandidates) >= 1 && scoredCandidates[0].score >= jobberScoreInLongWindow {
		if len(scoredCandidates) == 1 || scoredCandidates[0].score > scoredCandidates[1].score {
			autoID = scoredCandidates[0].candidate.ID
		}
	}

	return jobberRankResult{Candidates: result, AutoSelectedID: autoID}, hasDocDate
}

func executeJSONGraphQL(ctx context.Context, endpoint, accessToken, query string, variables map[string]interface{}, target interface{}) error {
	payload := map[string]interface{}{
		"query": query,
	}
	if variables != nil {
		payload["variables"] = variables
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-JOBBER-GRAPHQL-VERSION", jobberGraphQLVersion)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	bodyBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("graphql request failed: %d, %s", resp.StatusCode, string(bodyBytes))
	}
	return json.Unmarshal(bodyBytes, target)
}

type oauthProviderBase struct {
	name         string
	clientID     string
	clientSecret string
	authURL      string
	tokenURL     string
	scopes       []string
}

func (b oauthProviderBase) Name() string {
	return b.name
}

func (b oauthProviderBase) Configured() (bool, string) {
	if strings.TrimSpace(b.clientID) == "" || strings.TrimSpace(b.clientSecret) == "" {
		return false, "provider is not configured on server"
	}
	return true, ""
}

func (b oauthProviderBase) oauthConfig(c *gin.Context) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     b.clientID,
		ClientSecret: b.clientSecret,
		RedirectURL:  oauthCallbackURL(c, b.name),
		Scopes:       b.scopes,
		Endpoint: oauth2.Endpoint{
			AuthURL:  b.authURL,
			TokenURL: b.tokenURL,
		},
	}
}

func (b oauthProviderBase) AuthorizationURL(c *gin.Context, state string) (string, error) {
	configured, reason := b.Configured()
	if !configured {
		return "", providerNotConfiguredError{Reason: reason}
	}
	return b.oauthConfig(c).AuthCodeURL(state, oauth2.AccessTypeOffline), nil
}

func (b oauthProviderBase) ExchangeCode(ctx context.Context, c *gin.Context, code string) (*providerToken, error) {
	token, err := b.oauthConfig(c).Exchange(ctx, code)
	if err != nil {
		return nil, err
	}
	return providerTokenFromOAuthToken(token), nil
}

func (b oauthProviderBase) RefreshToken(ctx context.Context, conn *IntegrationConnection) (*providerToken, error) {
	if strings.TrimSpace(conn.RefreshToken) == "" {
		return nil, fmt.Errorf("refresh token not available")
	}
	config := &oauth2.Config{
		ClientID:     b.clientID,
		ClientSecret: b.clientSecret,
		Endpoint: oauth2.Endpoint{
			AuthURL:  b.authURL,
			TokenURL: b.tokenURL,
		},
	}
	tokenSource := config.TokenSource(ctx, &oauth2.Token{
		RefreshToken: conn.RefreshToken,
	})
	token, err := tokenSource.Token()
	if err != nil {
		return nil, err
	}
	return providerTokenFromOAuthToken(token), nil
}

func providerTokenFromOAuthToken(token *oauth2.Token) *providerToken {
	var expiresAt *time.Time
	if !token.Expiry.IsZero() {
		exp := token.Expiry
		expiresAt = &exp
	}
	return &providerToken{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		ExpiresAt:    expiresAt,
	}
}

type jobberProvider struct {
	oauthProviderBase
}

func newJobberProvider() jobberProvider {
	return jobberProvider{
		oauthProviderBase: oauthProviderBase{
			name:         integrationProviderJobber,
			clientID:     strings.TrimSpace(os.Getenv("JOBBER_CLIENT_ID")),
			clientSecret: strings.TrimSpace(os.Getenv("JOBBER_CLIENT_SECRET")),
			authURL:      "https://api.getjobber.com/api/oauth/authorize",
			tokenURL:     "https://api.getjobber.com/api/oauth/token",
			scopes: []string{
				"read_clients",
				"read_jobs",
				"write_expenses",
			},
		},
	}
}

func (p jobberProvider) FetchIdentity(ctx context.Context, conn *IntegrationConnection) (*providerIdentity, error) {
	if conn == nil {
		return nil, fmt.Errorf("jobber connection not found")
	}
	var response struct {
		Data struct {
			Account struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"account"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := executeJSONGraphQL(ctx, "https://api.getjobber.com/api/graphql", conn.AccessToken, `query AccountIdentity { account { id name } }`, nil, &response); err != nil {
		return nil, err
	}
	if len(response.Errors) > 0 {
		return nil, fmt.Errorf("jobber identity query error: %s", response.Errors[0].Message)
	}
	return &providerIdentity{
		AccountID:   response.Data.Account.ID,
		AccountName: response.Data.Account.Name,
	}, nil
}

// jobberTokenRefreshSkew is how close to expiry we consider the token "about to
// expire" and proactively refresh.  Jobber access tokens live for 60 minutes;
// a generous skew keeps us from ever sending a request with a few-seconds-
// left-to-live token that Jobber considers expired by the time it arrives.
const jobberTokenRefreshSkew = 60 * time.Second

func (p jobberProvider) ensureFreshToken(ctx context.Context, db *gorm.DB, conn *IntegrationConnection) (*IntegrationConnection, error) {
	if conn == nil {
		return nil, fmt.Errorf("jobber connection not found")
	}
	// If we know the expiry and we have comfortable runway, reuse the token.
	// Previously this early-returned even when AccessTokenExpiresAt was nil,
	// which meant a token without a recorded expiry would never refresh and
	// would eventually 401 forever.
	if conn.AccessTokenExpiresAt != nil && conn.AccessTokenExpiresAt.After(time.Now().Add(jobberTokenRefreshSkew)) {
		return conn, nil
	}
	return p.forceRefreshToken(ctx, db, conn)
}

// forceRefreshToken runs the OAuth refresh_token exchange unconditionally and
// persists the new tokens.  Use it from the reactive retry path when a live
// request has just returned 401 despite ensureFreshToken having approved the
// cached token.
func (p jobberProvider) forceRefreshToken(ctx context.Context, db *gorm.DB, conn *IntegrationConnection) (*IntegrationConnection, error) {
	if conn == nil {
		return nil, fmt.Errorf("jobber connection not found")
	}
	if strings.TrimSpace(conn.RefreshToken) == "" {
		return nil, fmt.Errorf("jobber refresh token not available; please reconnect Jobber")
	}
	token, err := p.RefreshToken(ctx, conn)
	if err != nil {
		return nil, fmt.Errorf("jobber token refresh failed: %w", err)
	}
	updated, err := upsertIntegrationConnection(db, integrationProviderJobber, token, &providerIdentity{
		AccountID:   conn.AccountID,
		AccountName: conn.AccountName,
		Metadata:    metadataMap(conn),
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

type googleDriveProvider struct {
	oauthProviderBase
}

func newGoogleDriveProvider() googleDriveProvider {
	return googleDriveProvider{
		oauthProviderBase: oauthProviderBase{
			name:         integrationProviderGoogleDrive,
			clientID:     strings.TrimSpace(os.Getenv("GOOGLE_DRIVE_CLIENT_ID")),
			clientSecret: strings.TrimSpace(os.Getenv("GOOGLE_DRIVE_CLIENT_SECRET")),
			authURL:      "https://accounts.google.com/o/oauth2/auth",
			tokenURL:     "https://oauth2.googleapis.com/token",
			scopes: []string{
				"https://www.googleapis.com/auth/drive.file",
				"openid",
				"profile",
				"email",
			},
		},
	}
}

func (p googleDriveProvider) FetchIdentity(ctx context.Context, conn *IntegrationConnection) (*providerIdentity, error) {
	if conn == nil {
		return nil, fmt.Errorf("google drive connection not found")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://www.googleapis.com/oauth2/v2/userinfo", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+conn.AccessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	bodyBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("google userinfo failed: %d, %s", resp.StatusCode, string(bodyBytes))
	}
	var payload struct {
		ID    string `json:"id"`
		Email string `json:"email"`
		Name  string `json:"name"`
	}
	if err := json.Unmarshal(bodyBytes, &payload); err != nil {
		return nil, err
	}
	accountName := strings.TrimSpace(payload.Email)
	if accountName == "" {
		accountName = strings.TrimSpace(payload.Name)
	}
	return &providerIdentity{
		AccountID:   payload.ID,
		AccountName: accountName,
		Metadata: map[string]string{
			"name": payload.Name,
		},
	}, nil
}

func (p googleDriveProvider) ensureFreshToken(ctx context.Context, db *gorm.DB, conn *IntegrationConnection) (*IntegrationConnection, error) {
	if conn == nil {
		return nil, fmt.Errorf("google drive connection not found")
	}
	// Mirror the Jobber fix: if expiry is nil we still refresh. A missing
	// expiry should be treated as "unknown, play it safe" not "valid forever".
	if conn.AccessTokenExpiresAt != nil && conn.AccessTokenExpiresAt.After(time.Now().Add(jobberTokenRefreshSkew)) {
		return conn, nil
	}
	if strings.TrimSpace(conn.RefreshToken) == "" {
		return nil, fmt.Errorf("google drive refresh token not available; please reconnect Google Drive")
	}
	token, err := p.RefreshToken(ctx, conn)
	if err != nil {
		return nil, fmt.Errorf("google drive token refresh failed: %w", err)
	}
	updated, err := upsertIntegrationConnection(db, integrationProviderGoogleDrive, token, &providerIdentity{
		AccountID:   conn.AccountID,
		AccountName: conn.AccountName,
		Metadata:    metadataMap(conn),
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

type quickBooksProvider struct {
	oauthProviderBase
}

func newQuickBooksProvider() quickBooksProvider {
	return quickBooksProvider{
		oauthProviderBase: oauthProviderBase{
			name:         integrationProviderQuickBooks,
			clientID:     strings.TrimSpace(os.Getenv("QUICKBOOKS_CLIENT_ID")),
			clientSecret: strings.TrimSpace(os.Getenv("QUICKBOOKS_CLIENT_SECRET")),
			authURL:      "https://appcenter.intuit.com/connect/oauth2",
			tokenURL:     "https://oauth.platform.intuit.com/oauth2/v1/tokens/bearer",
			scopes: []string{
				"com.intuit.quickbooks.accounting",
			},
		},
	}
}

func (p quickBooksProvider) FetchIdentity(ctx context.Context, conn *IntegrationConnection) (*providerIdentity, error) {
	metadata := metadataMap(conn)
	accountID := metadata["realm_id"]
	accountName := strings.TrimSpace(conn.AccountName)
	if accountName == "" {
		accountName = "QuickBooks company"
	}
	return &providerIdentity{
		AccountID:   accountID,
		AccountName: accountName,
		Metadata:    metadata,
	}, nil
}

func (p quickBooksProvider) ExchangeCode(ctx context.Context, c *gin.Context, code string) (*providerToken, error) {
	token, err := p.oauthProviderBase.ExchangeCode(ctx, c, code)
	if err != nil {
		return nil, err
	}
	if realmID := c.Query("realmId"); realmID != "" {
		token.Scopes = append(token.Scopes, "realm:"+realmID)
	}
	return token, nil
}

func (p quickBooksProvider) RefreshToken(ctx context.Context, conn *IntegrationConnection) (*providerToken, error) {
	return p.oauthProviderBase.RefreshToken(ctx, conn)
}

func init() {
	_ = url.QueryEscape
}
