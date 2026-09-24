package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"paperless-gpt/extension"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeHTTPExtension struct {
	name  string
	pages []extension.Page
}

func (e fakeHTTPExtension) Name() string                { return e.name }
func (e fakeHTTPExtension) EnvVars() []extension.EnvVar { return nil }
func (e fakeHTTPExtension) Start(context.Context) error { return nil }
func (e fakeHTTPExtension) Pages() []extension.Page     { return e.pages }
func (e fakeHTTPExtension) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, e.name+":"+r.URL.Path)
	})
}

func TestExtensionRoutesAndPages(t *testing.T) {
	gin.SetMode(gin.TestMode)
	extension.Reset()
	t.Cleanup(extension.Reset)
	extension.Register(fakeHTTPExtension{name: "master-data", pages: []extension.Page{{Title: "Master data", Path: ""}}})
	extension.Register(fakeHTTPExtension{name: "Not URL safe", pages: []extension.Page{{Title: "Hidden"}}})

	router := gin.New()
	router.GET("/api/extensions/pages", getExtensionPagesHandler)
	registerExtensionRoutes(router)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/extensions/master-data/api/import", nil))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "master-data:/api/import", rec.Body.String(), "the mount prefix is stripped")

	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/extensions/pages", nil))
	var pages []ExtensionPageView
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &pages))
	assert.Equal(t, []ExtensionPageView{{ID: "master-data/", Title: "Master data", URL: "extensions/master-data/"}}, pages,
		"an extension with an unsafe name is neither mounted nor listed")
}

func TestExtensionPagesEmptyByDefault(t *testing.T) {
	extension.Reset()
	t.Cleanup(extension.Reset)
	assert.Equal(t, []ExtensionPageView{}, extensionPages())
}
