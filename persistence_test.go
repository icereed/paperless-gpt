package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A trimmed /proc/self/mountinfo from a container that mounts ./prompts and
// ./config but not ./db, plus a host path with a space in it.
const sampleMountinfo = `1340 1219 0:385 / / rw,relatime master:504 - overlay overlay rw,lowerdir=/var/lib/docker/overlay2/l/A
1341 1340 0:387 / /proc rw,nosuid,nodev,noexec,relatime - proc proc rw
1350 1340 254:1 /home/u/setup/prompts /app/prompts rw,relatime - ext4 /dev/vda1 rw
1351 1340 254:1 /home/u/setup/config /app/config rw,relatime - ext4 /dev/vda1 rw
1352 1340 254:1 /home/u/my\040docs /data/my\040docs rw,relatime - ext4 /dev/vda1 rw
`

func TestParseMountPoints(t *testing.T) {
	mounts := parseMountPoints(strings.NewReader(sampleMountinfo))
	assert.True(t, mounts["/"])
	assert.True(t, mounts["/app/prompts"])
	assert.True(t, mounts["/app/config"])
	assert.True(t, mounts["/data/my docs"], "octal escapes are decoded")
	assert.False(t, mounts["/app/db"])
}

func TestPersistenceIssues(t *testing.T) {
	mounts := parseMountPoints(strings.NewReader(sampleMountinfo))

	issues := persistenceIssues("/app", mounts)
	require.Len(t, issues, 1, "only db is missing")
	assert.Equal(t, "db", issues[0].Dir)
	assert.Equal(t, "/app/db", issues[0].Path)
	assert.Equal(t, "./db:/app/db", issues[0].Fix)

	// Mounting a parent directory covers everything below it.
	issues = persistenceIssues("/app", map[string]bool{"/": true, "/app": true})
	assert.Empty(t, issues)

	// "/" itself is the container's own file system and does not count.
	issues = persistenceIssues("/app", map[string]bool{"/": true})
	assert.Len(t, issues, 3)
}

func TestPersistenceIssuesFlagsLegacyPromptsMount(t *testing.T) {
	mounts := map[string]bool{"/": true, "/app/config": true, "/app/db": true, "/root/prompts": true}
	issues := persistenceIssues("/app", mounts)

	var dirs []string
	for _, i := range issues {
		dirs = append(dirs, i.Dir)
	}
	assert.ElementsMatch(t, []string{"prompts", "/root/prompts"}, dirs,
		"the old /root/prompts mount does not persist /app/prompts and is reported itself")
	for _, i := range issues {
		if i.Dir == "/root/prompts" {
			assert.Equal(t, "./prompts:/app/prompts", i.Fix)
		}
	}
}
