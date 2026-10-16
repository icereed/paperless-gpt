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

// spaFallback serves the web app's index.html for client-side routes that
// have no explicit server route, such as pages contributed by a UI extension.
// Only plain single-segment page paths qualify; API, extension, asset and
// file-like paths keep their 404.
func spaFallback(serveIndex gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		path := strings.Trim(c.Request.URL.Path, "/")
		if c.Request.Method == http.MethodGet && spaRoutePattern.MatchString(path) &&
			path != "api" && path != "extensions" && path != "assets" &&
			strings.Contains(c.GetHeader("Accept"), "text/html") {
			serveIndex(c)
			return
		}
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
	}
}

var spaRoutePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
