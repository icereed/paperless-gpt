package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
)

const (
	fireflyExternalIDPrefix = "paperless-gpt-document-"
	fireflyDateWindowDays   = 7

	quickBooksEnvironmentProduction = "production"
	quickBooksEnvironmentSandbox    = "sandbox"

	quickBooksProductionAPIBaseURL = "https://quickbooks.api.intuit.com"
	quickBooksSandboxAPIBaseURL    = "https://sandbox-quickbooks.api.intuit.com"
)

type FireflyConfig struct {
	Enabled                    bool
	InstanceURL                string
	Token                      string
	DefaultSourceAccount       string
	DefaultDestinationAccount  string
	DefaultCurrency            string
	DefaultCategory            string
	DefaultBudget              string
	NotesTemplate              string
	DescriptionFieldRef        string
	DateFieldRef               string
	AmountFieldRef             string
	CurrencyFieldRef           string
	CategoryFieldRef           string
	BudgetFieldRef             string
	NotesFieldRef              string
	ExternalRefFieldRef        string
	SourceAccountFieldRef      string
	DestinationAccountFieldRef string
}

type fireflyDerivedTransaction struct {
	Description        string
	Date               string
	Amount             float64
	AmountString       string
	CurrencyCode       string
	Category           string
	Budget             string
	Notes              string
	ExternalReference  string
	SourceAccount      string
	DestinationAccount string
}

type FireflyApplyResult struct {
	Matched            bool
	Created            bool
	AttachmentUploaded bool
	TransactionID      string
	URL                string
}

type QuickBooksUploadResult struct {
	AttachableID string
	URL          string
}

func (s *IntegrationsService) FireflyStatus(ctx context.Context) IntegrationConnectionStatus {
	cfg, configured, reason := fireflyConfigFromSettings()
	status := IntegrationConnectionStatus{
		Provider:   integrationProviderFirefly,
		Configured: configured,
		Reason:     reason,
	}
	if !configured || !cfg.Enabled {
		if configured && !cfg.Enabled {
			status.Reason = "Firefly is disabled in settings"
		}
		return status
	}
	about, err := fireflyGET(ctx, cfg, "/api/v1/about")
	if err != nil {
		status.Reason = err.Error()
		return status
	}
	status.Connected = true
	status.AccountName = "Firefly III"
	var payload struct {
		Data struct {
			Version string `json:"version"`
		} `json:"data"`
	}
	if err := json.Unmarshal(about, &payload); err == nil && strings.TrimSpace(payload.Data.Version) != "" {
		status.AccountName = "Firefly III " + strings.TrimSpace(payload.Data.Version)
	}
	return status
}

func fireflyConfigFromSettings() (FireflyConfig, bool, string) {
	settingsMutex.RLock()
	defer settingsMutex.RUnlock()
	cfg := FireflyConfig{
		Enabled:                    settings.FireflyEnabled,
		InstanceURL:                strings.TrimRight(strings.TrimSpace(settings.FireflyInstanceURL), "/"),
		DefaultSourceAccount:       strings.TrimSpace(settings.FireflyDefaultSourceAccount),
		DefaultDestinationAccount:  strings.TrimSpace(settings.FireflyDefaultDestinationAccount),
		DefaultCurrency:            strings.TrimSpace(settings.FireflyDefaultCurrency),
		DefaultCategory:            strings.TrimSpace(settings.FireflyDefaultCategory),
		DefaultBudget:              strings.TrimSpace(settings.FireflyDefaultBudget),
		NotesTemplate:              strings.TrimSpace(settings.FireflyNotesTemplate),
		DescriptionFieldRef:        strings.TrimSpace(settings.FireflyDescriptionFieldRef),
		DateFieldRef:               strings.TrimSpace(settings.FireflyDateFieldRef),
		AmountFieldRef:             strings.TrimSpace(settings.FireflyAmountFieldRef),
		CurrencyFieldRef:           strings.TrimSpace(settings.FireflyCurrencyFieldRef),
		CategoryFieldRef:           strings.TrimSpace(settings.FireflyCategoryFieldRef),
		BudgetFieldRef:             strings.TrimSpace(settings.FireflyBudgetFieldRef),
		NotesFieldRef:              strings.TrimSpace(settings.FireflyNotesFieldRef),
		ExternalRefFieldRef:        strings.TrimSpace(settings.FireflyExternalRefFieldRef),
		SourceAccountFieldRef:      strings.TrimSpace(settings.FireflySourceAccountFieldRef),
		DestinationAccountFieldRef: strings.TrimSpace(settings.FireflyDestinationAccountFieldRef),
	}
	if settings.FireflyAPIToken != "" {
		token, err := DecryptSecret(settings.FireflyAPIToken)
		if err != nil {
			return cfg, false, "Firefly token could not be decrypted"
		}
		cfg.Token = token
	}
	switch {
	case cfg.InstanceURL == "":
		return cfg, false, "Firefly instance URL is required"
	case cfg.Token == "":
		return cfg, false, "Firefly API token is required"
	default:
		return cfg, true, ""
	}
}

