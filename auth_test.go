package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// newTestDB creates an isolated in-memory SQLite database with the auth schema migrated.
// Each call gets a unique DSN so tests never share state.
func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	// Use a unique file name per test so in-memory DBs are fully isolated.
	dsn := "file:" + t.Name() + "?mode=memory&cache=private"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&User{}, &UserSession{}))
	return db
}

// newAuthTestRouter builds a Gin router wired with the auth endpoints and
// session middleware, using the provided in-memory database.
func newAuthTestRouter(t *testing.T, db *gorm.DB) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(sessionAuthMiddleware(db, SecurityConfig{}))

	app := &App{Database: db}

	authGroup := r.Group("/api/auth")
	authGroup.GET("/setup/status", app.setupStatusHandler)
	authGroup.POST("/setup", app.createFirstAdminHandler)
	authGroup.POST("/login", app.loginHandler)
	authGroup.POST("/logout", app.logoutHandler)
	authGroup.GET("/me", app.meHandler)
	authGroup.POST("/change-password", app.changePasswordHandler)

	// A protected route that needs a valid session
	r.GET("/api/protected", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	return r
}

// jsonBody marshals v and returns a bytes.Buffer for use as a request body.
func jsonBody(t *testing.T, v interface{}) *bytes.Buffer {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return bytes.NewBuffer(b)
}

// ---------------------------------------------------------------------------
// Setup status
// ---------------------------------------------------------------------------

func TestSetupStatus_InitiallyRequired(t *testing.T) {
	db := newTestDB(t)
	r := newAuthTestRouter(t, db)

	req := httptest.NewRequest(http.MethodGet, "/api/auth/setup/status", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, true, resp["setup_required"])
}

func TestSetupStatus_NotRequiredAfterSetup(t *testing.T) {
	db := newTestDB(t)
	r := newAuthTestRouter(t, db)

	// Create the first user
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/auth/setup",
		jsonBody(t, gin.H{"username": "admin", "password": "password123"})))
	require.Equal(t, http.StatusCreated, w.Code)

	// Now check status
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, httptest.NewRequest(http.MethodGet, "/api/auth/setup/status", nil))
	require.Equal(t, http.StatusOK, w2.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w2.Body.Bytes(), &resp))
	assert.Equal(t, false, resp["setup_required"])
}

// ---------------------------------------------------------------------------
// First-run setup
// ---------------------------------------------------------------------------

func TestCreateFirstAdmin_Success(t *testing.T) {
	db := newTestDB(t)
	r := newAuthTestRouter(t, db)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/auth/setup",
		jsonBody(t, gin.H{"username": "admin", "password": "strongpass"})))

	require.Equal(t, http.StatusCreated, w.Code)
	var resp userOut
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "admin", resp.Username)
	// A session cookie should be set
	assert.NotEmpty(t, w.Result().Cookies())
}

func TestCreateFirstAdmin_PasswordTooShort(t *testing.T) {
	db := newTestDB(t)
	r := newAuthTestRouter(t, db)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/auth/setup",
		jsonBody(t, gin.H{"username": "admin", "password": "short"})))
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreateFirstAdmin_BlockedAfterFirstUser(t *testing.T) {
	db := newTestDB(t)
	r := newAuthTestRouter(t, db)

	// First call succeeds
	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, httptest.NewRequest(http.MethodPost, "/api/auth/setup",
		jsonBody(t, gin.H{"username": "admin", "password": "password123"})))
	require.Equal(t, http.StatusCreated, w1.Code)

	// Second call must be rejected
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, httptest.NewRequest(http.MethodPost, "/api/auth/setup",
		jsonBody(t, gin.H{"username": "hacker", "password": "password123"})))
	assert.Equal(t, http.StatusForbidden, w2.Code)
}

// ---------------------------------------------------------------------------
// Login / logout
// ---------------------------------------------------------------------------

