package cache

import (
	"sort"
	"strings"
	"testing"
	"sync"
	
	"pansou/plugin"
)

func TestCacheKeyOrderIndependence(t *testing.T) {
	// 测试频道顺序
	key1 := GenerateCacheKey("movie", []string{"channel1", "channel2"}, "all", nil)
	key2 := GenerateCacheKey("movie", []string{"channel2", "channel1"}, "all", nil)
	if key1 != key2 {
		t.Errorf("Cache keys should be the same regardless of channel order: %s != %s", key1, key2)
	}
	
	// 测试插件顺序
	key3 := GenerateCacheKey("movie", nil, "all", []string{"pan666", "panta"})
	key4 := GenerateCacheKey("movie", nil, "all", []string{"panta", "pan666"})
	if key3 != key4 {
		t.Errorf("Cache keys should be the same regardless of plugin order: %s != %s", key3, key4)
	}
	
	// 测试频道和插件组合顺序
	key5 := GenerateCacheKey("movie", []string{"channel1", "channel2"}, "all", []string{"pan666", "panta"})
	key6 := GenerateCacheKey("movie", []string{"channel2", "channel1"}, "all", []string{"panta", "pan666"})
	if key5 != key6 {
		t.Errorf("Cache keys should be the same regardless of channel and plugin order: %s != %s", key5, key6)
	}
}

func TestCacheKeyNullValueHandling(t *testing.T) {
	// 测试nil和空数组
	key1 := GenerateCacheKey("movie", nil, "all", nil)
	key2 := GenerateCacheKey("movie", []string{}, "all", []string{})
	if key1 != key2 {
		t.Errorf("Cache keys should be the same for nil and empty arrays: %s != %s", key1, key2)
	}
	
	// 测试空字符串
	key3 := GenerateCacheKey("movie", nil, "", nil)
	key4 := GenerateCacheKey("movie", nil, "all", nil)
	if key3 != key4 {
		t.Errorf("Cache keys should be the same for empty string and default value: %s != %s", key3, key4)
	}
	
	// 测试混合情况
	key5 := GenerateCacheKey("movie", nil, "", []string{})
	key6 := GenerateCacheKey("movie", []string{}, "all", nil)
	if key5 != key6 {
		t.Errorf("Cache keys should be the same for mixed null/empty values: %s != %s", key5, key6)
	}
}

func TestCacheKeyNormalization(t *testing.T) {
	// 测试关键词标准化（大小写、空格）
	key1 := GenerateCacheKey("Movie", nil, "all", nil)
	key2 := GenerateCacheKey("movie", nil, "all", nil)
	if key1 != key2 {
		t.Errorf("Cache keys should be case insensitive: %s != %s", key1, key2)
	}
	
	key3 := GenerateCacheKey(" movie ", nil, "all", nil)
	key4 := GenerateCacheKey("movie", nil, "all", nil)
	if key3 != key4 {
		t.Errorf("Cache keys should trim spaces: %s != %s", key3, key4)
	}
}

func TestLargeListHandling(t *testing.T) {
	// 创建大型频道列表
	largeChannelList := make([]string, 100)
	for i := 0; i < 100; i++ {
		largeChannelList[i] = "channel" + string(rune('a'+i%26))
	}
	
	// 创建大型插件列表
	largePluginList := make([]string, 50)
	for i := 0; i < 50; i++ {
		largePluginList[i] = "plugin" + string(rune('a'+i%26))
	}
	
	// 测试大型列表的哈希计算
	key1 := GenerateCacheKey("movie", largeChannelList, "all", largePluginList)
	key2 := GenerateCacheKey("movie", largeChannelList, "all", largePluginList)
	
	if key1 != key2 {
		t.Errorf("Cache keys should be consistent for large lists: %s != %s", key1, key2)
	}
	
	// 测试大型列表的顺序不变性
	// 反转频道列表
	reversedChannels := make([]string, len(largeChannelList))
	for i, ch := range largeChannelList {
		reversedChannels[len(largeChannelList)-1-i] = ch
	}
	
	key3 := GenerateCacheKey("movie", reversedChannels, "all", largePluginList)
	if key1 != key3 {
		t.Errorf("Cache keys should be the same regardless of large channel list order: %s != %s", key1, key3)
	}
}

