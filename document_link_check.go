package main

import (
	"bytes"
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"text/template"

	"paperless-gpt/internal/textsanitize"

	"github.com/sirupsen/logrus"
	"github.com/tmc/langchaingo/llms"
)

// The link check asks the LLM which of the documents that contain a reference
// the source document actually refers to with it. Counting matches alone
// can't tell a contract number apart from a child benefit number that is
// filled into every salary form: both are rare in the archive. Measured on a
// real archive, the check found almost every correct link while the "at most
// 3 matches" rule missed two out of three and still got every third wrong.
const (
	// maxDocumentLinkCandidates is how many matches a reference may have to
	// be offered to the link check; more is too generic even for that.
	maxDocumentLinkCandidates = 10

	// documentLinkExcerptChars is how much text the check sees on each side
	// of the reference, and of the beginning of a document.
	documentLinkExcerptChars = 300
)

// documentLinkPromptDocument is how the link prompt sees the source document
// and each candidate.
type documentLinkPromptDocument struct {
	ID            int
	Title         string
	CreatedDate   string
	Correspondent string
	DocumentType  string
	// Opening is the beginning of the document, where the subject and sender
	// usually are. Empty when Excerpt already starts there.
	Opening string
	// Excerpt is the text around the first occurrence of the reference: it
	// shows the role the reference has, e.g. "Ihre Rechnung Nr." or a form
	// field.
	Excerpt string
}

// documentLinkPromptData is what document_link_prompt.tmpl is rendered with.
type documentLinkPromptData struct {
	Reference  string
	Source     documentLinkPromptDocument
	Candidates []documentLinkPromptDocument
}

// sampleDocumentLinkPromptData renders a workflow's document link prompt
// once when it is saved, so a misspelled field is reported right away.
var sampleDocumentLinkPromptData = documentLinkPromptData{
	Reference:  "RE-2026-0417",
	Source:     documentLinkPromptDocument{ID: 2, Title: "Reminder", Excerpt: "Invoice RE-2026-0417 is overdue"},
	Candidates: []documentLinkPromptDocument{{ID: 1, Title: "Invoice", Excerpt: "Invoice no. RE-2026-0417"}},
}

// checkDocumentLinks returns the candidates the LLM confirms source refers to
// with reference. Everything that goes wrong (a candidate that can't be
// fetched, an LLM error, an answer that isn't a list of candidate ids) leads
// to fewer links, never to a guess.
func (app *App) checkDocumentLinks(ctx context.Context, source Document, reference string, candidateIDs []int, tmpl *template.Template, logger *logrus.Entry) []int {
	if tmpl == nil {
		templateMutex.RLock()
		tmpl = documentLinkTemplate
		templateMutex.RUnlock()
	}
	if tmpl == nil {
		logger.Warn("No document link prompt is loaded, not linking.")
		return nil
	}

	data := documentLinkPromptData{
		Reference: reference,
		Source:    documentLinkPromptDocumentFor(source, reference),
	}
	var offered []int
	for _, id := range candidateIDs {
		candidate, err := app.Client.GetDocument(ctx, id)
		if err != nil {
			logger.Warnf("Could not fetch document %d to check the link for %q, leaving it out: %v", id, reference, err)
			continue
		}
		candidate.ID = id
		data.Candidates = append(data.Candidates, documentLinkPromptDocumentFor(candidate, reference))
		offered = append(offered, id)
	}
	if len(offered) == 0 {
		return nil
	}

	var promptBuffer bytes.Buffer
	if err := tmpl.Execute(&promptBuffer, data); err != nil {
		logger.Warnf("Rendering the document link prompt failed, not linking %q: %v", reference, err)
		return nil
	}
	prompt := promptBuffer.String()
	logger.Debugf("Document link prompt for %q: %s", reference, prompt)

	completion, err := app.LLM.GenerateContent(ctx, []llms.MessageContent{
		{Role: llms.ChatMessageTypeHuman, Parts: []llms.ContentPart{llms.TextContent{Text: prompt}}},
	})
	if err != nil {
		logger.Warnf("Checking the links for %q with the LLM failed, not linking it: %v", reference, err)
		return nil
	}
	if len(completion.Choices) == 0 {
		logger.Warnf("The LLM gave no answer on the links for %q, not linking it.", reference)
		return nil
	}
	response := textsanitize.StripReasoning(completion.Choices[0].Content)
	logger.Debugf("LLM response for document links of %q: %s", reference, response)

	chosen, ok := parseDocumentLinkChoice(response, offered)
	if !ok {
		logger.Warnf("The answer on the links for %q is not a list of document ids, not linking it. Starts with: %s", reference, truncateForLog(response, 80))
		return nil
	}
	logger.Debugf("Document link reference %q: %d of %d candidates confirmed.", reference, len(chosen), len(offered))
	return chosen
}

// documentLinkPromptDocumentFor cuts a document down to what the link prompt
// shows of it.
func documentLinkPromptDocumentFor(doc Document, reference string) documentLinkPromptDocument {
	opening, excerpt := documentLinkExcerpts(doc.Content, reference)
	return documentLinkPromptDocument{
		ID:            doc.ID,
		Title:         doc.Title,
		CreatedDate:   doc.CreatedDate,
		Correspondent: doc.Correspondent,
		DocumentType:  doc.DocumentTypeName,
		Opening:       opening,
		Excerpt:       excerpt,
	}
}

// documentLinkExcerpts returns the beginning of content and the text around
// the first whole-word occurrence of reference, documentLinkExcerptChars on
// each side, with whitespace collapsed. When the reference is near the start,
// the excerpt runs from the beginning and opening is empty.
func documentLinkExcerpts(content, reference string) (opening, excerpt string) {
	const n = documentLinkExcerptChars
	loc := referencePattern(reference).FindStringIndex(content)
	if loc == nil {
		return collapseWhitespace(firstRunes(content, n)), ""
	}
	before, match, after := content[:loc[0]], content[loc[0]:loc[1]], content[loc[1]:]
	if len([]rune(before)) <= 2*n {
		return "", collapseWhitespace(before + match + firstRunes(after, n))
	}
	return collapseWhitespace(firstRunes(content, n)), collapseWhitespace("…" + lastRunes(before, n) + match + firstRunes(after, n))
}

func firstRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func lastRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[len(r)-n:])
}

func collapseWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// parseDocumentLinkChoice reads the LLM's answer: a JSON array of document
// ids. Ids may come as numbers or strings; only offered ones are kept, so the
// LLM can't link a document that wasn't a candidate. ok is false when the
// answer isn't such an array.
func parseDocumentLinkChoice(response string, offered []int) (chosen []int, ok bool) {
	cleaned := cleanLLMJSON(response)
	if cleaned == "" {
		return nil, false
	}
	var values []interface{}
	if err := json.Unmarshal([]byte(cleaned), &values); err != nil {
		return nil, false
	}
	for _, v := range values {
		var id int
		switch v := v.(type) {
		case float64:
			if v != float64(int(v)) {
				continue
			}
			id = int(v)
		case string:
			parsed, err := strconv.Atoi(strings.TrimSpace(v))
			if err != nil {
				continue
			}
			id = parsed
		default:
			continue
		}
		if slices.Contains(offered, id) && !slices.Contains(chosen, id) {
			chosen = append(chosen, id)
		}
	}
	return chosen, true
}
