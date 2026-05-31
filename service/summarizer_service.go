package service

import (
	"echo-core/models"
	"echo-core/remote"
	"echo-core/repository"
)

// Summarizer 对话摘要器
type Summarizer struct {
	aiClient *remote.AIClient
	memRepo  *repository.MemoryRepository
}

// NewSummarizer 创建摘要器
func NewSummarizer(aiClient *remote.AIClient, memRepo *repository.MemoryRepository) *Summarizer {
	return &Summarizer{
		aiClient: aiClient,
		memRepo:  memRepo,
	}
}

// ShouldSummarize 判断是否需要生成摘要
// 当消息数超过阈值时返回true
func (s *Summarizer) ShouldSummarize(messageCount int) bool {
	return messageCount > 20 // 超过20条消息时生成摘要
}

// GenerateSummary 生成摘要
func (s *Summarizer) GenerateSummary(sessionID, userID string, messages []remote.AIChatMessage) (string, error) {
	if len(messages) == 0 {
		return "", nil
	}

	// 调用AI生成摘要
	summary, err := s.aiClient.GenerateSummary(messages)
	if err != nil {
		return "", err
	}

	// 保存摘要到数据库
	convSummary := &models.ConversationSummary{
		SessionID: sessionID,
		UserID:    userID,
		Summary:   summary,
	}
	if err := s.memRepo.SaveConversationSummary(convSummary); err != nil {
		return summary, err
	}

	return summary, nil
}

// GetSummary 获取会话摘要
func (s *Summarizer) GetSummary(sessionID, userID string) (string, error) {
	summary, err := s.memRepo.GetOrCreateSummary(sessionID, userID)
	if err != nil {
		return "", err
	}
	return summary.Summary, nil
}

// BuildContext 构建带摘要的上下文
func (s *Summarizer) BuildContext(sessionID, userID string, messages []remote.AIChatMessage) ([]remote.AIChatMessage, error) {
	summary, err := s.GetSummary(sessionID, userID)
	if err != nil || summary == "" {
		return messages, nil
	}

	context := make([]remote.AIChatMessage, 0, len(messages)+1)
	context = append(context, remote.AIChatMessage{
		Role:    "system",
		Content: "【对话摘要】" + summary,
	})
	context = append(context, messages...)

	return context, nil
}