func sanitizeSettingsForResponse(s Settings) Settings {
	if s.FireflyAPIToken != "" {
		s.FireflyAPIToken = ""
		s.FireflyAPITokenConfigured = true
	}
	if s.JobberClientSecret != "" {
		s.JobberClientSecret = ""
		s.JobberClientSecretConfigured = true
	}
	if s.GoogleDriveClientSecret != "" {
		s.GoogleDriveClientSecret = ""
		s.GoogleDriveClientSecretConfigured = true
	}
	if s.QuickBooksClientSecret != "" {
		s.QuickBooksClientSecret = ""
		s.QuickBooksClientSecretConfigured = true
	}
	s.PaperlessWebhookSecret = ""
	return s
}

func mergeSecretSettings(current Settings, merged *Settings) error {
	secrets := []struct {
		current *string
		merged  *string
	}{
		{current: &current.FireflyAPIToken, merged: &merged.FireflyAPIToken},
		{current: &current.JobberClientSecret, merged: &merged.JobberClientSecret},
		{current: &current.GoogleDriveClientSecret, merged: &merged.GoogleDriveClientSecret},
		{current: &current.QuickBooksClientSecret, merged: &merged.QuickBooksClientSecret},
	}

	for _, secret := range secrets {
		if strings.TrimSpace(*secret.merged) == "" {
			*secret.merged = *secret.current
			continue
		}
		if IsEncryptedSecret(*secret.merged) {
			continue
		}
		encrypted, err := EncryptSecret(strings.TrimSpace(*secret.merged))
		if err != nil {
			return err
		}
		*secret.merged = encrypted
	}
	return nil
}

func fireflyGET(ctx context.Context, cfg FireflyConfig, apiPath string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.InstanceURL+apiPath, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Token)
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("firefly request failed: %d, %s", resp.StatusCode, string(body))
	}
	return body, nil
}

func fireflyJSON(ctx context.Context, cfg FireflyConfig, method, apiPath string, payload interface{}) ([]byte, error) {
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, cfg.InstanceURL+apiPath, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Token)
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("firefly request failed: %d, %s", resp.StatusCode, string(raw))
	}
	return raw, nil
}

func (s *IntegrationsService) FetchFireflyTransactionCandidates(ctx context.Context, suggestion DocumentSuggestion) ([]FireflyTransactionCandidate, string, error) {
	cfg, configured, reason := fireflyConfigFromSettings()
	if !configured || !cfg.Enabled {
		if reason == "" {
			reason = "firefly is disabled"
		}
		return nil, "", fmt.Errorf("%s", reason)
	}
	derived, err := deriveFireflyTransaction(suggestion, cfg)
	if err != nil {
		return nil, "", err
	}
	if derived.Amount <= 0 {
		return nil, "", fmt.Errorf("Firefly requires an amount; map an amount field or add a suggested custom field containing total, amount, or price")
	}

	candidates, err := s.searchFireflyTransactions(ctx, cfg, derived)
	if err != nil {
		return nil, "", err
	}
	ranked, auto := rankFireflyCandidates(derived, suggestion, candidates)
	return ranked, auto, nil
}

