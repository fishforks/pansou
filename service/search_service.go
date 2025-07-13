package service

import (
	"context" // Added for context.WithTimeout
	"io/ioutil"
	"net/http" // Added for http.Client
	"sort"
	"strings"
	"time"

	"pansou/config"
	"pansou/model"
	"pansou/plugin"
	"pansou/util"
	"pansou/util/cache"
	"pansou/util/pool"
)

// 优先关键词列表
var priorityKeywords = []string{"全", "合集", "系列", "完", "最新", "附", "花园墙外"}

// 全局缓存实例和缓存是否初始化标志
var (
	twoLevelCache *cache.TwoLevelCache
	cacheInitialized bool
)

// 初始化缓存
func init() {
	if config.AppConfig != nil && config.AppConfig.CacheEnabled {
		var err error
		twoLevelCache, err = cache.NewTwoLevelCache()
		if err == nil {
			cacheInitialized = true
		}
	}
}

// SearchService 搜索服务
type SearchService struct{
	pluginManager *plugin.PluginManager
}

// NewSearchService 创建搜索服务实例并确保缓存可用
func NewSearchService(pluginManager *plugin.PluginManager) *SearchService {
	// 检查缓存是否已初始化，如果未初始化则尝试重新初始化
	if !cacheInitialized && config.AppConfig != nil && config.AppConfig.CacheEnabled {
		var err error
		twoLevelCache, err = cache.NewTwoLevelCache()
		if err == nil {
			cacheInitialized = true
		}
	}
	
	return &SearchService{
		pluginManager: pluginManager,
	}
}

