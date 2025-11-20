package cache

import (
	"sync"
	"time"
)

type MemoryCache struct {
	mu    sync.RWMutex
	items map[string]cacheItem
}

type cacheItem struct {
	value      interface{}
	expiration time.Time
}

func NewMemoryCache() *MemoryCache {
	c := &MemoryCache{
		items: make(map[string]cacheItem),
	}
	go c.cleanup()
	return c
}

func (m *MemoryCache) Set(key string, value interface{}, ttl time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.items[key] = cacheItem{
		value:      value,
		expiration: time.Now().Add(ttl),
	}
}

func (m *MemoryCache) Get(key string) (interface{}, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	item, exists := m.items[key]
	if !exists {
		return nil, false
	}

	if time.Now().After(item.expiration) {
		return nil, false
	}

	return item.value, true
}

func (m *MemoryCache) cleanup() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		m.mu.Lock()
		now := time.Now()
		for key, item := range m.items {
			if now.After(item.expiration) {
				delete(m.items, key)
			}
		}
		m.mu.Unlock()
	}
}