func (s *IntegrationsService) searchFireflyTransactions(ctx context.Context, cfg FireflyConfig, derived fireflyDerivedTransaction) ([]FireflyTransactionCandidate, error) {
	date, ok := parseFireflyDate(derived.Date)
	if !ok {
		date = time.Now()
	}
	start := date.AddDate(0, 0, -fireflyDateWindowDays).Format("2006-01-02")
	end := date.AddDate(0, 0, fireflyDateWindowDays).Format("2006-01-02")
	q := url.Values{}
	q.Set("start", start)
	q.Set("end", end)
	q.Set("type", "withdrawal")
	body, err := fireflyGET(ctx, cfg, "/api/v1/transactions?"+q.Encode())
	if err != nil {
		return nil, err
	}
	var payload struct {
		Data []struct {
			ID         string `json:"id"`
			Attributes struct {
				Transactions []struct {
					Description     string `json:"description"`
					Date            string `json:"date"`
					Amount          string `json:"amount"`
					CurrencyCode    string `json:"currency_code"`
					SourceName      string `json:"source_name"`
					DestinationName string `json:"destination_name"`
					CategoryName    string `json:"category_name"`
					BudgetName      string `json:"budget_name"`
				} `json:"transactions"`
			} `json:"attributes"`
			Links struct {
				Self string `json:"self"`
			} `json:"links"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	var candidates []FireflyTransactionCandidate
	for _, group := range payload.Data {
		for _, tx := range group.Attributes.Transactions {
			candidates = append(candidates, FireflyTransactionCandidate{
				ID:              group.ID,
				Description:     tx.Description,
				Date:            dateOnly(tx.Date),
				Amount:          tx.Amount,
				CurrencyCode:    tx.CurrencyCode,
				SourceName:      tx.SourceName,
				DestinationName: tx.DestinationName,
				Category:        tx.CategoryName,
				Budget:          tx.BudgetName,
				URL:             fireflyTransactionURL(cfg.InstanceURL, group.ID, group.Links.Self),
			})
		}
	}
	return candidates, nil
}

func rankFireflyCandidates(derived fireflyDerivedTransaction, suggestion DocumentSuggestion, candidates []FireflyTransactionCandidate) ([]FireflyTransactionCandidate, string) {
	type scored struct {
		candidate FireflyTransactionCandidate
		score     int
	}
	docDate, hasDate := parseFireflyDate(derived.Date)
	text := strings.ToLower(strings.Join([]string{
		derived.Description,
		suggestion.OriginalDocument.Title,
		suggestion.SuggestedTitle,
		suggestion.OriginalDocument.Correspondent,
		suggestion.SuggestedCorrespondent,
	}, " "))
	scoredCandidates := make([]scored, 0, len(candidates))
	for _, candidate := range candidates {
		score := 0
		reasons := []string{}
		candidateAmount, amountOK := parseNumericValue(candidate.Amount)
		if amountOK && amountsEqual(candidateAmount, derived.Amount) {
			score += 70
			reasons = append(reasons, "same amount")
		}
		if strings.EqualFold(strings.TrimSpace(candidate.CurrencyCode), strings.TrimSpace(derived.CurrencyCode)) && strings.TrimSpace(derived.CurrencyCode) != "" {
			score += 15
			reasons = append(reasons, "same currency")
		}
		if candidateDate, ok := parseFireflyDate(candidate.Date); ok && hasDate {
			diff := absDays(candidateDate.Sub(docDate))
			if diff == 0 {
				score += 45
				reasons = append(reasons, "same date")
			} else if diff <= fireflyDateWindowDays {
				score += 20 - diff
				reasons = append(reasons, fmt.Sprintf("within %d day(s)", diff))
			}
		}
		desc := strings.ToLower(strings.TrimSpace(candidate.Description))
		if desc != "" && (strings.Contains(text, desc) || strings.Contains(desc, strings.ToLower(strings.TrimSpace(derived.Description)))) {
			score += 10
			reasons = append(reasons, "description overlap")
		}
		c := candidate
		if len(reasons) > 0 {
			c.MatchReason = "Matched on " + strings.Join(reasons, ", ")
		}
		scoredCandidates = append(scoredCandidates, scored{candidate: c, score: score})
	}
	sort.SliceStable(scoredCandidates, func(i, j int) bool {
		return scoredCandidates[i].score > scoredCandidates[j].score
	})
	ranked := make([]FireflyTransactionCandidate, 0, len(scoredCandidates))
	for _, item := range scoredCandidates {
		if item.score > 0 {
			ranked = append(ranked, item.candidate)
		}
	}
	auto := ""
	if len(scoredCandidates) > 0 && scoredCandidates[0].score >= 120 {
		if len(scoredCandidates) == 1 || scoredCandidates[0].score > scoredCandidates[1].score {
			auto = scoredCandidates[0].candidate.ID
		}
	}
	return ranked, auto
}

func deriveFireflyTransaction(suggestion DocumentSuggestion, cfg FireflyConfig) (fireflyDerivedTransaction, error) {
	description := resolveMappedString(suggestion, cfg.DescriptionFieldRef)
	if description == "" {
		description = strings.TrimSpace(suggestion.SuggestedTitle)
	}
	if description == "" {
		description = strings.TrimSpace(suggestion.OriginalDocument.Title)
	}
	if description == "" {
		description = fmt.Sprintf("Paperless document %d", suggestion.ID)
	}
	dateValue := resolveMappedString(suggestion, cfg.DateFieldRef)
	if dateValue == "" {
		dateValue = strings.TrimSpace(suggestion.SuggestedCreatedDate)
	}
	if dateValue == "" {
		dateValue = strings.TrimSpace(suggestion.OriginalDocument.CreatedDate)
	}
	if parsed, ok := parseFireflyDate(dateValue); ok {
		dateValue = parsed.Format("2006-01-02")
	}
	amount, amountString, hasAmount := deriveFireflyAmount(suggestion, cfg.AmountFieldRef)
	currency := resolveMappedString(suggestion, cfg.CurrencyFieldRef)
	if currency == "" {
		currency = cfg.DefaultCurrency
	}
	if currency == "" {
		currency = "USD"
	}
	category := firstNonEmpty(resolveMappedString(suggestion, cfg.CategoryFieldRef), cfg.DefaultCategory)
	budget := firstNonEmpty(resolveMappedString(suggestion, cfg.BudgetFieldRef), cfg.DefaultBudget)
	notes := firstNonEmpty(resolveMappedString(suggestion, cfg.NotesFieldRef), cfg.NotesTemplate)
	marker := fireflyExternalIDPrefix + strconv.Itoa(suggestion.ID)
	if notes == "" {
		notes = marker
	} else if !strings.Contains(notes, marker) {
		notes = notes + "\n" + marker
	}
	externalRef := firstNonEmpty(resolveMappedString(suggestion, cfg.ExternalRefFieldRef), marker)
	return fireflyDerivedTransaction{
			Description:        description,
			Date:               dateValue,
			Amount:             amount,
			AmountString:       amountString,
			CurrencyCode:       strings.ToUpper(currency),
			Category:           category,
			Budget:             budget,
			Notes:              notes,
			ExternalReference:  externalRef,
			SourceAccount:      firstNonEmpty(resolveMappedString(suggestion, cfg.SourceAccountFieldRef), cfg.DefaultSourceAccount),
			DestinationAccount: firstNonEmpty(resolveMappedString(suggestion, cfg.DestinationAccountFieldRef), cfg.DefaultDestinationAccount),
		}, func() error {
			if !hasAmount {
				return fmt.Errorf("Firefly requires an amount; map an amount field or add a suggested custom field containing total, amount, or price")
			}
			if dateValue == "" {
				return fmt.Errorf("Firefly requires a transaction date")
			}
			return nil
		}()
}

func deriveFireflyAmount(suggestion DocumentSuggestion, fieldRef string) (float64, string, bool) {
	if value, ok := resolveMappedFieldValue(suggestion, fieldRef); ok {
		if parsed, ok := parseNumericValue(value); ok {
			return parsed, strconv.FormatFloat(parsed, 'f', 2, 64), true
		}
	}
	for _, field := range suggestion.SuggestedCustomFields {
		name := strings.ToLower(strings.TrimSpace(field.Name))
		if strings.Contains(name, "total") || strings.Contains(name, "amount") || strings.Contains(name, "price") {
			if parsed, ok := parseNumericValue(field.Value); ok {
				return parsed, strconv.FormatFloat(parsed, 'f', 2, 64), true
			}
		}
	}
	return 0, "", false
}

func (s *IntegrationsService) ApplyFirefly(ctx context.Context, client ClientInterface, suggestion DocumentSuggestion, batchID ...uint) (*FireflyApplyResult, error) {
	if !suggestion.ApplyFirefly {
		return nil, nil
	}
	cfg, configured, reason := fireflyConfigFromSettings()
	if !configured || !cfg.Enabled {
		if reason == "" {
			reason = "firefly is disabled"
		}
		return nil, fmt.Errorf("%s", reason)
	}
	var appliedBatchID *uint
	if len(batchID) > 0 && batchID[0] > 0 {
		appliedBatchID = &batchID[0]
	}
	selectedID := strings.TrimSpace(suggestion.SelectedFireflyTransactionID)
	if selectedID == "" && !suggestion.CreateFireflyTransaction {
		return nil, nil
	}
	derived, err := deriveFireflyTransaction(suggestion, cfg)
	if err != nil {
		return nil, err
	}
	result := &FireflyApplyResult{}
	transactionID := selectedID
	if transactionID != "" {
		result.Matched = true
		insertIntegrationActionLog(s.DB.WithContext(ctx), &IntegrationActionLog{
			DocumentID:      suggestion.ID,
			BatchID:         appliedBatchID,
			Provider:        integrationProviderFirefly,
			ActionType:      "transaction_match",
			Status:          "success",
			ExternalID:      transactionID,
			ExternalURL:     fireflyTransactionURL(cfg.InstanceURL, transactionID, ""),
			RequestSummary:  "user selected existing transaction",
			ResponseSummary: transactionID,
		})
	} else {
		dupes, _, err := s.FetchFireflyTransactionCandidates(ctx, suggestion)
		if err == nil && len(dupes) > 0 {
			return nil, fmt.Errorf("possible Firefly duplicate found; select the existing transaction instead of creating a new one")
		}
		transactionID, err = s.createFireflyTransaction(ctx, cfg, suggestion.ID, derived, appliedBatchID)
		if err != nil {
			return nil, err
		}
		result.Created = true
	}
	result.TransactionID = transactionID
	result.URL = fireflyTransactionURL(cfg.InstanceURL, transactionID, "")
	if err := s.attachFireflyPDF(ctx, cfg, client, suggestion.ID, transactionID, appliedBatchID); err != nil {
		return result, err
	}
	result.AttachmentUploaded = true
	return result, nil
}

func (s *IntegrationsService) createFireflyTransaction(ctx context.Context, cfg FireflyConfig, documentID int, tx fireflyDerivedTransaction, batchID *uint) (string, error) {
	payloadTx := map[string]interface{}{
		"type":             "withdrawal",
		"description":      tx.Description,
		"date":             tx.Date,
		"amount":           tx.AmountString,
		"currency_code":    tx.CurrencyCode,
		"external_id":      tx.ExternalReference,
		"notes":            tx.Notes,
		"source_name":      tx.SourceAccount,
		"destination_name": tx.DestinationAccount,
	}
	if tx.Category != "" {
		payloadTx["category_name"] = tx.Category
	}
	if tx.Budget != "" {
		payloadTx["budget_name"] = tx.Budget
	}
	body, err := fireflyJSON(ctx, cfg, http.MethodPost, "/api/v1/transactions", map[string]interface{}{
		"transactions": []map[string]interface{}{payloadTx},
	})
	if err != nil {
		insertIntegrationActionLog(s.DB.WithContext(ctx), &IntegrationActionLog{DocumentID: documentID, BatchID: batchID, Provider: integrationProviderFirefly, ActionType: "transaction_create", Status: "error", ErrorMessage: err.Error()})
		return "", err
	}
	var resp struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", err
	}
	insertIntegrationActionLog(s.DB.WithContext(ctx), &IntegrationActionLog{
		DocumentID:      documentID,
		BatchID:         batchID,
		Provider:        integrationProviderFirefly,
		ActionType:      "transaction_create",
		Status:          "success",
		ExternalID:      resp.Data.ID,
		ExternalURL:     fireflyTransactionURL(cfg.InstanceURL, resp.Data.ID, ""),
		RequestSummary:  tx.Description,
		ResponseSummary: string(body),
	})
	return resp.Data.ID, nil
}

func (s *IntegrationsService) attachFireflyPDF(ctx context.Context, cfg FireflyConfig, client ClientInterface, documentID int, transactionID string, batchID *uint) error {
	document, err := client.GetDocument(ctx, documentID)
	if err != nil {
		return err
	}
	content, err := client.DownloadPDF(ctx, document)
	if err != nil {
		return err
	}
	filename := safePDFFilename(document, documentID)
	body, contentType, err := buildFireflyAttachmentUpload(transactionID, filename, content)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.InstanceURL+"/api/v1/attachments", body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", contentType)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		err := fmt.Errorf("firefly attachment upload failed: %d, %s", resp.StatusCode, string(raw))
		insertIntegrationActionLog(s.DB.WithContext(ctx), &IntegrationActionLog{DocumentID: documentID, BatchID: batchID, Provider: integrationProviderFirefly, ActionType: "attachment_upload", Status: "error", ExternalID: transactionID, ErrorMessage: err.Error()})
		return err
	}
	insertIntegrationActionLog(s.DB.WithContext(ctx), &IntegrationActionLog{
		DocumentID:      documentID,
		BatchID:         batchID,
		Provider:        integrationProviderFirefly,
		ActionType:      "attachment_upload",
		Status:          "success",
		ExternalID:      transactionID,
		ExternalURL:     fireflyTransactionURL(cfg.InstanceURL, transactionID, ""),
		ResponseSummary: string(raw),
	})
	return nil
}

func buildFireflyAttachmentUpload(transactionID, filename string, content []byte) (io.Reader, string, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	fields := map[string]string{
		"filename":        filename,
		"attachable_type": "TransactionJournal",
		"attachable_id":   transactionID,
	}
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			return nil, "", err
		}
	}
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, sanitizeMIMEFilename(filename)))
	header.Set("Content-Type", "application/pdf")
	part, err := writer.CreatePart(header)
	if err != nil {
		return nil, "", err
	}
	if _, err := part.Write(content); err != nil {
		return nil, "", err
	}
	if err := writer.Close(); err != nil {
		return nil, "", err
	}
	return bytes.NewReader(body.Bytes()), writer.FormDataContentType(), nil
}

func (s *IntegrationsService) UploadQuickBooksReceipt(ctx context.Context, client ClientInterface, suggestion DocumentSuggestion, batchID ...uint) (*QuickBooksUploadResult, error) {
	conn, err := getOptionalConnectionByProvider(s.DB.WithContext(ctx), integrationProviderQuickBooks)
	if err != nil {
		return nil, err
	}
	if conn == nil || conn.Status != integrationStatusConnected {
		return nil, fmt.Errorf("quickbooks is not connected")
	}
	settingsMutex.RLock()
	enabled := settings.QuickBooksEnabled && settings.QuickBooksReceiptUploadEnabled
	settingsMutex.RUnlock()
	if !enabled {
		return nil, fmt.Errorf("QuickBooks receipt upload is disabled in settings")
	}
	impl := newQuickBooksProvider()
	validConn, err := impl.ensureFreshToken(ctx, s.DB.WithContext(ctx), conn)
	if err != nil {
		return nil, err
	}
	realmID := quickBooksRealmID(validConn)
	if realmID == "" {
		return nil, fmt.Errorf("QuickBooks realm ID is missing; reconnect QuickBooks from Settings")
	}
	document, err := client.GetDocument(ctx, suggestion.ID)
	if err != nil {
		return nil, err
	}
	content, err := client.DownloadPDF(ctx, document)
	if err != nil {
		return nil, err
	}
	filename := safePDFFilename(document, suggestion.ID)
	body, contentType, err := buildQuickBooksReceiptUpload(filename, content, firstNonEmpty(suggestion.SuggestedTitle, document.Title))
	if err != nil {
		return nil, err
	}
	apiURL := quickBooksUploadURL(realmID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+validConn.AccessToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", contentType)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var appliedBatchID *uint
	if len(batchID) > 0 && batchID[0] > 0 {
		appliedBatchID = &batchID[0]
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		err := quickBooksReceiptUploadError(resp.StatusCode, raw)
		if isQuickBooksAuthorizationFailed(raw) {
			_ = disconnectIntegrationConnection(s.DB.WithContext(ctx), integrationProviderQuickBooks)
		}
		insertIntegrationActionLog(s.DB.WithContext(ctx), &IntegrationActionLog{DocumentID: suggestion.ID, BatchID: appliedBatchID, Provider: integrationProviderQuickBooks, ActionType: "receipt_upload", Status: "error", ErrorMessage: err.Error()})
		return nil, err
	}
	attachableID := parseQuickBooksAttachableID(raw)
	resultURL := fmt.Sprintf("https://app.qbo.intuit.com/app/receipts?companyId=%s", url.QueryEscape(realmID))
	insertIntegrationActionLog(s.DB.WithContext(ctx), &IntegrationActionLog{
		DocumentID:      suggestion.ID,
		BatchID:         appliedBatchID,
		Provider:        integrationProviderQuickBooks,
		ActionType:      "receipt_upload",
		Status:          "success",
		ExternalID:      attachableID,
		ExternalURL:     resultURL,
		RequestSummary:  filename,
		ResponseSummary: string(raw),
	})
	return &QuickBooksUploadResult{AttachableID: attachableID, URL: resultURL}, nil
}

func quickBooksReceiptUploadError(statusCode int, raw []byte) error {
	if isQuickBooksAuthorizationFailed(raw) {
		return fmt.Errorf("QuickBooks rejected the receipt upload because this app is not authorized for the connected company. Reconnect QuickBooks from Settings -> Integrations, and make sure the Intuit app has QuickBooks Online Accounting access enabled before reconnecting")
	}
	return fmt.Errorf("quickbooks receipt upload failed: %d, %s", statusCode, string(raw))
}

func quickBooksUploadURL(realmID string) string {
	return fmt.Sprintf("%s/v3/company/%s/upload", quickBooksAPIBaseURL(), url.PathEscape(realmID))
}

func quickBooksAPIBaseURL() string {
	settingsMutex.RLock()
	environment := normalizeQuickBooksEnvironment(settings.QuickBooksEnvironment)
	settingsMutex.RUnlock()
	if environment == quickBooksEnvironmentSandbox {
		return quickBooksSandboxAPIBaseURL
	}
	return quickBooksProductionAPIBaseURL
}

func normalizeQuickBooksEnvironment(environment string) string {
	if strings.EqualFold(strings.TrimSpace(environment), quickBooksEnvironmentSandbox) {
		return quickBooksEnvironmentSandbox
	}
	return quickBooksEnvironmentProduction
}

func isQuickBooksAuthorizationFailed(raw []byte) bool {
	lower := strings.ToLower(string(raw))
	return strings.Contains(lower, "applicationauthorizationfailed") ||
		strings.Contains(lower, `"code":"3100"`) ||
		strings.Contains(lower, `"code":3100`) ||
		strings.Contains(lower, "errorcode=003100")
}

func buildQuickBooksReceiptUpload(filename string, content []byte, note string) (io.Reader, string, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	metadata := map[string]interface{}{
		"AttachableRef": []map[string]interface{}{},
		"FileName":      filename,
		"ContentType":   "application/pdf",
		"Category":      "Receipt",
		"Note":          strings.TrimSpace(note),
	}
	rawMeta, _ := json.Marshal(metadata)
	metaHeader := textproto.MIMEHeader{}
	metaHeader.Set("Content-Disposition", `form-data; name="file_metadata_01"`)
	metaHeader.Set("Content-Type", "application/json")
	metaPart, err := writer.CreatePart(metaHeader)
	if err != nil {
		return nil, "", err
	}
	if _, err := metaPart.Write(rawMeta); err != nil {
		return nil, "", err
	}
	fileHeader := textproto.MIMEHeader{}
	fileHeader.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file_content_01"; filename="%s"`, sanitizeMIMEFilename(filename)))
	fileHeader.Set("Content-Type", "application/pdf")
	filePart, err := writer.CreatePart(fileHeader)
	if err != nil {
		return nil, "", err
	}
	if _, err := filePart.Write(content); err != nil {
		return nil, "", err
	}
	if err := writer.Close(); err != nil {
		return nil, "", err
	}
	return bytes.NewReader(body.Bytes()), writer.FormDataContentType(), nil
}

