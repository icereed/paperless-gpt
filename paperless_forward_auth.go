package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/sync/singleflight"
)

// forwardAuthLevel is the paperless-ngx account a visitor must be signed in
// with (PAPERLESS_FORWARD_AUTH). paperless-gpt has no users of its own: it
// forwards the visitor's paperless-ngx session cookie or API token to
// paperless-ngx and asks who it belongs to. See docs/adr/0003.
type forwardAuthLevel int

const (
	forwardAuthOff forwardAuthLevel = iota
	forwardAuthUser
	forwardAuthSuperuser
)

// There is deliberately no "staff" level: the paperless-ngx staff flag grants
// no access to other users' documents, while paperless-gpt shows everything
// its own (usually superuser) API token can see.
func parseForwardAuthLevel(raw string) (forwardAuthLevel, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "off":
		return forwardAuthOff, nil
	case "user":
		return forwardAuthUser, nil
	case "superuser":
		return forwardAuthSuperuser, nil
	}
	return forwardAuthOff, fmt.Errorf("invalid PAPERLESS_FORWARD_AUTH value %q (use off, superuser or user)", raw)
}

func (l forwardAuthLevel) String() string {
	switch l {
	case forwardAuthUser:
		return "user"
	case forwardAuthSuperuser:
		return "superuser"
	}
	return "off"
}

// paperlessUser is the part of paperless-ngx' /api/ui_settings/ answer we need;
// it is the only endpoint that reports is_superuser for the caller.
type paperlessUser struct {
	Username    string `json:"username"`
	IsSuperuser bool   `json:"is_superuser"`
}

func (u *paperlessUser) satisfies(level forwardAuthLevel) bool {
	if level == forwardAuthSuperuser {
		return u.IsSuperuser
	}
	return true
}

const (
	// The UI polls a lot; a short cache spares paperless-ngx without letting a
	// logout or a revoked superuser status linger.
	forwardAuthCacheTTL = 5 * time.Second
	forwardAuthCacheMax = 1000
	forwardAuthTimeout  = 10 * time.Second
)

type forwardAuthResult struct {
	user    *paperlessUser // nil: not signed in
	expires time.Time
}

type forwardAuth struct {
	level    forwardAuthLevel
	baseURL  string // how paperless-gpt reaches paperless-ngx
	loginURL string // where browsers sign in
	client   *http.Client
	csrf     *http.CrossOriginProtection
	now      func() time.Time

	// A page load fires several API calls at once; they share one check.
	inflight singleflight.Group

	mu    sync.Mutex
	cache map[[sha256.Size]byte]forwardAuthResult
}

// newForwardAuth: without a public URL the login link stays host-relative,
// which fits paperless-gpt served under a sub-path of paperless-ngx' host.
func newForwardAuth(level forwardAuthLevel, baseURL, publicURL string, transport http.RoundTripper) *forwardAuth {
	return &forwardAuth{
		level:    level,
		baseURL:  strings.TrimRight(baseURL, "/"),
		loginURL: strings.TrimRight(publicURL, "/") + "/accounts/login/",
		client: &http.Client{
			Transport: transport,
			Timeout:   forwardAuthTimeout,
			// Never carry the visitor's credentials anywhere else.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		csrf:  http.NewCrossOriginProtection(),
		now:   time.Now,
		cache: make(map[[sha256.Size]byte]forwardAuthResult),
	}
}

var errPaperlessUnavailable = errors.New("paperless-ngx did not answer the login check")

// lookup returns the signed-in paperless-ngx user, nil if the credentials are
// missing or not (any longer) valid.
func (a *forwardAuth) lookup(r *http.Request) (*paperlessUser, error) {
	cookie := r.Header.Get("Cookie")
	authorization := r.Header.Get("Authorization")
	if cookie == "" && authorization == "" {
		return nil, nil
	}

	key := sha256.Sum256([]byte(cookie + "\x00" + authorization))
	a.mu.Lock()
	hit, ok := a.cache[key]
	a.mu.Unlock()
	if ok && a.now().Before(hit.expires) {
		return hit.user, nil
	}

	// Detached from the first caller's context: the others waiting on the same
	// check must not fail because that one browser request was cancelled.
	ctx := context.WithoutCancel(r.Context())
	v, err, _ := a.inflight.Do(string(key[:]), func() (any, error) {
		user, err := a.ask(ctx, cookie, authorization)
		if err != nil {
			return nil, err
		}
		a.remember(key, user)
		return user, nil
	})
	if err != nil {
		return nil, err
	}
	return v.(*paperlessUser), nil
}

func (a *forwardAuth) remember(key [sha256.Size]byte, user *paperlessUser) {
	now := a.now()
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.cache) >= forwardAuthCacheMax {
		for k, v := range a.cache {
			if !now.Before(v.expires) {
				delete(a.cache, k)
			}
		}
		if len(a.cache) >= forwardAuthCacheMax {
			clear(a.cache)
		}
	}
	a.cache[key] = forwardAuthResult{user: user, expires: now.Add(forwardAuthCacheTTL)}
}

