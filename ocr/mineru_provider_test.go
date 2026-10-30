package ocr

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testStructured = `{
 "pages":[
  {"page_idx":0,"blocks":[
   {"type":"header","bbox":[0,0,1,1],"content":"ACME BANK"},
   {"type":"header","bbox":[0,0,1,1],"content":"Gold Card\nJANE EXAMPLE\nAccount Ending 1-23456"},
   {"type":"doc_title","bbox":[0,0,1,1],"level":1,"content":"Statement"},
   {"type":"paragraph_title","bbox":[0,0,1,1],"level":2,"content":"Account Summary"},
   {"type":"paragraph_title","bbox":[0,0,1,1],"content":"No Level"},
   {"type":"text","bbox":[0,0,1,1],"content":"Hello world."},
   {"type":"table","bbox":[0,0,1,1],"content":"| New Balance | $12.00 |\n| --- | --- |","captions":[],"footnotes":[],"image_source":"data:image/jpeg;base64,AAAA"},
   {"type":"image","bbox":[0,0,1,1],"content":"","captions":[],"footnotes":[],"image_source":"data:image/jpeg;base64,/9j/4AAQ"},
   {"type":"chart","bbox":[0,0,1,1],"content":"","captions":[{"bbox":[0,0,1,1],"content":"Asset Mix"}],"footnotes":[{"bbox":[0,0,1,1],"content":"Source: ACME"}],"image_source":"data:image/jpeg;base64,/9j/4AAQ"}
  ]},
  {"page_idx":1,"blocks":[
   {"type":"table","bbox":[0,0,1,1],"content":"<table><tr><td colspan=\"2\">Pay In Full</td></tr><tr><td>01/05/26</td><td>JOE&#x27;S HARDWARE &amp; GARDEN</td><td>$9.96</td></tr><tr><td><img src=\"data:image/jpeg;base64,/9j/4AAQ\"></td><td>Care 1-800-555-0100</td></tr></table>","image_source":"data:image/jpeg;base64,AAAA"},
   {"type":"text","bbox":[0,0,1,1],"content":"inline ![x](data:image/png;base64,iVBOR) text <img src=\"data:image/png;base64,zzzz\"> end"},
   {"type":"aside_text","bbox":[0,0,1,1],"content":"ACI-1 12/31/22"},
   {"type":"mystery_type","bbox":[0,0,1,1],"content":"unknown content"},
   {"type":"index","bbox":[0,0,1,1],"content":"- 1a Total 58.10"},
   {"type":"ref_text","bbox":[0,0,1,1],"content":"[1] Ref"},
   {"type":"footer","bbox":[0,0,1,1],"content":"Continued on reverse"},
   {"type":"page_number","bbox":[0,0,1,1],"content":"p. 2/2"}
  ]}
 ],
 "metadata":{"file_suffix":"pdf"},"is_full_document":true}`

type fakeMinerU struct {
	t   *testing.T
	srv *httptest.Server

	mu            sync.Mutex
	dedupe        bool
	relativeURL   bool
	uploadHost    string // if set, upload_url points at this base
	jobStatuses   []string
	fileStatus    string
	fileError     string
	redirectTo    string // download redirect target (full URL)
	structured    string
	pollCount     int
	jobsCreated   int
	jobBody       map[string]interface{}
	createBody    map[string]interface{}
	putBody       []byte
	putHeaders    http.Header
	authSeen      map[string]string // path -> Authorization
	failCreate    bool
	redirectAuth  string
	downloadAuthR string
	onPoll        func()      // called (without f.mu held) before each poll is answered
	cancelCount   int         // DELETE /v1/parse/jobs/job_1 requests received
	cancelStatus  int         // response code for DELETE; 0 means 200
	pollErrors    map[int]int // poll index -> HTTP status to answer with instead
}

// counts returns the poll and cancel counters under the lock.
func (f *fakeMinerU) counts() (polls, cancels int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.pollCount, f.cancelCount
}

