package pan666

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"pansou/model"
	"pansou/plugin"
	"pansou/util/json"
	"sync"
	"math/rand"
	"sort"
)

// 在init函数中注册插件
func init() {
	plugin.RegisterGlobalPlugin(NewPan666Plugin())
}

const (
	// API基础URL
	BaseURL = "https://pan666.net/api/discussions"
	
	// 默认参数
	DefaultTimeout = 6 * time.Second
	PageSize = 50 // 恢复为50，符合API实际返回数量
	MaxRetries = 2
)

// 常用UA列表
var userAgents = []string{
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/92.0.4515.107 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/14.1.2 Safari/605.1.15",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:90.0) Gecko/20100101 Firefox/90.0",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.114 Safari/537.36",
	"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/92.0.4515.107 Safari/537.36",
}

// Pan666Plugin pan666网盘搜索插件
type Pan666Plugin struct {
	client    *http.Client
	timeout   time.Duration
	retries    int
}

// NewPan666Plugin 创建新的pan666插件
func NewPan666Plugin() *Pan666Plugin {
	timeout := DefaultTimeout
	
	return &Pan666Plugin{
		client: &http.Client{
			Timeout: timeout,
		},
		timeout:    timeout,
		retries:    MaxRetries,
	}
}

// Name 返回插件名称
func (p *Pan666Plugin) Name() string {
	return "pan666"
}

// Priority 返回插件优先级
func (p *Pan666Plugin) Priority() int {
	return 3 // 中等优先级
}

// 生成随机IP
func generateRandomIP() string {
	return fmt.Sprintf("%d.%d.%d.%d", 
		rand.Intn(223)+1,  // 避免0和255
		rand.Intn(255),
		rand.Intn(255),
		rand.Intn(254)+1)  // 避免0
}

// 获取随机UA
func getRandomUA() string {
	return userAgents[rand.Intn(len(userAgents))]
}

// Search 执行搜索并返回结果
func (p *Pan666Plugin) Search(keyword string) ([]model.SearchResult, error) {
	
	// 初始化随机数种子
	rand.Seed(time.Now().UnixNano())
	
	// 只并发请求2个页面（0-1页）
	allResults, _, err := p.fetchBatch(keyword, 0, 2)
	if err != nil {
		return nil, err
	}
	
	// 去重
	uniqueResults := p.deduplicateResults(allResults)
	
	return uniqueResults, nil
}

// fetchBatch 获取一批页面的数据
func (p *Pan666Plugin) fetchBatch(keyword string, startOffset, pageCount int) ([]model.SearchResult, bool, error) {
	var wg sync.WaitGroup
	resultChan := make(chan struct{
		offset  int
		results []model.SearchResult
		hasMore bool
		err     error
	}, pageCount)
	
	// 并发请求多个页面，但每个请求之间添加随机延迟
	for i := 0; i < pageCount; i++ {
		offset := (startOffset + i) * PageSize
		wg.Add(1)
		
		go func(offset int, index int) {
			defer wg.Done()
			
			// 第一个请求立即执行，后续请求添加随机延迟
			if index > 0 {
				// 随机等待0-1秒
				randomDelay := time.Duration(100 + rand.Intn(900)) * time.Millisecond
				time.Sleep(randomDelay)
			}
			
			// 请求特定页面
			results, hasMore, err := p.fetchPage(keyword, offset)
			
			resultChan <- struct{
				offset  int
				results []model.SearchResult
				hasMore bool
				err     error
			}{
				offset:  offset,
				results: results,
				hasMore: hasMore,
				err:     err,
			}
		}(offset, i)
	}
	
	// 等待所有请求完成
	go func() {
		wg.Wait()
		close(resultChan)
	}()
	
	// 收集结果
	var allResults []model.SearchResult
	resultsByOffset := make(map[int][]model.SearchResult)
	errorsByOffset := make(map[int]error)
	hasMoreByOffset := make(map[int]bool)
	
	// 处理返回的结果
	for res := range resultChan {
		if res.err != nil {
			errorsByOffset[res.offset] = res.err
			continue
		}
		
		resultsByOffset[res.offset] = res.results
		hasMoreByOffset[res.offset] = res.hasMore
	}
	
	// 按偏移量顺序整理结果
	emptyPageCount := 0
	for i := 0; i < pageCount; i++ {
		offset := (startOffset + i) * PageSize
		results, ok := resultsByOffset[offset]
		
		if !ok {
			// 这个偏移量的请求失败了
			continue
		}
		
		if len(results) == 0 {
			emptyPageCount++
			// 如果连续两页没有结果，可能已经到达末尾，可以提前终止
			if emptyPageCount >= 2 {
				break
			}
		} else {
			emptyPageCount = 0 // 重置空页计数
			allResults = append(allResults, results...)
		}
	}
	
	// 检查是否所有请求都失败
	if len(errorsByOffset) == pageCount {
		for _, err := range errorsByOffset {
			return nil, false, fmt.Errorf("所有请求都失败: %w", err)
		}
	}
	
	// 检查是否需要继续请求
	needMoreRequests := false
	for _, hasMore := range hasMoreByOffset {
		if hasMore {
			needMoreRequests = true
			break
		}
	}
	
	return allResults, needMoreRequests, nil
}

