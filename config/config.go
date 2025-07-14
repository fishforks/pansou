package config

import (
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"time"
)

// Config 应用配置结构
type Config struct {
	DefaultChannels    []string
	DefaultConcurrency int
	Port               string
	ProxyURL           string
	UseProxy           bool
	// 缓存相关配置
	CacheEnabled    bool
	CachePath       string
	CacheMaxSizeMB  int
	CacheTTLMinutes int
	// 压缩相关配置
	EnableCompression bool
	MinSizeToCompress int // 最小压缩大小（字节）
	// GC相关配置
	GCPercent      int  // GC触发阈值百分比
	OptimizeMemory bool // 是否启用内存优化
	// 插件相关配置
	PluginTimeoutSeconds int           // 插件超时时间（秒）
	PluginTimeout        time.Duration // 插件超时时间（Duration）
	// 异步插件相关配置
	AsyncPluginEnabled        bool          // 是否启用异步插件
	AsyncResponseTimeout      int           // 响应超时时间（秒）
	AsyncResponseTimeoutDur   time.Duration // 响应超时时间（Duration）
	AsyncMaxBackgroundWorkers int           // 最大后台工作者数量
	AsyncMaxBackgroundTasks   int           // 最大后台任务数量
	AsyncCacheTTLHours        int           // 异步缓存有效期（小时）
}

// 全局配置实例
var AppConfig *Config

// 初始化配置
func Init() {
	proxyURL := getProxyURL()
	pluginTimeoutSeconds := getPluginTimeout()
	asyncResponseTimeoutSeconds := getAsyncResponseTimeout()
	
	AppConfig = &Config{
		DefaultChannels:    getDefaultChannels(),
		DefaultConcurrency: getDefaultConcurrency(),
		Port:               getPort(),
		ProxyURL:           proxyURL,
		UseProxy:           proxyURL != "",
		// 缓存相关配置
		CacheEnabled:    getCacheEnabled(),
		CachePath:       getCachePath(),
		CacheMaxSizeMB:  getCacheMaxSize(),
		CacheTTLMinutes: getCacheTTL(),
		// 压缩相关配置
		EnableCompression: getEnableCompression(),
		MinSizeToCompress: getMinSizeToCompress(),
		// GC相关配置
		GCPercent:      getGCPercent(),
		OptimizeMemory: getOptimizeMemory(),
		// 插件相关配置
		PluginTimeoutSeconds: pluginTimeoutSeconds,
		PluginTimeout:        time.Duration(pluginTimeoutSeconds) * time.Second,
		// 异步插件相关配置
		AsyncPluginEnabled:        getAsyncPluginEnabled(),
		AsyncResponseTimeout:      asyncResponseTimeoutSeconds,
		AsyncResponseTimeoutDur:   time.Duration(asyncResponseTimeoutSeconds) * time.Second,
		AsyncMaxBackgroundWorkers: getAsyncMaxBackgroundWorkers(),
		AsyncMaxBackgroundTasks:   getAsyncMaxBackgroundTasks(),
		AsyncCacheTTLHours:        getAsyncCacheTTLHours(),
	}
	
	// 应用GC配置
	applyGCSettings()
}

// 从环境变量获取默认频道列表，如果未设置则使用默认值
func getDefaultChannels() []string {
	channelsEnv := os.Getenv("CHANNELS")
	if channelsEnv == "" {
		return []string{"tgsearchers2"}
	}
	return strings.Split(channelsEnv, ",")
}

// 从环境变量获取默认并发数，如果未设置则使用默认值
func getDefaultConcurrency() int {
	concurrencyEnv := os.Getenv("CONCURRENCY")
	if concurrencyEnv == "" {
		return 3
	}
	concurrency, err := strconv.Atoi(concurrencyEnv)
	if err != nil || concurrency <= 0 {
		return 3
	}
	return concurrency
}

// 从环境变量获取服务端口，如果未设置则使用默认值
func getPort() string {
	port := os.Getenv("PORT")
	if port == "" {
		return "8888"
	}
	return port
}

// 从环境变量获取SOCKS5代理URL，如果未设置则返回空字符串
func getProxyURL() string {
	return os.Getenv("PROXY")
}

// 从环境变量获取是否启用缓存，如果未设置则默认启用
func getCacheEnabled() bool {
	enabled := os.Getenv("CACHE_ENABLED")
	if enabled == "" {
		return true
	}
	return enabled != "false" && enabled != "0"
}

// 从环境变量获取缓存路径，如果未设置则使用默认路径
func getCachePath() string {
	path := os.Getenv("CACHE_PATH")
	if path == "" {
		// 默认在当前目录下创建cache文件夹
		defaultPath, err := filepath.Abs("./cache")
		if err != nil {
			return "./cache"
		}
		return defaultPath
	}
	return path
}

