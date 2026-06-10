package service

import (
	"echo-core/agent"
	"echo-core/models"
	"echo-core/remote"
	"echo-core/repository"
	"echo-core/utils"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"
)

// ChatService 聊天服务
type ChatService struct {
	memRepo      *repository.MemoryRepository
	summarizer   *Summarizer
	aiClient     *remote.AIClient
	orchestrator *agent.MultiAgentOrchestrator
	ragClient    *agent.RAGClient
}

// NewChatService 创建聊天服务
func NewChatService(aiClient *remote.AIClient) *ChatService {
	memRepo := repository.NewMemoryRepository()

	// 自动迁移
	memRepo.AutoMigrate()

	svc := &ChatService{
		memRepo:    memRepo,
		summarizer: NewSummarizer(aiClient, memRepo),
		aiClient:   aiClient,
	}

	// 初始化RAG客户端
	ragBaseURL := "http://localhost:8000"
	ragDomain := utils.GetEnv("QINIU_DOMAIN", "tfpdkiq9g.hn-bkt.clouddn.com")
	svc.ragClient = agent.NewRAGClientWithDomain(ragBaseURL, "", ragDomain)

	// 初始化多Agent编排器
	svc.initOrchestrator()

	log.Printf("[ChatService] 服务初始化完成")
	return svc
}

// initOrchestrator 初始化编排器
func (s *ChatService) initOrchestrator() {
	log.Printf("[ChatService] 初始化Agent编排器")
	orchestrator := agent.NewMultiAgentOrchestrator("你是一个多Agent编排器，根据用户问题选择合适的Agent处理。")

	// 注册默认Agent（通用问题）
	defaultAgent := agent.NewAgent(
		"default",
		"默认Agent，处理通用问题",
		"你是一个有帮助的AI助手，请根据用户的问题给出准确、简洁的回答。",
		s.aiClient,
		agent.DefaultTools(),
	)
	orchestrator.RegisterAgent(defaultAgent)
	log.Printf("[ChatService] 默认Agent注册完成")

	// 注册搜索Agent（信息检索场景：先RAG知识库搜索，再网络搜索）
	searchAgent := agent.NewAgent(
		"search",
		"搜索Agent，处理信息检索类问题",
		"你是一个信息检索助手。当用户询问需要搜索信息的问题时，首先使用search_knowledge工具从RAG知识库搜索，如果知识库中没有相关信息，会提示你使用web_search进行网络搜索。请根据搜索结果给出准确、简洁的回答。",
		s.aiClient,
		agent.SearchTools(s.ragClient),
	)
	orchestrator.RegisterAgent(searchAgent)
	log.Printf("[ChatService] 搜索Agent注册完成")

	s.orchestrator = orchestrator
}

// ChatRequest 聊天请求
type ChatRequest struct {
	UserID    string `json:"userId"`
	SessionID string `json:"sessionId"`
	Message   string `json:"message"`
	Provider  string `json:"provider,omitempty"`
	Model     string `json:"model,omitempty"`
	BaseURL   string `json:"baseUrl,omitempty"`
	APIKey    string `json:"apiKey,omitempty"`
}

// ChatResponse 聊天响应
type ChatResponse struct {
	Reply     string `json:"reply"`
	SessionID string `json:"session_id"`
	Summary   string `json:"summary,omitempty"`
}

