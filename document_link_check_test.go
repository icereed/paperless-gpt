package main

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"text/template"

	"paperless-gpt/internal/textsanitize"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tmc/langchaingo/llms"
)

func useDocumentLinkLLMCheck(t *testing.T, on bool) {
	t.Helper()
	prev := documentLinkLLMCheck
	t.Cleanup(func() { documentLinkLLMCheck = prev })
	documentLinkLLMCheck = on
}

// scriptedLLM answers each call with the next response and records the
// prompts it was sent.
type scriptedLLM struct {
	mockLLM
	responses []string
	errs      []error
	prompts   []string
}

func (m *scriptedLLM) GenerateContent(_ context.Context, messages []llms.MessageContent, _ ...llms.CallOption) (*llms.ContentResponse, error) {
	i := len(m.prompts)
	m.prompts = append(m.prompts, messages[0].Parts[0].(llms.TextContent).Text)
	if i < len(m.errs) && m.errs[i] != nil {
		return nil, m.errs[i]
	}
	if i >= len(m.responses) {
		return nil, fmt.Errorf("unexpected LLM call %d", i+1)
	}
	return &llms.ContentResponse{Choices: []*llms.ContentChoice{{Content: m.responses[i]}}}, nil
}

// A reminder cites a contract number that also appears in the contract, a
// payslip form and two more documents: five matches, more than the lookup on
// its own allows. The LLM sees each of them and links only what it confirms.
func TestGetSuggestedCustomFields_DocumentLinkCheck(t *testing.T) {
	t.Setenv("TOKEN_LIMIT", "")
	resetTokenLimit()
	useDocumentLinkLLMCheck(t, true)

	const fieldsAnswer = `[{"field": "Related", "value": ["V-2026-17"]}]`
	candidates := map[int]Document{
		412: {Title: "Vertrag", Correspondent: "ACME", CreatedDate: "2026-01-02", Content: "Vertrag Nr. V-2026-17 zwischen ACME und Ihnen"},
		87:  {Title: "OFZ-Erklärung", Content: "Formular ... Vertragsnummer (Angabe zwingend erforderlich) V-2026-17"},
		90:  {Title: "a", Content: "V-2026-17"},
		91:  {Title: "b", Content: "V-2026-17"},
		92:  {Title: "c", Content: "V-2026-17"},
	}
	source := Document{ID: 533, Title: "Mahnung", Content: "Mahnung zu Vertrag V-2026-17"}

	tests := []struct {
		name      string
		answer    string
		llmErr    error
		matches   []int
		wantLinks []int // nil: the field is not suggested
		wantCalls int
	}{
		{name: "links the confirmed candidates", answer: "[412]", matches: []int{412, 87, 90, 91, 92}, wantLinks: []int{412}, wantCalls: 2},
		{name: "ids as strings in a code fence", answer: "```json\n[\"412\", \"90\"]\n```", matches: []int{412, 87, 90}, wantLinks: []int{412, 90}, wantCalls: 2},
		{name: "a document that wasn't offered is not linked", answer: "[412, 7]", matches: []int{412, 87}, wantLinks: []int{412}, wantCalls: 2},
		{name: "none confirmed", answer: "[]", matches: []int{412, 87}, wantCalls: 2},
		{name: "an answer that isn't a list links nothing", answer: "Document 412 fits.", matches: []int{412}, wantCalls: 2},
		{name: "an LLM error links nothing", llmErr: fmt.Errorf("rate limited"), matches: []int{412}, wantCalls: 2},
		{name: "more than the candidate limit is too generic to check", matches: []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}, wantCalls: 1},
		{name: "no match needs no check", matches: nil, wantCalls: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			llm := &scriptedLLM{responses: []string{fieldsAnswer, tt.answer}, errs: []error{nil, tt.llmErr}}
			client := &mockPaperlessClient{
				CustomFields:     []CustomField{{ID: 2, Name: "Related", DataType: documentLinkFieldType}},
				ReferenceMatches: map[string][]int{"V-2026-17": tt.matches},
				Documents:        candidates,
			}
			app := &App{LLM: llm, Client: client}
			usePromptsSandbox(t, map[string]string{"custom_field_prompt.tmpl": "{{ .CustomFieldsXML }}"})
			require.NoError(t, loadTemplates())

			suggestions, err := app.getSuggestedCustomFields(context.Background(), source, []int{2}, logrus.WithField("test", t.Name()), nil, nil)
			require.NoError(t, err)
			require.Len(t, llm.prompts, tt.wantCalls)

			field, ok := findFieldByID(suggestions, 2)
			if tt.wantLinks == nil {
				assert.False(t, ok, "got %v", field.Value)
			} else {
				require.True(t, ok)
				assert.Equal(t, tt.wantLinks, field.Value)
			}

			if tt.wantCalls == 2 {
				prompt := llm.prompts[1]
				assert.Contains(t, prompt, `cites the identifier "V-2026-17"`)
				assert.Contains(t, prompt, "Title: Mahnung")
				assert.Contains(t, prompt, `<candidate id="412">`)
				assert.Contains(t, prompt, "Vertrag Nr. V-2026-17 zwischen ACME", "the candidate's text around the reference")
				assert.Contains(t, prompt, "Correspondent: ACME")
				assert.NotContains(t, prompt, `<candidate id="533">`, "the document itself is never a candidate")
			}
		})
	}
}

