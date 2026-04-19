package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/sashabaranov/go-openai"
)

// WeaviateSearchTool 是查找图片的工具
type WeaviateSearchTool struct {
	weaviateService *WeaviateService
	vectorService   *VectorService
}

func NewWeaviateSearchTool(ws *WeaviateService, vs *VectorService) *WeaviateSearchTool {
	return &WeaviateSearchTool{
		weaviateService: ws,
		vectorService:   vs,
	}
}

func (t *WeaviateSearchTool) Name() string {
	return "search_images_by_text"
}

func (t *WeaviateSearchTool) Description() string {
	return "根据用户描述的文本或者要求，在向量数据库中搜索最相近的图片。如果用户想要找任何特定的图片，必须使用此工具。"
}

func (t *WeaviateSearchTool) Definition() openai.FunctionDefinition {
	// 动态返回参数描述。利用 map 构建 OpenAI Function 参数
	return openai.FunctionDefinition{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{
					"type":        "string",
					"description": "用户想搜索的图片具体特征、名字或内容的详细描述",
				},
			},
			"required": []string{"query"},
		},
	}
}

// SearchArgs 定义传入的JSON参数结构
type SearchArgs struct {
	Query string `json:"query"`
}

func (t *WeaviateSearchTool) Execute(ctx context.Context, args string) (string, error) {
	log.Printf("开始调用Execute方法，params={args: %s}", args)
	var input SearchArgs
	if err := json.Unmarshal([]byte(args), &input); err != nil {
		log.Printf("解析参数失败: %w", err)
		return "", fmt.Errorf("解析参数失败: %w", err)
	}

	// 1. 利用 vectorService 将查询文本转换为向量
	queryVector, err := t.vectorService.GetVectorFromText(input.Query)
	if err != nil {
		log.Printf("error converting query to vector: %w", err)
		return "", fmt.Errorf("error converting query to vector: %w", err)
	}

	// 2. 利用 weaviateService 在向量库中查找最匹配的图片实体
	results, err := t.weaviateService.SearchByVector(ctx, queryVector, 5)
	if err != nil {
		log.Printf("error searching Weaviate: %w", err)
		return "", fmt.Errorf("error searching Weaviate: %w", err)
	}

	if len(results) == 0 {
		log.Printf("没有在向量库中找到与该问题相关的图片信息。")
		return "没有在向量库中找到与该问题相关的图片信息。", nil
	}

	var builder strings.Builder
	builder.WriteString("检索到以下相关图片：\n")
	for idx, res := range results {
		builder.WriteString(fmt.Sprintf("%d. 文件名: %s", idx+1, res.Filename))
		if res.FileID != "" {
			builder.WriteString(fmt.Sprintf("；文件ID: %s", res.FileID))
		}
		builder.WriteString("\n")
	}
	log.Printf("图片检索结果：%s", builder.String())
	return strings.TrimSpace(builder.String()), nil
}