// Search 执行搜索
func (s *SearchService) Search(keyword string, channels []string, concurrency int, forceRefresh bool, resultType string, sourceType string, plugins []string) (model.SearchResponse, error) {
	// 参数预处理
	// 源类型标准化
	if sourceType == "" {
		sourceType = "all"
	}
	
	// 插件参数规范化处理
	if sourceType == "tg" {
		// 对于只搜索Telegram的请求，忽略插件参数
		plugins = nil
	} else if sourceType == "all" || sourceType == "plugin" {
		// 检查是否为空列表或只包含空字符串
		if plugins == nil || len(plugins) == 0 {
			plugins = nil
		} else {
			// 检查是否有非空元素
			hasNonEmpty := false
			for _, p := range plugins {
				if p != "" {
					hasNonEmpty = true
					break
				}
			}
			
			// 如果全是空字符串，视为未指定
			if !hasNonEmpty {
				plugins = nil
			} else {
				// 检查是否包含所有插件
				allPlugins := s.pluginManager.GetPlugins()
				allPluginNames := make([]string, 0, len(allPlugins))
				for _, p := range allPlugins {
					allPluginNames = append(allPluginNames, strings.ToLower(p.Name()))
				}
				
				// 创建请求的插件名称集合（忽略空字符串）
				requestedPlugins := make([]string, 0, len(plugins))
				for _, p := range plugins {
					if p != "" {
						requestedPlugins = append(requestedPlugins, strings.ToLower(p))
					}
				}
				
				// 如果请求的插件数量与所有插件数量相同，检查是否包含所有插件
				if len(requestedPlugins) == len(allPluginNames) {
					// 创建映射以便快速查找
					pluginMap := make(map[string]bool)
					for _, p := range requestedPlugins {
						pluginMap[p] = true
					}
					
					// 检查是否包含所有插件
					allIncluded := true
					for _, name := range allPluginNames {
						if !pluginMap[name] {
							allIncluded = false
							break
						}
					}
					
					// 如果包含所有插件，统一设为nil
					if allIncluded {
						plugins = nil
					}
				}
			}
		}
	}

	// 立即生成缓存键并检查缓存
	cacheKey := cache.GenerateCacheKey(keyword, channels, sourceType, plugins)
	
	// 如果未启用强制刷新，尝试从缓存获取结果
	if !forceRefresh && twoLevelCache != nil && config.AppConfig.CacheEnabled {
		data, hit, err := twoLevelCache.Get(cacheKey)
		
		if err == nil && hit {
			var response model.SearchResponse
			if err := cache.DeserializeWithPool(data, &response); err == nil {
				// 根据resultType过滤返回结果
				return filterResponseByType(response, resultType), nil
			}
		}
	}
	
	// 获取所有可用插件
	var availablePlugins []plugin.SearchPlugin
	if s.pluginManager != nil && (sourceType == "all" || sourceType == "plugin") {
		allPlugins := s.pluginManager.GetPlugins()
		
		// 确保plugins不为nil并且有非空元素
		hasPlugins := plugins != nil && len(plugins) > 0
		hasNonEmptyPlugin := false
		
		if hasPlugins {
			for _, p := range plugins {
				if p != "" {
					hasNonEmptyPlugin = true
					break
				}
			}
		}
		
		// 只有当plugins数组包含非空元素时才进行过滤
		if hasPlugins && hasNonEmptyPlugin {
			pluginMap := make(map[string]bool)
			for _, p := range plugins {
				if p != "" { // 忽略空字符串
					pluginMap[strings.ToLower(p)] = true
				}
			}
			
			for _, p := range allPlugins {
				if pluginMap[strings.ToLower(p.Name())] {
					availablePlugins = append(availablePlugins, p)
				}
			}
		} else {
			// 如果plugins为nil、空数组或只包含空字符串，视为未指定，使用所有插件
			availablePlugins = allPlugins
		}
	}
	
	// 控制并发数：如果用户没有指定有效值，则默认使用"频道数+插件数+10"的并发数
	pluginCount := len(availablePlugins)
	
	// 根据sourceType决定是否搜索Telegram频道
	channelCount := 0
	if sourceType == "all" || sourceType == "tg" {
		channelCount = len(channels)
	}
	
	if concurrency <= 0 {
		concurrency = channelCount + pluginCount + 10
		if concurrency < 1 {
			concurrency = 1
		}
	}

	// 计算任务总数（频道数 + 插件数）
	totalTasks := channelCount + pluginCount
	
	// 如果没有任务要执行，返回空结果
	if totalTasks == 0 {
		return model.SearchResponse{
			Total:        0,
			Results:      []model.SearchResult{},
			MergedByType: make(model.MergedLinks),
		}, nil
	}

	// 使用工作池执行并行搜索
	tasks := make([]pool.Task, 0, totalTasks)
	
	// 添加频道搜索任务（如果需要）
	if sourceType == "all" || sourceType == "tg" {
		for _, channel := range channels {
			ch := channel // 创建副本，避免闭包问题
			tasks = append(tasks, func() interface{} {
				results, err := s.searchChannel(keyword, ch)
				if err != nil {
					return nil
				}
				return results
			})
		}
	}
	
	// 添加插件搜索任务（如果需要）
	for _, p := range availablePlugins {
		plugin := p // 创建副本，避免闭包问题
		tasks = append(tasks, func() interface{} {
			results, err := plugin.Search(keyword)
			if err != nil {
				return nil
			}
			return results
		})
	}
	
	// 使用带超时控制的工作池执行所有任务并获取结果
	results := pool.ExecuteBatchWithTimeout(tasks, concurrency, config.AppConfig.PluginTimeout)
	
	// 预估每个任务平均返回22个结果
	allResults := make([]model.SearchResult, 0, totalTasks*22)
	
	// 合并所有结果
	for _, result := range results {
		if result != nil {
			channelResults := result.([]model.SearchResult)
			allResults = append(allResults, channelResults...)
		}
	}
	
	// 过滤结果，确保标题包含搜索关键词
	filteredResults := filterResultsByKeyword(allResults, keyword)
	
	// 按照优化后的规则排序结果
	sortResultsByTimeAndKeywords(filteredResults)
	
	// 过滤结果，只保留有时间的结果或包含优先关键词的结果到Results中
	filteredForResults := make([]model.SearchResult, 0, len(filteredResults))
	for _, result := range filteredResults {
		// 有时间的结果或包含优先关键词的结果保留在Results中
		if !result.Datetime.IsZero() || getKeywordPriority(result.Title) > 0 {
			filteredForResults = append(filteredForResults, result)
		}
	}
	
	// 合并链接按网盘类型分组（使用所有过滤后的结果）
	mergedLinks := mergeResultsByType(filteredResults)
	
	// 构建响应
	var total int
	if resultType == "merged_by_type" {
		// 计算所有类型链接的总数
		total = 0
		for _, links := range mergedLinks {
			total += len(links)
		}
	} else {
		// 只计算filteredForResults的数量
		total = len(filteredForResults)
	}
	
	response := model.SearchResponse{
		Total:        total,
		Results:      filteredForResults, // 使用进一步过滤的结果
		MergedByType: mergedLinks,
	}

	// 异步缓存搜索结果（缓存完整结果，以便后续可以根据不同resultType过滤）
	if twoLevelCache != nil && config.AppConfig.CacheEnabled {
		go func(resp model.SearchResponse) {
			data, err := cache.SerializeWithPool(resp)
			if err != nil {
				return
			}
			
			ttl := time.Duration(config.AppConfig.CacheTTLMinutes) * time.Minute
			twoLevelCache.Set(cacheKey, data, ttl)
		}(response)
	}
	
	// 根据resultType过滤返回结果
	return filterResponseByType(response, resultType), nil
}

