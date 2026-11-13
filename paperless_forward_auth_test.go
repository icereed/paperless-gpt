package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakePaperless answers /api/ui_settings/ like paperless-ngx: the user behind
// the sessionid cookie or the token, 401/403 otherwise.
type fakePaperless struct {
	*httptest.Server
	calls     atomic.Int32
	lastReq   atomic.Pointer[http.Request]
	status    int  // forces a status when set
	forbidAll bool // 403 on every endpoint, e.g. a changed paperless auth setup
	delay     time.Duration
}

// The "user" objects as /api/ui_settings/ reports them.
var fakePaperlessUsers = map[string]map[string]any{
	"sessionid=super": {"username": "root", "is_staff": true, "is_superuser": true},
	// The "Admin" checkbox in paperless-ngx; it is no superuser.
	"sessionid=staff": {"username": "admin", "is_staff": true, "is_superuser": false},
	"sessionid=plain": {"username": "jane", "is_staff": false, "is_superuser": false},
	"Token super":     {"username": "root", "is_staff": true, "is_superuser": true},
	// paperless keeps the flags apart: a superuser without the staff flag.
	"sessionid=superonly": {"username": "su", "is_staff": false, "is_superuser": true},
}

func newFakePaperless(t *testing.T) *fakePaperless {
	t.Helper()
	f := &fakePaperless{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.calls.Add(1)
		f.lastReq.Store(r.Clone(r.Context()))
		time.Sleep(f.delay)
		if f.status != 0 {
			w.WriteHeader(f.status)
			return
		}
		cookie, token := r.Header.Get("Cookie"), r.Header.Get("Authorization")
		user, ok := fakePaperlessUsers[cookie]
		if !ok {
			user, ok = fakePaperlessUsers[token]
		}
		// Signed in, but without the view_uisettings permission.
		noPerm := cookie == "sessionid=noperm" || f.forbidAll
		switch {
		case f.forbidAll:
			w.WriteHeader(http.StatusForbidden)
		case r.URL.Path == "/api/profile/" && (ok || noPerm):
			_ = json.NewEncoder(w).Encode(map[string]any{"email": ""})
		case r.URL.Path == "/api/ui_settings/" && noPerm:
			w.WriteHeader(http.StatusForbidden)
		case r.URL.Path == "/api/ui_settings/" && ok:
			_ = json.NewEncoder(w).Encode(map[string]any{"settings": map[string]any{}, "user": user})
		case r.URL.Path == "/api/ui_settings/" || r.URL.Path == "/api/profile/":
			w.WriteHeader(http.StatusUnauthorized)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(f.Close)
	return f
}

func newAuthRouter(t *testing.T, level forwardAuthLevel, f *fakePaperless) (*gin.Engine, *forwardAuth) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	auth := newForwardAuth(level, f.URL+"/", "https://paperless.example", http.DefaultTransport)
	router := gin.New()
	router.Use(auth.middleware())
	ok := func(c *gin.Context) { c.String(http.StatusOK, "reached") }
	router.GET("/", ok)
	router.GET("/api/version", ok)
	router.POST("/api/prompts", ok)
	return router, auth
}

func serve(router http.Handler, method, target string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestParseForwardAuthLevel(t *testing.T) {
	for raw, want := range map[string]forwardAuthLevel{
		"": forwardAuthOff, "OFF": forwardAuthOff,
		"user": forwardAuthUser, " Superuser ": forwardAuthSuperuser,
	} {
		got, err := parseForwardAuthLevel(raw)
		require.NoError(t, err, raw)
		assert.Equal(t, want, got, raw)
	}
	// A typo must not silently leave the app open. "staff" is no level:
	// the flag does not show paperless-ngx users other people's documents.
	for _, raw := range []string{"admin", "staff", "true", "false"} {
		_, err := parseForwardAuthLevel(raw)
		assert.Error(t, err, raw)
	}
}

func TestForwardAuthLevels(t *testing.T) {
	cases := []struct {
		level  forwardAuthLevel
		cookie string
		want   int
	}{
		{forwardAuthSuperuser, "sessionid=super", http.StatusOK},
		{forwardAuthSuperuser, "sessionid=superonly", http.StatusOK},
		{forwardAuthSuperuser, "sessionid=staff", http.StatusForbidden},
		{forwardAuthSuperuser, "sessionid=plain", http.StatusForbidden},
		{forwardAuthUser, "sessionid=plain", http.StatusOK},
		{forwardAuthSuperuser, "sessionid=noperm", http.StatusForbidden},
		{forwardAuthUser, "sessionid=noperm", http.StatusOK},
		{forwardAuthUser, "sessionid=forged", http.StatusUnauthorized},
		{forwardAuthUser, "", http.StatusUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.level.String()+"/"+tc.cookie, func(t *testing.T) {
			router, _ := newAuthRouter(t, tc.level, newFakePaperless(t))
			rec := serve(router, http.MethodGet, "/api/version", map[string]string{"Cookie": tc.cookie})
			assert.Equal(t, tc.want, rec.Code)
			assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
			if tc.want == http.StatusOK {
				assert.Equal(t, "reached", rec.Body.String())
			} else {
				assert.NotContains(t, rec.Body.String(), "reached")
			}
		})
	}
}

// Hashed bundles may stay in the browser; pages, the API and denials must not.
func TestForwardAuthCacheControl(t *testing.T) {
	router, _ := newAuthRouter(t, forwardAuthSuperuser, newFakePaperless(t))
	router.GET("/assets/*filepath", func(c *gin.Context) { c.String(http.StatusOK, "reached") })
	super := map[string]string{"Cookie": "sessionid=super"}

	t.Run("assets are kept", func(t *testing.T) {
		rec := serve(router, http.MethodGet, "/assets/index-abc123.js", super)
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "private, max-age=31536000, immutable", rec.Header().Get("Cache-Control"))
	})

	t.Run("pages are not", func(t *testing.T) {
		rec := serve(router, http.MethodGet, "/", super)
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	})

	t.Run("denied assets are not", func(t *testing.T) {
		rec := serve(router, http.MethodGet, "/assets/index-abc123.js", nil)
		assert.Equal(t, http.StatusFound, rec.Code)
		assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	})
}

// The image handlers set their own browser cache policy; behind forward auth
// the middleware's no-store must survive them.
func TestForwardAuthImageRoutesStayUncached(t *testing.T) {
	router, _ := newAuthRouter(t, forwardAuthSuperuser, newFakePaperless(t))
	app := &App{Client: &mockPaperlessClient{}}
	router.GET("/api/documents/:id/thumb", app.getDocumentThumbnailHandler)
	router.GET("/api/documents/:id/pages/:pageIndex/image", app.getDocumentPageImageHandler)

	for _, path := range []string{"/api/documents/1/thumb", "/api/documents/1/pages/0/image"} {
		t.Run(path, func(t *testing.T) {
			rec := serve(router, http.MethodGet, path, map[string]string{"Cookie": "sessionid=super"})
			assert.Equal(t, http.StatusOK, rec.Code)
			assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
		})
	}

	t.Run("without forward auth the handlers' policy applies", func(t *testing.T) {
		gin.SetMode(gin.TestMode)
		plain := gin.New()
		plain.GET("/api/documents/:id/thumb", app.getDocumentThumbnailHandler)
		rec := serve(plain, http.MethodGet, "/api/documents/1/thumb", nil)
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "private, max-age=3600", rec.Header().Get("Cache-Control"))
	})
}