func TestLogin_Success(t *testing.T) {
	db := newTestDB(t)
	r := newAuthTestRouter(t, db)

	// Create user first
	hashed, _ := hashPassword("mypassword")
	db.Create(&User{ID: generateUserID(), Username: "alice", HashedPassword: hashed})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/auth/login",
		jsonBody(t, gin.H{"username": "alice", "password": "mypassword"})))

	require.Equal(t, http.StatusOK, w.Code)
	var resp userOut
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "alice", resp.Username)
	assert.NotEmpty(t, w.Result().Cookies())
}

func TestLogin_WrongPassword(t *testing.T) {
	db := newTestDB(t)
	r := newAuthTestRouter(t, db)

	hashed, _ := hashPassword("correctpassword")
	db.Create(&User{ID: generateUserID(), Username: "bob", HashedPassword: hashed})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/auth/login",
		jsonBody(t, gin.H{"username": "bob", "password": "wrong"})))
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestLogin_UnknownUser(t *testing.T) {
	db := newTestDB(t)
	r := newAuthTestRouter(t, db)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/auth/login",
		jsonBody(t, gin.H{"username": "nobody", "password": "password123"})))
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestLogout_ClearsCookie(t *testing.T) {
	db := newTestDB(t)
	r := newAuthTestRouter(t, db)

	hashed, _ := hashPassword("pass")
	db.Create(&User{ID: generateUserID(), Username: "charlie", HashedPassword: hashed})

	// Login to get cookie
	wLogin := httptest.NewRecorder()
	reqLogin := httptest.NewRequest(http.MethodPost, "/api/auth/login",
		jsonBody(t, gin.H{"username": "charlie", "password": "pass"}))
	r.ServeHTTP(wLogin, reqLogin)
	require.Equal(t, http.StatusOK, wLogin.Code)

	cookie := wLogin.Result().Cookies()[0]

	// Logout
	wLogout := httptest.NewRecorder()
	reqLogout := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	reqLogout.AddCookie(cookie)
	r.ServeHTTP(wLogout, reqLogout)
	assert.Equal(t, http.StatusOK, wLogout.Code)
}

// ---------------------------------------------------------------------------
// /me endpoint
// ---------------------------------------------------------------------------

func TestMe_WithValidSession(t *testing.T) {
	db := newTestDB(t)
	r := newAuthTestRouter(t, db)

	hashed, _ := hashPassword("pass")
	db.Create(&User{ID: generateUserID(), Username: "diana", HashedPassword: hashed})

	// Login
	wLogin := httptest.NewRecorder()
	r.ServeHTTP(wLogin, httptest.NewRequest(http.MethodPost, "/api/auth/login",
		jsonBody(t, gin.H{"username": "diana", "password": "pass"})))
	require.Equal(t, http.StatusOK, wLogin.Code)
	cookie := wLogin.Result().Cookies()[0]

	// /me
	wMe := httptest.NewRecorder()
	reqMe := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	reqMe.AddCookie(cookie)
	r.ServeHTTP(wMe, reqMe)
	require.Equal(t, http.StatusOK, wMe.Code)
	var resp userOut
	require.NoError(t, json.Unmarshal(wMe.Body.Bytes(), &resp))
	assert.Equal(t, "diana", resp.Username)
}

func TestMe_WithoutSession_Unauthenticated(t *testing.T) {
	db := newTestDB(t)
	r := newAuthTestRouter(t, db)

	// Create a user so session auth is active
	hashed, _ := hashPassword("pass")
	db.Create(&User{ID: generateUserID(), Username: "eve", HashedPassword: hashed})

	wMe := httptest.NewRecorder()
	r.ServeHTTP(wMe, httptest.NewRequest(http.MethodGet, "/api/auth/me", nil))
	// /api/auth/* is exempt from the session gate, so the handler runs but returns 401
	assert.Equal(t, http.StatusUnauthorized, wMe.Code)
}

// ---------------------------------------------------------------------------
// Protected route requires session
// ---------------------------------------------------------------------------

