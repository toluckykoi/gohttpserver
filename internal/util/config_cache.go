package util

import (
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ParseFunc converts raw file bytes into a value of type T.
type ParseFunc[T any] func(data []byte) (*T, error)

// FileCache caches parsed file contents, invalidated by mtime.
//
// It is deliberately generic: the only caller that needs a config cache
// lives in the server package (which owns AccessConf), and putting the
// concrete type here would create an import cycle. The server supplies
// the parser when constructing the cache.
type FileCache[T any] struct {
	mu    sync.RWMutex
	cache map[string]*cachedEntry[T]
	parse ParseFunc[T]
}

// cachedEntry holds a parsed AccessConf with its filesystem metadata.
type cachedEntry[T any] struct {
	value   *T
	modTime time.Time
}

// NewFileCache creates a cache that parses files with the given function.
// If parse is nil, Get always re-reads and never caches.
func NewFileCache[T any](parse ParseFunc[T]) *FileCache[T] {
	return &FileCache[T]{
		cache: make(map[string]*cachedEntry[T]),
		parse: parse,
	}
}

// Get returns the cached value for filePath. If the file is missing it
// returns (nil, false). If the file changed since it was cached (mtime
// differs) it is re-parsed and the cache entry refreshed.
func (c *FileCache[T]) Get(filePath string) (*T, error) {
	key := filepath.Clean(filePath)

	// Stat before reading so the recorded mtime can only be older than
	// the bytes we read. If the file is rewritten between the stat and
	// the read, the entry is stored with the old mtime and the next Get
	// sees the mismatch and refreshes — never a stale entry that looks
	// fresh.
	info, err := os.Stat(key)
	if err != nil {
		if os.IsNotExist(err) {
			// File deleted: drop any stale entry so a later re-create
			// (possibly with the same mtime) is not served from cache.
			c.mu.Lock()
			delete(c.cache, key)
			c.mu.Unlock()
		}
		return nil, err
	}
	modTime := info.ModTime()

	c.mu.RLock()
	entry, ok := c.cache[key]
	c.mu.RUnlock()

	if ok && modTime.Equal(entry.modTime) {
		return entry.value, nil
	}

	if c.parse == nil {
		return nil, os.ErrInvalid
	}

	data, err := os.ReadFile(key)
	if err != nil {
		return nil, err
	}

	// Parse and cache
	value, err := c.parse(data)
	if err != nil {
		Logger().Warn("failed to parse cached file", ErrorAttr(err), slog.String("file", key))
		return nil, err
	}

	c.mu.Lock()
	c.cache[key] = &cachedEntry[T]{
		value:   value,
		modTime: modTime,
	}
	c.mu.Unlock()

	return value, nil
}

// Invalidate removes a single path from the cache.
func (c *FileCache[T]) Invalidate(filePath string) {
	key := filepath.Clean(filePath)
	c.mu.Lock()
	delete(c.cache, key)
	c.mu.Unlock()
}

// InvalidateAll clears the entire cache.
func (c *FileCache[T]) InvalidateAll() {
	c.mu.Lock()
	c.cache = make(map[string]*cachedEntry[T])
	c.mu.Unlock()
}
