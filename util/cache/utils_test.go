package cache

import (
	"testing"
	"time"
	
	"pansou/model"
	"pansou/util/json"
)

// 创建测试用的搜索响应
func createTestResponse() model.SearchResponse {
	results := make([]model.SearchResult, 50)
	for i := 0; i < 50; i++ {
		links := make([]model.Link, 3)
		for j := 0; j < 3; j++ {
			links[j] = model.Link{
				URL:      "https://example.com/file" + string(rune('a'+j)) + string(rune('0'+i%10)),
				Type:     "baidu",
				Password: "pwd123",
			}
		}
		
		results[i] = model.SearchResult{
			Title:    "测试结果 " + string(rune('A'+i%26)),
			Content:  "这是一个测试内容，包含一些描述信息，用于测试序列化性能。",
			Datetime: time.Now().Add(-time.Duration(i) * time.Hour),
			Links:    links,
		}
	}
	
	mergedLinks := make(model.MergedLinks)
	mergedLinks["baidu"] = make([]model.MergedLink, 20)
	mergedLinks["aliyun"] = make([]model.MergedLink, 15)
	
	for i := 0; i < 20; i++ {
		mergedLinks["baidu"][i] = model.MergedLink{
			URL:      "https://pan.baidu.com/s/" + string(rune('a'+i%26)),
			Password: "abcd",
			Note:     "百度网盘资源" + string(rune('0'+i%10)),
			Datetime: time.Now().Add(-time.Duration(i) * time.Hour),
		}
	}
	
	for i := 0; i < 15; i++ {
		mergedLinks["aliyun"][i] = model.MergedLink{
			URL:      "https://aliyundrive.com/s/" + string(rune('a'+i%26)),
			Password: "1234",
			Note:     "阿里云盘资源" + string(rune('0'+i%10)),
			Datetime: time.Now().Add(-time.Duration(i) * time.Hour),
		}
	}
	
	return model.SearchResponse{
		Total:        len(results),
		Results:      results,
		MergedByType: mergedLinks,
	}
}

func BenchmarkSerializeWithPool(b *testing.B) {
	resp := createTestResponse()
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		data, err := SerializeWithPool(resp)
		if err != nil {
			b.Fatal(err)
		}
		_ = data
	}
}

func BenchmarkStandardMarshal(b *testing.B) {
	resp := createTestResponse()
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		data, err := json.Marshal(resp)
		if err != nil {
			b.Fatal(err)
		}
		_ = data
	}
}

func BenchmarkDeserializeWithPool(b *testing.B) {
	resp := createTestResponse()
	data, _ := json.Marshal(resp)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var result model.SearchResponse
		err := DeserializeWithPool(data, &result)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkStandardUnmarshal(b *testing.B) {
	resp := createTestResponse()
	data, _ := json.Marshal(resp)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var result model.SearchResponse
		err := json.Unmarshal(data, &result)
		if err != nil {
			b.Fatal(err)
		}
	}
} 