func TestProtectedRoute_RequiresSession(t *testing.T) {
	db := newTestDB(t)
	r := newAuthTestRouter(t, db)

	hashed, _ := hashPassword("pass")
	db.Create(&User{ID: generateUserID(), Username: "frank", HashedPassword: hashed})

	// Without session
	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, httptest.NewRequest(http.MethodGet, "/api/protected", nil))
	assert.Equal(t, http.StatusUnauthorized, w1.Code)

	// Login
	wLogin := httptest.NewRecorder()
	r.ServeHTTP(wLogin, httptest.NewRequest(http.MethodPost, "/api/auth/login",
		jsonBody(t, gin.H{"username": "frank", "password": "pass"})))
	require.Equal(t, http.StatusOK, wLogin.Code)
	cookie := wLogin.Result().Cookies()[0]

	// With session
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/api/protected", nil)
	req2.AddCookie(cookie)
	r.ServeHTTP(w2, req2)
	assert.Equal(t, http.StatusOK, w2.Code)
}

// ---------------------------------------------------------------------------
// Change password
// ---------------------------------------------------------------------------

func TestChangePassword_Success(t *testing.T) {
	db := newTestDB(t)
	r := newAuthTestRouter(t, db)

	hashed, _ := hashPassword("oldpass123")
	db.Create(&User{ID: generateUserID(), Username: "grace", HashedPassword: hashed})

	// Login
	wLogin := httptest.NewRecorder()
	r.ServeHTTP(wLogin, httptest.NewRequest(http.MethodPost, "/api/auth/login",
		jsonBody(t, gin.H{"username": "grace", "password": "oldpass123"})))
	require.Equal(t, http.StatusOK, wLogin.Code)
	cookie := wLogin.Result().Cookies()[0]

	// Change password
	wCP := httptest.NewRecorder()
	reqCP := httptest.NewRequest(http.MethodPost, "/api/auth/change-password",
		jsonBody(t, gin.H{"current_password": "oldpass123", "new_password": "newpass456"}))
	reqCP.AddCookie(cookie)
	r.ServeHTTP(wCP, reqCP)
	assert.Equal(t, http.StatusOK, wCP.Code)

	// Old password should no longer work
	wOld := httptest.NewRecorder()
	r.ServeHTTP(wOld, httptest.NewRequest(http.MethodPost, "/api/auth/login",
		jsonBody(t, gin.H{"username": "grace", "password": "oldpass123"})))
	assert.Equal(t, http.StatusUnauthorized, wOld.Code)

	// New password should work
	wNew := httptest.NewRecorder()
	r.ServeHTTP(wNew, httptest.NewRequest(http.MethodPost, "/api/auth/login",
		jsonBody(t, gin.H{"username": "grace", "password": "newpass456"})))
	assert.Equal(t, http.StatusOK, wNew.Code)
}

func TestChangePassword_WrongCurrentPassword(t *testing.T) {
	db := newTestDB(t)
	r := newAuthTestRouter(t, db)

	hashed, _ := hashPassword("correct")
	db.Create(&User{ID: generateUserID(), Username: "henry", HashedPassword: hashed})

	wLogin := httptest.NewRecorder()
	r.ServeHTTP(wLogin, httptest.NewRequest(http.MethodPost, "/api/auth/login",
		jsonBody(t, gin.H{"username": "henry", "password": "correct"})))
	require.Equal(t, http.StatusOK, wLogin.Code)
	cookie := wLogin.Result().Cookies()[0]

	wCP := httptest.NewRecorder()
	reqCP := httptest.NewRequest(http.MethodPost, "/api/auth/change-password",
		jsonBody(t, gin.H{"current_password": "wrong", "new_password": "newpass456"}))
	reqCP.AddCookie(cookie)
	r.ServeHTTP(wCP, reqCP)
	assert.Equal(t, http.StatusBadRequest, wCP.Code)
}

// ---------------------------------------------------------------------------
// No-users mode: protected route is open
// ---------------------------------------------------------------------------

