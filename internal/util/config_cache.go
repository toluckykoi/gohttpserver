package util

import (
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ParseFunc 把文件原始字节解析为 T 类型值。
type ParseFunc[T any] func(data []byte) (*T, error)

// FileCache 按 mtime 失效缓存文件解析结果；为避开与 server 包的 import 循环而做成泛型。
type FileCache[T any] struct {
	mu    sync.RWMutex
	cache map[string]*cachedEntry[T]
	parse ParseFunc[T]
}

type cachedEntry[T any] struct {
	value   *T
	modTime time.Time
}

// NewFileCache 创建缓存。
func NewFileCache[T any](parse ParseFunc[T]) *FileCache[T] {
	return &FileCache[T]{
		cache: make(map[string]*cachedEntry[T]),
		parse: parse,
	}
}

// Get 返回 filePath 的缓存值；文件不存在时清除该缓存项，mtime 变化则重新解析并刷新。
func (c *FileCache[T]) Get(filePath string) (*T, error) {
	key := filepath.Clean(filePath)

	info, err := os.Stat(key)
	if err != nil {
		if os.IsNotExist(err) {
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

// Invalidate 从缓存中移除单个路径。
func (c *FileCache[T]) Invalidate(filePath string) {
	key := filepath.Clean(filePath)
	c.mu.Lock()
	delete(c.cache, key)
	c.mu.Unlock()
}

// InvalidateAll 清空整个缓存。
func (c *FileCache[T]) InvalidateAll() {
	c.mu.Lock()
	c.cache = make(map[string]*cachedEntry[T])
	c.mu.Unlock()
}
