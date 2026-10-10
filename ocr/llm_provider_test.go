package ocr

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