func TestForwardAuthForwardsOnlyVisitorCredentials(t *testing.T) {
	f := newFakePaperless(t)
	router, _ := newAuthRouter(t, forwardAuthSuperuser, f)

	rec := serve(router, http.MethodGet, "/api/version", map[string]string{"Authorization": "Token super"})
	assert.Equal(t, http.StatusOK, rec.Code)
	req := f.lastReq.Load()
	require.NotNil(t, req)
	assert.Equal(t, "Token super", req.Header.Get("Authorization"))
	assert.Empty(t, req.Header.Get("Cookie"))

	// No credentials: paperless is not even asked, so its own token can't leak in.
	before := f.calls.Load()
	rec = serve(router, http.MethodGet, "/api/version", nil)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Equal(t, before, f.calls.Load())
}

func TestForwardAuthUnauthenticatedAnswers(t *testing.T) {
	router, _ := newAuthRouter(t, forwardAuthSuperuser, newFakePaperless(t))

	t.Run("api gets JSON with the login URL", func(t *testing.T) {
		rec := serve(router, http.MethodGet, "/api/version", nil)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
		var body map[string]string
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		assert.Equal(t, "https://paperless.example/accounts/login/", body["login_url"])
		// A browser must not pop up a Basic Auth dialog.
		assert.Empty(t, rec.Header().Get("WWW-Authenticate"))
	})

	t.Run("pages redirect to the paperless login and back", func(t *testing.T) {
		rec := serve(router, http.MethodGet, "/?tab=x", map[string]string{"X-Forwarded-Prefix": "/gpt"})
		assert.Equal(t, http.StatusFound, rec.Code)
		loc, err := url.Parse(rec.Header().Get("Location"))
		require.NoError(t, err)
		assert.Equal(t, "paperless.example", loc.Host)
		assert.Equal(t, "/accounts/login/", loc.Path)
		assert.Equal(t, "/gpt/?tab=x", loc.Query().Get("next"))
	})

	t.Run("bogus forwarded prefix is ignored", func(t *testing.T) {
		for _, prefix := range []string{"//evil.example", "https://evil.example", "/x\\y"} {
			rec := serve(router, http.MethodGet, "/", map[string]string{"X-Forwarded-Prefix": prefix})
			loc, err := url.Parse(rec.Header().Get("Location"))
			require.NoError(t, err)
			assert.Equal(t, "/", loc.Query().Get("next"), prefix)
		}
	})

	t.Run("signed in without the role gets a notice page", func(t *testing.T) {
		rec := serve(router, http.MethodGet, "/", map[string]string{"Cookie": "sessionid=staff"})
		assert.Equal(t, http.StatusForbidden, rec.Code)
		assert.Contains(t, rec.Header().Get("Content-Type"), "text/html")
		assert.Contains(t, rec.Body.String(), "<strong>admin</strong>")
		assert.Contains(t, rec.Body.String(), "superuser")
	})
}

