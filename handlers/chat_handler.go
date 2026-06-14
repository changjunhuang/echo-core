package handlers

import (
	"echo-core/models"
	"echo-core/service"
	"fmt"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

type ChatHandler struct {
	svc *service.ChatService
}

func NewChatHandler(svc *service.ChatService) *ChatHandler {
	return &ChatHandler{svc: svc}
}

// ChatHandle 聊天接口
// POST /api/chat
func (h *ChatHandler) ChatHandle(c *gin.Context) {
	log.Printf("[ChatHandle] 收到聊天请求 | IP: %s | Method: %s", c.ClientIP(), c.Request.Method)

	var req service.ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("[ChatHandle] 请求参数解析失败: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	log.Printf("[ChatHandle] 请求解析成功 | userId: %s | sessionId: %s | messageLen: %d", req.UserID, req.SessionID, len(req.Message))

	// 设置默认值
	if req.SessionID == "" {
		log.Printf("[ChatHandle] sessionId 为空")
		c.JSON(http.StatusBadRequest, gin.H{"error": "sessionId is required"})
		return
	}
	if req.UserID == "" {
		log.Printf("[ChatHandle] userId为空")
		c.JSON(http.StatusBadRequest, gin.H{"error": "userId is required"})
		return
	}

	log.Printf("[ChatHandle] 开始调用ChatService | userId: %s | sessionId: %s", req.UserID, req.SessionID)
	resp, err := h.svc.Chat(req)
	if err != nil {
		log.Printf("[ChatHandle] ChatService调用失败 | userId: %s | sessionId: %s | error: %v", req.UserID, req.SessionID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	log.Printf("[ChatHandle] 聊天完成 | userId: %s | sessionId: %s | replyLen: %d", req.UserID, req.SessionID, len(resp.Reply))
	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": resp,
	})
}

// GetHistoryHandle 获取历史消息
// GET /api/chat/history?session_id=xxx&user_id=xxx&limit=50
func (h *ChatHandler) GetHistoryHandle(c *gin.Context) {
	sessionID := c.Query("session_id")
	userID := c.Query("user_id")
	if sessionID == "" || userID == "" {
		log.Printf("[GetHistoryHandle] 参数缺失 | session_id: %s | user_id: %s", sessionID, userID)
		c.JSON(http.StatusBadRequest, gin.H{"error": "session_id and user_id are required"})
		return
	}

	log.Printf("[GetHistoryHandle] 获取历史 | user_id: %s | session_id: %s", userID, sessionID)
	limit := 50
	if l := c.Query("limit"); l != "" {
		// 简单解析limit
		// 实际应用中可用strconv.Atoi
	}

	history, err := h.svc.GetHistory(sessionID, userID, limit)
	if err != nil {
		log.Printf("[GetHistoryHandle] 获取历史失败 | user_id: %s | session_id: %s | error: %v", userID, sessionID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	log.Printf("[GetHistoryHandle] 获取成功 | user_id: %s | session_id: %s | history_count: %d", userID, sessionID, len(history))
	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": history,
	})
}

// GetSummaryHandle 获取会话摘要
// GET /api/chat/summary?session_id=xxx&user_id=xxx
func (h *ChatHandler) GetSummaryHandle(c *gin.Context) {
	sessionID := c.Query("session_id")
	userID := c.Query("user_id")
	if sessionID == "" || userID == "" {
		log.Printf("[GetSummaryHandle] 参数缺失 | session_id: %s | user_id: %s", sessionID, userID)
		c.JSON(http.StatusBadRequest, gin.H{"error": "session_id and user_id are required"})
		return
	}

	log.Printf("[GetSummaryHandle] 获取摘要 | user_id: %s | session_id: %s", userID, sessionID)
	summary, err := h.svc.GetSummary(sessionID, userID)
	if err != nil {
		log.Printf("[GetSummaryHandle] 获取摘要失败 | user_id: %s | session_id: %s | error: %v", userID, sessionID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	log.Printf("[GetSummaryHandle] 获取成功 | user_id: %s | session_id: %s | summary_len: %d", userID, sessionID, len(summary))
	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": gin.H{"summary": summary},
	})
}

// GetUserMemoryHandle 获取用户记忆
// GET /api/chat/memory?user_id=xxx&type=preference
func (h *ChatHandler) GetUserMemoryHandle(c *gin.Context) {
	userID := c.Query("user_id")
	memoryType := c.Query("type")
	if userID == "" {
		log.Printf("[GetUserMemoryHandle] user_id为空")
		c.JSON(http.StatusBadRequest, gin.H{"error": "user_id is required"})
		return
	}
	if memoryType == "" {
		memoryType = "preference"
	}

	log.Printf("[GetUserMemoryHandle] 获取用户记忆 | user_id: %s | type: %s", userID, memoryType)
	memory, err := h.svc.GetUserMemory(userID, memoryType)
	if err != nil {
		log.Printf("[GetUserMemoryHandle] 获取失败 | user_id: %s | type: %s | error: %v", userID, memoryType, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	log.Printf("[GetUserMemoryHandle] 获取成功 | user_id: %s | type: %s", userID, memoryType)
	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": memory,
	})
}

// SaveUserMemoryHandle 保存用户记忆
// POST /api/chat/memory
type SaveMemoryRequest struct {
	UserID     string `json:"user_id" binding:"required"`
	MemoryType string `json:"type" binding:"required"`
	Content    string `json:"content" binding:"required"`
}

func (h *ChatHandler) SaveUserMemoryHandle(c *gin.Context) {
	var req SaveMemoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("[SaveUserMemoryHandle] 请求解析失败: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	log.Printf("[SaveUserMemoryHandle] 保存用户记忆 | user_id: %s | type: %s | content_len: %d", req.UserID, req.MemoryType, len(req.Content))
	if err := h.svc.SaveUserMemory(req.UserID, req.MemoryType, req.Content); err != nil {
		log.Printf("[SaveUserMemoryHandle] 保存失败 | user_id: %s | type: %s | error: %v", req.UserID, req.MemoryType, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	log.Printf("[SaveUserMemoryHandle] 保存成功 | user_id: %s | type: %s", req.UserID, req.MemoryType)
	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "memory saved",
	})
}

// ListUserMemoriesHandle 列出某用户全部长期记忆
// GET /api/chat/memory/all?user_id=xxx
func (h *ChatHandler) ListUserMemoriesHandle(c *gin.Context) {
	userID := c.Query("user_id")
	if userID == "" {
		log.Printf("[ListUserMemoriesHandle] user_id 为空")
		c.JSON(http.StatusBadRequest, gin.H{"error": "user_id is required"})
		return
	}

	log.Printf("[ListUserMemoriesHandle] 列出用户全部记忆 | user_id: %s", userID)
	memories, err := h.svc.ListUserMemories(userID)
	if err != nil {
		log.Printf("[ListUserMemoriesHandle] 列出失败 | user_id: %s | error: %v", userID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if memories == nil {
		memories = []models.UserMemory{}
	}

	log.Printf("[ListUserMemoriesHandle] 列出成功 | user_id: %s | count: %d", userID, len(memories))
	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": memories,
	})
}

// DeleteUserMemoryHandle 删除某用户指定类型的长期记忆
// DELETE /api/chat/memory?user_id=xxx&type=preference
func (h *ChatHandler) DeleteUserMemoryHandle(c *gin.Context) {
	userID := c.Query("user_id")
	memoryType := c.Query("type")
	if userID == "" {
		log.Printf("[DeleteUserMemoryHandle] user_id 为空")
		c.JSON(http.StatusBadRequest, gin.H{"error": "user_id is required"})
		return
	}
	if memoryType == "" {
		log.Printf("[DeleteUserMemoryHandle] type 为空")
		c.JSON(http.StatusBadRequest, gin.H{"error": "type is required"})
		return
	}

	log.Printf("[DeleteUserMemoryHandle] 删除用户记忆 | user_id: %s | type: %s", userID, memoryType)
	if err := h.svc.DeleteUserMemory(userID, memoryType); err != nil {
		log.Printf("[DeleteUserMemoryHandle] 删除失败 | user_id: %s | type: %s | error: %v", userID, memoryType, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	log.Printf("[DeleteUserMemoryHandle] 删除成功 | user_id: %s | type: %s", userID, memoryType)
	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "memory deleted",
	})
}

// GetAgentsHandle 获取Agent列表
// GET /api/chat/agents
func (h *ChatHandler) GetAgentsHandle(c *gin.Context) {
	log.Printf("[GetAgentsHandle] 获取Agent列表")
	agents := h.svc.GetAgents()
	log.Printf("[GetAgentsHandle] 获取成功 | agents_count: %d", len(agents))
	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": gin.H{"agents": agents},
	})
}

// ClearSessionHandle 清理会话
// DELETE /api/chat/session?session_id=xxx&user_id=xxx
func (h *ChatHandler) ClearSessionHandle(c *gin.Context) {
	sessionID := c.Query("session_id")
	userID := c.Query("user_id")
	if sessionID == "" || userID == "" {
		log.Printf("[ClearSessionHandle] 参数缺失 | session_id: %s | user_id: %s", sessionID, userID)
		c.JSON(http.StatusBadRequest, gin.H{"error": "session_id and user_id are required"})
		return
	}

	log.Printf("[ClearSessionHandle] 清理会话 | user_id: %s | session_id: %s", userID, sessionID)
	if err := h.svc.ClearSession(sessionID, userID); err != nil {
		log.Printf("[ClearSessionHandle] 清理失败 | user_id: %s | session_id: %s | error: %v", userID, sessionID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	log.Printf("[ClearSessionHandle] 清理成功 | user_id: %s | session_id: %s", userID, sessionID)
	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "session cleared",
	})
}

// CacheStatsHandle 返回系统提示词前缀缓存的命中统计
// GET /api/chat/cache/stats
func (h *ChatHandler) CacheStatsHandle(c *gin.Context) {
	stats := h.svc.CacheStats()
	total := stats.Hit + stats.Miss
	var rate float64
	if total > 0 {
		rate = float64(stats.Hit) / float64(total)
	}
	log.Printf("[CacheStatsHandle] hit=%d miss=%d rate=%.2f", stats.Hit, stats.Miss, rate)
	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": gin.H{
			"hit":       stats.Hit,
			"miss":      stats.Miss,
			"hit_rate":  rate,
			"hit_ratio": fmt.Sprintf("%.2f%%", rate*100),
		},
	})
}
