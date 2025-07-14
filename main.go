package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"pansou/api"
	"pansou/config"
	"pansou/plugin"
	// 以下是插件的空导入，用于触发各插件的init函数，实现自动注册
	// 添加新插件时，只需在此处添加对应的导入语句即可
	_ "pansou/plugin/hunhepan"
	_ "pansou/plugin/jikepan"
	_ "pansou/plugin/pan666"
	_ "pansou/plugin/pansearch"
	_ "pansou/plugin/panta"
	_ "pansou/plugin/qupansou"
	"pansou/service"
	"pansou/util"
)

func main() {
	// 初始化应用
	initApp()
	
	// 启动服务器
	startServer()
}

// initApp 初始化应用程序
func initApp() {
	// 初始化配置
	config.Init()
	
	// 初始化HTTP客户端
	util.InitHTTPClient()
	
	// 确保异步插件系统初始化
	plugin.InitAsyncPluginSystem()
}

// startServer 启动Web服务器
func startServer() {
	// 初始化插件管理器
	pluginManager := plugin.NewPluginManager()
	
	// 注册所有全局插件（通过init函数自动注册到全局注册表）
	pluginManager.RegisterAllGlobalPlugins()
	
	// 初始化搜索服务
	searchService := service.NewSearchService(pluginManager)
	
	// 设置路由
	router := api.SetupRouter(searchService)
	
	// 获取端口配置
	port := config.AppConfig.Port
	
	// 输出服务信息
	printServiceInfo(port, pluginManager)
	
	// 创建HTTP服务器
	srv := &http.Server{
		Addr:    ":" + port,
		Handler: router,
	}
	
	// 创建通道来接收操作系统信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	
	// 在单独的goroutine中启动服务器
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("启动服务器失败: %v", err)
		}
	}()
	
	// 等待中断信号
	<-quit
	fmt.Println("正在关闭服务器...")
	
	// 设置关闭超时时间
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	// 保存异步插件缓存
	plugin.SaveCacheToDisk()
	
	// 优雅关闭服务器
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("服务器关闭异常: %v", err)
	}
	
	fmt.Println("服务器已安全关闭")
}

// printServiceInfo 打印服务信息
func printServiceInfo(port string, pluginManager *plugin.PluginManager) {
	// 启动服务器
	fmt.Printf("服务器启动在 http://localhost:%s\n", port)
	
	// 输出代理信息
	if config.AppConfig.UseProxy {
		fmt.Printf("使用SOCKS5代理: %s\n", config.AppConfig.ProxyURL)
	} else {
		fmt.Println("未使用代理")
	}
	
	// 输出缓存信息
	if config.AppConfig.CacheEnabled {
		fmt.Printf("缓存已启用: 路径=%s, 最大大小=%dMB, TTL=%d分钟\n", 
			config.AppConfig.CachePath, 
			config.AppConfig.CacheMaxSizeMB,
			config.AppConfig.CacheTTLMinutes)
	} else {
		fmt.Println("缓存已禁用")
	}
	
	// 输出压缩信息
	if config.AppConfig.EnableCompression {
		fmt.Printf("响应压缩已启用: 最小压缩大小=%d字节\n", 
			config.AppConfig.MinSizeToCompress)
	} else {
		fmt.Println("响应压缩已禁用")
	}
	
	// 输出GC配置信息
	fmt.Printf("GC配置: 触发阈值=%d%%, 内存优化=%v\n", 
		config.AppConfig.GCPercent, 
		config.AppConfig.OptimizeMemory)
	
	// 输出异步插件配置信息
	if config.AppConfig.AsyncPluginEnabled {
		fmt.Printf("异步插件已启用: 响应超时=%d秒, 最大工作者=%d, 最大任务=%d, 缓存TTL=%d小时\n",
			config.AppConfig.AsyncResponseTimeout,
			config.AppConfig.AsyncMaxBackgroundWorkers,
			config.AppConfig.AsyncMaxBackgroundTasks,
			config.AppConfig.AsyncCacheTTLHours)
	} else {
		fmt.Println("异步插件已禁用")
	}
	
	// 输出插件信息
	fmt.Println("已加载插件:")
	for _, p := range pluginManager.GetPlugins() {
		fmt.Printf("  - %s (优先级: %d)\n", p.Name(), p.Priority())
	}
} 