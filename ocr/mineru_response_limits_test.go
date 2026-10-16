package ocr

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// endlessBody is a response body that never ends and counts what is read.
type endlessBody struct{ read *atomic.Int64 }

func (b endlessBody) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'x'
	}
	b.read.Add(int64(len(p)))
	return len(p), nil
}

func (endlessBody) Close() error { return nil }

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestMinerU_ErrorBodyReadIsBounded(t *testing.T) {
	p, err := newMinerUProvider(Config{MinerUURL: "http://mineru.invalid"})
	require.NoError(t, err)
	p.retryWait = 0
	var attempts atomic.Int32
	var read atomic.Int64
	p.httpClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		attempts.Add(1)
		return &http.Response{StatusCode: http.StatusBadGateway, Header: http.Header{}, Body: endlessBody{&read}, Request: r}, nil
	})

	_, err = p.request(context.Background(), "download", http.MethodGet, p.baseURL+"/v1/files/x/content", nil, nil, true, true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 502")
	assert.Less(t, len(err.Error()), mineruMaxErrBody+200)

	n := int64(attempts.Load())
	assert.Equal(t, int64(3), n, "5xx is still retried")
	assert.LessOrEqual(t, read.Load(), n*(mineruMaxErrRead+1),
		"each error body read must stop at mineruMaxErrRead")
}

func TestMinerU_LargeErrorBodyOverHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, strings.Repeat("e", 4<<20))
	}))
	defer srv.Close()
	p, err := newMinerUProvider(Config{MinerUURL: srv.URL})
	require.NoError(t, err)

	_, err = p.request(context.Background(), "poll", http.MethodGet, srv.URL+"/v1/parse/jobs/j", nil, nil, true, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 404")
	assert.Less(t, len(err.Error()), mineruMaxErrBody+200)
}

func TestMinerU_OversizedSuccessBodyFails(t *testing.T) {
	f := newFakeMinerU(t)
	p := f.provider("")
	p.maxResponse = 1024
	require.Greater(t, len(f.structured), 1024)

	_, err := p.ProcessImage(context.Background(), testPDF, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mineru download")
	assert.Contains(t, err.Error(), "exceeds 1024 bytes")
}

func TestMinerU_SuccessBodyAtLimitAccepted(t *testing.T) {
	f := newFakeMinerU(t)
	p := f.provider("")
	p.maxResponse = int64(len(f.structured))

	res, err := p.ProcessImage(context.Background(), testPDF, 0)
	require.NoError(t, err)
	assert.Contains(t, res.Text, "Hello world.")
}
