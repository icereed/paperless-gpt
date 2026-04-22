package main

import (
	"crypto/subtle"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

// SecurityConfig holds security-related configuration read from environment variables.
type SecurityConfig struct {
	// Authentication
	AuthUsername string
	AuthPassword string
	AuthToken    string

	// Trusted proxies (comma-separated IPs or CIDRs), empty means trust localhost only
	TrustedProxies []string

	// HTTP rate limiting per client IP
	RateLimitRPS   float64
	RateLimitBurst int

	// Maximum allowed request body size in bytes
	MaxBodyBytes int64
}

// loadSecurityConfig reads security configuration from environment variables.
func loadSecurityConfig() SecurityConfig {
	cfg := SecurityConfig{
		AuthUsername: os.Getenv("AUTH_USERNAME"),
		AuthPassword: os.Getenv("AUTH_PASSWORD"),
		AuthToken:    os.Getenv("AUTH_TOKEN"),
		RateLimitRPS: 10,
		RateLimitBurst: 30,
		MaxBodyBytes: 10 * 1024 * 1024, // 10 MiB
	}

	// Trusted proxies: default to loopback only
	if raw := os.Getenv("TRUSTED_PROXIES"); raw != "" {
		for _, p := range strings.Split(raw, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				cfg.TrustedProxies = append(cfg.TrustedProxies, p)
			}
		}
	} else {
		cfg.TrustedProxies = []string{"127.0.0.1", "::1"}
	}

	if raw := os.Getenv("HTTP_RATE_LIMIT_RPS"); raw != "" {
		if v, err := strconv.ParseFloat(raw, 64); err == nil && v >= 0 {
			cfg.RateLimitRPS = v
		} else if err != nil {
			log.Warnf("Invalid HTTP_RATE_LIMIT_RPS value %q, using default %.1f", raw, cfg.RateLimitRPS)
		}
	}
	if raw := os.Getenv("HTTP_RATE_LIMIT_BURST"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v >= 0 {
			cfg.RateLimitBurst = v
		} else if err != nil {
			log.Warnf("Invalid HTTP_RATE_LIMIT_BURST value %q, using default %d", raw, cfg.RateLimitBurst)
		}
	}
	if raw := os.Getenv("MAX_BODY_BYTES"); raw != "" {
		if v, err := strconv.ParseInt(raw, 10, 64); err == nil && v > 0 {
			cfg.MaxBodyBytes = v
		} else if err != nil {
			log.Warnf("Invalid MAX_BODY_BYTES value %q, using default %d", raw, cfg.MaxBodyBytes)
		}
	}

	return cfg
}

// isAuthEnabled reports whether at least one auth mechanism is configured.
func (cfg SecurityConfig) isAuthEnabled() bool {
	return (cfg.AuthUsername != "" && cfg.AuthPassword != "") || cfg.AuthToken != ""
}

// isExemptFromAuth reports whether the given request path should bypass authentication.
// OAuth provider callbacks cannot carry auth credentials (they are browser redirects from
// third-party servers), and receipt download tokens are self-authenticating.
// Session-based auth routes (/api/auth/*) are also exempt because they are either public
// (login, setup) or protected by the sessionAuthMiddleware that already runs before this
// middleware; re-requiring HTTP Basic/Bearer on them would break login when
// AUTH_USERNAME/PASSWORD is set.
func isExemptFromAuth(path string) bool {
	// Session auth endpoints – handled by sessionAuthMiddleware
	if strings.HasPrefix(path, "/api/auth/") {
		return true
	}
	// OAuth callbacks from third-party providers
	if strings.HasSuffix(path, "/oauth/callback") {
		return true
	}
	// Jobber receipt downloads – the receipt token is the credential
	if strings.Contains(path, "/integrations/jobber/receipt/") {
		return true
	}
	return false
}