func TestProtectedRoute_OpenWhenNoUsers(t *testing.T) {
	db := newTestDB(t)
	r := newAuthTestRouter(t, db)

	// No users → middleware is transparent
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/protected", nil))
	assert.Equal(t, http.StatusOK, w.Code)
}

// ---------------------------------------------------------------------------
// Login → logout → login cycle
// ---------------------------------------------------------------------------

// TestLoginLogoutLogin verifies the full cycle: log in, log out, then log in again.
// This is the scenario reported in the "Not authenticated" bug: after logout the
// session cookie is cleared, and a subsequent login should create a fresh session
// and return the user object without any 401.
func TestLoginLogoutLogin(t *testing.T) {
	db := newTestDB(t)
	r := newAuthTestRouter(t, db)

	hashed, _ := hashPassword("p@ssw0rd!")
	db.Create(&User{ID: generateUserID(), Username: "ivan", HashedPassword: hashed})

	// --- First login ---
	wLogin1 := httptest.NewRecorder()
	r.ServeHTTP(wLogin1, httptest.NewRequest(http.MethodPost, "/api/auth/login",
		jsonBody(t, gin.H{"username": "ivan", "password": "p@ssw0rd!"})))
	require.Equal(t, http.StatusOK, wLogin1.Code)
	cookie := wLogin1.Result().Cookies()[0]

	// Confirm /me works with session
	wMe1 := httptest.NewRecorder()
	reqMe1 := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	reqMe1.AddCookie(cookie)
	r.ServeHTTP(wMe1, reqMe1)
	require.Equal(t, http.StatusOK, wMe1.Code)

	// --- Logout ---
	wLogout := httptest.NewRecorder()
	reqLogout := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	reqLogout.AddCookie(cookie)
	r.ServeHTTP(wLogout, reqLogout)
	require.Equal(t, http.StatusOK, wLogout.Code)

	// Confirm /me now returns 401 (session deleted)
	wMeAfterLogout := httptest.NewRecorder()
	reqMeAfterLogout := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	reqMeAfterLogout.AddCookie(cookie)
	r.ServeHTTP(wMeAfterLogout, reqMeAfterLogout)
	assert.Equal(t, http.StatusUnauthorized, wMeAfterLogout.Code)

	// --- Second login ---
	wLogin2 := httptest.NewRecorder()
	r.ServeHTTP(wLogin2, httptest.NewRequest(http.MethodPost, "/api/auth/login",
		jsonBody(t, gin.H{"username": "ivan", "password": "p@ssw0rd!"})))
	require.Equal(t, http.StatusOK, wLogin2.Code, "login after logout must succeed")

	var resp userOut
	require.NoError(t, json.Unmarshal(wLogin2.Body.Bytes(), &resp))
	assert.Equal(t, "ivan", resp.Username)
	assert.NotEmpty(t, wLogin2.Result().Cookies(), "a new session cookie must be set")
}