func (p quickBooksProvider) ensureFreshToken(ctx context.Context, db *gorm.DB, conn *IntegrationConnection) (*IntegrationConnection, error) {
	if conn == nil {
		return nil, fmt.Errorf("quickbooks connection not found")
	}
	if conn.AccessTokenExpiresAt != nil && conn.AccessTokenExpiresAt.After(time.Now().Add(jobberTokenRefreshSkew)) {
		return conn, nil
	}
	if strings.TrimSpace(conn.RefreshToken) == "" {
		return nil, fmt.Errorf("quickbooks refresh token not available; please reconnect QuickBooks")
	}
	token, err := p.RefreshToken(ctx, conn)
	if err != nil {
		return nil, fmt.Errorf("quickbooks token refresh failed: %w", err)
	}
	updated, err := upsertIntegrationConnection(db, integrationProviderQuickBooks, token, &providerIdentity{
		AccountID:   conn.AccountID,
		AccountName: conn.AccountName,
		Metadata:    metadataMap(conn),
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

func quickBooksRealmID(conn *IntegrationConnection) string {
	if conn == nil {
		return ""
	}
	if strings.TrimSpace(conn.AccountID) != "" {
		return strings.TrimSpace(conn.AccountID)
	}
	metadata := metadataMap(conn)
	if strings.TrimSpace(metadata["realm_id"]) != "" {
		return strings.TrimSpace(metadata["realm_id"])
	}
	for _, scope := range strings.Fields(conn.Scopes) {
		if strings.HasPrefix(scope, "realm:") {
			return strings.TrimPrefix(scope, "realm:")
		}
	}
	return ""
}

func parseQuickBooksAttachableID(raw []byte) string {
	var payload map[string]interface{}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ""
	}
	var walk func(interface{}) string
	walk = func(value interface{}) string {
		switch v := value.(type) {
		case map[string]interface{}:
			if id, ok := v["Id"].(string); ok && id != "" {
				return id
			}
			if id, ok := v["id"].(string); ok && id != "" {
				return id
			}
			for _, child := range v {
				if id := walk(child); id != "" {
					return id
				}
			}
		case []interface{}:
			for _, child := range v {
				if id := walk(child); id != "" {
					return id
				}
			}
		}
		return ""
	}
	return walk(payload)
}

func resolveMappedString(suggestion DocumentSuggestion, fieldRef string) string {
	value, ok := resolveMappedFieldValue(suggestion, fieldRef)
	if !ok {
		return ""
	}
	return stringifyExpenseFieldValue(value)
}

func resolveMappedFieldValue(suggestion DocumentSuggestion, fieldRef string) (interface{}, bool) {
	return resolveJobberExpenseFieldValue(suggestion, fieldRef)
}

func parseFireflyDate(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
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

func dateOnly(value string) string {
	if parsed, ok := parseFireflyDate(value); ok {
		return parsed.Format("2006-01-02")
	}
	return value
}

func absDays(d time.Duration) int {
	if d < 0 {
		d = -d
	}
	return int(d.Hours() / 24)
}

func amountsEqual(a, b float64) bool {
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return diff < 0.005
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func safePDFFilename(document Document, documentID int) string {
	filename := firstNonEmpty(document.ArchivedFileName, document.OriginalFileName, fmt.Sprintf("document-%d.pdf", documentID))
	if !strings.HasSuffix(strings.ToLower(filename), ".pdf") {
		filename += ".pdf"
	}
	return path.Base(sanitizeMIMEFilename(filename))
}

func sanitizeMIMEFilename(filename string) string {
	safe := strings.Map(func(r rune) rune {
		if r == '\r' || r == '\n' || r == 0 || r == '"' {
			return -1
		}
		return r
	}, filename)
	if strings.TrimSpace(safe) == "" {
		return "document.pdf"
	}
	return safe
}

func fireflyTransactionURL(instanceURL, id, self string) string {
	if strings.TrimSpace(self) != "" {
		return self
	}
	return strings.TrimRight(instanceURL, "/") + "/transactions/show/" + url.PathEscape(id)
}