func newFakeMinerU(t *testing.T) *fakeMinerU {
	f := &fakeMinerU{t: t, jobStatuses: []string{"queued", "running", "completed"}, fileStatus: "completed",
		structured: testStructured, authSeen: map[string]string{}}
	f.srv = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeMinerU) writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (f *fakeMinerU) handle(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet && r.URL.Path == "/v1/parse/jobs/job_1" {
		f.mu.Lock()
		hook := f.onPoll
		f.mu.Unlock()
		if hook != nil {
			hook()
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.authSeen[r.Method+" "+r.URL.Path] = r.Header.Get("Authorization")
	p := r.URL.Path
	switch {
	case r.Method == http.MethodPost && p == "/v1/uploads":
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.createBody = body
		if f.failCreate {
			f.writeJSON(w, 401, map[string]interface{}{"error": map[string]interface{}{"code": "unauthorized", "message": "bad key", "param": nil}})
			return
		}
		if f.dedupe {
			f.writeJSON(w, 200, map[string]interface{}{"id": "upload_1", "status": "completed", "upload_url": nil, "file": map[string]interface{}{"id": "file-dedupe"}})
			return
		}
		uploadURL := f.srv.URL + "/v1/uploads/upload_1/content"
		if f.relativeURL {
			uploadURL = "/v1/uploads/upload_1/content"
		}
		if f.uploadHost != "" {
			uploadURL = f.uploadHost + "/v1/uploads/upload_1/content"
		}
		f.writeJSON(w, 200, map[string]interface{}{"id": "upload_1", "status": "pending", "upload_url": uploadURL, "upload_method": "PUT",
			"upload_headers": map[string]string{"Content-Type": body["mime_type"].(string), "X-Test": "yes"}, "file": nil})
	case r.Method == http.MethodPut && p == "/v1/uploads/upload_1/content":
		f.putBody, _ = io.ReadAll(r.Body)
		f.putHeaders = r.Header.Clone()
		w.WriteHeader(200)
	case r.Method == http.MethodPost && p == "/v1/uploads/upload_1/complete":
		f.writeJSON(w, 200, map[string]interface{}{"id": "upload_1", "status": "completed", "file": map[string]interface{}{"id": "file-up"}})
	case r.Method == http.MethodPost && p == "/v1/parse/jobs":
		f.jobsCreated++
		_ = json.NewDecoder(r.Body).Decode(&f.jobBody)
		f.writeJSON(w, 202, map[string]interface{}{"job_id": "job_1", "status": "queued"})
	case r.Method == http.MethodDelete && p == "/v1/parse/jobs/job_1":
		f.cancelCount++
		if f.cancelStatus != 0 && f.cancelStatus != 200 {
			f.writeJSON(w, f.cancelStatus, map[string]interface{}{"error": map[string]interface{}{"code": "cancel_failed", "message": "nope"}})
			return
		}
		f.writeJSON(w, 200, map[string]interface{}{"job_id": "job_1", "status": "canceled", "canceled_at": "2026-01-01T00:00:00Z"})
	case r.Method == http.MethodGet && p == "/v1/parse/jobs/job_1":
		if code, ok := f.pollErrors[f.pollCount]; ok {
			f.pollCount++
			f.writeJSON(w, code, map[string]interface{}{"error": map[string]interface{}{"code": "poll_error", "message": http.StatusText(code)}})
			return
		}
		idx := f.pollCount
		if idx >= len(f.jobStatuses) {
			idx = len(f.jobStatuses) - 1
		}
		f.pollCount++
		st := f.jobStatuses[idx]
		file := map[string]interface{}{"file_id": "file-up", "status": "queued", "error": nil, "output_files": map[string]interface{}{"structured_content": nil}}
		if st != "queued" && st != "running" {
			file["status"] = f.fileStatus
			if f.fileStatus == "completed" {
				file["output_files"] = map[string]interface{}{"structured_content": map[string]interface{}{"file_id": "file-out", "bytes": 10}}
			}
			if f.fileError != "" {
				file["error"] = map[string]interface{}{"code": "parse_failed", "message": f.fileError}
			}
		}
		f.writeJSON(w, 200, map[string]interface{}{"job_id": "job_1", "status": st, "files": []interface{}{file}})
	case r.Method == http.MethodGet && p == "/v1/files/file-out/content":
		if f.redirectTo != "" {
			http.Redirect(w, r, f.redirectTo, http.StatusFound)
			return
		}
		_, _ = io.WriteString(w, f.structured)
	case r.Method == http.MethodGet && p == "/blob/final":
		f.downloadAuthR = r.Header.Get("Authorization")
		_, _ = io.WriteString(w, f.structured)
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeMinerU) provider(key string) *MinerUProvider {
	p, err := newMinerUProvider(Config{MinerUURL: f.srv.URL, MinerUAPIKey: key})
	require.NoError(f.t, err)
	p.pollInterval = 5 * time.Millisecond
	p.retryWait = time.Millisecond
	return p
}

var testPDF = []byte("%PDF-1.4 synthetic")

func TestMinerU_FullFlow(t *testing.T) {
	f := newFakeMinerU(t)
	res, err := f.provider("sekret").ProcessImage(context.Background(), testPDF, 0)
	require.NoError(t, err)

	assert.Nil(t, res.HOCRPage)
	assert.Equal(t, "mineru", res.Metadata["provider"])
	assert.Equal(t, "standard", res.Metadata["tier"])
	assert.Equal(t, "2", res.Metadata["page_count"])

	assert.Equal(t, "document.pdf", f.createBody["filename"])
	assert.Equal(t, "application/pdf", f.createBody["mime_type"])
	assert.Equal(t, "parse", f.createBody["purpose"])
	assert.EqualValues(t, len(testPDF), f.createBody["bytes"])
	sum := sha256.Sum256(testPDF)
	assert.Equal(t, hex.EncodeToString(sum[:]), f.createBody["sha256sum"])

	assert.Equal(t, testPDF, f.putBody)
	assert.Equal(t, "application/pdf", f.putHeaders.Get("Content-Type"))
	assert.Equal(t, "yes", f.putHeaders.Get("X-Test"))

	files := f.jobBody["files"].([]interface{})
	src := files[0].(map[string]interface{})["source"].(map[string]interface{})
	assert.Equal(t, "file_id", src["type"])
	assert.Equal(t, "file-up", src["file_id"])
	assert.Equal(t, []interface{}{"structured_content"}, f.jobBody["output_formats"])
	assert.Equal(t, "standard", f.jobBody["tier"])
	assert.Equal(t, 3, f.pollCount)
	assert.Equal(t, 1, f.jobsCreated)
}

func TestMinerU_Dedupe(t *testing.T) {
	f := newFakeMinerU(t)
	f.dedupe = true
	res, err := f.provider("").ProcessImage(context.Background(), testPDF, 0)
	require.NoError(t, err)
	assert.Contains(t, res.Text, "Hello world.")
	assert.Nil(t, f.putBody)
	src := f.jobBody["files"].([]interface{})[0].(map[string]interface{})["source"].(map[string]interface{})
	assert.Equal(t, "file-dedupe", src["file_id"])
	_, completed := f.authSeen["POST /v1/uploads/upload_1/complete"]
	assert.False(t, completed)
}

func TestMinerU_BearerAuth(t *testing.T) {
	f := newFakeMinerU(t)
	_, err := f.provider("sekret").ProcessImage(context.Background(), testPDF, 0)
	require.NoError(t, err)
	for k, v := range f.authSeen {
		assert.Equal(t, "Bearer sekret", v, k)
	}
	assert.Len(t, f.authSeen, 6)

	f2 := newFakeMinerU(t)
	_, err = f2.provider("").ProcessImage(context.Background(), testPDF, 0)
	require.NoError(t, err)
	for k, v := range f2.authSeen {
		assert.Equal(t, "", v, k)
	}
}

func TestMinerU_NoKeyToOtherOrigin(t *testing.T) {
	var gotAuth, gotBody string
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(200)
	}))
	defer other.Close()

	f := newFakeMinerU(t)
	f.uploadHost = other.URL
	_, err := f.provider("sekret").ProcessImage(context.Background(), testPDF, 0)
	require.NoError(t, err)
	assert.Equal(t, "", gotAuth)
	assert.Equal(t, string(testPDF), gotBody)
	assert.Equal(t, "Bearer sekret", f.authSeen["POST /v1/uploads/upload_1/complete"])
}