// Standalone on its own host without PAPERLESS_PUBLIC_URL, the host-relative
// login link points back at paperless-gpt; it must not redirect in a loop.
func TestForwardAuthLoginLinkToItselfDoesNotLoop(t *testing.T) {
	auth := newForwardAuth(forwardAuthSuperuser, "http://127.0.0.1:1", "", http.DefaultTransport)
	router := gin.New()
	router.Use(auth.middleware())

	rec := serve(router, http.MethodGet, "/history", nil)
	require.Equal(t, http.StatusFound, rec.Code)
	assert.Equal(t, "/accounts/login/?next=%2Fhistory", rec.Header().Get("Location"))

	rec = serve(router, http.MethodGet, rec.Header().Get("Location"), nil)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), "PAPERLESS_PUBLIC_URL")
}

// A 403 alone must not count as "signed in": if paperless ever answered
// anonymous requests with 403, user level would otherwise let everyone in.
func TestForwardAuthForbiddenEverywhereIsNotSignedIn(t *testing.T) {
	f := newFakePaperless(t)
	f.forbidAll = true
	router, _ := newAuthRouter(t, forwardAuthUser, f)
	rec := serve(router, http.MethodGet, "/api/version", map[string]string{"Cookie": "sessionid=whatever"})
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestForwardAuthHTMLEscapesUsername(t *testing.T) {
	auth := newForwardAuth(forwardAuthSuperuser, "http://p", "", http.DefaultTransport)
	page := auth.forbiddenPage(&paperlessUser{Username: "<script>x</script>"})
	assert.NotContains(t, page, "<script>x")
	assert.Contains(t, page, `href="/accounts/login/"`)
}

func TestForwardAuthRejectsCrossOriginWrites(t *testing.T) {
	router, _ := newAuthRouter(t, forwardAuthSuperuser, newFakePaperless(t))
	session := map[string]string{"Cookie": "sessionid=super"}
	with := func(extra map[string]string) map[string]string {
		h := map[string]string{}
		for k, v := range session {
			h[k] = v
		}
		for k, v := range extra {
			h[k] = v
		}
		return h
	}

	for name, headers := range map[string]map[string]string{
		"same-site":                   {"Sec-Fetch-Site": "same-site"},
		"cross-site":                  {"Sec-Fetch-Site": "cross-site"},
		"old browser, foreign origin": {"Origin": "https://evil.example"},
	} {
		rec := serve(router, http.MethodPost, "http://gpt.example/api/prompts", with(headers))
		assert.Equal(t, http.StatusForbidden, rec.Code, name)
		assert.NotContains(t, rec.Body.String(), "reached", name)
	}

	for name, headers := range map[string]map[string]string{
		"same-origin":              {"Sec-Fetch-Site": "same-origin"},
		"old browser, same origin": {"Origin": "http://gpt.example"},
		"non-browser client":       {},
	} {
		rec := serve(router, http.MethodPost, "http://gpt.example/api/prompts", with(headers))
		assert.Equal(t, http.StatusOK, rec.Code, name)
	}

	// Reads stay possible cross-site (e.g. a link from paperless-ngx).
	rec := serve(router, http.MethodGet, "/api/version", with(map[string]string{"Sec-Fetch-Site": "cross-site"}))
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestForwardAuthCache(t *testing.T) {
	f := newFakePaperless(t)
	router, auth := newAuthRouter(t, forwardAuthSuperuser, f)
	now := time.Unix(1_000_000, 0)
	auth.now = func() time.Time { return now }
	headers := map[string]string{"Cookie": "sessionid=super"}

	for i := 0; i < 3; i++ {
		assert.Equal(t, http.StatusOK, serve(router, http.MethodGet, "/api/version", headers).Code)
	}
	assert.EqualValues(t, 1, f.calls.Load())

	// After the TTL paperless is asked again, so a logout takes effect.
	now = now.Add(forwardAuthCacheTTL)
	assert.Equal(t, http.StatusOK, serve(router, http.MethodGet, "/api/version", headers).Code)
	assert.EqualValues(t, 2, f.calls.Load())

	// Another session is checked on its own, never served from the cache.
	rec := serve(router, http.MethodGet, "/api/version", map[string]string{"Cookie": "sessionid=staff"})
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.EqualValues(t, 3, f.calls.Load())
}

func TestForwardAuthPaperlessDown(t *testing.T) {
	f := newFakePaperless(t)
	f.status = http.StatusInternalServerError
	router, _ := newAuthRouter(t, forwardAuthSuperuser, f)

	rec := serve(router, http.MethodGet, "/api/version", map[string]string{"Cookie": "sessionid=super"})
	assert.Equal(t, http.StatusBadGateway, rec.Code)

	// Errors are not cached: the next request asks again.
	f.status = 0
	rec = serve(router, http.MethodGet, "/api/version", map[string]string{"Cookie": "sessionid=super"})
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestForwardAuthDoesNotFollowRedirects(t *testing.T) {
	var leaked atomic.Bool
	elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked.Store(true)
	}))
	defer elsewhere.Close()
	redirecting := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL+"/api/ui_settings/", http.StatusFound)
	}))
	defer redirecting.Close()

	gin.SetMode(gin.TestMode)
	auth := newForwardAuth(forwardAuthSuperuser, redirecting.URL, "", http.DefaultTransport)
	router := gin.New()
	router.Use(auth.middleware())
	router.GET("/api/version", func(c *gin.Context) { c.Status(http.StatusOK) })

	rec := serve(router, http.MethodGet, "/api/version", map[string]string{"Cookie": "sessionid=super"})
	assert.Equal(t, http.StatusBadGateway, rec.Code)
	assert.False(t, leaked.Load())
}