// ask resolves the visitor's credentials with paperless-ngx.
func (a *forwardAuth) ask(ctx context.Context, cookie, authorization string) (*paperlessUser, error) {
	status, body, err := a.get(ctx, "/api/ui_settings/", cookie, authorization, true)
	if err != nil {
		return nil, err
	}
	switch status {
	case http.StatusOK:
		var settings struct {
			User *paperlessUser `json:"user"`
		}
		if err := json.Unmarshal(body, &settings); err != nil || settings.User == nil {
			return nil, fmt.Errorf("%w: unexpected answer from /api/ui_settings/", errPaperlessUnavailable)
		}
		return settings.User, nil
	case http.StatusUnauthorized:
		return nil, nil
	case http.StatusForbidden:
		// ui_settings needs the view_uisettings permission, which a signed-in
		// account may lack; superusers hold every permission, so it is none.
		// The profile only needs a login and tells "signed in" from "not".
		// Only its status counts: the profile body contains the user's API token.
		status, _, err := a.get(ctx, "/api/profile/", cookie, authorization, false)
		if err != nil {
			return nil, err
		}
		switch status {
		case http.StatusOK:
			return &paperlessUser{}, nil
		case http.StatusUnauthorized, http.StatusForbidden:
			return nil, nil
		}
		return nil, fmt.Errorf("%w: /api/profile/ status %d", errPaperlessUnavailable, status)
	}
	return nil, fmt.Errorf("%w: /api/ui_settings/ status %d", errPaperlessUnavailable, status)
}

func (a *forwardAuth) get(ctx context.Context, path, cookie, authorization string, readBody bool) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.baseURL+path, nil)
	if err != nil {
		return 0, nil, err
	}
	// Only the visitor's credentials, never paperless-gpt's own token.
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("%w: %v", errPaperlessUnavailable, err)
	}
	defer resp.Body.Close()
	if !readBody {
		return resp.StatusCode, nil, nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return 0, nil, fmt.Errorf("%w: %v", errPaperlessUnavailable, err)
	}
	return resp.StatusCode, body, nil
}

// middleware guards every route. Pages answer unauthenticated browsers with a
// redirect to the paperless-ngx login; the API answers with JSON errors.
func (a *forwardAuth) middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Session cookies are sent along with cross-site requests too, so
		// writes must come from paperless-gpt's own pages (Sec-Fetch-Site,
		// with an Origin fallback for older browsers).
		if err := a.csrf.Check(c.Request); err != nil {
			a.deny(c, http.StatusForbidden, "cross-origin request rejected", nil)
			return
		}

		user, err := a.lookup(c.Request)
		if err != nil {
			log.WithError(err).Warn("Login check against paperless-ngx failed")
			a.deny(c, http.StatusBadGateway, "could not verify the paperless-ngx login", nil)
			return
		}
		if user == nil {
			a.deny(c, http.StatusUnauthorized, "sign in to paperless-ngx first", nil)
			return
		}
		if !user.satisfies(a.level) {
			a.deny(c, http.StatusForbidden, fmt.Sprintf("requires a paperless-ngx %s account", a.level), user)
			return
		}
		// Answers depend on who asks; shared caches must not keep them.
		c.Header("Cache-Control", "no-store")
		c.Next()
	}
}

func (a *forwardAuth) deny(c *gin.Context, status int, message string, user *paperlessUser) {
	c.Header("Cache-Control", "no-store")
	isPage := !strings.HasPrefix(c.Request.URL.Path, "/api/") &&
		(c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead)
	if !isPage {
		body := gin.H{"error": message}
		if status == http.StatusUnauthorized {
			body["login_url"] = a.loginURL
		}
		c.AbortWithStatusJSON(status, body)
		return
	}
	switch status {
	case http.StatusUnauthorized:
		// A host-relative login link that lands here means paperless-gpt runs on
		// its own host or port; redirecting again would loop forever.
		if strings.HasPrefix(c.Request.URL.Path, "/accounts/login") {
			log.Warn("The paperless-ngx login link points to paperless-gpt itself; set PAPERLESS_PUBLIC_URL")
			c.Data(status, "text/plain; charset=utf-8", []byte("paperless-gpt cannot find the paperless-ngx login: set PAPERLESS_PUBLIC_URL to the address of paperless-ngx.\n"))
			break
		}
		c.Redirect(http.StatusFound, a.loginURL+"?next="+url.QueryEscape(returnPath(c.Request)))
	case http.StatusForbidden:
		c.Data(status, "text/html; charset=utf-8", []byte(a.forbiddenPage(user)))
	default:
		c.Data(status, "text/plain; charset=utf-8", []byte(message))
	}
	c.Abort()
}

// returnPath is where paperless-ngx sends the browser back to after sign-in.
// Behind a reverse proxy that strips a path prefix, X-Forwarded-Prefix tells
// the public path. paperless-ngx validates "next" itself; checking it here
// only keeps a bogus header from producing a useless redirect.
func returnPath(r *http.Request) string {
	prefix := strings.TrimRight(r.Header.Get("X-Forwarded-Prefix"), "/")
	if prefix != "" && (!strings.HasPrefix(prefix, "/") || strings.HasPrefix(prefix, "//") || strings.ContainsAny(prefix, "\\?#")) {
		prefix = ""
	}
	return prefix + r.URL.RequestURI()
}

func (a *forwardAuth) forbiddenPage(user *paperlessUser) string {
	name := ""
	if user != nil && user.Username != "" {
		name = " as <strong>" + html.EscapeString(user.Username) + "</strong>"
	}
	return `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>paperless-gpt – access denied</title>
<style>body{font-family:system-ui,sans-serif;max-width:36rem;margin:4rem auto;padding:0 1rem;line-height:1.5}</style>
</head><body>
<h1>Access denied</h1>
<p>You are signed in to paperless-ngx` + name + `, but paperless-gpt is only available to paperless-ngx accounts with <strong>` + a.level.String() + `</strong> status.</p>
<p>Ask an administrator of your paperless-ngx instance for access, or <a href="` + html.EscapeString(a.loginURL) + `">sign in with a different account</a>.</p>
</body></html>
`
}