func TestMinerU_RelativeUploadURL(t *testing.T) {
	f := newFakeMinerU(t)
	f.relativeURL = true
	_, err := f.provider("sekret").ProcessImage(context.Background(), testPDF, 0)
	require.NoError(t, err)
	assert.Equal(t, testPDF, f.putBody)
	assert.Equal(t, "Bearer sekret", f.authSeen["PUT /v1/uploads/upload_1/content"])
}

func TestMinerU_JobFailed(t *testing.T) {
	f := newFakeMinerU(t)
	f.jobStatuses = []string{"running", "failed"}
	f.fileStatus = "failed"
	f.fileError = "corrupt pdf xref"
	_, err := f.provider("").ProcessImage(context.Background(), testPDF, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed")
	assert.Contains(t, err.Error(), "corrupt pdf xref")
	assert.Contains(t, err.Error(), "parse_failed")
}

func TestMinerU_FileErrorOnCompletedJob(t *testing.T) {
	f := newFakeMinerU(t)
	f.jobStatuses = []string{"completed"}
	f.fileStatus = "failed"
	f.fileError = "bad page"
	_, err := f.provider("").ProcessImage(context.Background(), testPDF, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bad page")
}

func TestMinerU_PartialAndCanceled(t *testing.T) {
	for _, st := range []string{"partial", "canceled"} {
		f := newFakeMinerU(t)
		f.jobStatuses = []string{st}
		f.fileStatus = "failed"
		_, err := f.provider("").ProcessImage(context.Background(), testPDF, 0)
		require.Error(t, err, st)
		assert.Contains(t, err.Error(), st)
	}
}

func TestMinerU_CreateErrorSurfaced(t *testing.T) {
	f := newFakeMinerU(t)
	f.failCreate = true
	_, err := f.provider("").ProcessImage(context.Background(), testPDF, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "upload")
	assert.Contains(t, err.Error(), "401")
	assert.Contains(t, err.Error(), "unauthorized: bad key")
}

func TestMinerU_StringErrorBody(t *testing.T) {
	assert.Equal(t, "boom", describeMinerUError([]byte(`{"error":"boom"}`)))
	assert.Equal(t, "c: m", describeMinerUError([]byte(`{"error":{"code":"c","message":"m","param":null}}`)))
}

func TestMinerU_Timeout(t *testing.T) {
	f := newFakeMinerU(t)
	f.jobStatuses = []string{"running"}
	p := f.provider("")
	p.timeout = 100 * time.Millisecond
	start := time.Now()
	_, err := p.ProcessImage(context.Background(), testPDF, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "deadline exceeded")
	assert.Less(t, time.Since(start), 3*time.Second)
}

func TestMinerU_ContextCancel(t *testing.T) {
	f := newFakeMinerU(t)
	f.jobStatuses = []string{"running"}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	_, err := f.provider("").ProcessImage(ctx, testPDF, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "canceled")
}

func TestMinerU_ImageMime(t *testing.T) {
	cases := []struct {
		name, mime, file string
		data             []byte
	}{
		{"jpeg", "image/jpeg", "page.jpg", []byte{0xFF, 0xD8, 0xFF, 0xE0, 1, 2}},
		{"png", "image/png", "page.png", []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A}},
		{"pdf", "application/pdf", "document.pdf", testPDF},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFakeMinerU(t)
			_, err := f.provider("").ProcessImage(context.Background(), c.data, 1)
			require.NoError(t, err)
			assert.Equal(t, c.mime, f.createBody["mime_type"])
			assert.Equal(t, c.file, f.createBody["filename"])
			assert.Equal(t, c.mime, f.putHeaders.Get("Content-Type"))
			assert.Equal(t, c.data, f.putBody)
		})
	}
}

