package handlers

import (
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

	// 调用 AgentService 处理聊天请求
	reply, err := h.agentService.Query(c.Request.Context(), req.Message, service.LLMRequestOptions{
		Provider: req.Provider,
		Model:    req.Model,
		BaseURL:  req.BaseURL,
		APIKey:   req.APIKey,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, dto.ChatResponse{Reply: err.Error()})
		return
	}

	c.JSON(http.StatusOK, dto.ChatResponse{Reply: reply})
}
