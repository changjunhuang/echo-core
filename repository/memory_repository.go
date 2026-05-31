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
	return config.GetDB().Model(&existing).Where("id = ?", existing.ID).Update("content", memory.Content).Error
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