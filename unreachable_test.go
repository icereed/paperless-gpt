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

	"github.com/stretchr/testify/assert"
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
