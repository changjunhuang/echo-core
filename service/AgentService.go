package service

import (
	"context"
	"fmt"
	"log"
	"strings"

	vectorModel "go-start/models/vector"

	"github.com/sashabaranov/go-openai"
)

// AgentService handles interactions with the LLM agent.
type AgentService struct {
	weaviateService *WeaviateService
	vectorService   *VectorService // 添加 VectorService
}

// NewAgentService creates a new AgentService.
func NewAgentService(weaviateService *WeaviateService, vectorService *VectorService) (*AgentService, error) { // 添加 vectorService 参数
	if _, err := LoadLLMConfig(); err != nil {
		return nil, err
	}

	return &AgentService{
		weaviateService: weaviateService,
		vectorService:   vectorService, // 初始化
	}, nil
}

// Query sends a query to the agent and returns the response.
func (s *AgentService) Query(ctx context.Context, query string, options LLMRequestOptions) (string, error) {
	// 1. 将查询文本转换为向量
	queryVector, err := s.vectorService.GetVectorFromText(query)
	if err != nil {
		return "", fmt.Errorf("error converting query to vector: %w", err)
	}

	// 2. 使用向量进行搜索，而不是 NearText
	results, err := s.weaviateService.SearchByVector(ctx, queryVector, 5)
	if err != nil {
		return "", fmt.Errorf("error searching Weaviate by vector: %w", err)
	}

	if len(results) == 0 {
		return "我没有在向量库中找到与该问题相关的图片信息。", nil
	}

	cfg, err := ResolveLLMConfig(options)
	if err != nil {
		return "", err
	}

	response, err := s.generateLLMReply(ctx, cfg, query, results)
	if err != nil {
		return s.buildFallbackReply(cfg, results), nil
	}

	return response, nil
}

func (s *AgentService) generateLLMReply(ctx context.Context, cfg LLMConfig, query string, results []vectorModel.DocumentVector) (string, error) {
	clientConfig := openai.DefaultConfig(cfg.APIKey)
	clientConfig.BaseURL = cfg.BaseURL
	client := openai.NewClientWithConfig(clientConfig)

	messages := []openai.ChatCompletionMessage{
		{
			Role:    openai.ChatMessageRoleSystem,
			Content: "你是一个图片知识问答助手。你必须优先依据向量检索结果回答问题；如果检索结果不足以支撑结论，就明确说明信息不足，不要编造图片内容。",
		},
		{
			Role:    openai.ChatMessageRoleUser,
			Content: fmt.Sprintf("用户问题是：%s。\n向量检索结果：\n %s。\n请基于以上检索结果，用简洁、自然的中文回答用户，并尽量指出对应图片文件名。", query, buildSearchContext(results)),
		},
	}

	resp, err := client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model:       cfg.Model,
		Messages:    messages,
		Temperature: 0.2,
	})
	if err != nil {
		log.Println("call llm provider %s failed: %w", cfg.Provider, err)
		return "", fmt.Errorf("call llm provider %s failed: %w", cfg.Provider, err)
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("empty llm response from provider %s", cfg.Provider)
	}

	content := strings.TrimSpace(resp.Choices[0].Message.Content)
	if content == "" {
		return "", fmt.Errorf("empty llm content from provider %s", cfg.Provider)
	}
	return content, nil
}

func buildSearchContext(results []vectorModel.DocumentVector) string {
	var builder strings.Builder
	for idx, res := range results {
		builder.WriteString(fmt.Sprintf("%d. 文件名: %s", idx+1, res.Filename))
		if res.FileID != "" {
			builder.WriteString(fmt.Sprintf("；文件ID: %s", res.FileID))
		}
		builder.WriteString("\n")
	}
	return strings.TrimSpace(builder.String())
}

func (s *AgentService) buildFallbackReply(cfg LLMConfig, results []vectorModel.DocumentVector) string {
	var builder strings.Builder
	builder.WriteString("已检索到相关图片，但大模型暂时不可用。")
	if cfg.Provider != "" {
		builder.WriteString("当前模型供应商: ")
		builder.WriteString(cfg.Provider)
		builder.WriteString("。")
	}
	builder.WriteString("你可以先查看这些候选图片：\n")
	for _, res := range results {
		builder.WriteString("- ")
		builder.WriteString(res.Filename)
		if res.FileID != "" {
			builder.WriteString(" (ID: ")
			builder.WriteString(res.FileID)
			builder.WriteString(")")
		}
		builder.WriteString("\n")
	}
	return strings.TrimSpace(builder.String())
}