// TestFrontendShellReachableWithoutSession is the regression test for the lockout
// bug: once a user exists, sessionAuthMiddleware must still allow GET /, /history,
// /settings, /assets/*, etc. to load so the React app can render LoginPage. Before
// the fix, a browser with no (or an expired) session cookie would receive
// {"error":"Not authenticated"} as the HTML shell response and the UI could never
// be reached again without wiping the users table.
func TestFrontendShellReachableWithoutSession(t *testing.T) {
	db := newTestDB(t)

	// Create a user so isSessionAuthEnabled returns true – this is the state
	// in which the original bug manifested (single-user deployment).
	hashed, _ := hashPassword("irrelevant1")
	db.Create(&User{ID: generateUserID(), Username: "owner", HashedPassword: hashed})

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(sessionAuthMiddleware(db, SecurityConfig{}))
	// Register the same frontend shell routes as main.go
	for _, p := range []string{"/", "/history", "/settings", "/experimental-ocr", "/favicon.ico"} {
		r.GET(p, func(c *gin.Context) { c.Status(http.StatusOK) })
	}
	r.GET("/assets/*filepath", func(c *gin.Context) { c.Status(http.StatusOK) })
	r.GET("/api/external/v1/health", func(c *gin.Context) { c.Status(http.StatusOK) })
	// And a protected API route, which must still return 401
	r.GET("/api/documents", func(c *gin.Context) { c.Status(http.StatusOK) })

	// Public frontend paths must be reachable without a session cookie
	for _, p := range []string{
		"/",
		"/history",
		"/settings",
		"/experimental-ocr",
		"/favicon.ico",
		"/assets/index.js",
		"/assets/logo-abc123.svg",
	} {
		req := httptest.NewRequest(http.MethodGet, p, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code, "path %q must load without a session so LoginPage can render", p)
	}

	reqExternal := httptest.NewRequest(http.MethodGet, "/api/external/v1/health", nil)
	wExternal := httptest.NewRecorder()
	r.ServeHTTP(wExternal, reqExternal)
	assert.Equal(t, http.StatusOK, wExternal.Code, "external API auth is handled by API-key middleware, not browser sessions")

	// API routes must still require a session
	reqAPI := httptest.NewRequest(http.MethodGet, "/api/documents", nil)
	wAPI := httptest.NewRecorder()
	r.ServeHTTP(wAPI, reqAPI)
	assert.Equal(t, http.StatusUnauthorized, wAPI.Code, "protected API routes must still require a session")
}

// TestLoginWithBasicAuthConfigured verifies that /api/auth/login is reachable even
// when HTTP Basic Auth (AUTH_USERNAME/AUTH_PASSWORD) is configured. The auth routes
// are exempted from the static-credentials middleware by isExemptFromAuth.
func TestLoginWithBasicAuthConfigured(t *testing.T) {
	db := newTestDB(t)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	staticCfg := SecurityConfig{AuthUsername: "admin", AuthPassword: "hunter2"}
	r.Use(sessionAuthMiddleware(db, staticCfg))
	// Simulate AUTH_USERNAME + AUTH_PASSWORD being set
	r.Use(authMiddleware(staticCfg))

	app := &App{Database: db}
	authGroup := r.Group("/api/auth")
	authGroup.GET("/setup/status", app.setupStatusHandler)
	authGroup.POST("/setup", app.createFirstAdminHandler)
	authGroup.POST("/login", app.loginHandler)
	authGroup.POST("/logout", app.logoutHandler)
	authGroup.GET("/me", app.meHandler)

	hashed, _ := hashPassword("userpass")
	db.Create(&User{ID: generateUserID(), Username: "julia", HashedPassword: hashed})

	// Login must succeed without providing HTTP Basic credentials
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/auth/login",
		jsonBody(t, gin.H{"username": "julia", "password": "userpass"})))
	assert.Equal(t, http.StatusOK, w.Code, "/api/auth/login must not require HTTP Basic Auth")
}

// ---------------------------------------------------------------------------
// AUTH_TOKEN bearer bypass for machine-to-machine integrations
// (e.g. the Bricopro HQ paperless-gpt connector).
// ---------------------------------------------------------------------------

// newBearerTestRouter builds a router that mirrors the real middleware stack
// (sessionAuthMiddleware first, then authMiddleware) with AUTH_TOKEN set, so
// we can exercise the static-bearer bypass end-to-end.
func newBearerTestRouter(t *testing.T, db *gorm.DB, token string) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	cfg := SecurityConfig{AuthToken: token}
	r.Use(sessionAuthMiddleware(db, cfg))
	r.Use(authMiddleware(cfg))
	r.GET("/api/documents", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	r.GET("/api/version", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"version": "test"})
	})
	return r
}