// filterResponseByType 根据结果类型过滤响应
func filterResponseByType(response model.SearchResponse, resultType string) model.SearchResponse {
	switch resultType {
	case "results":
		// 只返回Results
		return model.SearchResponse{
			Total:   response.Total,
			Results: response.Results,
		}
	case "merged_by_type":
		// 只返回MergedByType，Results设为nil，结合omitempty标签，JSON序列化时会忽略此字段
		return model.SearchResponse{
			Total:        response.Total,
			MergedByType: response.MergedByType,
			Results:      nil,
		}
	default:
		// 默认返回全部
		return response
	}
}

// 过滤结果，确保标题包含搜索关键词
func filterResultsByKeyword(results []model.SearchResult, keyword string) []model.SearchResult {
	// 预估过滤后会保留80%的结果
	filteredResults := make([]model.SearchResult, 0, len(results)*8/10)
	
	// 将关键词转为小写，用于不区分大小写的比较
	lowerKeyword := strings.ToLower(keyword)
	
	// 将关键词按空格分割，用于支持多关键词搜索
	keywords := strings.Fields(lowerKeyword)
	
	for _, result := range results {
		// 将标题和内容转为小写
		lowerTitle := strings.ToLower(result.Title)
		lowerContent := strings.ToLower(result.Content)
		
		// 检查每个关键词是否在标题或内容中
		matched := true
		for _, kw := range keywords {
			// 如果关键词是"pwd"，特殊处理，只要标题、内容或链接中包含即可
			if kw == "pwd" {
				// 检查标题、内容
				pwdInTitle := strings.Contains(lowerTitle, kw)
				pwdInContent := strings.Contains(lowerContent, kw)
				
				// 检查链接中是否包含pwd参数
				pwdInLinks := false
				for _, link := range result.Links {
					if strings.Contains(strings.ToLower(link.URL), "pwd=") {
						pwdInLinks = true
						break
					}
				}
				
				// 只要有一个包含pwd，就算匹配
				if pwdInTitle || pwdInContent || pwdInLinks {
					continue // 匹配成功，检查下一个关键词
				} else {
					matched = false
					break
				}
			} else {
				// 对于其他关键词，检查是否同时在标题和内容中
				if !strings.Contains(lowerTitle, kw) && !strings.Contains(lowerContent, kw) {
					matched = false
					break
				}
			}
		}
		
		if matched {
			filteredResults = append(filteredResults, result)
		}
	}
	
	return filteredResults
}

