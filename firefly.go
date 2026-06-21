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
)

const (
	fireflyExternalIDPrefix = "paperless-gpt-document-"
	fireflyDateWindowDays   = 7
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

type fireflyCandidateSignals struct {
	amountMatch        bool
	currencyMatch      bool
	exactDateMatch     bool
	dateDiffDays       int
	descriptionOverlap bool
}

type scoredFireflyCandidate struct {
	candidate FireflyTransactionCandidate
	score     int
	signals   fireflyCandidateSignals
}

type fireflyCandidateEvaluation struct {
	Ranked          []FireflyTransactionCandidate
	AutoSelectedID  string
	StrongDuplicate bool
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
		InstanceURL:                normalizeFireflyInstanceURL(settings.FireflyInstanceURL),
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
		token, _, err := DecryptSecretFromStorage(settings.FireflyAPIToken)
		if err != nil {
			return cfg, false, "Firefly API token could not be decrypted"
		}
		cfg.Token = strings.TrimSpace(token)
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

func normalizeFireflyInstanceURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	return strings.TrimRight(raw, "/")
}

func probeFireflyHealth(ctx context.Context, cfg FireflyConfig) error {
	if cfg.InstanceURL == "" {
		return fmt.Errorf("firefly instance url is required")
	}
	if cfg.Token == "" {
		return fmt.Errorf("firefly api token is required")
	}
	_, err := fireflyGET(ctx, cfg, "/api/v1/about")
	if err != nil {
		return fmt.Errorf("firefly health check failed: %w", err)
	}
	return nil
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
	}

	for _, secret := range secrets {
		if strings.TrimSpace(*secret.merged) == "" {
			*secret.merged = *secret.current
			continue
		}
		encrypted, err := EncryptSecretForStorage(strings.TrimSpace(*secret.merged))
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

	evaluation, err := s.evaluateFireflyCandidates(ctx, cfg, suggestion, derived)
	if err != nil {
		return nil, "", err
	}
	return evaluation.Ranked, evaluation.AutoSelectedID, nil
}

func (s *IntegrationsService) evaluateFireflyCandidates(ctx context.Context, cfg FireflyConfig, suggestion DocumentSuggestion, derived fireflyDerivedTransaction) (fireflyCandidateEvaluation, error) {
	candidates, err := s.searchFireflyTransactions(ctx, cfg, derived)
	if err != nil {
		return fireflyCandidateEvaluation{}, err
	}
	scored := scoreFireflyCandidates(derived, suggestion, candidates)
	return fireflyCandidateEvaluation{
		Ranked:          rankedFireflyCandidates(scored),
		AutoSelectedID:  autoSelectFireflyCandidate(scored),
		StrongDuplicate: hasStrongFireflyDuplicate(scored),
	}, nil
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
	scoredCandidates := scoreFireflyCandidates(derived, suggestion, candidates)
	return rankedFireflyCandidates(scoredCandidates), autoSelectFireflyCandidate(scoredCandidates)
}

func scoreFireflyCandidates(derived fireflyDerivedTransaction, suggestion DocumentSuggestion, candidates []FireflyTransactionCandidate) []scoredFireflyCandidate {
	docDate, hasDate := parseFireflyDate(derived.Date)
	text := strings.ToLower(strings.Join([]string{
		derived.Description,
		suggestion.OriginalDocument.Title,
		suggestion.SuggestedTitle,
		suggestion.OriginalDocument.Correspondent,
		suggestion.SuggestedCorrespondent,
	}, " "))
	scoredCandidates := make([]scoredFireflyCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		score := 0
		reasons := []string{}
		signals := fireflyCandidateSignals{dateDiffDays: -1}
		candidateAmount, amountOK := parseNumericValue(candidate.Amount)
		if amountOK && amountsEqual(candidateAmount, derived.Amount) {
			signals.amountMatch = true
			score += 70
			reasons = append(reasons, "same amount")
		}
		if strings.EqualFold(strings.TrimSpace(candidate.CurrencyCode), strings.TrimSpace(derived.CurrencyCode)) && strings.TrimSpace(derived.CurrencyCode) != "" {
			signals.currencyMatch = true
			score += 15
			reasons = append(reasons, "same currency")
		}
		if candidateDate, ok := parseFireflyDate(candidate.Date); ok && hasDate {
			diff := absDays(candidateDate.Sub(docDate))
			signals.dateDiffDays = diff
			if diff == 0 {
				signals.exactDateMatch = true
				score += 45
				reasons = append(reasons, "same date")
			} else if diff <= fireflyDateWindowDays {
				score += 20 - diff
				reasons = append(reasons, fmt.Sprintf("within %d day(s)", diff))
			}
		}
		desc := strings.ToLower(strings.TrimSpace(candidate.Description))
		if desc != "" && (strings.Contains(text, desc) || strings.Contains(desc, strings.ToLower(strings.TrimSpace(derived.Description)))) {
			signals.descriptionOverlap = true
			score += 10
			reasons = append(reasons, "description overlap")
		}
		c := candidate
		if len(reasons) > 0 {
			c.MatchReason = "Matched on " + strings.Join(reasons, ", ")
		}
		scoredCandidates = append(scoredCandidates, scoredFireflyCandidate{candidate: c, score: score, signals: signals})
	}
	sort.SliceStable(scoredCandidates, func(i, j int) bool {
		if scoredCandidates[i].score != scoredCandidates[j].score {
			return scoredCandidates[i].score > scoredCandidates[j].score
		}
		return strings.Compare(scoredCandidates[i].candidate.ID, scoredCandidates[j].candidate.ID) < 0
	})
	return scoredCandidates
}

func rankedFireflyCandidates(scoredCandidates []scoredFireflyCandidate) []FireflyTransactionCandidate {
	ranked := make([]FireflyTransactionCandidate, 0, len(scoredCandidates))
	for _, item := range scoredCandidates {
		candidate := item.candidate
		if item.score <= 0 && strings.TrimSpace(candidate.MatchReason) == "" {
			candidate.MatchReason = "Shown for manual review because it falls within the Firefly date window."
		}
		ranked = append(ranked, candidate)
	}
	return ranked
}

func autoSelectFireflyCandidate(scoredCandidates []scoredFireflyCandidate) string {
	if len(scoredCandidates) == 0 {
		return ""
	}
	top := scoredCandidates[0]
	if top.score <= 0 || !top.signals.amountMatch || !top.signals.currencyMatch || !top.signals.exactDateMatch {
		return ""
	}
	if len(scoredCandidates) > 1 && top.score-scoredCandidates[1].score < 10 {
		return ""
	}
	return top.candidate.ID
}

func hasStrongFireflyDuplicate(scoredCandidates []scoredFireflyCandidate) bool {
	for _, item := range scoredCandidates {
		if item.score <= 0 || !item.signals.amountMatch || !item.signals.currencyMatch {
			continue
		}
		if item.signals.exactDateMatch {
			return true
		}
		if item.signals.dateDiffDays >= 0 && item.signals.dateDiffDays <= 3 {
			return true
		}
	}
	return false
}

func deriveFireflyTransaction(suggestion DocumentSuggestion, cfg FireflyConfig) (fireflyDerivedTransaction, error) {
	description := resolveFireflyMappedString(suggestion, cfg.DescriptionFieldRef)
	if description == "" {
		description = strings.TrimSpace(suggestion.SuggestedTitle)
	}
	if description == "" {
		description = strings.TrimSpace(suggestion.OriginalDocument.Title)
	}
	if description == "" {
		description = fmt.Sprintf("Paperless document %d", suggestion.ID)
	}
	dateValue := resolveFireflyMappedString(suggestion, cfg.DateFieldRef)
	if dateValue == "" {
		dateValue = strings.TrimSpace(suggestion.SuggestedCreatedDate)
	}
	if dateValue == "" {
		dateValue = strings.TrimSpace(suggestion.OriginalDocument.CreatedDate)
	}
	if parsed, ok := parseFireflyDate(dateValue); ok {
		dateValue = parsed.Format("2006-01-02")
	} else if dateValue != "" {
		return fireflyDerivedTransaction{}, fmt.Errorf("Firefly requires a transaction date in YYYY-MM-DD or RFC3339 format")
	}
	amount, amountString, hasAmount := deriveFireflyAmount(suggestion, cfg.AmountFieldRef)
	currency := resolveFireflyMappedString(suggestion, cfg.CurrencyFieldRef)
	if currency == "" {
		currency = cfg.DefaultCurrency
	}
	if currency == "" {
		currency = "USD"
	}
	category := firstNonEmpty(resolveFireflyMappedString(suggestion, cfg.CategoryFieldRef), cfg.DefaultCategory)
	budget := firstNonEmpty(resolveFireflyMappedString(suggestion, cfg.BudgetFieldRef), cfg.DefaultBudget)
	notes := firstNonEmpty(resolveFireflyMappedString(suggestion, cfg.NotesFieldRef), cfg.NotesTemplate)
	marker := fireflyExternalIDPrefix + strconv.Itoa(suggestion.ID)
	if notes == "" {
		notes = marker
	} else if !strings.Contains(notes, marker) {
		notes = notes + "\n" + marker
	}
	externalRef := firstNonEmpty(resolveFireflyMappedString(suggestion, cfg.ExternalRefFieldRef), marker)
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
			SourceAccount:      firstNonEmpty(resolveFireflyMappedString(suggestion, cfg.SourceAccountFieldRef), cfg.DefaultSourceAccount),
			DestinationAccount: firstNonEmpty(resolveFireflyMappedString(suggestion, cfg.DestinationAccountFieldRef), cfg.DefaultDestinationAccount),
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
	if value, ok := resolveSuggestionFieldValue(suggestion, fieldRef); ok {
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
	if selectedID != "" {
		result := &FireflyApplyResult{
			Matched:       true,
			TransactionID: selectedID,
			URL:           fireflyTransactionURL(cfg.InstanceURL, selectedID, ""),
		}
		insertIntegrationActionLog(s.DB.WithContext(ctx), &IntegrationActionLog{
			DocumentID:      suggestion.ID,
			BatchID:         appliedBatchID,
			Provider:        integrationProviderFirefly,
			ActionType:      "transaction_match",
			Status:          "success",
			ExternalID:      selectedID,
			ExternalURL:     result.URL,
			RequestSummary:  "user selected existing transaction",
			ResponseSummary: selectedID,
		})
		if err := s.attachFireflyPDF(ctx, cfg, client, suggestion.ID, selectedID, appliedBatchID); err != nil {
			return result, fmt.Errorf("firefly receipt attachment failed for document %d: %w", suggestion.ID, err)
		}
		result.AttachmentUploaded = true
		return result, nil
	}
	if !suggestion.CreateFireflyTransaction {
		return nil, nil
	}
	derived, err := deriveFireflyTransaction(suggestion, cfg)
	if err != nil {
		return nil, err
	}
	evaluation, err := s.evaluateFireflyCandidates(ctx, cfg, suggestion, derived)
	if err != nil {
		return nil, fmt.Errorf("firefly duplicate check failed before create: %w", err)
	}
	if evaluation.StrongDuplicate {
		return nil, fmt.Errorf("possible Firefly duplicate found; review suggested transactions and select an existing one instead of creating a new transaction")
	}
	transactionID, err := s.createFireflyTransaction(ctx, cfg, suggestion.ID, derived, appliedBatchID)
	if err != nil {
		return nil, err
	}
	result := &FireflyApplyResult{
		Created:       true,
		TransactionID: transactionID,
		URL:           fireflyTransactionURL(cfg.InstanceURL, transactionID, ""),
	}
	if err := s.attachFireflyPDF(ctx, cfg, client, suggestion.ID, transactionID, appliedBatchID); err != nil {
		return result, fmt.Errorf("firefly receipt attachment failed for document %d: %w", suggestion.ID, err)
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
		return fmt.Errorf("load document %d for Firefly attachment: %w", documentID, err)
	}
	content, err := client.DownloadPDF(ctx, document)
	if err != nil {
		return fmt.Errorf("download document %d PDF for Firefly attachment: %w", documentID, err)
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

func resolveFireflyMappedString(suggestion DocumentSuggestion, fieldRef string) string {
	value, ok := resolveSuggestionFieldValue(suggestion, fieldRef)
	if !ok {
		return ""
	}
	return stringifyExpenseFieldValue(value)
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