func TestHashCaching(t *testing.T) {
	// 创建中等大小的频道列表
	channels := make([]string, 10)
	for i := 0; i < 10; i++ {
		channels[i] = "channel" + string(rune('a'+i))
	}
	
	// 第一次调用应该计算哈希并缓存
	hash1 := getChannelsHash(channels)
	
	// 清除channelsCopy，确保第二次调用使用缓存
	channelsCopy := make([]string, len(channels))
	copy(channelsCopy, channels)
	sort.Strings(channelsCopy)
	key := strings.Join(channelsCopy, ",")
	
	// 检查缓存中是否有值
	_, ok := channelHashCache.Load(key)
	if !ok {
		t.Errorf("Hash should be cached after first call")
	}
	
	// 第二次调用应该使用缓存
	hash2 := getChannelsHash(channels)
	
	if hash1 != hash2 {
		t.Errorf("Hash values should be the same: %s != %s", hash1, hash2)
	}
}

func TestPrecomputedHashes(t *testing.T) {
	// 测试空列表预计算哈希
	emptyHash1 := getChannelsHash(nil)
	emptyHash2 := getChannelsHash([]string{})
	
	if emptyHash1 != "all" || emptyHash2 != "all" {
		t.Errorf("Empty list hash should be 'all', got %s and %s", emptyHash1, emptyHash2)
	}
	
	// 测试常用频道组合预计算哈希
	commonChannels := []string{"dongman", "anime"}
	hash1 := getChannelsHash(commonChannels)
	hash2 := getChannelsHash([]string{"anime", "dongman"}) // 顺序不同
	
	if hash1 != hash2 {
		t.Errorf("Common channel hashes should be the same: %s != %s", hash1, hash2)
	}
	
	// 测试常用插件组合预计算哈希
	commonPlugins := []string{"pan666", "panta"}
	hash3 := getPluginsHash(commonPlugins)
	hash4 := getPluginsHash([]string{"panta", "pan666"}) // 顺序不同
	
	if hash3 != hash4 {
		t.Errorf("Common plugin hashes should be the same: %s != %s", hash3, hash4)
	}
}

func TestEmptyPluginsUseAllPluginsHash(t *testing.T) {
	// 获取所有插件的哈希值
	allPlugins := plugin.GetRegisteredPlugins()
	allPluginNames := make([]string, 0, len(allPlugins))
	for _, p := range allPlugins {
		allPluginNames = append(allPluginNames, p.Name())
	}
	sort.Strings(allPluginNames)
	expectedHash := calculateListHash(allPluginNames)
	
	// 测试空插件列表使用所有插件哈希
	emptyHash1 := getPluginsHash(nil)
	emptyHash2 := getPluginsHash([]string{})
	
	if emptyHash1 != expectedHash || emptyHash2 != expectedHash {
		t.Errorf("Empty plugins hash should use all plugins hash, got %s and %s, expected %s", 
			emptyHash1, emptyHash2, expectedHash)
	}
	
	// 测试缓存键生成时空插件列表使用所有插件哈希
	key1 := GenerateCacheKey("movie", nil, "all", nil)
	key2 := GenerateCacheKey("movie", nil, "all", []string{})
	
	if key1 != key2 {
		t.Errorf("Cache keys with nil and empty plugins should be the same: %s != %s", key1, key2)
	}
	
	// 测试显式列出所有插件与空插件列表生成相同的缓存键
	key3 := GenerateCacheKey("movie", nil, "all", allPluginNames)
	
	if key1 != key3 {
		t.Errorf("Cache key with all plugins explicitly listed should be the same as with empty plugins list: %s != %s", key1, key3)
	}
}

