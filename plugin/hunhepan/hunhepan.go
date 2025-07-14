package hunhepan

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"pansou/model"
	"pansou/plugin"
	"pansou/util/json"
)

// 缓存相关变量
var (
	// API响应缓存，键为"apiURL:keyword"，值为[]HunhepanItem
	apiResponseCache = sync.Map{}
	
	// 最后一次清理缓存的时间
	lastCacheCleanTime = time.Now()
	
	// 缓存有效期（1小时）
	cacheTTL = 1 * time.Hour
)

// 在init函数中注册插件
func init() {
	// 使用全局超时时间创建插件实例并注册
	plugin.RegisterGlobalPlugin(NewHunhepanPlugin())
	
	// 启动缓存清理goroutine
	go startCacheCleaner()
}

// startCacheCleaner 启动一个定期清理缓存的goroutine
func startCacheCleaner() {
	// 每小时清理一次缓存
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()
	
	for range ticker.C {
		// 清空所有缓存
		apiResponseCache = sync.Map{}
		lastCacheCleanTime = time.Now()
	}
}

const (
	// API端点
	HunhepanAPI = "https://hunhepan.com/open/search/disk"
	QkpansoAPI  = "https://qkpanso.com/v1/search/disk"
	KuakeAPI    = "https://kuake8.com/v1/search/disk"
	
	// 默认超时时间
	DefaultTimeout = 6 * time.Second
	
	// 默认页大小
	DefaultPageSize = 30
)

// HunhepanPlugin 混合盘搜索插件
type HunhepanPlugin struct {
	client  *http.Client
	timeout time.Duration
}

// NewHunhepanPlugin 创建新的混合盘搜索插件
func NewHunhepanPlugin() *HunhepanPlugin {
	timeout := DefaultTimeout
	
	return &HunhepanPlugin{
		client: &http.Client{
			Timeout: timeout,
		},
		timeout: timeout,
	}
}

// Name 返回插件名称
func (p *HunhepanPlugin) Name() string {
	return "hunhepan"
}

// Priority 返回插件优先级
func (p *HunhepanPlugin) Priority() int {
	return 3 // 中等优先级
}

// Search 执行搜索并返回结果
func (p *HunhepanPlugin) Search(keyword string) ([]model.SearchResult, error) {
	// 创建结果通道和错误通道
	resultChan := make(chan []HunhepanItem, 3)
	errChan := make(chan error, 3)
	
	// 创建等待组
	var wg sync.WaitGroup
	wg.Add(3)
	
	// 并行请求三个API
	go func() {
		defer wg.Done()
		items, err := p.searchAPI(HunhepanAPI, keyword)
		if err != nil {
			errChan <- fmt.Errorf("hunhepan API error: %w", err)
			return
		}
		resultChan <- items
	}()
	
	go func() {
		defer wg.Done()
		items, err := p.searchAPI(QkpansoAPI, keyword)
		if err != nil {
			errChan <- fmt.Errorf("qkpanso API error: %w", err)
			return
		}
		resultChan <- items
	}()
	
	go func() {
		defer wg.Done()
		items, err := p.searchAPI(KuakeAPI, keyword)
		if err != nil {
			errChan <- fmt.Errorf("kuake API error: %w", err)
			return
		}
		resultChan <- items
	}()
	
	// 启动一个goroutine等待所有请求完成并关闭通道
	go func() {
		wg.Wait()
		close(resultChan)
		close(errChan)
	}()
	
	// 收集结果
	var allItems []HunhepanItem
	var errors []error
	
	// 从通道读取结果
	for items := range resultChan {
		allItems = append(allItems, items...)
	}
	
	// 收集错误（不阻止处理）
	for err := range errChan {
		errors = append(errors, err)
	}
	
	// 如果没有获取到任何结果且有错误，则返回第一个错误
	if len(allItems) == 0 && len(errors) > 0 {
		return nil, errors[0]
	}
	
	// 去重处理
	uniqueItems := p.deduplicateItems(allItems)
	
	// 转换为标准格式
	results := p.convertResults(uniqueItems)
	
	return results, nil
}