// 从环境变量获取缓存最大大小(MB)，如果未设置则使用默认值
func getCacheMaxSize() int {
	sizeEnv := os.Getenv("CACHE_MAX_SIZE")
	if sizeEnv == "" {
		return 100 // 默认100MB
	}
	size, err := strconv.Atoi(sizeEnv)
	if err != nil || size <= 0 {
		return 100
	}
	return size
}

// 从环境变量获取缓存TTL(分钟)，如果未设置则使用默认值
func getCacheTTL() int {
	ttlEnv := os.Getenv("CACHE_TTL")
	if ttlEnv == "" {
		return 60 // 默认60分钟
	}
	ttl, err := strconv.Atoi(ttlEnv)
	if err != nil || ttl <= 0 {
		return 60
	}
	return ttl
}

// 从环境变量获取是否启用压缩，如果未设置则默认禁用
func getEnableCompression() bool {
	enabled := os.Getenv("ENABLE_COMPRESSION")
	if enabled == "" {
		return false // 默认禁用，因为通常由Nginx等处理
	}
	return enabled == "true" || enabled == "1"
}

// 从环境变量获取最小压缩大小，如果未设置则使用默认值
func getMinSizeToCompress() int {
	sizeEnv := os.Getenv("MIN_SIZE_TO_COMPRESS")
	if sizeEnv == "" {
		return 1024 // 默认1KB
	}
	size, err := strconv.Atoi(sizeEnv)
	if err != nil || size <= 0 {
		return 1024
	}
	return size
}

// 从环境变量获取GC百分比，如果未设置则使用默认值
func getGCPercent() int {
	percentEnv := os.Getenv("GC_PERCENT")
	if percentEnv == "" {
		return 100 // 默认100%
	}
	percent, err := strconv.Atoi(percentEnv)
	if err != nil || percent <= 0 {
		return 100
	}
	return percent
}

// 从环境变量获取是否优化内存，如果未设置则默认启用
func getOptimizeMemory() bool {
	enabled := os.Getenv("OPTIMIZE_MEMORY")
	if enabled == "" {
		return true // 默认启用
	}
	return enabled != "false" && enabled != "0"
}

// 从环境变量获取插件超时时间（秒），如果未设置则使用默认值
func getPluginTimeout() int {
	timeoutEnv := os.Getenv("PLUGIN_TIMEOUT")
	if timeoutEnv == "" {
		return 30 // 默认30秒
	}
	timeout, err := strconv.Atoi(timeoutEnv)
	if err != nil || timeout <= 0 {
		return 30
	}
	return timeout
}

// 从环境变量获取是否启用异步插件，如果未设置则默认启用
func getAsyncPluginEnabled() bool {
	enabled := os.Getenv("ASYNC_PLUGIN_ENABLED")
	if enabled == "" {
		return true // 默认启用
	}
	return enabled != "false" && enabled != "0"
}

// 从环境变量获取异步响应超时时间（秒），如果未设置则使用默认值
func getAsyncResponseTimeout() int {
	timeoutEnv := os.Getenv("ASYNC_RESPONSE_TIMEOUT")
	if timeoutEnv == "" {
		return 4 // 默认4秒
	}
	timeout, err := strconv.Atoi(timeoutEnv)
	if err != nil || timeout <= 0 {
		return 4
	}
	return timeout
}

// 从环境变量获取最大后台工作者数量，如果未设置则使用默认值
func getAsyncMaxBackgroundWorkers() int {
	sizeEnv := os.Getenv("ASYNC_MAX_BACKGROUND_WORKERS")
	if sizeEnv == "" {
		return 20 // 默认20个工作者
	}
	size, err := strconv.Atoi(sizeEnv)
	if err != nil || size <= 0 {
		return 20
	}
	return size
}

// 从环境变量获取最大后台任务数量，如果未设置则使用默认值
func getAsyncMaxBackgroundTasks() int {
	sizeEnv := os.Getenv("ASYNC_MAX_BACKGROUND_TASKS")
	if sizeEnv == "" {
		return 100 // 默认100个任务
	}
	size, err := strconv.Atoi(sizeEnv)
	if err != nil || size <= 0 {
		return 100
	}
	return size
}

// 从环境变量获取异步缓存有效期（小时），如果未设置则使用默认值
func getAsyncCacheTTLHours() int {
	ttlEnv := os.Getenv("ASYNC_CACHE_TTL_HOURS")
	if ttlEnv == "" {
		return 1 // 默认1小时
	}
	ttl, err := strconv.Atoi(ttlEnv)
	if err != nil || ttl <= 0 {
		return 1
	}
	return ttl
}

// 应用GC设置
func applyGCSettings() {
	// 设置GC百分比
	debug.SetGCPercent(AppConfig.GCPercent)
	
	// 如果启用内存优化
	if AppConfig.OptimizeMemory {
		// 释放操作系统内存
		debug.FreeOSMemory()
	}
} 