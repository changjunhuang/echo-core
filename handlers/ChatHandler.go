package handlers

import (
	"go-start/config"
	"net/http"
	"strings"

	"go-start/dto"
	"go-start/service"

	"github.com/gin-gonic/gin"
)

// ChatHandler handles chat-related requests.
type ChatHandler struct {
	agentService *service.AgentService
}

// NewChatHandler creates a new ChatHandler.
func NewChatHandler(agentService *service.AgentService) *ChatHandler {
	return &ChatHandler{
		agentService: agentService,
	}
}

// HandleChat handles the chat endpoint.
func (h *ChatHandler) HandleChat(c *gin.Context) {
	var req dto.ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ChatResponse{Reply: "Invalid request"})
		return
	}

	req.Message = strings.TrimSpace(req.Message)
	if req.Message == "" {
		c.JSON(http.StatusBadRequest, dto.ChatResponse{Reply: "Message cannot be empty"})
		return
	}

	sessionID := req.SessionID
	if sessionID == "" {
		sessionID = "default_session" // 或者生成新的UUID
	}

	// 将 History 传递到 AgentService (暂用 options 包装或者扩展接口)
	options := config.LLMRequestOptions{
		Provider: req.Provider,
		Model:    req.Model,
		BaseURL:  req.BaseURL,
		APIKey:   req.APIKey,
	}

	// AgentService 需要改造以支持接收历史对话记录，并在生成回复时考虑这些历史记录
	reply, err := h.agentService.ChatWithHistory(c.Request.Context(), sessionID, req.Message, req.History, options)
	if err != nil {
		c.JSON(http.StatusInternalServerError, dto.ChatResponse{Reply: err.Error()})
		return
	}

	c.JSON(http.StatusOK, dto.ChatResponse{Reply: reply})
}
