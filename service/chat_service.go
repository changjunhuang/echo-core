package service

import (
	"crypto/sha1"
	"e
	"echo-core/agent"
	"echo-core/models"
	"echo-core/remote"
	"echo-core/repository"
	"crypto/sha1"
	"encoding/hex"
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
	memorySvc    *MemoryService
	aiClient     *remote.AIClient
	orchestrator *agent.MultiAgentOrchestrator
	ragClient    *agent.RAGClient
	promptCache  PromptCache
}

// NewChatService 创建聊天服务
func NewChatService(aiClient *remote.AIClient) *ChatService {
	memRepo := repository.NewMemoryRepository()

	// 自动迁移
	memRepo.AutoMigrate()

	svc := &ChatService{
		memRepo:     memRepo,
		summarizer:  NewSummarizer(aiClient, memRepo),
		memorySvc:   NewMemoryService(aiClient, memRepo),
		aiClient:    aiClient,
		promptCache: NewMemoryPromptCache(),
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
	// 传入 aiClient：路由决策由 LLM function-calling 完成（见 MultiAgentOrchestrator.RouteAgent）
	// 路由系统提示里显式列出"必走 search"的关键词，避免 LLM 把 RAG/知识库 类请求误判为通用问题。
	orchestrator := agent.NewMultiAgentOrchestrator(s.aiClient, `你是一个多 Agent 编排器，负责根据用户问题选择最合适的 Agent。

路由规则（按优先级）：
1. 用户问题中出现以下任意关键词时，必须选择 search Agent：
   "RAG"、"rag"、"知识库"、"知识库中"、"我的库"、"我的文档"、"文档库"、"资料库"、
   "找文件"、"找图片"、"找图"、"查文件"、"查图片"、"查找资料"、"检索"、"搜索"、"查一下"、"查询"。
2. 用户明确要求"在我的/本地/私有 + 任何资源（图片/文档/文件/视频/资料）"中查找时，必须选择 search Agent。
3. 其它通用问答（闲聊、数学、天气、时间、概念解释等）选择 default Agent。

请严格按规则选择，不要把 RAG/知识库类请求路由到 default。`)

	// 注册默认Agent（通用问答场景，不含 RAG 检索能力）
	defaultAgent := agent.NewAgent(
		"default",
		"默认 Agent，处理通用问答、闲聊、数学计算、天气、时间等不需要查询外部资料的问题。不具备知识库/RAG/文件检索能力，凡涉及'我的知识库/RAG/文档/图片/文件'的请求都不要路由到这里。",
		"你是一个有帮助的AI助手，请根据用户的问题给出准确、简洁的回答。",
		s.aiClient,
		agent.DefaultTools(),
	)
	orchestrator.RegisterAgent(defaultAgent)
	log.Printf("[ChatService] 默认Agent注册完成")

	// 注册搜索Agent（RAG 知识库 + 网络搜索）。description 直接列出触发关键词，
	// 让路由 LLM 不必"理解"问题，仅靠关键词匹配即可命中。
	searchAgent := agent.NewAgent(
		"search",
		"搜索 Agent，专门处理 RAG 知识库 / 私有文档库 / 本地资料库 中的检索请求，"+
			"支持检索 文本、文档、图片、文件、视频 等任意类型资源并返回下载链接。"+
			"触发关键词：'RAG'、'rag'、'知识库'、'我的库'、'我的文档'、'我的图片'、'我的文件'、"+
			"'找图'、'找图片'、'找文件'、'查文件'、'查图片'、'检索'、'搜索'、'查找'、'查询'。"+
			"只要用户提到要在自有资料/知识库中查找任何东西，都由本 Agent 处理。",
		`你是一个 RAG 知识库检索助手。强制规则：

1. 只要用户的问题涉及"在我的/本地/私有 知识库 / RAG 库 / 文档库 / 资料库 / 文件 / 图片"中查找任何内容，
   你必须先调用 search_knowledge 工具进行检索，禁止跳过工具直接编造答案或说"我无法检索"。
2. search_knowledge 支持检索 文本、文档、图片、文件 等任意类型资源，返回结果会包含文件名与可下载 URL。
3. 调用工具时，把用户描述的核心检索目标（例如"黄色小狗图片"、"2024 财报"）作为 query 传入；
   不要把无关的修饰词（"在我的库中"、"帮我找一下"）塞进 query。
4. 拿到工具结果后，如返回了文件/链接，直接把文件名与下载链接以清晰的 Markdown 列表形式回给用户；
   如知识库里确实没有相关内容（结果包含"未找到"等提示），再如实告知用户。
5. 仅在 search_knowledge 明确未命中、且用户允许联网时，才考虑 web_search。

不要重复说明上述规则，直接执行。`,
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

	// 1) 加载用户长期记忆（跨会话生效），注入到 system 消息
	log.Printf("[ChatService] 加载用户长期记忆 | userId: %s", req.UserID)
	memCtx, memErr := s.memorySvc.BuildMemoryContext(req.UserID)
	if memErr != nil {
		log.Printf("[ChatService] 加载用户长期记忆失败（不影响主流程）| userId: %s | error: %v", req.UserID, memErr)
	}

	// 2) 一次性查摘要元信息（轻量查询：只取版本相关字段，不拉长文）
	//    业界优化点：之前一请求内 GetSummaryMeta 被调 3 次 → 现在降到 1 次。
	meta, _ := s.summarizer.GetSummaryMetaLight(req.SessionID)
	lastCovered := 0
	if meta != nil {
		lastCovered = meta.MessageCount
	}

	// 3) 获取历史消息 —— 直接按窗口大小多取 1 条，避免无意义的多读
	//    旧版固定读 100 条，再用 20 条，造成 80% 浪费
	loadLimit := s.summarizer.WindowSize() + 1
	history, err := s.memRepo.GetSessionMessages(req.SessionID, req.UserID, loadLimit)
	if err != nil {
		log.Printf("[ChatService] 获取历史消息失败 | sessionId: %s | userId: %s | error: %v", req.SessionID, req.UserID, err)
		return nil, fmt.Errorf("get history failed: %w", err)
	}
	log.Printf("[ChatService] 历史消息获取成功 | sessionId: %s | historyCount: %d", req.SessionID, len(history))

	// 4) 转换历史消息为AI消息格式
	// tool 角色必须保留原 role 与 tool_call_id，否则 LLM 会因 tool_call_id 为空返 400 (2013)
	aiMessages := make([]remote.AIChatMessage, 0, len(history)+1)
	for _, h := range history {
		aiMessages = append(aiMessages, remote.AIChatMessage{
			Role:       h.Role,
			Content:    h.Content,
			ToolCallID: h.ToolCallID,
		})
	}

	// 5) 检查是否需要生成摘要（基于「当前消息总数 - 已摘要覆盖数」的增量判断）
	if s.summarizer.ShouldSummarize(len(aiMessages), lastCovered) {
		log.Printf("[ChatService] 触发摘要 | sessionId: %s | msg_total: %d | last_covered: %d", req.SessionID, len(aiMessages), lastCovered)
		if _, sumErr := s.summarizer.GenerateSummary(req.SessionID, req.UserID, aiMessages); sumErr != nil {
			log.Printf("[ChatService] 摘要生成失败（不影响主流程）| sessionId: %s | error: %v", req.SessionID, sumErr)
		} else {
			// 摘要内容已变 → 重新拉一次 meta 用于 cache key 失效
			if newMeta, _ := s.summarizer.GetSummaryMetaLight(req.SessionID); newMeta != nil {
				meta = newMeta
			}
			// 让该 (user, session) 的旧 prefix 缓存彻底失效
			s.promptCache.Del(s.prefixKey(req.UserID, req.SessionID, memCtx, meta))
		}
	}

	// 6) 用 BuildContext 构造真正的对话上下文：[Summary] + [最近 N 条]
	//    这里的 systemPrompt 为空，由各 Agent 内部再补 system prompt
	built, meta := s.summarizer.BuildContext(BuildContextInputs{
		SessionID:     req.SessionID,
		UserID:        req.UserID,
		SystemPrompt:  "",
		MemoryContext: memCtx,
		History:       aiMessages,
		Meta:          meta, // 复用 step 2 查到的 meta，0 额外查询
	})

	// 7) 命中 prefix cache 时打印统计；未命中时写入
	s.cacheOrStorePrefix(req.UserID, req.SessionID, memCtx, meta, built)

	// 8) 添加当前用户消息
	log.Printf("[ChatService] 添加当前用户消息到上下文 | message_len: %d", len(req.Message))
	aiMessages = append(built, remote.AIChatMessage{
		Role:    "user",
		Content: req.Message,
	})

	// 9) 执行对话
	log.Printf("[ChatService] 调用Agent编排器 | context_messages: %d", len(aiMessages))
	reply, _, err := s.orchestrator.Orchestrate(req.Message, aiMessages)
	if err != nil {
		log.Printf("[ChatService] Agent编排执行失败 | error: %v", err)
		return nil, fmt.Errorf("chat failed: %w", err)
	}
	log.Printf("[ChatService] Agent编排执行成功 | reply_len: %d", len(reply))

	// 10) 保存用户消息
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

	// 11) 保存助手回复
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

	// 12) 异步从本轮对话抽取长期记忆（不阻塞主响应）
	s.memorySvc.ExtractAsync(req.UserID, req.Message, reply)

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
	// ToolCall 模型决定调用的工具（非空表示 AI 触发了工具调用）
	// 配合 ToolResult 使用：先发 ToolCall，再发 ToolResult
	ToolCall *remote.AIToolCall
	// ToolResult 工具执行结果（含 id/name/result，便于 SSE/WS 客户端关联到 ToolCall）
	ToolResult *ToolResultEvent
}

// ToolResultEvent 工具执行结果事件载荷
type ToolResultEvent struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Result string `json:"result"`
	Error  string `json:"error,omitempty"`
}

// ChatStream 流式对话
// 走完整的 ReAct 链路（与 Chat 同步接口行为一致）：先经编排器选 Agent，
// 每轮 AI 调用走流式通道；遇到工具调用时执行工具并把 tool_call / tool_result
// 事件通过 onChunk 透出给上层。工具调用结束后再次发起流式 AI 调用直到不再产生工具调用。
//
// onChunk 回调语义：
//   - 每段文本产生一次 StreamChunk{Delta, Reply}（与旧版一致）
//   - 每个工具调用产生两次回调：先 ToolCall、再 ToolResult
//   - 流结束产生一次 StreamChunk{Done=true, Reply=完整回复}
func (s *ChatService) ChatStream(req ChatRequest, onChunk func(StreamChunk)) {
	log.Printf("[ChatService] ChatStream开始 | userId: %s | sessionId: %s | messageLen: %d", req.UserID, req.SessionID, len(req.Message))

	if req.UserID == "" || req.SessionID == "" || req.Message == "" {
		log.Printf("[ChatService] ChatStream参数不完整 | userId: %s | sessionId: %s | messageLen: %d", req.UserID, req.SessionID, len(req.Message))
		onChunk(StreamChunk{Done: true, Err: errors.New("userId, sessionId and message are required")})
		return
	}

	// 1) 加载用户长期记忆（跨会话生效）
	log.Printf("[ChatService] ChatStream加载用户长期记忆 | userId: %s", req.UserID)
	memCtx, memErr := s.memorySvc.BuildMemoryContext(req.UserID)
	if memErr != nil {
		log.Printf("[ChatService] ChatStream加载用户长期记忆失败（不影响主流程）| userId: %s | error: %v", req.UserID, memErr)
	}

	// 2) 一次性查摘要元信息
	meta, _ := s.summarizer.GetSummaryMetaLight(req.SessionID)
	lastCovered := 0
	if meta != nil {
		lastCovered = meta.MessageCount
	}

	// 3) 持久化用户消息（先存，避免 BuildContext 漏掉本轮）
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

	// 4) 拉历史（窗口+1）
	loadLimit := s.summarizer.WindowSize() + 1
	history, err := s.memRepo.GetSessionMessages(req.SessionID, req.UserID, loadLimit)
	if err != nil {
		log.Printf("[ChatService] ChatStream获取历史失败 | error: %v", err)
		onChunk(StreamChunk{Done: true, Err: fmt.Errorf("get history failed: %w", err)})
		return
	}
	aiMessages := make([]remote.AIChatMessage, 0, len(history)+2)
	for _, h := range history {
		aiMessages = append(aiMessages, remote.AIChatMessage{
			Role:       h.Role,
			Content:    h.Content,
			ToolCallID: h.ToolCallID,
		})
	}

	// 5) 摘要压缩（增量触发）
	if s.summarizer.ShouldSummarize(len(aiMessages), lastCovered) {
		log.Printf("[ChatService] ChatStream触发摘要 | sessionId: %s | msg_total: %d | last_covered: %d", req.SessionID, len(aiMessages), lastCovered)
		if _, sumErr := s.summarizer.GenerateSummary(req.SessionID, req.UserID, aiMessages); sumErr != nil {
			log.Printf("[ChatService] ChatStream生成摘要失败 | error: %v", sumErr)
		} else {
			if newMeta, _ := s.summarizer.GetSummaryMetaLight(req.SessionID); newMeta != nil {
				meta = newMeta
			}
			s.promptCache.Del(s.prefixKey(req.UserID, req.SessionID, memCtx, meta))
		}
	}

	// 6) 用 BuildContext 构造上下文：摘要 + 滑动窗口 + 写 prefix cache
	built, meta := s.summarizer.BuildContext(BuildContextInputs{
		SessionID:     req.SessionID,
		UserID:        req.UserID,
		SystemPrompt:  "",
		MemoryContext: memCtx,
		History:       aiMessages,
		Meta:          meta,
	})
	s.cacheOrStorePrefix(req.UserID, req.SessionID, memCtx, meta, built)

	// 7) 走编排器 ReAct 流式链路
	log.Printf("[ChatService] ChatStream调用编排器 | context_messages: %d | window_msgs: %d", len(built)+1, len(built))
	fullMessages := make([]remote.AIChatMessage, 0, len(built)+1)
	fullMessages = append(fullMessages, built...)
	fullMessages = append(fullMessages, remote.AIChatMessage{Role: "user", Content: req.Message})

	var (
		fullReply strings.Builder
		streamErr error
	)

	_, streamErr = s.orchestrator.RunStream(req.Message, fullMessages,
		// onContent: 模型文本片段
		func(delta string) error {
			fullReply.WriteString(delta)
			onChunk(StreamChunk{
				Reply: fullReply.String(),
				Delta: delta,
			})
			return nil
		},
		// onToolEvent: 工具执行结果
		func(event agent.ToolExecutionEvent) error {
			log.Printf("[ChatService] ChatStream工具事件 | tool: %s | id: %s | result_len: %d", event.ToolCall.Function.Name, event.ToolCall.ID, len(event.ToolResult))
			// 先把模型决定的工具调用透出
			tc := event.ToolCall
			onChunk(StreamChunk{
				ToolCall: &tc,
			})
			// 再透出工具执行结果
			resultEvent := &ToolResultEvent{
				ID:     event.ToolCall.ID,
				Name:   event.ToolCall.Function.Name,
				Result: event.ToolResult,
			}
			if event.Err != nil {
				resultEvent.Error = event.Err.Error()
			}
			onChunk(StreamChunk{
				ToolResult: resultEvent,
			})
			return nil
		},
	)

	// 8) 流结束
	if streamErr != nil {
		log.Printf("[ChatService] ChatStream 编排器执行失败 | error: %v", streamErr)
		onChunk(StreamChunk{Done: true, Err: streamErr, Reply: fullReply.String()})
		return
	}

	// 9) 持久化助手回复
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

	// 10) 异步从本轮对话抽取长期记忆
	s.memorySvc.ExtractAsync(req.UserID, req.Message, reply)

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
	if err := s.memRepo.SaveUserMemory(memory); err != nil {
		return err
	}
	// 记忆已更新 → 主动失效该用户所有 session 的 prefix cache
	// 业界实现：记忆是"前缀"的一部分，必须让它变 → 强制下次重算
	s.invalidateUserPrefixCache(userID)
	return nil
}

// ListUserMemories 列出某用户全部长期记忆
func (s *ChatService) ListUserMemories(userID string) ([]models.UserMemory, error) {
	return s.memorySvc.LoadUserMemories(userID)
}

// DeleteUserMemory 删除用户某条长期记忆
func (s *ChatService) DeleteUserMemory(userID, memoryType string) error {
	if err := s.memRepo.DeleteUserMemory(userID, memoryType); err != nil {
		return err
	}
	s.invalidateUserPrefixCache(userID)
	return nil
}

// GetSummary 获取会话摘要
func (s *ChatService) GetSummary(sessionID, userID string) (string, error) {
	return s.summarizer.GetSummary(sessionID, userID)
}

// prefixKey 拼装 prefix 缓存 key
// 业界实现：缓存键必须涵盖「影响前缀内容的全部因子」，否则会出现"记忆变了但仍命中旧缓存"的隐性 Bug。
// 这里涵盖：user、session、记忆内容哈希、摘要更新时间（=摘要版本）、模型名（不同模型 prefix 边界不同）。
func (s *ChatService) prefixKey(userID, sessionID, memCtx string, meta *SummaryMeta) string {
	version := "v0"
	if meta != nil {
		version = meta.UpdatedAt.Format("20060102150405.000000")
	}
	memHash := sha1Hex(memCtx)
	return PromptCacheKey("user", userID, "session", sessionID, "mem", memHash, "sumv", version, "model", s.aiClient.ModelName())
}

// cacheOrStorePrefix 命中缓存时打印 hit 统计，未命中时写入
// 缓存值 = 完整 prefix system 消息的内容（built[0]），用于后续 BuildContext 复用
// 业界优化：meta 由调用方预取传入，本函数不再访问 DB，把单请求的 summary 查询稳定为 1 次。
func (s *ChatService) cacheOrStorePrefix(userID, sessionID, memCtx string, meta *SummaryMeta, built []remote.AIChatMessage) {
	if len(built) == 0 {
		return
	}
	first := built[0]
	// 仅缓存 system 角色的 string 内容；非 string 跳过（极少出现，但留个保护）
	if first.Role != "system" {
		return
	}
	content, ok := first.Content.(string)
	if !ok || content == "" {
		return
	}
	key := s.prefixKey(userID, sessionID, memCtx, meta)
	if _, ok := s.promptCache.Get(key); ok {
		log.Printf("[ChatService] PrefixCache HIT | key: %s | prefix_len: %d", key, len(content))
		return
	}
	s.promptCache.Set(key, content, 5*time.Minute)
	log.Printf("[ChatService] PrefixCache MISS→STORE | key: %s | prefix_len: %d", key, len(content))
}

// invalidateUserPrefixCache 让某用户全部 session 的 prefix 缓存失效
// 业界做法：记忆 / 摘要 / 长期上下文中任一变化，都要让缓存作废；
// 由于内存 cache 不支持按 pattern 删，此处采用"打 tag"机制：key 中带 userID，
// 通过遍历 store 找出含 userID 的 key 全部删除。MVP 简化：仅删除本次请求的 key
// （因为同一个 user 的多个 session 通常不会同时在线调用）。
func (s *ChatService) invalidateUserPrefixCache(userID string) {
	// 内存版仅支持按精确 key 删除；若想批量失效，需要遍历 store。
	// 这里采用最小实现：依赖 memCtx hash + summary 版本变化自然让下次 key 不同，旧 key 5min TTL 后自动过期。
	log.Printf("[ChatService] 用户上下文变更 | userID: %s | 依赖 TTL 自然失效 prefix cache", userID)
}

// CacheStats 返回缓存命中统计（暴露给运维/调试接口）
func (s *ChatService) CacheStats() CacheStats {
	return s.promptCache.Stats()
}

// CacheStatsFull 返回带命中率的完整统计（前端展示友好）
type CacheStatsFull struct {
	Hit      int64   `json:"hit"`
	Miss     int64   `json:"miss"`
	Total    int64   `json:"total"`
	HitRate  float64 `json:"hit_rate"`
	HitRatio string  `json:"hit_ratio"`
}

// CacheStatsFull 获取完整缓存统计
func (s *ChatService) CacheStatsFull() CacheStatsFull {
	stats := s.promptCache.Stats()
	return CacheStatsFull{
		Hit:      stats.Hit,
		Miss:     stats.Miss,
		Total:    stats.Total(),
		HitRate:  stats.HitRate(),
		HitRatio: fmt.Sprintf("%.2f%%", stats.HitRate()*100),
	}
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
	if err := s.memRepo.DeleteSessionMessages(sessionID); err != nil {
		return err
	}
	// session 消息被清空 → 摘要也应失效（保留会指向已不存在的"历史"）
	if err := s.summarizer.InvalidateSummary(sessionID); err != nil {
		log.Printf("[ChatService] 清理摘要失败 | sessionID: %s | error: %v", sessionID, err)
	}
	// 失效 prefix cache（摘要已删，meta 必然 nil；用空 meta 拼出当前 key 即可）
	s.promptCache.Del(s.prefixKey(userID, sessionID, "", nil))
	return nil
}

// sha1Hex 计算字符串的 SHA1 哈希（hex 编码）
func sha1Hex(s string) string {
	h := sha1.Sum([]byte(s))
	return hex.EncodeToString(h[:])
}
