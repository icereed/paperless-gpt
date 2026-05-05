package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// ---------------------------------------------------------------------------
// DB models
// ---------------------------------------------------------------------------

// User stores a local paperless-gpt account.
type User struct {
	ID                  string `gorm:"primaryKey;size:64"`
	Username            string `gorm:"uniqueIndex;size:255;not null"`
	HashedPassword      string `gorm:"size:255;not null"`
	ForcePasswordChange bool   `gorm:"not null;default:false"`
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

// UserSession is a server-side session record; its ID is the opaque cookie value.
type UserSession struct {
	ID         string `gorm:"primaryKey;size:64"`
	UserID     string `gorm:"index;size:64;not null"`
	CreatedAt  time.Time
	ExpiresAt  time.Time `gorm:"index;not null"`
	LastSeenAt time.Time
	IPAddress  string `gorm:"size:64"`
	UserAgent  string `gorm:"size:512"`
}

// ---------------------------------------------------------------------------
// Password helpers
// ---------------------------------------------------------------------------

func hashPassword(plain string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	return string(b), err
}

func verifyPassword(plain, hashed string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hashed), []byte(plain)) == nil
}

// ---------------------------------------------------------------------------
// Session helpers
// ---------------------------------------------------------------------------

const (
	sessionIdleSeconds    = 24 * 60 * 60     // 24 h sliding window
	sessionHardMaxSeconds = 7 * 24 * 60 * 60 // 7-day absolute ceiling
)

func generateSessionID() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func generateUserID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func createSession(db *gorm.DB, userID, ip, ua string) *UserSession {
	now := time.Now().UTC()
	s := &UserSession{
		ID:         generateSessionID(),
		UserID:     userID,
		CreatedAt:  now,
		ExpiresAt:  now.Add(sessionIdleSeconds * time.Second),
		LastSeenAt: now,
		IPAddress:  ip,
		UserAgent:  ua,
	}
	if err := db.Create(s).Error; err != nil {
		log.Errorf("createSession: %v", err)
		return nil
	}
	return s
}

func getSession(db *gorm.DB, id string) *UserSession {
	now := time.Now().UTC()
	var s UserSession
	if err := db.First(&s, "id = ?", id).Error; err != nil {
		return nil
	}
	if s.ExpiresAt.Before(now) {
		db.Delete(&s)
		return nil
	}
	if now.Sub(s.CreatedAt) > sessionHardMaxSeconds*time.Second {
		db.Delete(&s)
		return nil
	}
	// Slide the window
	s.ExpiresAt = now.Add(sessionIdleSeconds * time.Second)
	s.LastSeenAt = now
	if err := db.Save(&s).Error; err != nil {
		log.Errorf("getSession: failed to slide session expiry for %s: %v", id, err)
	}
	return &s
}

func deleteSession(db *gorm.DB, id string) {
	db.Delete(&UserSession{}, "id = ?", id)
}

// ---------------------------------------------------------------------------
// Session cookie helpers
// ---------------------------------------------------------------------------

const sessionCookieName = "paperless_gpt_session"

func setSessionCookie(c *gin.Context, sessionID string, secure bool) {
	cookie := &http.Cookie{
		Name:     sessionCookieName,
		Value:    sessionID,
		MaxAge:   sessionIdleSeconds,
		Path:     "/",
		Secure:   secure,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	}
	http.SetCookie(c.Writer, cookie)
}

func clearSessionCookie(c *gin.Context) {
	secure := c.Request.TLS != nil || c.GetHeader("X-Forwarded-Proto") == "https"
	cookie := &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		MaxAge:   -1,
		Path:     "/",
		Secure:   secure,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	}
	http.SetCookie(c.Writer, cookie)
}

// ---------------------------------------------------------------------------
// Request / response types
// ---------------------------------------------------------------------------

type loginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type userOut struct {
	ID                  string `json:"id"`
	Username            string `json:"username"`
	ForcePasswordChange bool   `json:"force_password_change"`
}

type setupRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password" binding:"required"`
	NewPassword     string `json:"new_password"     binding:"required"`
}