// authMiddleware returns a Gin middleware that enforces HTTP authentication.
//
// Two mechanisms are supported and can be used in combination:
//   - HTTP Basic Auth  (AUTH_USERNAME + AUTH_PASSWORD)
//   - Bearer token     (AUTH_TOKEN)
//
// When neither is configured the middleware is a no-op (backward compatible with
// deployments that rely on network-level access control).
func authMiddleware(cfg SecurityConfig) gin.HandlerFunc {
	if !cfg.isAuthEnabled() {
		// Session-based user auth (auth.go) may still be active; only warn when
		// that is also not in use.  The warning is intentionally deferred to
		// runtime because the DB is not available at middleware construction
		// time.  Session auth logs its own warnings during startup.
		return func(c *gin.Context) { c.Next() }
	}

	log.Info("HTTP authentication is enabled.")

	return func(c *gin.Context) {
		if isExemptFromAuth(c.Request.URL.Path) {
			c.Next()
			return
		}

		authHeader := c.GetHeader("Authorization")

		// --- Basic Auth ---
		if cfg.AuthUsername != "" && cfg.AuthPassword != "" {
			if strings.HasPrefix(authHeader, "Basic ") {
				u, p, ok := c.Request.BasicAuth()
				if ok &&
					subtle.ConstantTimeCompare([]byte(u), []byte(cfg.AuthUsername)) == 1 &&
					subtle.ConstantTimeCompare([]byte(p), []byte(cfg.AuthPassword)) == 1 {
					c.Next()
					return
				}
			}
		}

		// --- Bearer token ---
		if cfg.AuthToken != "" {
			if strings.HasPrefix(authHeader, "Bearer ") {
				provided := strings.TrimPrefix(authHeader, "Bearer ")
				if subtle.ConstantTimeCompare([]byte(provided), []byte(cfg.AuthToken)) == 1 {
					c.Next()
					return
				}
			}
		}

		// Prompt browsers to show a login dialog; API clients receive 401.
		c.Header("WWW-Authenticate", `Basic realm="paperless-gpt", charset="UTF-8"`)
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
	}
}

// securityHeadersMiddleware adds standard defensive HTTP response headers to every response.
func securityHeadersMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Header("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		// Content-Security-Policy: restrict resource origins to self.
		// 'unsafe-inline' is required for the OAuth popup's inline <script>;
		// all other content (images, styles, fonts, API calls) is self-only.
		c.Header("Content-Security-Policy",
			"default-src 'self'; "+
				"script-src 'self' 'unsafe-inline'; "+
				"style-src 'self' 'unsafe-inline'; "+
				"img-src 'self' data:; "+
				"font-src 'self' data:; "+
				"connect-src 'self'; "+
				"frame-ancestors 'none'")
		c.Next()
	}
}

// ipVisitor holds a rate limiter and last-seen timestamp for a single client IP.
type ipVisitor struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// loginRateLimitMiddleware returns a stricter per-IP rate limiter for authentication
// endpoints to slow credential-stuffing attacks. Allows 5 attempts per minute per IP.
func loginRateLimitMiddleware() gin.HandlerFunc {
	// 1 attempt per 12 seconds = ~5/minute, burst of 5
	return rateLimitMiddleware(1.0/12, 5)
}

// rateLimitMiddleware returns a Gin middleware that enforces per-client-IP rate limiting.
//
// rps  – sustained requests per second allowed per IP (0 disables limiting).
// burst – maximum burst size (must be >= 1 when rps > 0).
func rateLimitMiddleware(rps float64, burst int) gin.HandlerFunc {
	if rps <= 0 {
		return func(c *gin.Context) { c.Next() }
	}

	var (
		mu       sync.Mutex
		visitors = make(map[string]*ipVisitor)
	)

	// Periodically remove idle visitor entries to avoid unbounded memory growth.
	go func() {
		ticker := time.NewTicker(2 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			mu.Lock()
			for ip, v := range visitors {
				if time.Since(v.lastSeen) > 5*time.Minute {
					delete(visitors, ip)
				}
			}
			mu.Unlock()
		}
	}()

	getLimiter := func(ip string) *rate.Limiter {
		mu.Lock()
		defer mu.Unlock()
		v, ok := visitors[ip]
		if !ok {
			v = &ipVisitor{limiter: rate.NewLimiter(rate.Limit(rps), burst)}
			visitors[ip] = v
		}
		v.lastSeen = time.Now()
		return v.limiter
	}

	return func(c *gin.Context) {
		ip := c.ClientIP()
		if !getLimiter(ip).Allow() {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": "Too many requests – please slow down.",
			})
			return
		}
		c.Next()
	}
}

// maxBodySizeMiddleware limits the size of incoming request bodies to prevent
// resource exhaustion attacks.
func maxBodySizeMiddleware(maxBytes int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.ContentLength > maxBytes {
			c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{
				"error": "Request body too large.",
			})
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
		c.Next()
	}
}
