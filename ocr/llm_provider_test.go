package ocr

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseRetryAfterHTTPDateRoundsUp(t *testing.T) {
	now := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	delay, ok := parseRetryAfter("Sat, 10 Oct 2026 00:00:02 GMT", now.Add(500*time.Millisecond))
	require.True(t, ok)
	assert.Equal(t, 1500*time.Millisecond, delay)
}

func TestParseRetryAfterHTTPDateRoundingDoesNotOverflow(t *testing.T) {
	now := time.Unix(0, 0).UTC()
	when := now.Add(time.Duration(1<<63 - 1))
	delay, ok := parseRetryAfter(when.Format(http.TimeFormat), now)
	require.True(t, ok)
	assert.Greater(t, delay, time.Duration(0))
	assert.LessOrEqual(t, delay, time.Duration(1<<63-1))
}

func TestRetryAfterDelayRejectsOverflow(t *testing.T) {
	backoff := 5 * time.Millisecond
	assert.Equal(t, backoff, retryAfterDelay(errors.New("retry-after-ms:9223372036854775807"), backoff))
	assert.Equal(t, time.Second, retryAfterDelay(errors.New("retry-after-ms:1000"), backoff))
}

func TestParseHeaderList(t *testing.T) {
	got := ParseHeaderList(" A=1, B = two words ,C=x=y,broken,=nokey,D=")
	assert.Equal(t, map[string]string{"A": "1", "B": "two words", "C": "x=y", "D": ""}, got)
	assert.Empty(t, ParseHeaderList(""))
}

func TestParseHeaderListCanonicalizesNames(t *testing.T) {
	got := ParseHeaderList("x-title=a, X-TITLE=b")
	assert.Equal(t, map[string]string{"X-Title": "b"}, got, "names differing only in case are one header")
}

func TestHeaderClientDoesNotFollowRedirectsToOtherHosts(t *testing.T) {
	var leaked string
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked = r.Header.Get("Authorization")
	}))
	defer other.Close()
	var sameHostHits int
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/moved":
			http.Redirect(w, r, "/v1", http.StatusFound) // same host: fine
		case "/v1":
			sameHostHits++
			assert.Equal(t, "Bearer secret", r.Header.Get("Authorization"))
		default:
			http.Redirect(w, r, other.URL, http.StatusFound) // another host
		}
	}))
	defer gateway.Close()

	client := headerClient(map[string]string{"Authorization": "Bearer secret"})

	resp, err := client.Get(gateway.URL + "/moved")
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, 1, sameHostHits, "redirects on the same host are followed")

	_, err = client.Get(gateway.URL + "/elsewhere")
	assert.ErrorContains(t, err, "refusing to follow a redirect")
	assert.Empty(t, leaked, "the credentials must not reach another host")
}

func TestHeaderClientPreservesRetryAfterOn429(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "2")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	resp, err := headerClient(nil).Get(server.URL)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, readErr := io.ReadAll(resp.Body)
	require.NoError(t, readErr)
	assert.Equal(t, http.StatusTooManyRequests, resp.StatusCode)
	assert.Contains(t, string(body), "retry-after-ms:2000")
}
