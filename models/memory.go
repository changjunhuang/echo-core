package models

import (
	"time"
)

// SessionMessage 会话消息 - 短期记忆，存储会话级别的对话历史
type SessionMessage struct {
	ID         uint      `json:"id" gorm:"primaryKey"`
	SessionID  string    `json:"session_id" gorm:"index;size:64;not null"`
	UserID     string    `json:"user_id" gorm:"index;size:64;not null"`
	Role       string    `json:"role" gorm:"size:20;not null"` // user/assistant/system/tool
	Content    string    `json:"content" gorm:"type:text"`
	ToolCalls  string    `json:"tool_calls" gorm:"type:text"`       // assistant 角色的工具调用(JSON)
	ToolResult string    `json:"tool_result" gorm:"type:text"`      // 工具结果(JSON)；tool 角色时使用
	ToolCallID string    `json:"tool_call_id" gorm:"size:64;index"` // tool 角色消息引用的 assistant tool_call.id
	CreatedAt  time.Time `json:"created_at" gorm:"autoCreateTime"`
}

func (SessionMessage) TableName() string {
	return "session_message"
}

// UserMemory 用户记忆 - 长期记忆，存储用户级别的偏好和知识
type UserMemory struct {
	ID         uint      `json:"id" gorm:"primaryKey"`
	UserID     string    `json:"user_id" gorm:"index;size:64;not null"`
	MemoryType string    `json:"memory_type" gorm:"size:50;not null"` // preference/info/summary/knowledge
	Content    string    `json:"content" gorm:"type:text;not null"`
	Embedding  string    `json:"embedding" gorm:"type:text"` // 向量数据(JSON格式)
	CreatedAt  time.Time `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt  time.Time `json:"updated_at" gorm:"autoUpdateTime"`
}

func (UserMemory) TableName() string {
	return "user_memory"
}

// ConversationSummary 对话摘要 - 用于压缩超长对话上下文
// 每个 session 最多保留一份最新摘要（Upsert 语义）：
//   - UpdatedAt 用于识别 "同一 session 的最新摘要"
//   - MessageCount 记录该摘要覆盖到的消息条数，用于控制触发间隔
type ConversationSummary struct {
	ID           uint      `json:"id" gorm:"primaryKey"`
	SessionID    string    `json:"session_id" gorm:"index;size:64;not null"`
	UserID       string    `json:"user_id" gorm:"index;size:64;not null"`
	Summary      string    `json:"summary" gorm:"type:text;not null"`
	MessageCount int       `json:"message_count" gorm:"default:0;comment:该摘要覆盖到的消息条数,用于增量触发判断"`
	CreatedAt    time.Time `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt    time.Time `json:"updated_at" gorm:"autoUpdateTime"`
}

func (ConversationSummary) TableName() string {
	return "conversation_summary"
}

// AgentConfig Agent配置 - 为多Agent编排预留
type AgentConfig struct {
	ID          uint      `json:"id" gorm:"primaryKey"`
	AgentType   string    `json:"agent_type" gorm:"size:50;not null;uniqueIndex"`
	Name        string    `json:"name" gorm:"size:100;not null"`
	Description string    `json:"description" gorm:"type:text"`
	Tools       string    `json:"tools" gorm:"type:text"`  // JSON格式存储工具配置
	Prompt      string    `json:"prompt" gorm:"type:text"` // 提示词模板
	IsDefault   bool      `json:"is_default" gorm:"default:false"`
	CreatedAt   time.Time `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt   time.Time `json:"updated_at" gorm:"autoUpdateTime"`
}

func (AgentConfig) TableName() string {
	return "agent_config"
}