// TestBearerToken_AllowsAPIWhenSessionAuthIsActive verifies that a request
// carrying the configured AUTH_TOKEN as a Bearer header is allowed through
// /api/* even when at least one user row exists (which would otherwise force
// session-cookie auth). This is the contract the Bricopro HQ connector
// depends on.
func TestBearerToken_AllowsAPIWhenSessionAuthIsActive(t *testing.T) {
	db := newTestDB(t)
	hashed, _ := hashPassword("irrelevant1")
	db.Create(&User{ID: generateUserID(), Username: "owner", HashedPassword: hashed})

	const token = "test-machine-token-9f3a2b"
	r := newBearerTestRouter(t, db, token)

	req := httptest.NewRequest(http.MethodGet, "/api/documents", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code, "valid AUTH_TOKEN bearer must bypass the session cookie gate")
}

// TestBearerToken_WrongTokenStill401 verifies that a wrong bearer value does
// NOT bypass either middleware: it should still be rejected with 401.
func TestBearerToken_WrongTokenStill401(t *testing.T) {
	db := newTestDB(t)
	hashed, _ := hashPassword("irrelevant1")
	db.Create(&User{ID: generateUserID(), Username: "owner", HashedPassword: hashed})

	const token = "the-real-token"
	r := newBearerTestRouter(t, db, token)

	req := httptest.NewRequest(http.MethodGet, "/api/documents", nil)
	req.Header.Set("Authorization", "Bearer not-the-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code,
		"a wrong Bearer value must not bypass session-auth and must not be accepted by authMiddleware")
}

// TestBearerToken_EmptyTokenIsIgnored verifies that an empty AUTH_TOKEN does
// not enable a "anyone with `Bearer ` prefix passes" bypass.
func TestBearerToken_EmptyTokenIsIgnored(t *testing.T) {
	db := newTestDB(t)
	hashed, _ := hashPassword("irrelevant1")
	db.Create(&User{ID: generateUserID(), Username: "owner", HashedPassword: hashed})

	r := newBearerTestRouter(t, db, "")

	req := httptest.NewRequest(http.MethodGet, "/api/documents", nil)
	req.Header.Set("Authorization", "Bearer anything-at-all")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code,
		"empty AUTH_TOKEN must not enable a wildcard bearer bypass")
}

// TestBearerToken_DoesNotInterfereWithCookieAuth verifies that a request
// with NO bearer header but a valid session cookie still works after the
// bypass change (i.e. browsers continue to function exactly as before).
func TestBearerToken_DoesNotInterfereWithCookieAuth(t *testing.T) {
	db := newTestDB(t)
	hashed, _ := hashPassword("password123")
	user := &User{ID: generateUserID(), Username: "browser-user", HashedPassword: hashed}
	require.NoError(t, db.Create(user).Error)
	session := createSession(db, user.ID, "127.0.0.1", "test-agent")
	require.NotNil(t, session)

	r := newBearerTestRouter(t, db, "some-machine-token")

	req := httptest.NewRequest(http.MethodGet, "/api/documents", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session.ID})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code,
		"a valid session cookie must continue to authenticate /api/* even when AUTH_TOKEN is set")
}

// TestBearerToken_NoBearerNoCookieStill401 verifies that with AUTH_TOKEN set
// and a user provisioned, a request with neither header nor cookie is still
// rejected — the bypass must not weaken the default-deny posture.
func TestBearerToken_NoBearerNoCookieStill401(t *testing.T) {
	db := newTestDB(t)
	hashed, _ := hashPassword("irrelevant1")
	db.Create(&User{ID: generateUserID(), Username: "owner", HashedPassword: hashed})

	r := newBearerTestRouter(t, db, "the-token")

	req := httptest.NewRequest(http.MethodGet, "/api/documents", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code,
		"no bearer + no cookie must still 401 when session auth is active")
}

// ---------------------------------------------------------------------------
// Diagnostic 401 body for failed /api/* bearer attempts
// ---------------------------------------------------------------------------