// searchAPI 向单个API发送请求
func (p *HunhepanPlugin) searchAPI(apiURL, keyword string) ([]HunhepanItem, error) {
	// 生成缓存键
	cacheKey := fmt.Sprintf("%s:%s", apiURL, keyword)
	
	// 检查缓存中是否已有结果
	if cachedItems, ok := apiResponseCache.Load(cacheKey); ok {
		// 检查缓存是否过期
		cachedResult := cachedItems.(cachedResponse)
		if time.Since(cachedResult.timestamp) < cacheTTL {
			return cachedResult.items, nil
		}
	}
	
	// 构建请求体
	reqBody := map[string]interface{}{
		"q":      keyword,
		"exact":  true,
		"page":   1,
		"size":   DefaultPageSize,
		"type":   "",
		"time":   "",
		"from":   "web",
		"user_id": 0,
		"filter": true,
	}
	
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request failed: %w", err)
	}
	
	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("create request failed: %w", err)
	}
	
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")
	
	// 根据不同的API设置不同的Referer
	if strings.Contains(apiURL, "qkpanso.com") {
		req.Header.Set("Referer", "https://qkpanso.com/search")
	} else if strings.Contains(apiURL, "kuake8.com") {
		req.Header.Set("Referer", "https://kuake8.com/search")
	} else if strings.Contains(apiURL, "hunhepan.com") {
		req.Header.Set("Referer", "https://hunhepan.com/search")
	}
	
	// 发送请求
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	
	// 读取响应体
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body failed: %w", err)
	}
	
	// 解析响应
	var apiResp HunhepanResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("decode response failed: %w", err)
	}
	
	// 检查响应状态
	if apiResp.Code != 200 {
		return nil, fmt.Errorf("API returned error: %s", apiResp.Msg)
	}
	
	// 缓存结果
	apiResponseCache.Store(cacheKey, cachedResponse{
		items:     apiResp.Data.List,
		timestamp: time.Now(),
	})
	
	return apiResp.Data.List, nil
}

// 缓存响应结构
type cachedResponse struct {
	items     []HunhepanItem
	timestamp time.Time
}

// deduplicateItems 去重处理
func (p *HunhepanPlugin) deduplicateItems(items []HunhepanItem) []HunhepanItem {
	// 使用map进行去重
	uniqueMap := make(map[string]HunhepanItem)
	
	for _, item := range items {
		// 清理DiskName中的HTML标签
		cleanedName := cleanTitle(item.DiskName)
		item.DiskName = cleanedName
		
		// 创建复合键：优先使用DiskID，如果为空则使用Link+DiskName组合
		var key string
		if item.DiskID != "" {
			key = item.DiskID
		} else if item.Link != "" {
			// 使用Link和清理后的DiskName组合作为键
			key = item.Link + "|" + cleanedName
		} else {
			// 如果DiskID和Link都为空，则使用DiskName+DiskType作为键
			key = cleanedName + "|" + item.DiskType
		}
		
		// 如果已存在，保留信息更丰富的那个
		if existing, exists := uniqueMap[key]; exists {
			// 比较文件列表长度和其他信息
			existingScore := len(existing.Files)
			newScore := len(item.Files)
			
			// 如果新项有密码而现有项没有，增加新项分数
			if existing.DiskPass == "" && item.DiskPass != "" {
				newScore += 5
			}
			
			// 如果新项有时间而现有项没有，增加新项分数
			if existing.SharedTime == "" && item.SharedTime != "" {
				newScore += 3
			}
			
			if newScore > existingScore {
				uniqueMap[key] = item
			}
		} else {
			uniqueMap[key] = item
		}
	}
	
	// 将map转回切片
	result := make([]HunhepanItem, 0, len(uniqueMap))
	for _, item := range uniqueMap {
		result = append(result, item)
	}
	
	return result
}