// deduplicateResults 去除重复的搜索结果
func (p *Pan666Plugin) deduplicateResults(results []model.SearchResult) []model.SearchResult {
	seen := make(map[string]bool)
	var uniqueResults []model.SearchResult
	
	for _, result := range results {
		if !seen[result.UniqueID] {
			seen[result.UniqueID] = true
			uniqueResults = append(uniqueResults, result)
		}
	}
	
	return uniqueResults
}

// fetchPage 获取指定偏移量的页面数据
func (p *Pan666Plugin) fetchPage(keyword string, offset int) ([]model.SearchResult, bool, error) {
	// 构建请求URL，包含查询参数
	reqURL := fmt.Sprintf("%s?filter%%5Bq%%5D=%s&page%%5Blimit%%5D=%d", 
		BaseURL, url.QueryEscape(keyword), PageSize)
	
	// 添加偏移量参数
	if offset > 0 {
		reqURL += fmt.Sprintf("&page%%5Boffset%%5D=%d", offset)
	}
	
	// 添加包含mostRelevantPost参数
	reqURL += "&include=mostRelevantPost"
	
	// 发送请求
	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return nil, false, fmt.Errorf("创建请求失败: %w", err)
	}
	
	// 使用随机UA和IP
	randomUA := getRandomUA()
	randomIP := generateRandomIP()
	
	req.Header.Set("User-Agent", randomUA)
	req.Header.Set("Referer", "https://pan666.net/")
	req.Header.Set("X-Forwarded-For", randomIP)
	req.Header.Set("X-Real-IP", randomIP)
	
	// 添加一些常见请求头，使请求更真实
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	req.Header.Set("Connection", "keep-alive")
	
	// 发送请求
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, false, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()
	
	// 读取响应体
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, false, fmt.Errorf("读取响应失败: %w", err)
	}
	
	// 解析响应
	var apiResp Pan666Response
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, false, fmt.Errorf("解析响应失败: %w", err)
	}
	
	// 如果没有数据，返回空结果
	if len(apiResp.Data) == 0 {
		return []model.SearchResult{}, false, nil
	}
	
	// 判断是否有更多页面
	hasMore := len(apiResp.Data) >= PageSize && apiResp.Links.Next != ""
	
	// 构建ID到included post的映射
	postMap := make(map[string]Pan666Post)
	for _, post := range apiResp.Included {
		if post.Type == "posts" {
			postMap[post.ID] = post
		}
	}
	
	// 处理搜索结果
	results := make([]model.SearchResult, 0, len(apiResp.Data))
	
	for _, item := range apiResp.Data {
		// 获取关联的post内容
		postID := item.Relationships.MostRelevantPost.Data.ID
		post, exists := postMap[postID]
		
		if !exists {
			continue // 跳过没有关联内容的结果
		}
		
		// 解析时间
		createdAt, _ := time.Parse(time.RFC3339, item.Attributes.CreatedAt)
		
		// 先清理HTML，保留纯文本内容
		cleanContent := cleanHTML(post.Attributes.ContentHTML)
		
		// 提取网盘链接
		links := extractLinksFromText(cleanContent)
		
		// 只有当links数组不为空时，才添加结果
		if len(links) > 0 {
			// 创建搜索结果
			result := model.SearchResult{
				MessageID: item.ID,
				UniqueID:  fmt.Sprintf("pan666_%s", item.ID),
				Channel:   "", // 设置为空字符串，因为不是TG频道
				Datetime:  createdAt,
				Title:     item.Attributes.Title,
				Content:   cleanContent,
				Links:     links,
			}
			
			results = append(results, result)
		}
	}
	
	return results, hasMore, nil
}

