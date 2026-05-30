package core

import (
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	CacheStatusHit  = "hit"
	CacheStatusMiss = "miss"
)

// DefaultCacheMaxEntries bounds each of the search/extract cache maps so a
// long-lived MCP server issuing many distinct queries cannot grow the cache
// without limit (entries were previously only freed lazily on a Get that found
// them TTL-expired, which may never happen for one-off queries). Override via
// NOLE_CACHE_MAX_ENTRIES.
const DefaultCacheMaxEntries = 1024

type ResponseCache interface {
	GetSearch(SearchRequest) (SearchResponse, bool)
	SetSearch(SearchRequest, SearchResponse)
	GetExtract(ExtractRequest) (ExtractResponse, bool)
	SetExtract(ExtractRequest, ExtractResponse)
}

type MemoryResponseCache struct {
	mu         sync.Mutex
	ttl        time.Duration
	now        func() time.Time
	maxEntries int
	search     map[string]cachedSearchResponse
	extract    map[string]cachedExtractResponse
}

type cachedSearchResponse struct {
	storedAt time.Time
	resp     SearchResponse
}

type cachedExtractResponse struct {
	storedAt time.Time
	resp     ExtractResponse
}

func NewMemoryResponseCache(ttl time.Duration) *MemoryResponseCache {
	return NewMemoryResponseCacheWithClock(ttl, time.Now)
}

func NewMemoryResponseCacheWithClock(ttl time.Duration, now func() time.Time) *MemoryResponseCache {
	if now == nil {
		now = time.Now
	}
	return &MemoryResponseCache{
		ttl:        ttl,
		now:        now,
		maxEntries: DefaultCacheMaxEntries,
		search:     map[string]cachedSearchResponse{},
		extract:    map[string]cachedExtractResponse{},
	}
}

// SetMaxEntries overrides the per-map entry cap. Values <= 0 are ignored so the
// built-in DefaultCacheMaxEntries bound always remains in force.
func (c *MemoryResponseCache) SetMaxEntries(n int) {
	if c == nil || n <= 0 {
		return
	}
	c.mu.Lock()
	c.maxEntries = n
	c.mu.Unlock()
}

func (c *MemoryResponseCache) GetSearch(req SearchRequest) (SearchResponse, bool) {
	if c == nil || c.ttl <= 0 {
		return SearchResponse{}, false
	}
	key := searchCacheKey(req)
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.search[key]
	if !ok {
		return SearchResponse{}, false
	}
	if c.now().Sub(entry.storedAt) >= c.ttl {
		delete(c.search, key)
		return SearchResponse{}, false
	}
	return cloneSearchResponse(entry.resp), true
}

func (c *MemoryResponseCache) SetSearch(req SearchRequest, resp SearchResponse) {
	if c == nil || c.ttl <= 0 {
		return
	}
	key := searchCacheKey(req)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.search[key] = cachedSearchResponse{storedAt: c.now(), resp: cloneSearchResponse(resp)}
	for c.maxEntries > 0 && len(c.search) > c.maxEntries {
		c.evictOldestSearchLocked()
	}
}

// evictOldestSearchLocked removes the entry with the earliest storedAt. Callers
// hold c.mu. Eviction is FIFO-by-insertion-time, which suits a TTL cache: the
// oldest entry is also the closest to expiry.
func (c *MemoryResponseCache) evictOldestSearchLocked() {
	var oldestKey string
	var oldest time.Time
	found := false
	for k, v := range c.search {
		if !found || v.storedAt.Before(oldest) {
			oldest, oldestKey, found = v.storedAt, k, true
		}
	}
	if found {
		delete(c.search, oldestKey)
	}
}

func (c *MemoryResponseCache) GetExtract(req ExtractRequest) (ExtractResponse, bool) {
	if c == nil || c.ttl <= 0 {
		return ExtractResponse{}, false
	}
	key := extractCacheKey(req)
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.extract[key]
	if !ok {
		return ExtractResponse{}, false
	}
	if c.now().Sub(entry.storedAt) >= c.ttl {
		delete(c.extract, key)
		return ExtractResponse{}, false
	}
	return cloneExtractResponse(entry.resp), true
}

func (c *MemoryResponseCache) SetExtract(req ExtractRequest, resp ExtractResponse) {
	if c == nil || c.ttl <= 0 {
		return
	}
	key := extractCacheKey(req)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.extract[key] = cachedExtractResponse{storedAt: c.now(), resp: cloneExtractResponse(resp)}
	for c.maxEntries > 0 && len(c.extract) > c.maxEntries {
		c.evictOldestExtractLocked()
	}
}

// evictOldestExtractLocked removes the extract entry with the earliest
// storedAt. Callers hold c.mu.
func (c *MemoryResponseCache) evictOldestExtractLocked() {
	var oldestKey string
	var oldest time.Time
	found := false
	for k, v := range c.extract {
		if !found || v.storedAt.Before(oldest) {
			oldest, oldestKey, found = v.storedAt, k, true
		}
	}
	if found {
		delete(c.extract, oldestKey)
	}
}

func searchCacheKey(req SearchRequest) string {
	task := req.Task
	if task == "" {
		task = TaskGeneral
	}
	return strings.Join([]string{
		"search",
		string(task),
		normalizeCacheText(req.Query),
		intCachePart(req.Limit),
	}, "\x00")
}

func extractCacheKey(req ExtractRequest) string {
	format := strings.ToLower(strings.TrimSpace(req.Format))
	if format == "" {
		format = "markdown"
	}
	return strings.Join([]string{
		"extract",
		strings.TrimSpace(req.URL),
		format,
	}, "\x00")
}

func normalizeCacheText(text string) string {
	return strings.ToLower(strings.Join(strings.Fields(text), " "))
}

func intCachePart(v int) string {
	return strconv.Itoa(v)
}

func cloneSearchResponse(resp SearchResponse) SearchResponse {
	resp.Results = append([]SearchResult(nil), resp.Results...)
	resp.Route = append([]string(nil), resp.Route...)
	resp.RouteTrace = append([]RouteAttempt(nil), resp.RouteTrace...)
	return resp
}

func cloneExtractResponse(resp ExtractResponse) ExtractResponse {
	if resp.Metadata != nil {
		metadata := make(map[string]string, len(resp.Metadata))
		for k, v := range resp.Metadata {
			metadata[k] = v
		}
		resp.Metadata = metadata
	}
	resp.Route = append([]string(nil), resp.Route...)
	resp.RouteTrace = append([]RouteAttempt(nil), resp.RouteTrace...)
	return resp
}
