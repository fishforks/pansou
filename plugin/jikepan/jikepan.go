package jikepan

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"pansou/model"
	"pansou/plugin"
	"pansou/util/json"
)

// 在init函数中注册插件
func init() {
	// 使用全局超时时间创建插件实例并注册
	plugin.RegisterGlobalPlugin(NewJikepanPlugin())
}

const (
	// JikepanAPIURL 即刻盘API地址
	JikepanAPIURL = "https://api.jikepan.xyz/search"
	// DefaultTimeout 默认超时时间
	DefaultTimeout = 6 * time.Second
)

// JikepanPlugin 即刻盘搜索插件
type JikepanPlugin struct {
	client  *http.Client
	timeout time.Duration
}

// NewJikepanPlugin 创建新的即刻盘搜索插件
func NewJikepanPlugin() *JikepanPlugin {
	timeout := DefaultTimeout
	
	return &JikepanPlugin{
		client: &http.Client{
			Timeout: timeout,
		},
		timeout: timeout,
	}
}

// Name 返回插件名称
func (p *JikepanPlugin) Name() string {
	return "jikepan"
}

// Priority 返回插件优先级
func (p *JikepanPlugin) Priority() int {
	return 3 // 中等优先级
}

// Search 执行搜索并返回结果
func (p *JikepanPlugin) Search(keyword string) ([]model.SearchResult, error) {
	// 构建请求
	reqBody := map[string]interface{}{
		"name":   keyword,
		"is_all": false,
	}
	
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request failed: %w", err)
	}
	
	req, err := http.NewRequest("POST", JikepanAPIURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("create request failed: %w", err)
	}
	
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("referer", "https://jikepan.xyz/")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")
	
	// 发送请求
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	
	// 解析响应
	var apiResp JikepanResponse
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body failed: %w", err)
	}
	
	if err := json.Unmarshal(bodyBytes, &apiResp); err != nil {
		return nil, fmt.Errorf("decode response failed: %w", err)
	}
	
	// 检查响应状态
	if apiResp.Msg != "success" {
		return nil, fmt.Errorf("API returned error: %s", apiResp.Msg)
	}
	
	// 转换结果格式
	return p.convertResults(apiResp.List), nil
}

// convertResults 将API响应转换为标准SearchResult格式
func (p *JikepanPlugin) convertResults(items []JikepanItem) []model.SearchResult {
	results := make([]model.SearchResult, 0, len(items))
	
	for i, item := range items {
		// 跳过没有链接的结果
		if len(item.Links) == 0 {
			continue
		}
		
		// 创建链接列表
		links := make([]model.Link, 0, len(item.Links))
		for _, link := range item.Links {
			linkType := p.convertLinkType(link.Service)
			
			// 特殊处理other类型，检查链接URL
			if linkType == "others" && strings.Contains(strings.ToLower(link.Link), "drive.uc.cn") {
				linkType = "uc"
			}
			
			// 跳过未知类型的链接（linkType为空）
			if linkType == "" {
				continue
			}
			
			// 创建链接
			links = append(links, model.Link{
				URL:      link.Link,
				Type:     linkType,
				Password: link.Pwd,
			})
		}
		
		// 创建唯一ID：插件名-索引
		uniqueID := fmt.Sprintf("jikepan-%d", i)
		
		// 创建搜索结果
		result := model.SearchResult{
			UniqueID:  uniqueID,
			Title:     item.Name,
			Datetime:  time.Time{}, // 使用零值表示无时间，而不是time.Now()
			Links:     links,
		}
		
		results = append(results, result)
	}
	
	return results
}

// convertLinkType 将API的服务类型转换为标准链接类型
func (p *JikepanPlugin) convertLinkType(service string) string {
	service = strings.ToLower(service)
	
	switch service {
	case "baidu":
		return "baidu"
	case "aliyun":
		return "aliyun"
	case "xunlei":
		return "xunlei"
	case "quark":
		return "quark"
	case "189cloud":
		return "tianyi"
	case "115":
		return "115"
	case "123":
		return "123"
	case "weiyun":
		return "weiyun"
	case "pikpak":
		return "pikpak"
	case "lanzou":
		return "lanzou"
	case "jianguoyun":
		return "jianguoyun"
	case "caiyun":
		return "mobile"
	case "chengtong":
		return "chengtong"
	case "ed2k":
		return "ed2k"
	case "magnet":
		return "magnet"
	case "unknown":
		// 对于未知类型，返回空字符串，以便在后续处理中跳过
		return ""
	default:
		return "others"
	}
}

// JikepanResponse API响应结构
type JikepanResponse struct {
	Msg  string       `json:"msg"`
	List []JikepanItem `json:"list"`
}

// JikepanItem API响应中的单个结果项
type JikepanItem struct {
	Name  string        `json:"name"`
	Links []JikepanLink `json:"links"`
}

// JikepanLink API响应中的链接
type JikepanLink struct {
	Service string `json:"service"`
	Link    string `json:"link"`
	Pwd     string `json:"pwd,omitempty"`
} 