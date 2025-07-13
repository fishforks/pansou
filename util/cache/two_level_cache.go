package cache

import (
	"sync"
	"time"

	"pansou/config"
)

// 简单的内存缓存项
type memoryCacheItem struct {
	data      []byte
	expiry    time.Time
	lastUsed  time.Time
	size      int
}

// 内存缓存
type MemoryCache struct {
	items     map[string]*memoryCacheItem
	mutex     sync.RWMutex
	maxItems  int
	maxSize   int64
	currSize  int64
}

// 创建新的内存缓存
func NewMemoryCache(maxItems int, maxSizeMB int) *MemoryCache {
	return &MemoryCache{
		items:    make(map[string]*memoryCacheItem),
		maxItems: maxItems,
		maxSize:  int64(maxSizeMB) * 1024 * 1024,
	}
}

// 设置缓存
func (c *MemoryCache) Set(key string, data []byte, ttl time.Duration) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	// 如果已存在，先减去旧项的大小
	if item, exists := c.items[key]; exists {
		c.currSize -= int64(item.size)
	}

	// 创建新的缓存项
	now := time.Now()
	item := &memoryCacheItem{
		data:     data,
		expiry:   now.Add(ttl),
		lastUsed: now,
		size:     len(data),
	}

	// 检查是否需要清理空间
	if len(c.items) >= c.maxItems || c.currSize+int64(len(data)) > c.maxSize {
		c.evict()
	}

	// 存储新项
	c.items[key] = item
	c.currSize += int64(len(data))
}

// 获取缓存
func (c *MemoryCache) Get(key string) ([]byte, bool) {
	c.mutex.RLock()
	item, exists := c.items[key]
	c.mutex.RUnlock()

	if !exists {
		return nil, false
	}

	// 检查是否过期
	if time.Now().After(item.expiry) {
		c.mutex.Lock()
		delete(c.items, key)
		c.currSize -= int64(item.size)
		c.mutex.Unlock()
		return nil, false
	}

	// 更新最后使用时间
	c.mutex.Lock()
	item.lastUsed = time.Now()
	c.mutex.Unlock()

	return item.data, true
}

// 驱逐策略 - LRU
func (c *MemoryCache) evict() {
	// 找出最久未使用的项
	var oldestKey string
	var oldestTime time.Time

	// 初始化为当前时间
	oldestTime = time.Now()

	for k, v := range c.items {
		if v.lastUsed.Before(oldestTime) {
			oldestKey = k
			oldestTime = v.lastUsed
		}
	}

	// 如果找到了最久未使用的项，删除它
	if oldestKey != "" {
		item := c.items[oldestKey]
		c.currSize -= int64(item.size)
		delete(c.items, oldestKey)
	}
}

// 清理过期项
func (c *MemoryCache) CleanExpired() {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	now := time.Now()
	for k, v := range c.items {
		if now.After(v.expiry) {
			c.currSize -= int64(v.size)
			delete(c.items, k)
		}
	}
}

// 启动定期清理
func (c *MemoryCache) StartCleanupTask() {
	ticker := time.NewTicker(5 * time.Minute)
	go func() {
		for range ticker.C {
			c.CleanExpired()
		}
	}()
}

// 两级缓存
type TwoLevelCache struct {
	memCache  *MemoryCache
	diskCache *DiskCache
}

// 创建新的两级缓存
func NewTwoLevelCache() (*TwoLevelCache, error) {
	// 内存缓存大小为磁盘缓存的60%
	memCacheMaxItems := 5000
	memCacheSizeMB := config.AppConfig.CacheMaxSizeMB * 3 / 5
	
	memCache := NewMemoryCache(memCacheMaxItems, memCacheSizeMB)
	memCache.StartCleanupTask()

	diskCache, err := NewDiskCache(config.AppConfig.CachePath, config.AppConfig.CacheMaxSizeMB)
	if err != nil {
		return nil, err
	}

	return &TwoLevelCache{
		memCache:  memCache,
		diskCache: diskCache,
	}, nil
}

// 设置缓存
func (c *TwoLevelCache) Set(key string, data []byte, ttl time.Duration) error {
	// 先设置内存缓存（这是快速操作，直接在当前goroutine中执行）
	c.memCache.Set(key, data, ttl)
	
	// 异步设置磁盘缓存（这是IO操作，可能较慢）
	go func(k string, d []byte, t time.Duration) {
		// 使用独立的goroutine写入磁盘，避免阻塞调用者
		_ = c.diskCache.Set(k, d, t)
	}(key, data, ttl)
	
	return nil
}

// 获取缓存
func (c *TwoLevelCache) Get(key string) ([]byte, bool, error) {
	// 优先检查内存缓存
	if data, found := c.memCache.Get(key); found {
		return data, true, nil
	}
	
	// 内存未命中，检查磁盘缓存
	data, found, err := c.diskCache.Get(key)
	if err != nil {
		return nil, false, err
	}
	
	if found {
		// 磁盘命中，更新内存缓存
		ttl := time.Duration(config.AppConfig.CacheTTLMinutes) * time.Minute
		c.memCache.Set(key, data, ttl)
		return data, true, nil
	}
	
	return nil, false, nil
}

// 删除缓存
func (c *TwoLevelCache) Delete(key string) error {
	// 从内存缓存删除
	c.memCache.mutex.Lock()
	if item, exists := c.memCache.items[key]; exists {
		c.memCache.currSize -= int64(item.size)
		delete(c.memCache.items, key)
	}
	c.memCache.mutex.Unlock()
	
	// 从磁盘缓存删除
	return c.diskCache.Delete(key)
}

// 清空所有缓存
func (c *TwoLevelCache) Clear() error {
	// 清空内存缓存
	c.memCache.mutex.Lock()
	c.memCache.items = make(map[string]*memoryCacheItem)
	c.memCache.currSize = 0
	c.memCache.mutex.Unlock()
	
	// 清空磁盘缓存
	return c.diskCache.Clear()
} 