// Chat 核心聊天功能
func (s *ChatService) Chat(req ChatRequest) (*ChatResponse, error) {
	log.Printf("[ChatService] 开始处理聊天请求 | userId: %s | sessionId: %s | message: %s", req.UserID, req.SessionID, req.Message)

	if req.UserID == "" {
		log.Printf("[ChatService] userId 为空")
		return nil, errors.New("userId is required")
	}
	if req.SessionID == "" {
		log.Printf("[ChatService] sessionId 为空")
		return nil, errors.New("sessionId is required")
	}
	if req.Message == "" {
		log.Printf("[ChatService] message为空")
		return nil, errors.New("message is required")
	}

	// 获取历史消息
	log.Printf("[ChatService] 正在获取历史消息 | sessionId: %s | userId: %s", req.SessionID, req.UserID)
	history, err := s.memRepo.GetSessionMessages(req.SessionID, req.UserID, 100)
	if err != nil {
		log.Printf("[ChatService] 获取历史消息失败 | sessionId: %s | userId: %s | error: %v", req.SessionID, req.UserID, err)
		return nil, fmt.Errorf("get history failed: %w", err)
	}
	log.Printf("[ChatService] 历史消息获取成功 | sessionId: %s | historyCount: %d", req.SessionID, len(history))

	// 转换历史消息为AI消息格式
	aiMessages := make([]remote.AIChatMessage, 0, len(history))
	for _, h := range history {
		role := h.Role
		if role == "tool" {
			role = "assistant" // 工具返回作为assistant处理
		}
		aiMessages = append(aiMessages, remote.AIChatMessage{
			Role:    role,
			Content: h.Content,
		})
	}

	// 检查是否需要生成摘要
	if s.summarizer.ShouldSummarize(len(aiMessages)) {
		log.Printf("[ChatService] 消息数超过阈值，开始生成摘要 | message_count: %d", len(aiMessages))
		summary, err := s.summarizer.GenerateSummary(req.SessionID, req.UserID, aiMessages)
		if err != nil {
			log.Printf("[ChatService] 生成摘要失败 | session_id: %s | error: %v", req.SessionID, err)
			// 摘要失败不影响主流程
		} else {
			log.Printf("[ChatService] 摘要生成成功 | session_id: %s | summary_len: %d", req.SessionID, len(summary))
		}
	}

	// 添加当前用户消息
	log.Printf("[ChatService] 添加当前用户消息到上下文 | message_len: %d", len(req.Message))
	aiMessages = append(aiMessages, remote.AIChatMessage{
		Role:    "user",
		Content: req.Message,
	})

	// 执行对话
	log.Printf("[ChatService] 调用Agent编排器 | context_messages: %d", len(aiMessages))
	reply, _, err := s.orchestrator.Orchestrate(req.Message, aiMessages)
	if err != nil {
		log.Printf("[ChatService] Agent编排执行失败 | error: %v", err)
		return nil, fmt.Errorf("chat failed: %w", err)
	}
	log.Printf("[ChatService] Agent编排执行成功 | reply_len: %d", len(reply))

	// 保存用户消息
	log.Printf("[ChatService] 保存用户消息到数据库 | session_id: %s | user_id: %s", req.SessionID, req.UserID)
	userMsg := &models.SessionMessage{
		SessionID: req.SessionID,
		UserID:    req.UserID,
		Role:      "user",
		Content:   req.Message,
		CreatedAt: time.Now(),
	}
	if err := s.memRepo.SaveSessionMessage(userMsg); err != nil {
		log.Printf("[ChatService] 保存用户消息失败: %v", err)
		// 不影响主流程
	}

	// 保存助手回复
	log.Printf("[ChatService] 保存助手回复到数据库 | session_id: %s", req.SessionID)
	assistantMsg := &models.SessionMessage{
		SessionID: req.SessionID,
		UserID:    req.UserID,
		Role:      "assistant",
		Content:   reply,
		CreatedAt: time.Now(),
	}
	if err := s.memRepo.SaveSessionMessage(assistantMsg); err != nil {
		log.Printf("[ChatService] 保存助手回复失败: %v", err)
		// 不影响主流程
	}

	log.Printf("[ChatService] 聊天处理完成 | user_id: %s | session_id: %s | reply: %s", req.UserID, req.SessionID, reply)
	return &ChatResponse{
		Reply:     reply,
		SessionID: req.SessionID,
	}, nil
}

// GetHistory 获取会话历史
func (s *ChatService) GetHistory(sessionID, userID string, limit int) ([]models.SessionMessage, error) {
	if limit <= 0 {
		limit = 50
	}
	return s.memRepo.GetSessionMessages(sessionID, userID, limit)
}

// StreamChunk 流式回调载荷（统一供 SSE / WebSocket 复用）
type StreamChunk struct {
	// Reply 当前累计的完整回复文本
	Reply string
	// Delta 本次回调新增的文本片段
	Delta string
	// Done 是否为最后一块（流结束）
	Done bool
	// Err 流过程中出现的错误（非空时 Done=true）
	Err error
}

