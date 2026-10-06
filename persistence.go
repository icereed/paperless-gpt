package main

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// paperless-gpt keeps everything users create in the UI in three
// directories. In a container, each of them is lost when the container is
// recreated (every image update) unless it is mounted as a volume. That
// failure is silent: settings, prompts and workflows simply reset after the
// next update. The check below makes it visible at startup and in the UI.

// PersistenceIssue is a directory whose content will not survive the
// container being recreated.
type PersistenceIssue struct {
	Dir   string `json:"dir"`   // as used by the app, e.g. "config"
	Path  string `json:"path"`  // absolute path inside the container
	Holds string `json:"holds"` // what is lost; empty for the legacy prompts mount
	Fix   string `json:"fix"`   // compose volume line that fixes it
}

var persistentDirs = []struct{ dir, holds string }{
	{"config", "settings made in the UI (custom fields, OCR defaults)"},
	{"prompts", "prompt templates and AI workflows"},
	{"db", "the history used for undo, and the OCR run log"},
}

// legacyPromptsMount is where images before #30 (October 2024) read prompts
// from. Compose files from that time still mount it, and the mount has had
// no effect since.
const legacyPromptsMount = "/root/prompts"

// checkPersistence returns the directories that are not on a volume. Outside
// a container it returns nothing: there the directories are on the host.
func checkPersistence() []PersistenceIssue {
	if !runningInContainer() {
		return nil
	}
	f, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return nil
	}
	defer f.Close()
	mounts := parseMountPoints(f)
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	return persistenceIssues(cwd, mounts)
}

func persistenceIssues(cwd string, mounts map[string]bool) []PersistenceIssue {
	var issues []PersistenceIssue
	for _, d := range persistentDirs {
		path := filepath.Join(cwd, d.dir)
		if onMount(path, mounts) {
			continue
		}
		issues = append(issues, PersistenceIssue{
			Dir:   d.dir,
			Path:  path,
			Holds: d.holds,
			Fix:   fmt.Sprintf("./%s:%s", d.dir, path),
		})
	}
	if mounts[legacyPromptsMount] && filepath.Join(cwd, "prompts") != legacyPromptsMount {
		issues = append(issues, PersistenceIssue{
			Dir:   legacyPromptsMount,
			Path:  legacyPromptsMount,
			Holds: "",
			Fix:   "./prompts:" + filepath.Join(cwd, "prompts"),
		})
	}
	return issues
}

// onMount reports whether path, or a directory above it other than "/", is
// a mount point.
func onMount(path string, mounts map[string]bool) bool {
	for p := filepath.Clean(path); p != "/" && p != "."; p = filepath.Dir(p) {
		if mounts[p] {
			return true
		}
	}
	return false
}

// parseMountPoints reads /proc/self/mountinfo and returns the mount points
// that keep their content: memory-backed file systems (tmpfs, ramfs) are
// left out, since they are empty after every restart. The mount point is
// the fifth field (octal-escaped, e.g. \040 for a space); the file system
// type follows the "-" separator.
func parseMountPoints(r io.Reader) map[string]bool {
	mounts := map[string]bool{}
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 5 {
			continue
		}
		for i := 5; i+1 < len(fields); i++ {
			if fields[i] == "-" {
				if fsType := fields[i+1]; fsType == "tmpfs" || fsType == "ramfs" {
					fields = nil
				}
				break
			}
		}
		if fields == nil {
			continue
		}
		mounts[unescapeMountPath(fields[4])] = true
	}
	return mounts
}

func unescapeMountPath(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			if n, err := strconv.ParseUint(s[i+1:i+4], 8, 8); err == nil {
				b.WriteByte(byte(n))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func runningInContainer() bool {
	for _, marker := range []string{"/.dockerenv", "/run/.containerenv"} {
		if _, err := os.Stat(marker); err == nil {
			return true
		}
	}
	return false
}

// logPersistenceIssues warns at startup, where operators look first.
func logPersistenceIssues(issues []PersistenceIssue) {
	for _, issue := range issues {
		if issue.Dir == legacyPromptsMount {
			log.Warnf("%s is mounted, but paperless-gpt reads prompts from %s. Its contents are ignored. Change the volume to %q.", legacyPromptsMount, strings.TrimPrefix(issue.Fix, "./prompts:"), issue.Fix)
			continue
		}
		log.Warnf("%s is not on a volume: %s will be lost when the container is recreated (e.g. on every update). Add the volume %q.", issue.Path, issue.Holds, issue.Fix)
	}
}

// persistenceHandler handles GET /api/persistence so the UI can show the
// same warning where settings and workflows are made.
func persistenceHandler(issues []PersistenceIssue) gin.HandlerFunc {
	return func(c *gin.Context) {
		if issues == nil {
			issues = []PersistenceIssue{}
		}
		c.JSON(http.StatusOK, gin.H{"issues": issues})
	}
}