// TestBearerToken_WrongTokenReturnsDiagnosticReason verifies the upgraded
// 401 body. When AUTH_TOKEN is set on the server but the caller sends a
// different bearer to /api/*, the server should respond with a 401 whose
// JSON body includes a `reason` and `diagnostic` field so the admin can
// debug from curl output without grepping container logs. The contract is
// what the response body LOOKS LIKE, not the literal English wording.
func TestBearerToken_WrongTokenReturnsDiagnosticReason(t *testing.T) {
	db := newTestDB(t)
	hashed, _ := hashPassword("irrelevant1")
	db.Create(&User{ID: generateUserID(), Username: "owner", HashedPassword: hashed})

	r := newBearerTestRouter(t, db, "the-real-token")

	req := httptest.NewRequest(http.MethodGet, "/api/documents", nil)
	req.Header.Set("Authorization", "Bearer not-the-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusUnauthorized, w.Code)
	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "Not authenticated", body["error"])
	assert.Equal(t, "bearer_value_does_not_match_configured_auth_token", body["reason"],
		"server must classify why bearer was rejected so admins can self-diagnose curl 401s")
	assert.NotEmpty(t, body["diagnostic"], "diagnostic hint must be populated")
	// Must not leak the configured token, the provided token, or any
	// substring of either.
	for _, secret := range []string{"the-real-token", "not-the-token"} {
		assert.NotContains(t, w.Body.String(), secret,
			"diagnostic 401 body must not echo any token bytes")
	}
}

// TestBearerToken_HeaderButNoServerTokenReturnsDiagnosticReason verifies the
// case where the *client* sent a bearer but the server has no AUTH_TOKEN
// configured at all. This was the previously-silent failure mode: a curl
// with a bearer would 401 without any indication that the cause was
// "AUTH_TOKEN env var not set on server".
func TestBearerToken_HeaderButNoServerTokenReturnsDiagnosticReason(t *testing.T) {
	db := newTestDB(t)
	hashed, _ := hashPassword("irrelevant1")
	db.Create(&User{ID: generateUserID(), Username: "owner", HashedPassword: hashed})

	r := newBearerTestRouter(t, db, "")

	req := httptest.NewRequest(http.MethodGet, "/api/documents", nil)
	req.Header.Set("Authorization", "Bearer something-anything")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusUnauthorized, w.Code)
	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "bearer_received_but_auth_token_not_configured_on_server", body["reason"])
	assert.NotEmpty(t, body["diagnostic"])
}

// TestBearerToken_NoAuthHeaderKeepsLegacyBody verifies that a request with
// no Authorization header at all still gets the original generic 401 body,
// so we don't leak "AUTH_TOKEN feature exists" to anonymous browsers
// hitting /api/* with no credentials.
func TestBearerToken_NoAuthHeaderKeepsLegacyBody(t *testing.T) {
	db := newTestDB(t)
	hashed, _ := hashPassword("irrelevant1")
	db.Create(&User{ID: generateUserID(), Username: "owner", HashedPassword: hashed})

	r := newBearerTestRouter(t, db, "the-token")

	req := httptest.NewRequest(http.MethodGet, "/api/documents", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusUnauthorized, w.Code)
	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "Not authenticated", body["error"])
	_, hasReason := body["reason"]
	assert.False(t, hasReason, "anonymous /api/* requests with no Authorization header must keep the legacy generic 401")
}

// ---------------------------------------------------------------------------
// /api/auth/bearer-check diagnostic endpoint
// ---------------------------------------------------------------------------

// newBearerCheckTestRouter wires the bearer-check endpoint with the same
// session middleware as production.
func newBearerCheckTestRouter(t *testing.T, db *gorm.DB, token string) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	cfg := SecurityConfig{AuthToken: token}
	r.Use(sessionAuthMiddleware(db, cfg))
	r.Use(authMiddleware(cfg))
	app := &App{Database: db}
	r.GET("/api/auth/bearer-check", app.bearerCheckHandler)
	return r
}

