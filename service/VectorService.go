package service

import (
	"context"
	"fmt"
	"go-start/config"
	"go-start/dto"
	vectorModel "go-start/models/vector"
	"go-start/remote"
	repositoryVector "go-start/repository/vector"
	"log"

	"github.com/sashabaranov/go-openai"
)

type VectorService struct {
	fileService  *FileService
	echoRemote   *remote.EchoRemote
	weaviateRepo repositoryVector.WeaviateRepository
	historyClass string
}

func NewVectorService() *VectorService {
	repo, err := repositoryVector.NewWeaviateRepository()
	if err != nil {
		log.Printf("Warning: failed to init Weaviate struct in VectorService: %v", err)
	}
	s := &VectorService{
		echoRemote:   remote.NewEchoRemote(),
		weaviateRepo: repo,
		historyClass: "ChatHistoryVector",
	}

	// 尝试初始化ChatHistory schema
	if s.weaviateRepo != nil {
		_ = s.weaviateRepo.EnsureSchema(context.Background(), s.historyClass)
	}

	return s
}

// GetVectorFromEcho 从 Echo 获取向量数据
func (s *VectorService) GetVectorFromEcho(imageData []byte) ([]float32, error) {
	return s.echoRemote.GetImageEmbedding(imageData)
}

// GetVectorFromText converts text to a vector using the remote service.
func (s *VectorService) GetVectorFromText(text string) ([]float32, error) {
	return s.echoRemote.GetTextEmbedding(text)
}

// StoreChatSegment 将大段多轮对话的历史文本打碎成向量保存至 Weaviate
func (s *VectorService) StoreChatSegment(ctx context.Context, sessionID string, content string) error {
	if s.weaviateRepo == nil {
		return fmt.Errorf("weaviate not initialized")
	}

	vector, err := s.GetVectorFromText(content)
	if err != nil {
		return fmt.Errorf("get vector space failed: %v", err)
	}

	doc := vectorModel.DocumentVector{
		FileID:   sessionID,
		Filename: "chat_history",
		Vector:   vector,
		Metadata: map[string]interface{}{
			"content": content,
		},
	}

	return s.weaviateRepo.InsertDocument(ctx, s.historyClass, doc)
}

// SearchRelatedChatHistory 检索当前提问强相关过的历史上下文
func (s *VectorService) SearchRelatedChatHistory(ctx context.Context, sessionID string, query string, limit int) ([]string, error) {
	if s.weaviateRepo == nil {
		return nil, fmt.Errorf("weaviate not initialized")
	}

	queryVec, err := s.GetVectorFromText(query)
	if err != nil {
		return nil, err
	}

	docs, err := s.weaviateRepo.SearchByVector(ctx, s.historyClass, queryVec, limit)
	if err != nil {
		return nil, err
	}

	var results []string
	for _, raw := range docs {
		// 校验其SessionID是否匹配
		if raw.FileID == sessionID {
			if floatContent, ok := raw.Metadata["content"]; ok {
				if contentStr, okStr := floatContent.(string); okStr {
					results = append(results, contentStr)
				}
			}
		}
	}
	return results, nil
}

// AsyncGenerateSummary 离线/异步对历史大段文本生成意图级摘要
func (s *VectorService) AsyncGenerateSummary(ctx context.Context, sessionID string, history []dto.ChatMessage, options config.LLMRequestOptions) {
	// 创建新的 context 避免随着接口立马结束而取消掉执行进程
	bgCtx := context.Background()
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("Async summary generation panic: %v", r)
			}
		}()

		log.Printf("Starting Async Summary generation for SessionID: %s, Messages: %d", sessionID, len(history))

		cfg, err := config.ResolveLLMConfig(options)
		if err != nil {
			log.Printf("Summary generation config error: %v", err)
			return
		}

		clientConfig := openai.DefaultConfig(cfg.APIKey)
		clientConfig.BaseURL = cfg.BaseURL
		client := openai.NewClientWithConfig(clientConfig)

		// 简单将对话拼接起来作为提词器
		comboText := ""
		for _, m := range history {
			comboText += fmt.Sprintf("%s: %s\n", m.Role, m.Content)
		}

		prompt := fmt.Sprintf("请总结以下这些多轮对话。只需提取关键信息、任务状态及重点实体信息:\n%s", comboText)

		req := openai.ChatCompletionRequest{
			Model: cfg.Model,
			Messages: []openai.ChatCompletionMessage{
				{Role: openai.ChatMessageRoleSystem, Content: "你是一个专门负责生成对话摘要的系统工程师助手。"},
				{Role: openai.ChatMessageRoleUser, Content: prompt},
			},
			Temperature: 0.1,
		}

		resp, err := client.CreateChatCompletion(bgCtx, req)
		if err != nil {
			log.Printf("Async Summary LLM failure: %v", err)
			return
		}

		summary := resp.Choices[0].Message.Content
		log.Printf("==> Successfully generated async summary for %s: \n%s", sessionID, summary)

		// 将这次摘要也打入Weaviate中（这里也可以放在Redis或者其他内存里，这里统一放入Weaviate作为一个特定Segment）
		err = s.StoreChatSegment(bgCtx, sessionID, "【核心摘要记忆】: "+summary)
		if err != nil {
			log.Printf("Store summary back to Weaviate error: %v", err)
		}
	}()
}