// 根据时间和关键词排序结果
func sortResultsByTimeAndKeywords(results []model.SearchResult) {
	sort.Slice(results, func(i, j int) bool {
		// 检查是否有零值时间
		iZeroTime := results[i].Datetime.IsZero()
		jZeroTime := results[j].Datetime.IsZero()
		
		// 如果两者都是零值时间，按关键词优先级排序
		if iZeroTime && jZeroTime {
			iPriority := getKeywordPriority(results[i].Title)
			jPriority := getKeywordPriority(results[j].Title)
			if iPriority != jPriority {
				return iPriority > jPriority
			}
			// 如果优先级也相同，按标题字母顺序排序
			return results[i].Title < results[j].Title
		}
		
		// 如果只有一个是零值时间，将其排在后面
		if iZeroTime {
			return false // i排在后面
		}
		if jZeroTime {
			return true // j排在后面，i排在前面
		}
		
		// 两者都有正常时间，使用原有逻辑
		// 计算两个结果的时间差（以天为单位）
		timeDiff := daysBetween(results[i].Datetime, results[j].Datetime)
		
		// 如果时间差超过30天，按时间排序（新的在前面）
		if abs(timeDiff) > 30 {
			return results[i].Datetime.After(results[j].Datetime)
		}
		
		// 如果时间差在30天内，先检查时间差是否超过1天
		if abs(timeDiff) > 1 {
			return results[i].Datetime.After(results[j].Datetime)
		}
		
		// 如果时间差在1天内，检查关键词优先级
		iPriority := getKeywordPriority(results[i].Title)
		jPriority := getKeywordPriority(results[j].Title)
		
		// 如果优先级不同，优先级高的排在前面
		if iPriority != jPriority {
			return iPriority > jPriority
		}
		
		// 如果优先级相同且时间差在1天内，仍然按时间排序（新的在前面）
		return results[i].Datetime.After(results[j].Datetime)
	})
}

// 计算两个时间之间的天数差
func daysBetween(t1, t2 time.Time) float64 {
	duration := t1.Sub(t2)
	return duration.Hours() / 24
}

// 绝对值
func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// 获取标题中包含优先关键词的优先级
func getKeywordPriority(title string) int {
	title = strings.ToLower(title)
	for i, keyword := range priorityKeywords {
		if strings.Contains(title, keyword) {
			// 返回优先级（数组索引越小，优先级越高）
			return len(priorityKeywords) - i
		}
	}
	return 0
}

// 搜索单个频道
func (s *SearchService) searchChannel(keyword string, channel string) ([]model.SearchResult, error) {
	// 构建搜索URL
	url := util.BuildSearchURL(channel, keyword, "")
	
	// 使用全局HTTP客户端（已配置代理）
	client := util.GetHTTPClient()
	
	// 创建一个带超时的上下文
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	
	// 创建请求
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	
	// 发送请求
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	
	// 读取响应体
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	
	// 解析响应
	results, _, err := util.ParseSearchResults(string(body), channel)
	if err != nil {
		return nil, err
	}
	
	return results, nil
}

// 将搜索结果按网盘类型分组
func mergeResultsByType(results []model.SearchResult) model.MergedLinks {
	// 创建合并结果的映射
	mergedLinks := make(model.MergedLinks, 10) // 预分配容量，假设有10种不同的网盘类型
	
	// 用于去重的映射，键为URL
	uniqueLinks := make(map[string]model.MergedLink)
	
	// 遍历所有搜索结果
	for _, result := range results {
		for _, link := range result.Links {
			// 创建合并后的链接
			mergedLink := model.MergedLink{
				URL:      link.URL,
				Password: link.Password,
				Note:     result.Title,
				Datetime: result.Datetime,
			}
			
			// 检查是否已存在相同URL的链接
			if existingLink, exists := uniqueLinks[link.URL]; exists {
				// 如果已存在，只有当当前链接的时间更新时才替换
				if mergedLink.Datetime.After(existingLink.Datetime) {
					uniqueLinks[link.URL] = mergedLink
				}
			} else {
				// 如果不存在，直接添加
				uniqueLinks[link.URL] = mergedLink
			}
		}
	}
	
	// 将去重后的链接按类型分组
	for url, mergedLink := range uniqueLinks {
		// 获取链接类型
		linkType := ""
		for _, result := range results {
			for _, link := range result.Links {
				if link.URL == url {
					linkType = link.Type
					break
				}
			}
			if linkType != "" {
				break
			}
		}
		
		// 如果没有找到类型，使用"unknown"
		if linkType == "" {
			linkType = "unknown"
		}
		
		// 添加到对应类型的列表中
		mergedLinks[linkType] = append(mergedLinks[linkType], mergedLink)
	}
	
	// 对每种类型的链接按时间排序（新的在前面）
	for linkType, links := range mergedLinks {
		sort.Slice(links, func(i, j int) bool {
			return links[i].Datetime.After(links[j].Datetime)
		})
		mergedLinks[linkType] = links
	}
	
	return mergedLinks
} 

// GetPluginManager 获取插件管理器
func (s *SearchService) GetPluginManager() *plugin.PluginManager {
	return s.pluginManager
} 