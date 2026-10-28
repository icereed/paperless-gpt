package main

import (
	"errors"
	"net"
	"syscall"
)

// isUnreachable reports whether err means a service (the LLM or OCR
// provider, or paperless-ngx itself) could not be reached at all: a DNS
// lookup that failed, a connection that was refused or could not be opened,
// or a connection the other host dropped. That is not the document's fault,
// so the retry limits (OCR_MAX_RETRIES, AUTO_TAG_MAX_RETRIES) don't count it.
// A self-hosted provider such as Ollama on a desktop is off for hours at a
// time; counting those attempts fail-tagged every document consumed in the
// meantime within minutes.
//
// Timeouts of an established request (an HTTP client timeout, a provider's
// own deadline) still count: a page the model loops on ends that way too.
func isUnreachable(err error) bool {
	if err == nil {
		return false
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return true
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) && opErr.Op == "dial" {
		return true
	}
	// A host that went to sleep or off the network mid-request shows up as a
	// reset or a TCP-level timeout on the open connection.
	for _, errno := range []syscall.Errno{
		syscall.ECONNREFUSED, syscall.ECONNRESET, syscall.EHOSTUNREACH,
		syscall.ENETUNREACH, syscall.ETIMEDOUT,
	} {
		if errors.Is(err, errno) {
			return true
		}
	}
	return false
}