// extractLinks 从HTML内容中提取网盘链接
func extractLinks(content string) []model.Link {
	links := make([]model.Link, 0)
	
	// 定义网盘类型及其对应的链接关键词
	categories := map[string][]string{
		"magnet":  {"magnet"},                                                                  // 磁力链接
		"ed2k":    {"ed2k"},                                                                    // 电驴链接
		"uc":      {"drive.uc.cn"},                                                             // UC网盘
		"mobile":  {"caiyun.139.com"},                                                          // 移动云盘
		"tianyi":  {"cloud.189.cn"},                                                            // 天翼云盘
		"quark":   {"pan.quark.cn"},                                                            // 夸克网盘
		"115":     {"115cdn.com", "115.com", "anxia.com"},                                      // 115网盘
		"aliyun":  {"alipan.com", "aliyundrive.com"},                                           // 阿里云盘
		"pikpak":  {"mypikpak.com"},                                                            // PikPak网盘
		"baidu":   {"pan.baidu.com"},                                                           // 百度网盘
		"123":     {"123684.com", "123685.com", "123912.com", "123pan.com", "123pan.cn", "123592.com"}, // 123网盘
		"lanzou":  {"lanzou", "lanzoux"},                                                       // 蓝奏云
		"xunlei":  {"pan.xunlei.com"},                                                          // 迅雷网盘
		"weiyun":  {"weiyun.com"},                                                              // 微云
		"jianguoyun": {"jianguoyun.com"},                                                       // 坚果云
	}
	
	// 遍历所有分类，提取对应的链接
	for category, patterns := range categories {
		for _, pattern := range patterns {
			categoryLinks := extractLinksByPattern(content, pattern, "", category)
			links = append(links, categoryLinks...)
		}
	}
	
	return links
}

// extractLinksByPattern 根据特定模式提取链接
func extractLinksByPattern(content, pattern, altPattern, linkType string) []model.Link {
	links := make([]model.Link, 0)
	
	// 查找所有包含pattern的行
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		// 提取主要pattern的链接
		if idx := strings.Index(line, pattern); idx != -1 {
			link := extractLinkFromLine(line[idx:], pattern)
			if link.URL != "" {
				link.Type = linkType
				links = append(links, link)
			}
		}
		
		// 如果有替代pattern，也提取
		if altPattern != "" {
			if idx := strings.Index(line, altPattern); idx != -1 {
				link := extractLinkFromLine(line[idx:], altPattern)
				if link.URL != "" {
					link.Type = linkType
					links = append(links, link)
				}
			}
		}
	}
	
	return links
}

// extractLinkFromLine 从行中提取链接和密码
func extractLinkFromLine(line, prefix string) model.Link {
	link := model.Link{}
	
	// 提取URL
	endIdx := strings.Index(line, "\"")
	if endIdx == -1 {
		endIdx = strings.Index(line, "'")
	}
	if endIdx == -1 {
		endIdx = strings.Index(line, " ")
	}
	if endIdx == -1 {
		endIdx = strings.Index(line, "<")
	}
	if endIdx == -1 {
		endIdx = len(line)
	}
	
	url := line[:endIdx]
	link.URL = url
	
	// 查找密码
	pwdKeywords := []string{"提取码", "密码", "提取密码", "pwd", "password", "提取"}
	for _, keyword := range pwdKeywords {
		if pwdIdx := strings.Index(strings.ToLower(line), strings.ToLower(keyword)); pwdIdx != -1 {
			// 密码通常在关键词后面
			restOfLine := line[pwdIdx+len(keyword):]
			
			// 跳过可能的分隔符
			restOfLine = strings.TrimLeft(restOfLine, " :：=")
			
			// 提取密码（通常是4个字符）
			if len(restOfLine) >= 4 {
				// 获取前4个字符作为密码
				password := strings.TrimSpace(restOfLine[:4])
				// 确保密码不包含HTML标签或其他非法字符
				if !strings.ContainsAny(password, "<>\"'") {
					link.Password = password
					break
				}
			}
		}
	}
	
	return link
}

