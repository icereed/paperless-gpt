package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// newSecurityTestRouter builds a minimal Gin engine with the given middleware
// and registers GET /test that always returns 200.
func newSecurityTestRouter(middleware ...gin.HandlerFunc) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	for _, m := range middleware {
		r.Use(m)
	}
	r.GET("/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	return r
}

// ---------------------------------------------------------------------------
// authMiddleware tests
// ---------------------------------------------------------------------------

func TestAuthMiddleware_Disabled(t *testing.T) {
	cfg := SecurityConfig{} // no credentials set
	r := newSecurityTestRouter(authMiddleware(cfg))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code, "unauthenticated request must succeed when auth is disabled")
}

func TestAuthMiddleware_BasicAuth_Success(t *testing.T) {
	cfg := SecurityConfig{AuthUsername: "admin", AuthPassword: "secret"}
	r := newSecurityTestRouter(authMiddleware(cfg))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestAuthMiddleware_BasicAuth_WrongPassword(t *testing.T) {
	cfg := SecurityConfig{AuthUsername: "admin", AuthPassword: "secret"}
	r := newSecurityTestRouter(authMiddleware(cfg))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.SetBasicAuth("admin", "wrong")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Equal(t, `Basic realm="paperless-gpt", charset="UTF-8"`, w.Header().Get("WWW-Authenticate"))
}

func TestAuthMiddleware_BasicAuth_NoCredentials(t *testing.T) {
	cfg := SecurityConfig{AuthUsername: "admin", AuthPassword: "secret"}
	r := newSecurityTestRouter(authMiddleware(cfg))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAuthMiddleware_BearerToken_Success(t *testing.T) {
	cfg := SecurityConfig{AuthToken: "my-secret-token"}
	r := newSecurityTestRouter(authMiddleware(cfg))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer my-secret-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestAuthMiddleware_BearerToken_Wrong(t *testing.T) {
	cfg := SecurityConfig{AuthToken: "my-secret-token"}
	r := newSecurityTestRouter(authMiddleware(cfg))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAuthMiddleware_BothMechanisms_BasicAuthWins(t *testing.T) {
	cfg := SecurityConfig{
		AuthUsername: "admin",
		AuthPassword: "secret",
		AuthToken:    "also-valid",
	}
	r := newSecurityTestRouter(authMiddleware(cfg))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestAuthMiddleware_OAuthCallbackExempt(t *testing.T) {
	cfg := SecurityConfig{AuthUsername: "admin", AuthPassword: "secret"}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(authMiddleware(cfg))
	r.GET("/api/integrations/jobber/oauth/callback", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/integrations/jobber/oauth/callback", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Must be accessible without credentials
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestAuthMiddleware_ReceiptExempt(t *testing.T) {
	cfg := SecurityConfig{AuthUsername: "admin", AuthPassword: "secret"}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(authMiddleware(cfg))
	r.GET("/api/integrations/jobber/receipt/:token", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/integrations/jobber/receipt/abc123", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// ---------------------------------------------------------------------------
// securityHeadersMiddleware tests
// ---------------------------------------------------------------------------

func TestSecurityHeadersMiddleware(t *testing.T) {
	r := newSecurityTestRouter(securityHeadersMiddleware())

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, "nosniff", w.Header().Get("X-Content-Type-Options"))
	assert.Equal(t, "DENY", w.Header().Get("X-Frame-Options"))
	assert.Empty(t, w.Header().Get("X-XSS-Protection"), "X-XSS-Protection should not be set (deprecated)")
	assert.Equal(t, "strict-origin-when-cross-origin", w.Header().Get("Referrer-Policy"))
	assert.NotEmpty(t, w.Header().Get("Permissions-Policy"))
	assert.Contains(t, w.Header().Get("Content-Security-Policy"), "default-src 'self'")
	assert.Contains(t, w.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'")
}

// ---------------------------------------------------------------------------
// maxBodySizeMiddleware tests
// ---------------------------------------------------------------------------

func TestMaxBodySizeMiddleware_UnderLimit(t *testing.T) {
	r := newSecurityTestRouter(maxBodySizeMiddleware(100))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestMaxBodySizeMiddleware_OverLimit(t *testing.T) {
	r := newSecurityTestRouter(maxBodySizeMiddleware(10))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.ContentLength = 1024 * 1024 // 1 MiB – exceeds limit of 10 bytes
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
}

// ---------------------------------------------------------------------------
// rateLimitMiddleware tests
// ---------------------------------------------------------------------------

func TestRateLimitMiddleware_Disabled(t *testing.T) {
	r := newSecurityTestRouter(rateLimitMiddleware(0, 0))

	for i := 0; i < 100; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	}
}

func TestRateLimitMiddleware_BurstAllowed(t *testing.T) {
	// burst=5 means the first 5 requests from the same IP must succeed
	r := newSecurityTestRouter(rateLimitMiddleware(0.001, 5))

	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code, "request %d should be within burst", i+1)
	}
}

func TestRateLimitMiddleware_ExceedBurst(t *testing.T) {
	// Very low rate (0.001 r/s) with burst of 1 – the second request from the
	// same IP must be rate-limited.
	r := newSecurityTestRouter(rateLimitMiddleware(0.001, 1))

	req1 := httptest.NewRequest(http.MethodGet, "/test", nil)
	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, req1)
	assert.Equal(t, http.StatusOK, w1.Code)

	req2 := httptest.NewRequest(http.MethodGet, "/test", nil)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	assert.Equal(t, http.StatusTooManyRequests, w2.Code)
}

// ---------------------------------------------------------------------------
// isExemptFromAuth tests
// ---------------------------------------------------------------------------

func TestIsExemptFromAuth(t *testing.T) {
	exempt := []string{
		// OAuth callbacks
		"/api/integrations/jobber/oauth/callback",
		"/api/integrations/google_drive/oauth/callback",
		// Jobber receipt tokens
		"/api/integrations/jobber/receipt/sometoken",
		// Session auth endpoints (handled by sessionAuthMiddleware)
		"/api/auth/login",
		"/api/auth/logout",
		"/api/auth/setup",
		"/api/auth/setup/status",
		"/api/auth/me",
		"/api/auth/change-password",
	}
	for _, p := range exempt {
		assert.True(t, isExemptFromAuth(p), "expected %q to be exempt", p)
	}

	notExempt := []string{
		"/api/documents",
		"/api/settings",
		"/api/prompts",
		"/",
	}
	for _, p := range notExempt {
		assert.False(t, isExemptFromAuth(p), "expected %q NOT to be exempt", p)
	}
}

// TestAuthMiddleware_AuthEndpointsExempt verifies that /api/auth/* routes are always
// reachable even when HTTP Basic/Bearer authentication is configured. This prevents the
// login endpoint from being blocked by the static-credentials layer.
func TestAuthMiddleware_AuthEndpointsExempt(t *testing.T) {
	cfg := SecurityConfig{AuthUsername: "admin", AuthPassword: "secret"}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(authMiddleware(cfg))
	for _, path := range []string{
		"/api/auth/login",
		"/api/auth/logout",
		"/api/auth/setup",
		"/api/auth/setup/status",
		"/api/auth/me",
	} {
		r.POST(path, func(c *gin.Context) { c.Status(http.StatusOK) })
		r.GET(path, func(c *gin.Context) { c.Status(http.StatusOK) })
	}

	for _, path := range []string{
		"/api/auth/login",
		"/api/auth/setup",
		"/api/auth/setup/status",
		"/api/auth/me",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		// Must be accessible without HTTP Basic credentials
		assert.NotEqual(t, http.StatusUnauthorized, w.Code, "path %q should be exempt from HTTP auth", path)
	}
}

// ---------------------------------------------------------------------------
// loadSecurityConfig tests
// ---------------------------------------------------------------------------

func TestLoadSecurityConfig_Defaults(t *testing.T) {
	t.Setenv("AUTH_USERNAME", "")
	t.Setenv("AUTH_PASSWORD", "")
	t.Setenv("AUTH_TOKEN", "")
	t.Setenv("TRUSTED_PROXIES", "")
	t.Setenv("HTTP_RATE_LIMIT_RPS", "")
	t.Setenv("HTTP_RATE_LIMIT_BURST", "")
	t.Setenv("MAX_BODY_BYTES", "")

	cfg := loadSecurityConfig()

	assert.False(t, cfg.isAuthEnabled())
	assert.Equal(t, float64(10), cfg.RateLimitRPS)
	assert.Equal(t, 30, cfg.RateLimitBurst)
	assert.Equal(t, int64(10*1024*1024), cfg.MaxBodyBytes)
	assert.Equal(t, []string{"127.0.0.1", "::1"}, cfg.TrustedProxies)
}

func TestLoadSecurityConfig_CustomValues(t *testing.T) {
	t.Setenv("AUTH_USERNAME", "user")
	t.Setenv("AUTH_PASSWORD", "pass")
	t.Setenv("AUTH_TOKEN", "tok")
	t.Setenv("TRUSTED_PROXIES", "10.0.0.1, 10.0.0.2")
	t.Setenv("HTTP_RATE_LIMIT_RPS", "5.5")
	t.Setenv("HTTP_RATE_LIMIT_BURST", "20")
	t.Setenv("MAX_BODY_BYTES", "1048576")

	cfg := loadSecurityConfig()

	assert.True(t, cfg.isAuthEnabled())
	assert.Equal(t, "user", cfg.AuthUsername)
	assert.Equal(t, "pass", cfg.AuthPassword)
	assert.Equal(t, "tok", cfg.AuthToken)
	assert.Equal(t, []string{"10.0.0.1", "10.0.0.2"}, cfg.TrustedProxies)
	assert.Equal(t, 5.5, cfg.RateLimitRPS)
	assert.Equal(t, 20, cfg.RateLimitBurst)
	assert.Equal(t, int64(1048576), cfg.MaxBodyBytes)
}