// convertResults 将API响应转换为标准SearchResult格式
func (p *HunhepanPlugin) convertResults(items []HunhepanItem) []model.SearchResult {
	results := make([]model.SearchResult, 0, len(items))
	
	for i, item := range items {
		// 创建链接
		link := model.Link{
			URL:      item.Link,
			Type:     p.convertDiskType(item.DiskType),
			Password: item.DiskPass,
		}
		
		// 创建唯一ID
		uniqueID := fmt.Sprintf("hunhepan-%d", i)
		
		// 解析时间
		var datetime time.Time
		if item.SharedTime != "" {
			// 尝试解析时间，格式：2025-07-07 13:19:48
			parsedTime, err := time.Parse("2006-01-02 15:04:05", item.SharedTime)
			if err == nil {
				datetime = parsedTime
			}
		}
		
		// 如果时间解析失败，使用零值
		if datetime.IsZero() {
			datetime = time.Time{}
		}
		
		// 创建搜索结果
		result := model.SearchResult{
			UniqueID:  uniqueID,
			Title:     cleanTitle(item.DiskName),
			Content:   item.Files,
			Datetime:  datetime,
			Links:     []model.Link{link},
		}
		
		results = append(results, result)
	}
	
	return results
}

// convertDiskType 将API的网盘类型转换为标准链接类型
func (p *HunhepanPlugin) convertDiskType(diskType string) string {
	switch diskType {
	case "BDY":
		return "baidu"
	case "ALY":
		return "aliyun"
	case "QUARK":
		return "quark"
	case "TIANYI":
		return "tianyi"
	case "UC":
		return "uc"
	case "CAIYUN":
		return "mobile"
	case "115":
		return "115"
	case "XUNLEI":
		return "xunlei"
	case "123PAN":
		return "123"
	case "PIKPAK":
		return "pikpak"
	default:
		return "others"
	}
}

// cleanTitle 清理标题中的HTML标签
func cleanTitle(title string) string {
	// 一次性替换所有常见HTML标签
	replacements := map[string]string{
		"<em>": "",
		"</em>": "",
		"<b>": "",
		"</b>": "",
		"<strong>": "",
		"</strong>": "",
		"<i>": "",
		"</i>": "",
	}
	
	result := title
	for tag, replacement := range replacements {
		result = strings.Replace(result, tag, replacement, -1)
	}
	
	// 移除多余的空格
	return strings.TrimSpace(result)
}

// replaceAll 替换字符串中的所有子串
func replaceAll(s, old, new string) string {
	for {
		if s2 := replace(s, old, new); s2 == s {
			return s
		} else {
			s = s2
		}
	}
}

// replace 替换字符串中的第一个子串
func replace(s, old, new string) string {
	return replace_substr(s, old, new, 1)
}

// replace_substr 替换字符串中的前n个子串
func replace_substr(s, old, new string, n int) string {
	if old == new || n == 0 {
		return s // 避免无限循环
	}
	
	if old == "" {
		if len(s) == 0 {
			return new
		}
		return new + s
	}
	
	// 计算结果字符串的长度
	count := 0
	t := s
	for i := 0; i < len(s) && count < n; i += len(old) {
		if i+len(old) <= len(s) {
			if s[i:i+len(old)] == old {
				count++
				i = i + len(old) - 1
			}
		}
	}
	
	if count == 0 {
		return s
	}
	
	b := make([]byte, len(s)+count*(len(new)-len(old)))
	bs := b
	
	// 替换前n个old为new
	for i := 0; i < count; i++ {
		j := 0
		for j < len(t) {
			if j+len(old) <= len(t) && t[j:j+len(old)] == old {
				copy(bs, t[:j])
				bs = bs[j:]
				copy(bs, new)
				bs = bs[len(new):]
				t = t[j+len(old):]
				break
			}
			j++
		}
	}
	
	copy(bs, t)
	return string(b)
}

// HunhepanResponse API响应结构
type HunhepanResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		Total   int           `json:"total"`
		PerSize int           `json:"per_size"`
		List    []HunhepanItem `json:"list"`
	} `json:"data"`
}

// HunhepanItem API响应中的单个结果项
type HunhepanItem struct {
	DiskID     string `json:"disk_id"`
	DiskName   string `json:"disk_name"`
	DiskPass   string `json:"disk_pass"`
	DiskType   string `json:"disk_type"`
	Files      string `json:"files"`
	DocID      string `json:"doc_id"`
	ShareUser  string `json:"share_user"`
	SharedTime string `json:"shared_time"`
	Link       string `json:"link"`
	Enabled    bool   `json:"enabled"`
	Weight     int    `json:"weight"`
	Status     int    `json:"status"`
} 