// cleanHTML 清理HTML标签，保留纯文本内容
func cleanHTML(html string) string {
	// 移除HTML标签
	text := html
	
	// 移除<script>标签及其内容
	for {
		startIdx := strings.Index(text, "<script")
		if startIdx == -1 {
			break
		}
		
		endIdx := strings.Index(text[startIdx:], "</script>")
		if endIdx == -1 {
			break
		}
		
		text = text[:startIdx] + text[startIdx+endIdx+9:]
	}
	
	// 移除<style>标签及其内容
	for {
		startIdx := strings.Index(text, "<style")
		if startIdx == -1 {
			break
		}
		
		endIdx := strings.Index(text[startIdx:], "</style>")
		if endIdx == -1 {
			break
		}
		
		text = text[:startIdx] + text[startIdx+endIdx+8:]
	}
	
	// 移除其他HTML标签
	for {
		startIdx := strings.Index(text, "<")
		if startIdx == -1 {
			break
		}
		
		endIdx := strings.Index(text[startIdx:], ">")
		if endIdx == -1 {
			break
		}
		
		text = text[:startIdx] + " " + text[startIdx+endIdx+1:]
	}
	
	// 替换HTML实体
	text = strings.ReplaceAll(text, "&nbsp;", " ")
	text = strings.ReplaceAll(text, "&lt;", "<")
	text = strings.ReplaceAll(text, "&gt;", ">")
	text = strings.ReplaceAll(text, "&amp;", "&")
	text = strings.ReplaceAll(text, "&quot;", "\"")
	
	// 移除多余空白
	text = strings.Join(strings.Fields(text), " ")
	
	return text
}

// min 返回两个整数中的较小值
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Pan666Response API响应结构
type Pan666Response struct {
	Links struct {
		First string `json:"first"`
		Next  string `json:"next,omitempty"`
	} `json:"links"`
	Data     []Pan666Discussion `json:"data"`
	Included []Pan666Post       `json:"included"`
}

// Pan666Discussion 讨论数据结构
type Pan666Discussion struct {
	Type       string `json:"type"`
	ID         string `json:"id"`
	Attributes struct {
		Title          string    `json:"title"`
		Slug           string    `json:"slug"`
		CommentCount   int       `json:"commentCount"`
		CreatedAt      string    `json:"createdAt"`
		LastPostedAt   string    `json:"lastPostedAt"`
		LastPostNumber int       `json:"lastPostNumber"`
		IsApproved     bool      `json:"isApproved"`
	} `json:"attributes"`
	Relationships struct {
		MostRelevantPost struct {
			Data struct {
				Type string `json:"type"`
				ID   string `json:"id"`
			} `json:"data"`
		} `json:"mostRelevantPost"`
	} `json:"relationships"`
}

// Pan666Post 帖子内容结构
type Pan666Post struct {
	Type       string `json:"type"`
	ID         string `json:"id"`
	Attributes struct {
		Number      int    `json:"number"`
		CreatedAt   string `json:"createdAt"`
		ContentType string `json:"contentType"`
		ContentHTML string `json:"contentHtml"`
	} `json:"attributes"`
} 

