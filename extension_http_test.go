package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"paperless-gpt/extension"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

type fakeHTTPExtension struct {
	name string
}

func (e fakeHTTPExtension) Name() string                { return e.name }
func (e fakeHTTPExtension) EnvVars() []extension.EnvVar { return nil }
func (e fakeHTTPExtension) Start(context.Context) error { return nil }
func (e fakeHTTPExtension) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, e.name+":"+r.URL.Path)
	})
}

func TestExtensionRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	extension.Reset()
	t.Cleanup(extension.Reset)
	extension.Register(fakeHTTPExtension{name: "reference-data"})
	extension.Register(fakeHTTPExtension{name: "Not URL safe"})

	router := gin.New()
	registerExtensionRoutes(router)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/extensions/reference-data/api/import", nil))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "reference-data:/api/import", rec.Body.String(), "the mount prefix is stripped")

	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/extensions/Not%20URL%20safe/x", nil))
	assert.Equal(t, http.StatusNotFound, rec.Code, "an extension with an unsafe name is not mounted")
}

func TestSPAFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.NoRoute(spaFallback(func(c *gin.Context) { c.String(http.StatusOK, "index") }))

	get := func(path, accept string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Accept", accept)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}
	const html = "text/html,application/xhtml+xml"
	assert.Equal(t, "index", get("/reference-data", html).Body.String(), "extension page routes load the app")
	assert.Equal(t, "index", get("/branding/", html).Body.String())
	for _, path := range []string{"/api", "/api/unknown", "/extensions", "/assets", "/a/b", "/logo.png", "/Upper"} {
		assert.Equal(t, http.StatusNotFound, get(path, html).Code, path)
	}
	assert.Equal(t, http.StatusNotFound, get("/reference-data", "application/json").Code, "only browser navigations")
}
