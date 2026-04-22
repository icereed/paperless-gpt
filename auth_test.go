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
	r.Use(sessionAuthMiddleware(db))

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

// TestLoginWithBasicAuthConfigured verifies that /api/auth/login is reachable even
// when HTTP Basic Auth (AUTH_USERNAME/AUTH_PASSWORD) is configured. The auth routes
// are exempted from the static-credentials middleware by isExemptFromAuth.
func TestLoginWithBasicAuthConfigured(t *testing.T) {
	db := newTestDB(t)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(sessionAuthMiddleware(db))
	// Simulate AUTH_USERNAME + AUTH_PASSWORD being set
	staticCfg := SecurityConfig{AuthUsername: "admin", AuthPassword: "hunter2"}
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