// extractLinksFromText 从清理后的文本中提取网盘链接
func extractLinksFromText(content string) []model.Link {
	// 定义网盘类型及其对应的链接关键词
	categories := map[string][]string{
		"magnet":  {"magnet"},                                                                  // 磁力链接
		"ed2k":    {"ed2k"},                                                                    // 电驴链接
		"uc":      {"drive.uc.cn"},                                                             // UC网盘
		"mobile":  {"caiyun.139.com"},                                                          // 移动云盘
		"tianyi":  {"cloud.189.cn"},                                                            // 天翼云盘
		"quark":   {"pan.quark.cn"},                                                            // 夸克网盘
		"115":     {"115cdn.com", "115.com", "anxia.com"},                                      // 115网盘
		"aliyun":  {"alipan.com", "aliyundrive.com"},                                           // 阿里云盘
		"pikpak":  {"mypikpak.com"},                                                            // PikPak网盘
		"baidu":   {"pan.baidu.com"},                                                           // 百度网盘
		"123":     {"123684.com", "123685.com", "123912.com", "123pan.com", "123pan.cn", "123592.com"}, // 123网盘
		"lanzou":  {"lanzou", "lanzoux"},                                                       // 蓝奏云
		"xunlei":  {"pan.xunlei.com"},                                                          // 迅雷网盘
		"weiyun":  {"weiyun.com"},                                                              // 微云
		"jianguoyun": {"jianguoyun.com"},                                                       // 坚果云
	}
	
	// 存储所有找到的链接及其在文本中的位置
	type linkInfo struct {
		link     model.Link
		position int
		category string
	}
	var allLinks []linkInfo
	
	// 第一步：提取所有链接及其位置
	for category, patterns := range categories {
		for _, pattern := range patterns {
			pos := 0
			for {
				idx := strings.Index(content[pos:], pattern)
				if idx == -1 {
					break
				}
				
				// 计算实际位置
				actualPos := pos + idx
				
				// 提取URL
				url := extractURLFromText(content[actualPos:])
				if url != "" {
					// 检查URL是否已包含密码参数
					password := extractPasswordFromURL(url)
					
					// 创建链接
					link := model.Link{
						Type:     category,
						URL:      url,
						Password: password,
					}
					
					// 存储链接及其位置
					allLinks = append(allLinks, linkInfo{
						link:     link,
						position: actualPos,
						category: category,
					})
				}
				
				// 移动位置继续查找
				pos = actualPos + len(pattern)
			}
		}
	}
	
	// 按位置排序链接
	sort.Slice(allLinks, func(i, j int) bool {
		return allLinks[i].position < allLinks[j].position
	})
	
	// 第二步：提取所有密码关键词及其位置
	type passwordInfo struct {
		keyword   string
		position  int
		password  string
	}
	var allPasswords []passwordInfo
	
	// 密码关键词
	pwdKeywords := []string{"提取码", "密码", "提取密码", "pwd", "password", "提取码:", "密码:", "提取密码:", "pwd:", "password:", "提取:"}
	
	for _, keyword := range pwdKeywords {
		pos := 0
		for {
			idx := strings.Index(strings.ToLower(content[pos:]), strings.ToLower(keyword))
			if idx == -1 {
				break
			}
			
			// 计算实际位置
			actualPos := pos + idx
			
			// 提取密码
			restContent := content[actualPos+len(keyword):]
			restContent = strings.TrimLeft(restContent, " :：=")
			
			var password string
			if len(restContent) >= 4 {
				possiblePwd := strings.TrimSpace(restContent[:4])
				if !strings.ContainsAny(possiblePwd, "<>\"'\t\n\r") {
					password = possiblePwd
				}
			}
			
			if password != "" {
				allPasswords = append(allPasswords, passwordInfo{
					keyword:  keyword,
					position: actualPos,
					password: password,
				})
			}
			
			// 移动位置继续查找
			pos = actualPos + len(keyword)
		}
	}
	
	// 按位置排序密码
	sort.Slice(allPasswords, func(i, j int) bool {
		return allPasswords[i].position < allPasswords[j].position
	})
	
	// 第三步：为每个密码找到它前面最近的链接
	// 创建链接的副本，用于最终结果
	finalLinks := make([]model.Link, len(allLinks))
	for i, linkInfo := range allLinks {
		finalLinks[i] = linkInfo.link
	}
	
	// 对于每个密码，找到它前面最近的链接
	for _, pwdInfo := range allPasswords {
		// 找到密码前面最近的链接
		var closestLinkIndex int = -1
		minDistance := 1000000
		
		for i, linkInfo := range allLinks {
			// 只考虑密码前面的链接
			if linkInfo.position < pwdInfo.position {
				distance := pwdInfo.position - linkInfo.position
				
				// 密码必须在链接后的200个字符内
				if distance < 200 && distance < minDistance {
					minDistance = distance
					closestLinkIndex = i
				}
			}
		}
		
		// 如果找到了链接，并且该链接没有从URL中提取的密码
		if closestLinkIndex != -1 && finalLinks[closestLinkIndex].Password == "" {
			// 检查这个链接后面是否有其他链接
			hasNextLink := false
			for _, linkInfo := range allLinks {
				// 如果有链接在当前链接和密码之间，说明当前链接不需要密码
				if linkInfo.position > allLinks[closestLinkIndex].position && 
				   linkInfo.position < pwdInfo.position {
					hasNextLink = true
					break
				}
			}
			
			// 只有当没有其他链接在当前链接和密码之间时，才将密码关联到链接
			if !hasNextLink {
				finalLinks[closestLinkIndex].Password = pwdInfo.password
			}
		}
	}
	
	return finalLinks
}

// extractURLFromText 从文本中提取URL
func extractURLFromText(text string) string {
	// 查找URL的结束位置
	endIdx := strings.IndexAny(text, " \t\n\r\"'<>")
	if endIdx == -1 {
		endIdx = len(text)
	}
	
	// 提取URL
	url := text[:endIdx]
	
	// 清理URL
	url = strings.TrimPrefix(url, "http://")
	url = strings.TrimPrefix(url, "https://")
	url = strings.TrimPrefix(url, "www.")
	
	return url
}

// extractPasswordFromURL 从URL中提取密码参数
func extractPasswordFromURL(url string) string {
	// 检查URL是否包含密码参数
	if strings.Contains(url, "?pwd=") {
		parts := strings.Split(url, "?pwd=")
		if len(parts) > 1 {
			// 提取密码参数
			pwd := parts[1]
			// 如果密码后面还有其他参数，只取密码部分
			if idx := strings.IndexAny(pwd, "&?"); idx != -1 {
				pwd = pwd[:idx]
			}
			return pwd
		}
	}
	return ""
}

// abs 返回整数的绝对值
func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
} 