// ---------------------------------------------------------------------------
// isSessionAuthEnabled reports whether session-based user auth is active.
// It is enabled when at least one User row exists OR when AUTH_USER_ENABLED
// is explicitly set to "true". This allows the first-run setup to proceed
// without blocking the API before any user exists.
// ---------------------------------------------------------------------------

func isSessionAuthEnabled(db *gorm.DB) bool {
	if strings.ToLower(os.Getenv("AUTH_USER_ENABLED")) == "true" {
		return true
	}
	var count int64
	db.Model(&User{}).Count(&count)
	return count > 0
}

// ---------------------------------------------------------------------------
// sessionAuthMiddleware gates all API routes behind a valid session cookie.
//
// Paths that are always allowed without a session:
//   - /api/auth/*          (login, logout, setup, me, change-password)
//   - OAuth callbacks      (third-party redirects)
//   - Jobber receipt URLs  (self-authenticating token in path)
//
// When no users exist yet the middleware is a transparent no-op so the
// first-run setup wizard can reach /api/auth/setup.
// ---------------------------------------------------------------------------

// publicAuthPaths are the auth endpoints that must work without any session
// (login, setup, setup-status). All other /api/auth/* endpoints still require
// a valid session so that e.g. /me and /change-password are protected.
var publicAuthPaths = map[string]bool{
	"/api/auth/login":        true,
	"/api/auth/logout":       true,
	"/api/auth/setup":        true,
	"/api/auth/setup/status": true,
}

func sessionAuthMiddleware(db *gorm.DB, cfg SecurityConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path

		// Always-public paths
		// Frontend shell/assets MUST be reachable without a session so the React app
		// can load and render LoginPage when the session is missing/expired. Without
		// this, an expired session wedges the whole UI: GET / returns a JSON 401 and
		// the user has no way back in short of wiping the users table.
		if isPublicFrontendPath(path) ||
			publicAuthPaths[path] ||
			path == "/api/paperless/webhook" ||
			isBricoproHQAPIPath(path) ||
			strings.HasSuffix(path, "/oauth/callback") ||
			strings.Contains(path, "/integrations/jobber/receipt/") {
			c.Next()
			return
		}

		// No users yet → no auth required (first-run mode)
		if !isSessionAuthEnabled(db) {
			c.Next()
			return
		}

		// Resolve session from cookie
		sessionID, err := c.Cookie(sessionCookieName)
		if err != nil || strings.TrimSpace(sessionID) == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Not authenticated"})
			return
		}

		session := getSession(db, sessionID)
		if session == nil {
			clearSessionCookie(c)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Session expired or invalid"})
			return
		}

		// Load user
		var user User
		if err := db.First(&user, "id = ?", session.UserID).Error; err != nil {
			clearSessionCookie(c)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "User not found"})
			return
		}

		c.Set("currentUser", &user)
		c.Set("currentSession", session)
		c.Next()
	}
}

