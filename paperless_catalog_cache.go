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
	// unresolved records IDs a fetch could not resolve (e.g. objects the
	// API user cannot see) and when, so they do not trigger another refresh
	// until the TTL has passed.
	unresolved map[int]time.Time
}

// get returns the cached catalog while it is younger than ttl and otherwise
// fetches and stores it. hit reports whether the value came from the cache.
// With caching enabled the lock is held across the fetch so concurrent misses
// share one request and an invalidate cannot be overwritten by a fetch that
// started before it. With caching disabled fetches run in parallel.
func (c *catalogCache[T]) get(ttl time.Duration, fetch func() (T, error)) (val T, hit bool, err error) {
	if ttl <= 0 {
		val, err = fetch()
		return val, false, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.valid && time.Since(c.cachedAt) < ttl {
		return c.val, true, nil
	}
	val, err = fetch()
	if err == nil {
		c.val, c.cachedAt, c.valid = val, time.Now(), true
	}
	return val, false, err
}

// refresh fetches the catalog and replaces the cached value only on success,
// so a failed fetch leaves the previous catalog valid until its TTL expires.
func (c *catalogCache[T]) refresh(fetch func() (T, error)) (T, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	val, err := fetch()
	if err == nil {
		c.val, c.cachedAt, c.valid = val, time.Now(), true
	}
	return val, err
}

func (c *catalogCache[T]) invalidate() {
	c.mu.Lock()
	c.valid = false
	c.mu.Unlock()
}

// resolving returns the catalog for resolving ids; idsOf lists the IDs a
// catalog knows. A cached catalog that does not know one of ids is refreshed
// once: the ID most likely belongs to an object created after the cache was
// filled, and reporting it as unknown would drop or overwrite it. An ID a
// fresh fetch cannot resolve does not trigger a refresh until the TTL has
// passed. A failed refresh keeps the cached catalog and returns the error,
// as the unknown ID cannot be resolved without it.
func (c *catalogCache[T]) resolving(ttl time.Duration, ids []int, idsOf func(T) []int, fetch func() (T, error)) (T, error) {
	val, hit, err := c.get(ttl, fetch)
	if err != nil || ttl <= 0 {
		return val, err
	}
	if hit {
		if !c.hasUnresolved(ttl, val, ids, idsOf, false) {
			return val, nil
		}
		if val, err = c.refresh(fetch); err != nil {
			return val, err
		}
	}
	c.hasUnresolved(ttl, val, ids, idsOf, true)
	return val, nil
}

// hasUnresolved reports whether one of ids is missing from val and was not
// already found missing by a fetch within ttl. With remember set, the
// missing IDs are recorded as found missing now.
func (c *catalogCache[T]) hasUnresolved(ttl time.Duration, val T, ids []int, idsOf func(T) []int, remember bool) bool {
	if len(ids) == 0 {
		return false
	}
	known := make(map[int]bool)
	for _, id := range idsOf(val) {
		known[id] = true
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.unresolved == nil {
		c.unresolved = map[int]time.Time{}
	}
	unresolved := false
	for _, id := range ids {
		if known[id] {
			continue
		}
		if remember {
			c.unresolved[id] = time.Now()
			continue
		}
		if seen, ok := c.unresolved[id]; ok && time.Since(seen) < ttl {
			continue
		}
		unresolved = true
	}
	return unresolved
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

func (client *PaperlessClient) fetchTags(ctx context.Context) func() (map[string]int, error) {
	return func() (map[string]int, error) { return client.fetchAllTags(ctx) }
}

// GetAllTags returns the tag catalog (name -> ID).
func (client *PaperlessClient) GetAllTags(ctx context.Context) (map[string]int, error) {
	tags, _, err := client.tagCache.get(client.CatalogCacheTTL, client.fetchTags(ctx))
	return maps.Clone(tags), err
}

// tagsResolving returns the tag catalog for resolving the given tag IDs.
func (client *PaperlessClient) tagsResolving(ctx context.Context, ids []int) (map[string]int, error) {
	tags, err := client.tagCache.resolving(client.CatalogCacheTTL, ids, catalogIDs, client.fetchTags(ctx))
	return maps.Clone(tags), err
}

// GetAllCorrespondents returns the correspondent catalog (name -> ID).
func (client *PaperlessClient) GetAllCorrespondents(ctx context.Context) (map[string]int, error) {
	return client.correspondentsResolving(ctx, 0)
}

// correspondentsResolving returns the correspondent catalog for resolving id
// (0 = none).
func (client *PaperlessClient) correspondentsResolving(ctx context.Context, id int) (map[string]int, error) {
	correspondents, err := client.correspondentCache.resolving(client.CatalogCacheTTL, nonZeroID(id), catalogIDs,
		func() (map[string]int, error) { return client.fetchAllCorrespondents(ctx) })
	return maps.Clone(correspondents), err
}

// GetAllDocumentTypes returns the document-type catalog.
func (client *PaperlessClient) GetAllDocumentTypes(ctx context.Context) ([]DocumentType, error) {
	return client.documentTypesResolving(ctx, 0)
}

// documentTypesResolving returns the document-type catalog for resolving id
// (0 = none).
func (client *PaperlessClient) documentTypesResolving(ctx context.Context, id int) ([]DocumentType, error) {
	documentTypes, err := client.documentTypeCache.resolving(client.CatalogCacheTTL, nonZeroID(id), documentTypeIDs,
		func() ([]DocumentType, error) { return client.fetchAllDocumentTypes(ctx) })
	return slices.Clone(documentTypes), err
}

func catalogIDs(catalog map[string]int) []int { return slices.Collect(maps.Values(catalog)) }

func documentTypeIDs(documentTypes []DocumentType) []int {
	ids := make([]int, len(documentTypes))
	for i, dt := range documentTypes {
		ids[i] = dt.ID
	}
	return ids
}

func nonZeroID(id int) []int {
	if id == 0 {
		return nil
	}
	return []int{id}
}
