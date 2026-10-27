package main

import (
	"context"
	"maps"
	"os"
	"slices"
	"sync"
	"time"
)

// catalogCache keeps one paperless-ngx catalog (tags, correspondents or
// document types) for a bounded time. It is opt-in: with a TTL of 0 every call
// fetches, exactly as without a cache.
type catalogCache[T any] struct {
	mu       sync.Mutex
	val      T
	cachedAt time.Time
	valid    bool
}

// get returns the cached catalog while it is younger than ttl and otherwise
// fetches and stores it. hit reports whether the value came from the cache.
// The lock is held across the fetch so concurrent misses share one request
// and an invalidate cannot be overwritten by a fetch that started before it.
func (c *catalogCache[T]) get(ttl time.Duration, fetch func() (T, error)) (val T, hit bool, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.valid && time.Since(c.cachedAt) < ttl {
		return c.val, true, nil
	}
	val, err = fetch()
	if err == nil && ttl > 0 {
		c.val, c.cachedAt, c.valid = val, time.Now(), true
	}
	return val, false, err
}

func (c *catalogCache[T]) invalidate() {
	c.mu.Lock()
	c.valid = false
	c.mu.Unlock()
}

func catalogCacheTTLFromEnv() time.Duration {
	raw := os.Getenv("PAPERLESS_CATALOG_CACHE_TTL")
	if raw == "" {
		return 0
	}
	ttl, err := time.ParseDuration(raw)
	if err != nil || ttl < 0 {
		log.Warnf("Ignoring invalid PAPERLESS_CATALOG_CACHE_TTL %q; catalog cache disabled", raw)
		return 0
	}
	return ttl
}

// invalidateCatalogs drops every cached catalog.
func (client *PaperlessClient) invalidateCatalogs() {
	client.tagCache.invalidate()
	client.correspondentCache.invalidate()
	client.documentTypeCache.invalidate()
}

func (client *PaperlessClient) tags(ctx context.Context) (map[string]int, bool, error) {
	tags, hit, err := client.tagCache.get(client.CatalogCacheTTL, func() (map[string]int, error) {
		return client.fetchAllTags(ctx)
	})
	return maps.Clone(tags), hit, err
}

// GetAllTags returns the tag catalog (name -> ID).
func (client *PaperlessClient) GetAllTags(ctx context.Context) (map[string]int, error) {
	tags, _, err := client.tags(ctx)
	return tags, err
}

// tagsResolving returns the tag catalog for resolving the given tag IDs. A
// cached catalog that does not know one of them is treated as a miss and
// refetched once: the ID most likely belongs to a tag created after the cache
// was filled, and reporting it as invisible would keep it on the document.
func (client *PaperlessClient) tagsResolving(ctx context.Context, ids []int) (map[string]int, error) {
	tags, hit, err := client.tags(ctx)
	if err != nil || !hit {
		return tags, err
	}
	known := make(map[int]bool, len(tags))
	for _, id := range tags {
		known[id] = true
	}
	for _, id := range ids {
		if !known[id] {
			client.tagCache.invalidate()
			return client.GetAllTags(ctx)
		}
	}
	return tags, nil
}

// GetAllCorrespondents returns the correspondent catalog (name -> ID).
func (client *PaperlessClient) GetAllCorrespondents(ctx context.Context) (map[string]int, error) {
	correspondents, _, err := client.correspondentCache.get(client.CatalogCacheTTL, func() (map[string]int, error) {
		return client.fetchAllCorrespondents(ctx)
	})
	return maps.Clone(correspondents), err
}

// GetAllDocumentTypes returns the document-type catalog.
func (client *PaperlessClient) GetAllDocumentTypes(ctx context.Context) ([]DocumentType, error) {
	documentTypes, _, err := client.documentTypeCache.get(client.CatalogCacheTTL, func() ([]DocumentType, error) {
		return client.fetchAllDocumentTypes(ctx)
	})
	return slices.Clone(documentTypes), err
}
