package repository

import (
	"echo-core/config"
	"echo-core/models"
	"time"
)

type MemoryRepository struct{}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{}
}

func (r *MemoryRepository) SaveSessionMessage(msg *models.SessionMessage) error {
	return config.GetDB().Create(msg).Error
}

func (r *MemoryRepository) GetSessionMessages(sessionID, userID string, limit int) ([]models.SessionMessage, error) {
	var messages []models.SessionMessage
	err := config.GetDB().Where("session_id = ? AND user_id = ?", sessionID, userID).
		Order("created_at asc").Limit(limit).Find(&messages).Error
	return messages, err
}

func (r *MemoryRepository) GetRecentSessions(userID string, limit int) ([]models.ConversationSummary, error) {
	var summaries []models.ConversationSummary
	err := config.GetDB().Where("user_id = ?", userID).
		Order("created_at desc").Limit(limit).Find(&summaries).Error
	return summaries, err
}

func (r *MemoryRepository) SaveConversationSummary(summary *models.ConversationSummary) error {
	return config.GetDB().Create(summary).Error
}

// GetLatestSummary 获取某 session 的最新摘要（按 UpdatedAt 倒序取第一条）
// 业界实现说明：UpdatedAt 是 GORM autoUpdateTime 自动维护的，识别"最新摘要"完全可靠。
// 查询走 session_id 索引（小表全表足够，10w+ 行也毫秒级返回）。
func (r *MemoryRepository) GetLatestSummary(sessionID string) (*models.ConversationSummary, error) {
	var summary models.ConversationSummary
	err := config.GetDB().Where("session_id = ?", sessionID).
		Order("updated_at DESC").First(&summary).Error
	if err != nil {
		return nil, err
	}
	return &summary, nil
}

// GetLatestSummaryVersion 轻量查询：只取 (id, updated_at, message_count) 三列
// 用于高频路径（每次聊天都查一次），避免 SELECT * 拉回可能很长的 summary 文本。
// 调用方拿到版本号后可用于 prefix cache 失效判断。
func (r *MemoryRepository) GetLatestSummaryVersion(sessionID string) (*models.ConversationSummary, error) {
	var summary models.ConversationSummary
	err := config.GetDB().Select("id", "session_id", "user_id", "updated_at", "message_count").
		Where("session_id = ?", sessionID).
		Order("updated_at DESC").First(&summary).Error
	if err != nil {
		return nil, err
	}
	return &summary, nil
}

// DeleteSummary 按 sessionID 删除摘要（用于"主动失效"，如记忆大幅更新、用户清空会话）
func (r *MemoryRepository) DeleteSummary(sessionID string) error {
	return config.GetDB().Where("session_id = ?", sessionID).
		Delete(&models.ConversationSummary{}).Error
}

// UpsertConversationSummary 按 (sessionID, userID) 唯一化保存摘要
// 同一 session 多次摘要不会产生重复行；返回最新一条
func (r *MemoryRepository) UpsertConversationSummary(sessionID, userID, content string, messageCount int) error {
	var existing models.ConversationSummary
	err := config.GetDB().Where("session_id = ? AND user_id = ?", sessionID, userID).First(&existing).Error
	if err != nil {
		// 不存在则创建
		now := time.Now()
		return config.GetDB().Create(&models.ConversationSummary{
			SessionID:    sessionID,
			UserID:       userID,
			Summary:      content,
			MessageCount: messageCount,
			CreatedAt:    now,
			UpdatedAt:    now,
		}).Error
	}
	// 已存在则覆盖
	return config.GetDB().Model(&existing).Where("id = ?", existing.ID).Updates(map[string]interface{}{
		"summary":       content,
		"message_count": messageCount,
		"updated_at":    time.Now(),
	}).Error
}

func (r *MemoryRepository) GetUserMemory(userID, memoryType string) (*models.UserMemory, error) {
	var memory models.UserMemory
	err := config.GetDB().Where("user_id = ? AND memory_type = ?", userID, memoryType).First(&memory).Error
	if err != nil {
		return nil, err
	}
	return &memory, nil
}

func (r *MemoryRepository) SaveUserMemory(memory *models.UserMemory) error {
	var existing models.UserMemory
	err := config.GetDB().Where("user_id = ? AND memory_type = ?", memory.UserID, memory.MemoryType).First(&existing).Error
	if err != nil {
		return config.GetDB().Create(memory).Error
	}
	// 已存在则覆盖内容（content 已包含合并后的最新记忆）
	return config.GetDB().Model(&existing).Where("id = ?", existing.ID).Updates(map[string]interface{}{
		"content":    memory.Content,
		"updated_at": time.Now(),
	}).Error
}

// ListUserMemories 列出某用户全部长期记忆（按更新时间倒序）
func (r *MemoryRepository) ListUserMemories(userID string) ([]models.UserMemory, error) {
	var memories []models.UserMemory
	err := config.GetDB().Where("user_id = ?", userID).
		Order("updated_at DESC").Find(&memories).Error
	return memories, err
}

// DeleteUserMemory 按 userID + memoryType 删除单条长期记忆
func (r *MemoryRepository) DeleteUserMemory(userID, memoryType string) error {
	return config.GetDB().Where("user_id = ? AND memory_type = ?", userID, memoryType).
		Delete(&models.UserMemory{}).Error
}

func (r *MemoryRepository) DeleteSessionMessages(sessionID string) error {
	return config.GetDB().Where("session_id = ?", sessionID).Delete(&models.SessionMessage{}).Error
}

func (r *MemoryRepository) GetOrCreateSummary(sessionID, userID string) (*models.ConversationSummary, error) {
	var summary models.ConversationSummary
	err := config.GetDB().Where("session_id = ? AND user_id = ?", sessionID, userID).First(&summary).Error
	if err != nil {
		summary = models.ConversationSummary{
			SessionID: sessionID,
			UserID:    userID,
			Summary:   "",
			CreatedAt: time.Now(),
		}
		if err := config.GetDB().Create(&summary).Error; err != nil {
			return nil, err
		}
	}
	return &summary, nil
}

func (r *MemoryRepository) UpdateSummary(id uint, content string) error {
	return config.GetDB().Model(&models.ConversationSummary{}).Where("id = ?", id).Update("summary", content).Error
}

func (r *MemoryRepository) AutoMigrate() error {
	return config.GetDB().AutoMigrate(&models.SessionMessage{}, &models.UserMemory{}, &models.ConversationSummary{}, &models.AgentConfig{})
}
