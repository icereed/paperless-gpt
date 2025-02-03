package main

import (
	"embed"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/gin-gonic/gin"
)

//go:embed dist/*
var webappContent embed.FS

// CreateEmbeddedFileServer creates a http.FileSystem from our embedded files
func createEmbeddedFileServer() http.FileSystem {
	// Strip the "dist" prefix from the embedded files
	stripped, err := fs.Sub(webappContent, "dist")
	if err != nil {
		panic(err)
	}
	return http.FS(stripped)
}

// ServeEmbeddedFile serves a file from the embedded filesystem
func serveEmbeddedFile(c *gin.Context, prefix string, filepath string) {
	// If the path is empty or ends with "/", serve index.html
	if filepath == "" || strings.HasSuffix(filepath, "/") {
		filepath = path.Join(filepath, "index.html")
	}

	// Try to open the file from our embedded filesystem
	fullPath := path.Join("dist", prefix, filepath)
	f, err := webappContent.Open(fullPath)
	if err != nil {
		// If file not found, serve 404
		log.Warnf("File not found: %s", fullPath)
		http.Error(c.Writer, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		c.Status(http.StatusNotFound)
		return
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}

	// Serve the file
	http.ServeContent(c.Writer, c.Request, stat.Name(), stat.ModTime(), f.(io.ReadSeeker))
}
