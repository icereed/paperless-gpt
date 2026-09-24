package main

import (
	"net/http"
	"regexp"
	"strings"

	"paperless-gpt/extension"

	"github.com/gin-gonic/gin"
)

// extensionNamePattern keeps extension mount points URL-safe.
var extensionNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// ExtensionPageView is one sidebar entry for a page an extension contributes.
type ExtensionPageView struct {
	// ID identifies the page in the /extension?page= route.
	ID    string `json:"id"`
	Title string `json:"title"`
	// URL is relative to the app's base path, so it survives reverse-proxy
	// prefixes.
	URL string `json:"url"`
}

// mountableExtensions returns the HTTP extensions whose name can be used as
// a mount point; the others are skipped with an error log.
func mountableExtensions() []extension.HTTPExtension {
	var result []extension.HTTPExtension
	for _, e := range extension.HTTPExtensions() {
		if !extensionNamePattern.MatchString(e.Name()) {
			log.Errorf("Extension %q is not mounted: its name must match %s", e.Name(), extensionNamePattern)
			continue
		}
		result = append(result, e)
	}
	return result
}

// registerExtensionRoutes mounts every HTTP extension at
// /extensions/<name>/, stripping that prefix before calling its handler.
func registerExtensionRoutes(router *gin.Engine) {
	for _, e := range mountableExtensions() {
		prefix := "/extensions/" + e.Name()
		handler := http.StripPrefix(prefix, e.Handler())
		router.Any(prefix+"/*path", gin.WrapH(handler))
		log.Infof("Extension %s mounted at %s/", e.Name(), prefix)
	}
}

// extensionPages lists the pages of all mounted extensions.
func extensionPages() []ExtensionPageView {
	pages := []ExtensionPageView{}
	for _, e := range mountableExtensions() {
		for _, p := range e.Pages() {
			path := strings.TrimPrefix(p.Path, "/")
			pages = append(pages, ExtensionPageView{
				ID:    e.Name() + "/" + path,
				Title: p.Title,
				URL:   "extensions/" + e.Name() + "/" + path,
			})
		}
	}
	return pages
}

// getExtensionPagesHandler serves GET /api/extensions/pages for the sidebar.
func getExtensionPagesHandler(c *gin.Context) {
	c.JSON(http.StatusOK, extensionPages())
}