func TestPluginParameterNormalization(t *testing.T) {
	// 获取所有插件名称
	allPlugins := plugin.GetRegisteredPlugins()
	allPluginNames := make([]string, 0, len(allPlugins))
	for _, p := range allPlugins {
		allPluginNames = append(allPluginNames, p.Name())
	}
	
	// 测试不传插件参数
	key1 := GenerateCacheKey("movie", nil, "all", nil)
	
	// 测试传空插件数组
	key2 := GenerateCacheKey("movie", nil, "all", []string{})
	
	// 测试传只包含空字符串的插件数组
	key3 := GenerateCacheKey("movie", nil, "all", []string{""})
	
	// 测试传所有插件
	key4 := GenerateCacheKey("movie", nil, "all", allPluginNames)
	
	// 所有情况应该生成相同的缓存键
	if key1 != key2 || key1 != key3 || key1 != key4 {
		t.Errorf("Different plugin parameter forms should generate the same cache key:\nnil: %s\nempty: %s\nempty string: %s\nall plugins: %s", 
			key1, key2, key3, key4)
	}
	
	// 测试sourceType=tg时忽略插件参数
	key5 := GenerateCacheKey("movie", nil, "tg", nil)
	key6 := GenerateCacheKey("movie", nil, "tg", allPluginNames)
	
	if key5 != key6 {
		t.Errorf("With sourceType=tg, plugin parameters should be ignored: %s != %s", key5, key6)
	}
}

func BenchmarkCacheKeyGeneration(b *testing.B) {
	// 创建测试数据
	channels := []string{"channel1", "channel2", "channel3", "channel4", "channel5"}
	plugins := []string{"plugin1", "plugin2", "plugin3"}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		GenerateCacheKey("test keyword", channels, "all", plugins)
	}
}

func BenchmarkCacheKeyGenerationLarge(b *testing.B) {
	// 创建大型测试数据
	channels := make([]string, 100)
	for i := 0; i < 100; i++ {
		channels[i] = "channel" + string(rune('a'+i%26))
	}
	
	plugins := make([]string, 50)
	for i := 0; i < 50; i++ {
		plugins[i] = "plugin" + string(rune('a'+i%26))
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		GenerateCacheKey("test keyword", channels, "all", plugins)
	}
}

func BenchmarkCacheKeyGenerationV2(b *testing.B) {
	// 创建测试数据，与上面相同以便比较
	channels := []string{"channel1", "channel2", "channel3", "channel4", "channel5"}
	plugins := []string{"plugin1", "plugin2", "plugin3"}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		GenerateCacheKeyV2("test keyword", channels, "all", plugins)
	}
}

func BenchmarkCacheKeyGenerationV2Large(b *testing.B) {
	// 创建大型测试数据，与上面相同以便比较
	channels := make([]string, 100)
	for i := 0; i < 100; i++ {
		channels[i] = "channel" + string(rune('a'+i%26))
	}
	
	plugins := make([]string, 50)
	for i := 0; i < 50; i++ {
		plugins[i] = "plugin" + string(rune('a'+i%26))
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		GenerateCacheKeyV2("test keyword", channels, "all", plugins)
	}
} 

func BenchmarkHashCaching(b *testing.B) {
	// 创建测试数据
	channels := make([]string, 20)
	for i := 0; i < 20; i++ {
		channels[i] = "channel" + string(rune('a'+i%26))
	}
	
	// 预热缓存
	_ = getChannelsHash(channels)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = getChannelsHash(channels)
	}
}

func BenchmarkHashCachingWithoutPrecomputed(b *testing.B) {
	// 创建测试数据，使用不在预计算列表中的频道
	channels := make([]string, 20)
	for i := 0; i < 20; i++ {
		channels[i] = "unknown" + string(rune('a'+i%26))
	}
	
	// 预热缓存
	_ = getChannelsHash(channels)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = getChannelsHash(channels)
	}
}

func BenchmarkHashCalculation(b *testing.B) {
	// 创建测试数据
	channels := make([]string, 20)
	for i := 0; i < 20; i++ {
		channels[i] = "channel" + string(rune('a'+i%26))
	}
	
	// 清除缓存
	channelHashCache = sync.Map{}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		channelsCopy := make([]string, len(channels))
		copy(channelsCopy, channels)
		sort.Strings(channelsCopy)
		_ = calculateListHash(channelsCopy)
	}
} 