func TestForwardAuthSharesConcurrentChecks(t *testing.T) {
	f := newFakePaperless(t)
	f.delay = 100 * time.Millisecond
	router, _ := newAuthRouter(t, forwardAuthSuperuser, f)

	var wg sync.WaitGroup
	codes := make([]int, 8)
	for i := range codes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes[i] = serve(router, http.MethodGet, "/api/version", map[string]string{"Cookie": "sessionid=super"}).Code
		}()
	}
	wg.Wait()
	for _, code := range codes {
		assert.Equal(t, http.StatusOK, code)
	}
	assert.EqualValues(t, 1, f.calls.Load())
}

func TestForwardAuthCancelledRequestDoesNotFailOthers(t *testing.T) {
	f := newFakePaperless(t)
	f.delay = 100 * time.Millisecond
	_, auth := newAuthRouter(t, forwardAuthSuperuser, f)

	ctx, cancel := context.WithCancel(context.Background())
	first := httptest.NewRequest(http.MethodGet, "/api/version", nil).WithContext(ctx)
	first.Header.Set("Cookie", "sessionid=super")
	second := httptest.NewRequest(http.MethodGet, "/api/version", nil)
	second.Header.Set("Cookie", "sessionid=super")

	done := make(chan error, 1)
	go func() { _, err := auth.lookup(first); done <- err }()
	time.Sleep(20 * time.Millisecond)
	cancel()
	user, err := auth.lookup(second)
	require.NoError(t, err)
	require.NotNil(t, user)
	assert.True(t, user.IsSuperuser)
	<-done
}