func TestMinerU_DownloadRedirect(t *testing.T) {
	var otherAuth string
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		otherAuth = r.Header.Get("Authorization")
		_, _ = io.WriteString(w, testStructured)
	}))
	defer other.Close()

	// Same-origin redirect keeps the key.
	f := newFakeMinerU(t)
	f.redirectTo = "/blob/final"
	res, err := f.provider("sekret").ProcessImage(context.Background(), testPDF, 0)
	require.NoError(t, err)
	assert.Contains(t, res.Text, "Hello world.")
	assert.Equal(t, "Bearer sekret", f.downloadAuthR)

	// Cross-origin redirect (different port on the same host) drops it.
	f2 := newFakeMinerU(t)
	f2.redirectTo = other.URL + "/blob"
	res, err = f2.provider("sekret").ProcessImage(context.Background(), testPDF, 0)
	require.NoError(t, err)
	assert.Contains(t, res.Text, "Hello world.")
	assert.Equal(t, "", otherAuth)
}

func TestMinerU_DecodeError(t *testing.T) {
	f := newFakeMinerU(t)
	f.structured = "not json"
	_, err := f.provider("").ProcessImage(context.Background(), testPDF, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decode")
}

func TestMinerU_TextAssembly(t *testing.T) {
	text, pages, err := assembleMinerUText([]byte(testStructured))
	require.NoError(t, err)
	assert.Equal(t, 2, pages)

	for _, want := range []string{
		"ACME BANK", "Gold Card\nJANE EXAMPLE\nAccount Ending 1-23456",
		"# Statement", "## Account Summary", "## No Level",
		"Hello world.", "| New Balance | $12.00 |\n| --- | --- |",
		"Asset Mix", "Source: ACME",
		"| Pay In Full |",
		"| 01/05/26 | JOE'S HARDWARE & GARDEN | $9.96 |",
		"|  | Care 1-800-555-0100 |",
		"inline  text  end",
		"ACI-1 12/31/22", "unknown content", "- 1a Total 58.10", "[1] Ref",
		"Continued on reverse", "p. 2/2",
	} {
		assert.Contains(t, text, want)
	}
	assert.NotContains(t, text, "data:")
	assert.NotContains(t, text, "<img")
	assert.NotContains(t, text, "base64")
	assert.NotContains(t, text, "<table")
	assert.NotContains(t, text, "&#x27;")
	assert.NotContains(t, text, "\n\n\n")

	// Ordering: page 1 content precedes page 2 content; blocks separated by blank lines.
	assert.Less(t, strings.Index(text, "ACME BANK"), strings.Index(text, "Hello world."))
	assert.Less(t, strings.Index(text, "Asset Mix"), strings.Index(text, "Pay In Full"))
	assert.Contains(t, text, "# Statement\n\n## Account Summary")
}

func TestMinerU_HTMLTableMalformed(t *testing.T) {
	got := htmlTableToRows(`<table><tr><td>a<td>b &amp; c<tr><td>d`)
	assert.Equal(t, "| a | b & c |\n| d |", got)
	assert.NotPanics(t, func() { htmlTableToRows(`<tr><td><b>x`) })
}

func TestNewProvider_MinerU(t *testing.T) {
	_, err := NewProvider(Config{Provider: "mineru"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "MINERU_URL")

	p, err := NewProvider(Config{Provider: "mineru", MinerUURL: "http://mineru-host:8000/v1/"})
	require.NoError(t, err)
	mp, ok := p.(*MinerUProvider)
	require.True(t, ok)
	assert.Equal(t, "http://mineru-host:8000", mp.baseURL)
	assert.Equal(t, "standard", mp.tier)
	assert.Equal(t, 600*time.Second, mp.timeout)

	p, err = NewProvider(Config{Provider: "mineru", MinerUURL: "http://h:1/", MinerUTier: "Advanced", MinerUTimeout: 30})
	require.NoError(t, err)
	assert.Equal(t, "advanced", p.(*MinerUProvider).tier)
	assert.Equal(t, 30*time.Second, p.(*MinerUProvider).timeout)

	_, err = NewProvider(Config{Provider: "mineru", MinerUURL: "http://h:1", MinerUTier: "ultra"})
	require.Error(t, err)
}

func TestMinerU_SameOrigin(t *testing.T) {
	f := func(a, b string) bool {
		u := mustURL(t, a)
		return sameOrigin(u, b)
	}
	assert.True(t, f("http://h/x", "http://h:80"))
	assert.True(t, f("https://H:443/x", "https://h"))
	assert.False(t, f("http://h:81/x", "http://h"))
	assert.False(t, f("https://h/x", "http://h"))
	assert.False(t, f("http://other/x", "http://h"))
}

func mustURL(t *testing.T, s string) *url.URL {
	u, err := url.Parse(s)
	require.NoError(t, err)
	return u
}
