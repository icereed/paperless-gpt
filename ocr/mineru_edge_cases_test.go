package ocr

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMinerU_EntitiesDecodedInTextBlocks(t *testing.T) {
	text, _, err := assembleMinerUText([]byte(`{"pages":[{"blocks":[
		{"type":"text","content":"AT&amp;T pays &lt;5% &#x27;ACME&#x27;"},
		{"type":"doc_title","level":1,"content":"JANE &amp; EXAMPLE"},
		{"type":"text","content":"literal <5% stays"},
		{"type":"chart","content":"","captions":[{"content":"Q&amp;A"}]}]}]}`))
	require.NoError(t, err)
	assert.Contains(t, text, "AT&T pays <5% 'ACME'")
	assert.Contains(t, text, "# JANE & EXAMPLE")
	assert.Contains(t, text, "literal <5% stays")
	assert.Contains(t, text, "Q&A")
	assert.NotContains(t, text, "&amp;")
}

func TestMinerU_TextAroundTableKept(t *testing.T) {
	got := htmlTableToRows(`Statement period <b>Jan</b> &amp; Feb <table><tr><td>a</td><td>b</td></tr></table> Rates subject to change`)
	assert.Equal(t, "Statement period Jan & Feb\n| a | b |\nRates subject to change", got)
}

func TestMinerU_TolerantBlockDecoding(t *testing.T) {
	text, pages, err := assembleMinerUText([]byte(`{"pages":[{"blocks":[
		{"type":"paragraph_title","level":"3","content":"Numeric String"},
		{"type":"text","content":["part one","part two"]},
		{"type":"text","content":[{"content":"obj one"},{"text":"obj two"},7]},
		{"type":"text","content":null},
		{"type":"text","content":42},
		{"type":"text","level":{"x":1},"content":"after odd"},
		"not an object",
		{"type":"chart","captions":"single caption","footnotes":["f1",{"content":"f2"},5]}]}]}`))
	require.NoError(t, err)
	assert.Equal(t, 1, pages)
	for _, want := range []string{"### Numeric String", "part one\npart two", "obj one\nobj two", "after odd", "single caption", "f1", "f2"} {
		assert.Contains(t, text, want)
	}
	assert.NotContains(t, text, "42")
}

func TestMinerU_UnexpectedJobStatus(t *testing.T) {
	for _, body := range []string{`{}`, `{"status":"weird"}`} {
		polls := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			polls++
			_, _ = io.WriteString(w, body)
		}))
		p, err := newMinerUProvider(Config{MinerUURL: srv.URL, MinerUTimeout: 30})
		require.NoError(t, err)
		p.pollInterval = 5 * time.Millisecond
		_, err = p.waitForJob(context.Background(), "job_1")
		srv.Close()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unexpected status")
		assert.Equal(t, 1, polls)
	}
}

func TestMinerU_ErrorBodyIn2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/obj":
			_, _ = io.WriteString(w, `{"error":{"code":"quota","message":"out of credit"}}`)
		case "/str":
			_, _ = io.WriteString(w, `{"error":"plain failure"}`)
		case "/null":
			_, _ = io.WriteString(w, `{"error":null,"ok":true}`)
		default:
			w.WriteHeader(200)
		}
	}))
	defer srv.Close()
	p, err := newMinerUProvider(Config{MinerUURL: srv.URL})
	require.NoError(t, err)
	ctx := context.Background()
	_, err = p.request(ctx, "upload", http.MethodPost, srv.URL+"/obj", nil, nil, true, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "upload")
	assert.Contains(t, err.Error(), "quota: out of credit")
	_, err = p.request(ctx, "poll", http.MethodGet, srv.URL+"/str", nil, nil, true, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "plain failure")
	_, err = p.request(ctx, "x", http.MethodGet, srv.URL+"/null", nil, nil, true, false)
	assert.NoError(t, err)
	_, err = p.request(ctx, "upload", http.MethodPut, srv.URL+"/empty", nil, []byte("data"), true, false)
	assert.NoError(t, err)
}

func TestMinerU_ResolveUploadURLPathPrefix(t *testing.T) {
	p, err := newMinerUProvider(Config{MinerUURL: "https://proxy.example/mineru/v1/"})
	require.NoError(t, err)
	got, err := p.resolveUploadURL("/v1/uploads/x/content")
	require.NoError(t, err)
	assert.Equal(t, "https://proxy.example/mineru/v1/uploads/x/content", got)
	got, err = p.resolveUploadURL("https://blob.example/put?sig=1")
	require.NoError(t, err)
	assert.Equal(t, "https://blob.example/put?sig=1", got)
}

func TestMinerU_DetectMedia(t *testing.T) {
	cases := []struct {
		name, mime, file string
		data             []byte
	}{
		{"tiff-le", "image/tiff", "page.tiff", []byte("II*\x00rest")},
		{"tiff-be", "image/tiff", "page.tiff", []byte("MM\x00*rest")},
		{"webp", "image/webp", "page.webp", []byte("RIFF\x10\x00\x00\x00WEBPVP8 ")},
	}
	for _, c := range cases {
		file, mime, err := detectMinerUMedia(c.data)
		require.NoError(t, err, c.name)
		assert.Equal(t, c.file, file)
		assert.Equal(t, c.mime, mime)
	}
	for _, bad := range [][]byte{[]byte("garbage"), []byte("RIFF\x10\x00\x00\x00WAVEfmt "), nil} {
		_, _, err := detectMinerUMedia(bad)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported input format")
	}

	// No HTTP call is made for unsupported input.
	f := newFakeMinerU(t)
	_, err := f.provider("").ProcessImage(context.Background(), []byte("garbage"), 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported input format")
	assert.Equal(t, 0, len(f.authSeen))
}

func TestRedactURL(t *testing.T) {
	assert.Equal(t, "http://host:8000/mineru", redactURL("http://user:pass@host:8000/mineru"))
	assert.Equal(t, "http://host:8000", redactURL("http://host:8000"))
	assert.Equal(t, "<invalid url>", redactURL("://bad url"))
	assert.Equal(t, "<invalid url>", redactURL("user:pass@nohost"))
}