// With the check off, the lookup alone decides, as before: up to 3 matches
// are linked without asking the LLM.
func TestGetSuggestedCustomFields_DocumentLinkCheckOff(t *testing.T) {
	t.Setenv("TOKEN_LIMIT", "")
	resetTokenLimit()
	useDocumentLinkLLMCheck(t, false)

	llm := &scriptedLLM{responses: []string{`[{"field": "Related", "value": ["V-2026-17"]}]`}}
	client := &mockPaperlessClient{
		CustomFields:     []CustomField{{ID: 2, Name: "Related", DataType: documentLinkFieldType}},
		ReferenceMatches: map[string][]int{"V-2026-17": {412, 87}},
	}
	app := &App{LLM: llm, Client: client}
	usePromptsSandbox(t, map[string]string{"custom_field_prompt.tmpl": "{{ .CustomFieldsXML }}"})
	require.NoError(t, loadTemplates())

	suggestions, err := app.getSuggestedCustomFields(context.Background(), Document{ID: 533, Content: "x"}, []int{2}, logrus.WithField("test", t.Name()), nil, nil)
	require.NoError(t, err)
	require.Len(t, llm.prompts, 1)
	field, ok := findFieldByID(suggestions, 2)
	require.True(t, ok)
	assert.Equal(t, []int{412, 87}, field.Value)
}

// A workflow's own document link prompt replaces the global one.
func TestGetSuggestedCustomFields_DocumentLinkWorkflowPrompt(t *testing.T) {
	t.Setenv("TOKEN_LIMIT", "")
	resetTokenLimit()
	useDocumentLinkLLMCheck(t, true)

	llm := &scriptedLLM{responses: []string{`[{"field": "Related", "value": ["V-2026-17"]}]`, "[412]"}}
	client := &mockPaperlessClient{
		CustomFields:     []CustomField{{ID: 2, Name: "Related", DataType: documentLinkFieldType}},
		ReferenceMatches: map[string][]int{"V-2026-17": {412}},
		Documents:        map[int]Document{412: {Title: "Vertrag", Content: "V-2026-17"}},
	}
	app := &App{LLM: llm, Client: client}
	usePromptsSandbox(t, map[string]string{"custom_field_prompt.tmpl": "{{ .CustomFieldsXML }}"})
	require.NoError(t, loadTemplates())

	own := template.Must(template.New("own").Parse(`Invoices only. {{ .Reference }}:{{ range .Candidates }} {{ .ID }}={{ .Title }}{{ end }}`))
	suggestions, err := app.getSuggestedCustomFields(context.Background(), Document{ID: 533, Content: "V-2026-17"}, []int{2}, logrus.WithField("test", t.Name()), nil, own)
	require.NoError(t, err)
	require.Len(t, llm.prompts, 2)
	assert.Equal(t, "Invoices only. V-2026-17: 412=Vertrag", llm.prompts[1])
	field, ok := findFieldByID(suggestions, 2)
	require.True(t, ok)
	assert.Equal(t, []int{412}, field.Value)
}

func TestDocumentLinkExcerpts(t *testing.T) {
	long := strings.Repeat("Absender Betreff ", 60) // ~1000 characters before the reference

	t.Run("reference near the start", func(t *testing.T) {
		opening, excerpt := documentLinkExcerpts("Rechnung\n\nNr.  R123 vom 1.2.", "R123")
		assert.Empty(t, opening)
		assert.Equal(t, "Rechnung Nr. R123 vom 1.2.", excerpt)
	})
	t.Run("reference further down", func(t *testing.T) {
		opening, excerpt := documentLinkExcerpts("Kopfzeile "+long+"Kindergeldnummer R123 (Angabe zwingend)", "R123")
		assert.True(t, strings.HasPrefix(opening, "Kopfzeile "))
		assert.True(t, strings.HasSuffix(opening, "…"))
		assert.True(t, strings.HasPrefix(excerpt, "…"))
		assert.Contains(t, excerpt, "Kindergeldnummer R123 (Angabe zwingend)")
		assert.LessOrEqual(t, len([]rune(excerpt)), 2*documentLinkExcerptChars+10)
	})
	t.Run("whole words only", func(t *testing.T) {
		_, excerpt := documentLinkExcerpts("R1234 steht hier, R123 erst dort", "R123")
		assert.Equal(t, "R1234 steht hier, R123 erst dort", excerpt)
		_, excerpt = documentLinkExcerpts("nur R1234", "R123")
		assert.Empty(t, excerpt)
	})
	t.Run("multibyte text is cut on character boundaries", func(t *testing.T) {
		_, excerpt := documentLinkExcerpts(strings.Repeat("ä", 1000)+" R123 "+strings.Repeat("ö", 1000), "R123")
		assert.True(t, strings.Contains(excerpt, "R123"))
		assert.NotContains(t, excerpt, "�")
	})
}

func TestParseDocumentLinkChoice(t *testing.T) {
	offered := []int{412, 87}
	tests := []struct {
		in     string
		want   []int
		wantOK bool
	}{
		{"[412]", []int{412}, true},
		{"[]", nil, true},
		{`["87", 412, 412]`, []int{87, 412}, true},
		{"[412, 9]", []int{412}, true},
		{"[412.5]", nil, true},
		{"<think>maybe 87</think>[87]", []int{87}, true},
		{"", nil, false},
		{"412", nil, false},
		{`{"chosen": [412]}`, nil, false},
	}
	for _, tt := range tests {
		got, ok := parseDocumentLinkChoice(textsanitize.StripReasoning(tt.in), offered)
		assert.Equal(t, tt.wantOK, ok, tt.in)
		assert.Equal(t, tt.want, got, tt.in)
	}
}

func TestSampleDocumentLinkPromptDataRendersDefault(t *testing.T) {
	usePromptsSandbox(t, nil)
	require.NoError(t, loadTemplates())
	var b strings.Builder
	require.NoError(t, documentLinkTemplate.Execute(&b, sampleDocumentLinkPromptData))
	assert.Contains(t, b.String(), `<candidate id="1">`)
}
