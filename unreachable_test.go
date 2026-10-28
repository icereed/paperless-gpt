package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"syscall"
	"testing"
	"text/template"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// dialRefused is what an HTTP request to a stopped Ollama returns.
func dialRefused() error {
	return &url.Error{Op: "Post", URL: "http://desktop:11434/api/chat", Err: &net.OpError{
		Op: "dial", Net: "tcp", Err: &os.SyscallError{Syscall: "connect", Err: syscall.ECONNREFUSED},
	}}
}

func TestIsUnreachable(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"connection refused", dialRefused(), true},
		{"DNS lookup failed", &url.Error{Op: "Post", URL: "http://desktop:11434", Err: &net.OpError{
			Op: "dial", Net: "tcp", Err: &net.DNSError{Err: "server misbehaving", Name: "desktop", IsTemporary: true},
		}}, true},
		{"dial timeout", &net.OpError{Op: "dial", Net: "tcp", Err: os.ErrDeadlineExceeded}, true},
		{"host gone mid-request", &net.OpError{
			Op: "read", Net: "tcp", Err: &os.SyscallError{Syscall: "read", Err: syscall.ETIMEDOUT},
		}, true},
		{"connection reset", &net.OpError{
			Op: "read", Net: "tcp", Err: &os.SyscallError{Syscall: "read", Err: syscall.ECONNRESET},
		}, true},
		{"wrapped through the LLM call chain", fmt.Errorf("Document 7: %w",
			fmt.Errorf("error getting response from LLM: %w",
				fmt.Errorf("all retry attempts failed, last error: %w",
					fmt.Errorf("Ollama chat request: %w", dialRefused())))), true},
		{"HTTP client timeout", &url.Error{Op: "Post", URL: "http://desktop:11434", Err: context.DeadlineExceeded}, false},
		{"provider deadline", fmt.Errorf("operation timed out after 2m0s: %w", context.DeadlineExceeded), false},
		{"read deadline on an open connection", &net.OpError{Op: "read", Net: "tcp", Err: os.ErrDeadlineExceeded}, false},
		{"model error", errors.New("token repeat limit reached"), false},
		{"connection error flattened to text", fmt.Errorf("error: %v", dialRefused()), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, isUnreachable(tc.err))
		})
	}
}

// The suggestion path wraps the LLM error several times on its way to the
// retry counter; each layer has to keep the chain (%w) for isUnreachable to
// see the network error.
func TestProcessTagDocuments_UnreachableLLMDoesNotCount(t *testing.T) {
	const trigger = "unreachable-in"
	prevAutoTag, prevFail, prevTitle, prevMax, prevLimit := autoTag, failTag, titleTemplate, autoTagMaxRetries, tokenLimit
	t.Cleanup(func() {
		autoTag, failTag, titleTemplate, autoTagMaxRetries, tokenLimit = prevAutoTag, prevFail, prevTitle, prevMax, prevLimit
	})
	autoTag, failTag = "paperless-gpt-auto", "paperless-gpt-failed"
	titleTemplate = template.Must(template.New("title").Parse("{{.Content}}"))
	autoTagMaxRetries = 1
	tokenLimit = 0
	isolateWorkflowSettings(t)
	on, off := true, false
	useWorkflows(t, WorkflowConfig{
		ID: "unreachable-llm", Name: "Unreachable LLM", TriggerTag: trigger,
		GenerateTitles: &on, GenerateTags: &off, GenerateCorrespondents: &off,
		GenerateCreatedDate: &off, GenerateDocumentTypes: &off, GenerateCustomFields: &off,
	})

	client := &approvalClient{recordingClient{taggedDocuments: map[string][]Document{
		trigger: {{ID: 51, Title: "scan_0051.pdf", Content: "ACME invoice", Tags: []string{trigger}}},
	}}}
	llm := &mockLLM{Error: fmt.Errorf("all retry attempts failed, last error: %w",
		fmt.Errorf("Ollama chat request: %w", dialRefused()))}
	app := &App{Client: client, LLM: llm}

	for round := 1; round <= 3; round++ {
		_, err := app.processAutoTagDocuments(context.Background())
		require.Error(t, err, "round %d should surface the connection error", round)
		assert.True(t, isUnreachable(err), "round %d: the network error must survive the wrapping", round)
	}
	assert.Empty(t, client.calls, "an unreachable LLM must not fail-tag the document")

	// The same document with an LLM error that isn't a connection failure is
	// fail-tagged on the first attempt (AUTO_TAG_MAX_RETRIES=1).
	llm.Error = errors.New("model not found")
	_, err := app.processAutoTagDocuments(context.Background())
	assert.NoError(t, err)
	require.Len(t, client.calls, 1)
	assert.Equal(t, []string{failTag}, client.calls[0].SuggestedTags)
}