// ChatStream 流式对话
// 直接调用 AI 客户端的 ChatStream，不经过 ReAct/工具调用链路，
// 以保证 SSE / WebSocket 端到端的低延迟体验。
// onChunk 回调会被逐段调用，Done=true 时表示本次流结束。
func (s *ChatService) ChatStream(req ChatRequest, onChunk func(StreamChunk)) {
	log.Printf("[ChatService] ChatStream开始 | userId: %s | sessionId: %s | messageLen: %d", req.UserID, req.SessionID, len(req.Message))

	if req.UserID == "" || req.SessionID == "" || req.Message == "" {
		log.Printf("[ChatService] ChatStream参数不完整 | userId: %s | sessionId: %s | messageLen: %d", req.UserID, req.SessionID, len(req.Message))
		onChunk(StreamChunk{Done: true, Err: errors.New("userId, sessionId and message are required")})
		return
	}

	// 1. 加载历史上下文
	history, err := s.memRepo.GetSessionMessages(req.SessionID, req.UserID, 100)
	if err != nil {
		log.Printf("[ChatService] ChatStream获取历史失败 | error: %v", err)
		onChunk(StreamChunk{Done: true, Err: fmt.Errorf("get history failed: %w", err)})
		return
	}
	aiMessages := make([]remote.AIChatMessage, 0, len(history)+1)
	for _, h := range history {
		role := h.Role
		if role == "tool" {
			role = "assistant"
		}
		aiMessages = append(aiMessages, remote.AIChatMessage{
			Role:    role,
			Content: h.Content,
		})
	}

	// 2. 摘要压缩（与 Chat 行为一致）
	if s.summarizer.ShouldSummarize(len(aiMessages)) {
		log.Printf("[ChatService] ChatStream触发摘要 | message_count: %d", len(aiMessages))
		if summary, sumErr := s.summarizer.GenerateSummary(req.SessionID, req.UserID, aiMessages); sumErr != nil {
			log.Printf("[ChatService] ChatStream生成摘要失败 | error: %v", sumErr)
		} else {
			log.Printf("[ChatService] ChatStream摘要生成成功 | summary_len: %d", len(summary))
		}
	}

	// 3. 追加当前用户消息
	aiMessages = append(aiMessages, remote.AIChatMessage{
		Role:    "user",
		Content: req.Message,
	})

	// 4. 持久化用户消息
	userMsg := &models.SessionMessage{
		SessionID: req.SessionID,
		UserID:    req.UserID,
		Role:      "user",
		Content:   req.Message,
		CreatedAt: time.Now(),
	}
	if saveErr := s.memRepo.SaveSessionMessage(userMsg); saveErr != nil {
		log.Printf("[ChatService] ChatStream保存用户消息失败: %v", saveErr)
	}

	// 5. 流式调用AI
	log.Printf("[ChatService] ChatStream调用AI | context_messages: %d", len(aiMessages))
	var (
		fullReply strings.Builder
	)
	streamErr := s.aiClient.ChatStream(aiMessages, nil, func(delta string) error {
		fullReply.WriteString(delta)
		onChunk(StreamChunk{
			Reply: fullReply.String(),
			Delta: delta,
		})
		return nil
	})

	// 6. 流结束
	if streamErr != nil {
		log.Printf("[ChatService] ChatStream AI调用失败 | error: %v", streamErr)
		onChunk(StreamChunk{Done: true, Err: streamErr, Reply: fullReply.String()})
		return
	}

	// 7. 持久化助手回复
	reply := fullReply.String()
	assistantMsg := &models.SessionMessage{
		SessionID: req.SessionID,
		UserID:    req.UserID,
		Role:      "assistant",
		Content:   reply,
		CreatedAt: time.Now(),
	}
	if saveErr := s.memRepo.SaveSessionMessage(assistantMsg); saveErr != nil {
		log.Printf("[ChatService] ChatStream保存助手回复失败: %v", saveErr)
	}

	log.Printf("[ChatService] ChatStream完成 | userId: %s | sessionId: %s | reply_len: %d", req.UserID, req.SessionID, len(reply))
	onChunk(StreamChunk{Done: true, Reply: reply})
}

// GetUserMemory 获取用户记忆
func (s *ChatService) GetUserMemory(userID, memoryType string) (*models.UserMemory, error) {
	return s.memRepo.GetUserMemory(userID, memoryType)
}

// SaveUserMemory 保存用户记忆
func (s *ChatService) SaveUserMemory(userID, memoryType, content string) error {
	memory := &models.UserMemory{
		UserID:     userID,
		MemoryType: memoryType,
		Content:    content,
	}
	return s.memRepo.SaveUserMemory(memory)
}

// GetSummary 获取会话摘要
func (s *ChatService) GetSummary(sessionID, userID string) (string, error) {
	return s.summarizer.GetSummary(sessionID, userID)
}

// RegisterAgent 注册自定义Agent
func (s *ChatService) RegisterAgent(name, description, prompt string, tools []agent.Tool) error {
	if s.orchestrator == nil {
		return errors.New("orchestrator not initialized")
	}
	agentInstance := agent.NewAgent(name, description, prompt, s.aiClient, tools)
	s.orchestrator.RegisterAgent(agentInstance)
	return nil
}

// GetAgents 获取所有Agent
func (s *ChatService) GetAgents() []string {
	if s.orchestrator == nil {
		return nil
	}
	return s.orchestrator.ListAgents()
}

// SaveMessageWithTools 保存带工具调用的消息
func (s *ChatService) SaveMessageWithTools(sessionID, userID, role, content, toolCalls, toolResult string) error {
	msg := &models.SessionMessage{
		SessionID:  sessionID,
		UserID:     userID,
		Role:       role,
		Content:    content,
		ToolCalls:  toolCalls,
		ToolResult: toolResult,
		CreatedAt:  time.Now(),
	}

	toolCallsJSON, _ := json.Marshal(toolCalls)
	toolResultJSON, _ := json.Marshal(toolResult)
	msg.ToolCalls = string(toolCallsJSON)
	msg.ToolResult = string(toolResultJSON)

	return s.memRepo.SaveSessionMessage(msg)
}

// ClearSession 清理会话（保留摘要）
func (s *ChatService) ClearSession(sessionID, userID string) error {
	return s.memRepo.DeleteSessionMessages(sessionID)
}
