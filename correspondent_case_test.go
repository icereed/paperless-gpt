package main

import (
	"context"
	"testing"
	"text/template"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A correspondent suggestion that differs from an existing correspondent only
// in case must come back with the existing spelling, like tags and document
// types already do. Otherwise a model that copies a letterhead in capitals
// shows the unchanged correspondent as a change in the review.
func TestGenerateSuggestion_CorrespondentKeepsExistingCase(t *testing.T) {
	tests := []struct {
		name        string
		existing    []string
		promptLimit int
		notInPrompt string
		content     string
		answer      string
		want        string
	}{
		{
			name:     "differs only in case",
			existing: []string{"Northwind Dental Clinic"},
			content:  "NORTHWIND DENTAL CLINIC patient receipt",
			answer:   "NORTHWIND DENTAL CLINIC",
			want:     "Northwind Dental Clinic",
		},
		{
			name:        "existing name left out of the prompt",
			existing:    []string{"Contoso Tire Centre", "Northwind Dental Clinic"},
			promptLimit: 1,
			notInPrompt: "Northwind Dental Clinic",
			content:     "CONTOSO TIRE CENTRE invoice",
			answer:      "northwind dental clinic",
			want:        "Northwind Dental Clinic",
		},
		{
			name:     "new correspondent",
			existing: []string{"Contoso Tire Centre"},
			content:  "Contoso Optical contact lens invoice",
			answer:   "Contoso Optical",
			want:     "Contoso Optical",
		},
		{
			name:     "several existing names differ only in case",
			existing: []string{"ACME", "Acme"},
			content:  "acme invoice",
			answer:   "acme",
			want:     "acme",
		},
	}

	previousTemplate := correspondentTemplate
	correspondentTemplate = template.Must(template.New("correspondent").Parse("{{.AvailableCorrespondents}}\n{{.Content}}"))
	t.Cleanup(func() { correspondentTemplate = previousTemplate })

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			previousLimit := correspondentPromptLimit
			correspondentPromptLimit = tt.promptLimit
			t.Cleanup(func() { correspondentPromptLimit = previousLimit })

			llm := &mockLLM{Response: tt.answer}
			app := &App{LLM: llm}
			suggestion, err := app.generateSingleDocumentSuggestion(
				context.Background(),
				GenerateSuggestionsRequest{GenerateCorrespondents: true},
				Document{ID: 1, Title: "scan.pdf", Content: tt.content},
				suggestionGenerationContext{availableCorrespondentNames: tt.existing},
				logrus.WithField("test", t.Name()),
			)
			require.NoError(t, err)
			if tt.notInPrompt != "" {
				require.NotContains(t, llm.lastPrompt, tt.notInPrompt, "the prompt limit should have left this name out")
			}
			assert.Equal(t, tt.want, suggestion.SuggestedCorrespondent)
		})
	}
}