func TestBearerCheck_PublicWhenNoCredentials(t *testing.T) {
	db := newTestDB(t)
	hashed, _ := hashPassword("irrelevant1")
	db.Create(&User{ID: generateUserID(), Username: "owner", HashedPassword: hashed})

	t.Setenv("AUTH_TOKEN", "the-real-token")
	r := newBearerCheckTestRouter(t, db, "the-real-token")

	req := httptest.NewRequest(http.MethodGet, "/api/auth/bearer-check", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, "bearer-check must be reachable without any credentials so admins can debug")

	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, true, body["auth_token_configured"])
	assert.Equal(t, true, body["session_auth_required"])
	assert.Equal(t, false, body["authorization_header_seen"])
	assert.Equal(t, false, body["bearer_matches"])
}

func TestBearerCheck_ReportsMatchForCorrectBearer(t *testing.T) {
	db := newTestDB(t)
	hashed, _ := hashPassword("irrelevant1")
	db.Create(&User{ID: generateUserID(), Username: "owner", HashedPassword: hashed})

	const token = "the-real-token-abc123"
	t.Setenv("AUTH_TOKEN", token)
	r := newBearerCheckTestRouter(t, db, token)

	req := httptest.NewRequest(http.MethodGet, "/api/auth/bearer-check", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, true, body["bearer_matches"], "matching bearer must report bearer_matches=true")
	assert.Equal(t, true, body["is_bearer_scheme"])
	assert.NotContains(t, w.Body.String(), token,
		"bearer-check response must never echo the configured token")
}

func TestBearerCheck_ReportsMismatchForWrongBearer(t *testing.T) {
	db := newTestDB(t)
	hashed, _ := hashPassword("irrelevant1")
	db.Create(&User{ID: generateUserID(), Username: "owner", HashedPassword: hashed})

	t.Setenv("AUTH_TOKEN", "the-real-token")
	r := newBearerCheckTestRouter(t, db, "the-real-token")

	req := httptest.NewRequest(http.MethodGet, "/api/auth/bearer-check", nil)
	req.Header.Set("Authorization", "Bearer not-the-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, false, body["bearer_matches"])
	assert.Equal(t, "bearer_value_does_not_match_configured_auth_token", body["reason"])
	assert.NotEmpty(t, body["diagnostic"])
	assert.NotContains(t, w.Body.String(), "the-real-token",
		"bearer-check response must never echo the configured token even on mismatch")
}

// ---------------------------------------------------------------------------
// AUTH_TOKEN env-var normalisation
// ---------------------------------------------------------------------------

// TestNormaliseEnvCredential_StripsQuotesAndWhitespace exercises the
// docker-compose `.env` interaction: when an admin writes
// `AUTH_TOKEN="hex..."` (or has a trailing newline from `printf`), the
// container previously got a literal `"hex..."` value that would never
// byte-match what curl sends. Trimming quotes/whitespace at config-load
// time eliminates this entire class of silent 401s.
func TestNormaliseEnvCredential_StripsQuotesAndWhitespace(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{raw: "abc", want: "abc"},
		{raw: "  abc  ", want: "abc"},
		{raw: "abc\n", want: "abc"},
		{raw: "\"abc\"", want: "abc"},
		{raw: "'abc'", want: "abc"},
		{raw: "  \"abc\"  ", want: "abc"},
		{raw: "  'abc'\n", want: "abc"},
		{raw: "\"abc'", want: "\"abc'"}, // mismatched quotes preserved
		{raw: "", want: ""},
		{raw: "\"\"", want: ""}, // explicit empty-string after strip
	}
	for _, tc := range cases {
		got := normaliseEnvCredential(tc.raw)
		assert.Equal(t, tc.want, got, "normaliseEnvCredential(%q)", tc.raw)
	}
}

// TestLoadSecurityConfig_TrimsAuthToken verifies the integration: if
// AUTH_TOKEN is set with surrounding quotes (the natural docker-compose
// shape), the bearer comparison still works.
func TestLoadSecurityConfig_TrimsAuthToken(t *testing.T) {
	t.Setenv("AUTH_TOKEN", "\"my-static-token\"\n")
	cfg := loadSecurityConfig()
	assert.Equal(t, "my-static-token", cfg.AuthToken,
		"loadSecurityConfig must strip trailing whitespace and surrounding quotes from AUTH_TOKEN")
}