// currentUser extracts the authenticated user from Gin context (set by sessionAuthMiddleware).
func currentUser(c *gin.Context) *User {
	if v, ok := c.Get("currentUser"); ok {
		if u, ok := v.(*User); ok {
			return u
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Auth HTTP handlers
// ---------------------------------------------------------------------------

// setupStatusHandler — GET /api/auth/setup/status
// Always public: tells the frontend whether first-run setup is still needed.
func (app *App) setupStatusHandler(c *gin.Context) {
	var count int64
	app.Database.Model(&User{}).Count(&count)
	c.JSON(http.StatusOK, gin.H{"setup_required": count == 0})
}

// createFirstAdminHandler — POST /api/auth/setup
// Creates the first admin account. Returns 403 once a user already exists.
// The count-check and insert run inside a single transaction to prevent a
// race where two concurrent requests both see count==0 and create two accounts.
func (app *App) createFirstAdminHandler(c *gin.Context) {
	var req setupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "username and password are required"})
		return
	}
	if len(req.Password) < 8 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Password must be at least 8 characters"})
		return
	}
	if strings.TrimSpace(req.Username) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Username must not be empty"})
		return
	}

	hashed, err := hashPassword(req.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to hash password"})
		return
	}

	user := &User{
		ID:             generateUserID(),
		Username:       strings.TrimSpace(req.Username),
		HashedPassword: hashed,
	}

	var created bool
	txErr := app.Database.Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&User{}).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return nil // signal "already exists" without error
		}
		created = true
		return tx.Create(user).Error
	})
	if txErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create user"})
		log.Errorf("createFirstAdminHandler: %v", txErr)
		return
	}
	if !created {
		c.JSON(http.StatusForbidden, gin.H{"error": "Setup already completed"})
		return
	}

	log.Infof("First admin account created: %s", user.Username)

	session := createSession(app.Database, user.ID, c.ClientIP(), c.GetHeader("User-Agent"))
	if session == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Account created but failed to start session — please log in"})
		return
	}
	secure := c.Request.TLS != nil || c.GetHeader("X-Forwarded-Proto") == "https"
	setSessionCookie(c, session.ID, secure)

	c.JSON(http.StatusCreated, userOut{
		ID:                  user.ID,
		Username:            user.Username,
		ForcePasswordChange: user.ForcePasswordChange,
	})
}

// loginHandler — POST /api/auth/login
func (app *App) loginHandler(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "username and password are required"})
		return
	}

	var user User
	needle := strings.TrimSpace(req.Username)
	if err := app.Database.First(&user, "username = ?", needle).Error; err != nil {
		// Use constant-time comparison placeholder to prevent timing leaks
		_ = subtle.ConstantTimeCompare([]byte("x"), []byte("y"))
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid username or password"})
		return
	}

	if !verifyPassword(req.Password, user.HashedPassword) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid username or password"})
		return
	}

	session := createSession(app.Database, user.ID, c.ClientIP(), c.GetHeader("User-Agent"))
	if session == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create session"})
		return
	}

	secure := c.Request.TLS != nil || c.GetHeader("X-Forwarded-Proto") == "https"
	setSessionCookie(c, session.ID, secure)

	c.JSON(http.StatusOK, userOut{
		ID:                  user.ID,
		Username:            user.Username,
		ForcePasswordChange: user.ForcePasswordChange,
	})
}

// logoutHandler — POST /api/auth/logout
func (app *App) logoutHandler(c *gin.Context) {
	sessionID, err := c.Cookie(sessionCookieName)
	if err == nil && sessionID != "" {
		deleteSession(app.Database, sessionID)
	}
	clearSessionCookie(c)
	c.JSON(http.StatusOK, gin.H{"logged_out": true})
}

// meHandler — GET /api/auth/me
func (app *App) meHandler(c *gin.Context) {
	user := currentUser(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Not authenticated"})
		return
	}
	c.JSON(http.StatusOK, userOut{
		ID:                  user.ID,
		Username:            user.Username,
		ForcePasswordChange: user.ForcePasswordChange,
	})
}

// changePasswordHandler — POST /api/auth/change-password
func (app *App) changePasswordHandler(c *gin.Context) {
	user := currentUser(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Not authenticated"})
		return
	}

	var req changePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "current_password and new_password are required"})
		return
	}

	if !verifyPassword(req.CurrentPassword, user.HashedPassword) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Current password is incorrect"})
		return
	}
	if len(req.NewPassword) < 8 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "New password must be at least 8 characters"})
		return
	}

	hashed, err := hashPassword(req.NewPassword)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to hash password"})
		return
	}

	if err := app.Database.Model(user).Updates(map[string]interface{}{
		"hashed_password":       hashed,
		"force_password_change": false,
	}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update password"})
		log.Errorf("changePasswordHandler: %v", err)
		return
	}

	// Invalidate all other sessions for this user
	app.Database.Delete(&UserSession{}, "user_id = ? AND id != ?", user.ID,
		func() string {
			if s, ok := c.Get("currentSession"); ok {
				if sess, ok := s.(*UserSession); ok {
					return sess.ID
				}
			}
			return ""
		}())

	c.JSON(http.StatusOK, gin.H{"changed": true})
}
