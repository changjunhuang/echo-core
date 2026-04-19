package service

import (
	"context"
	"fmt"
	"go-start/config"
	"go-start/dto"
	"log"

	"github.com/sashabaranov/go-openai"
)

// AgentService 负责结合大模型完成自然语言的理解、意图识别与工具调用
type AgentService struct {
	sessionManager *SessionManager
	registry       *Registry
	vectorService  *VectorService // 添加对 vectorService 的引用
}

// NewAgentService 创建一个新的 AgentService，并利用依赖注入传递组件
func NewAgentService(weaviateService *WeaviateService, vectorService *VectorService) (*AgentService, error) {
	if _, err := config.LoadLLMConfig(); err != nil {
		return nil, err
	}
	sm := NewSessionManager()
	reg := NewRegistry()
	// 注册向量搜索工具
	reg.Register(NewWeaviateSearchTool(weaviateService, vectorService))
	return &AgentService{
		sessionManager: sm,
		registry:       reg,
		vectorService:  vectorService,
	}, nil
}

// Chat 处理用户的请求流，利用循环实现自动任务规划和多轮追问
func (s *AgentService) Chat(ctx context.Context, sessionID string, query string, options config.LLMRequestOptions) (string, error) {
	log.Printf("开始调用Chat方法，params={sessionID: %s, query: %s, options: %v}", sessionID, query, options)
	config, err := config.ResolveLLMConfig(options)
	if err != nil {
		return "", err
	}
	clientConfig := openai.DefaultConfig(config.APIKey)
	clientConfig.BaseURL = config.BaseURL
	client := openai.NewClientWithConfig(clientConfig)
	session := s.sessionManager.GetSession(sessionID)
	// 如果是新会话，注入 System Prompt (系统级角色设定，规范澄清行为)
	if len(session.Messages) == 0 {
		systemMsg := openai.ChatCompletionMessage{
			Role: openai.ChatMessageRoleSystem,
			Content: "你是一个高级智能企业助手。你有能力调用多种外部工具来帮助用户找图或查询数据。" +
				"\n你的核心运行逻辑如下：" +
				"\n1. 分析用户的请求是否清晰、检索条件是否充足。" +
				"\n2. 如果用户意图含糊，或者缺少调用工具必要的参数，请直接反问用户以澄清需求。(例如请求找部门照片，你要先询问特定部门名等)。" +
				"\n3. 当信息充分时，立刻使用可用工具(Tools)获取真实数据。" +
				"\n4. 基于工具返回的结果生成最终回答，必须告知用户对应信息的详细字段(例如文件名及文件ID等)。" +
				"绝不可凭空捏造数据或者图片信息。",
		}
		s.sessionManager.AddMessage(sessionID, systemMsg)
	}
	// 追加用户当前的信息
	userMsg := openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleUser,
		Content: query,
	}
	s.sessionManager.AddMessage(sessionID, userMsg)
	// maxLoops 限制最大工具链调用深度，防止死循环
	maxLoops := 5
	for i := 0; i < maxLoops; i++ {
		session = s.sessionManager.GetSession(sessionID) // 重新获取最新上下文
		req := openai.ChatCompletionRequest{
			Model:       config.Model,
			Messages:    session.Messages,
			Temperature: 0.2,
			Tools:       s.registry.GetOpenAITools(),
		}
		resp, err := client.CreateChatCompletion(ctx, req)
		if err != nil {
			log.Printf("LLM API 调用失败: %v", err)
			return "抱歉，我现在遇到点网络或服务问题，请稍后再试。", nil
		}
		if len(resp.Choices) == 0 {
			return "LLM 返回为空", nil
		}
		msg := resp.Choices[0].Message
		// 每次获取模型的输出后，均要录入会话中
		s.sessionManager.AddMessage(sessionID, msg)
		// 核心逻辑: 判断大模型是否决定调用工具
		if len(msg.ToolCalls) > 0 {
			for _, toolCall := range msg.ToolCalls {
				toolName := toolCall.Function.Name
				toolArgs := toolCall.Function.Arguments
				log.Printf("LLM 请求调用工具: [%s] 参数: [%s]\n", toolName, toolArgs)
				// 查找并执行对应的 Tool
				tool, ok := s.registry.GetTool(toolName)
				var toolResult string
				if !ok {
					toolResult = fmt.Sprintf("未找到名为 %s 的工具", toolName)
				} else {
					res, exeErr := tool.Execute(ctx, toolArgs)
					if exeErr != nil {
						toolResult = fmt.Sprintf("工具执行失败: %v", exeErr)
					} else {
						toolResult = res
					}
				}
				// 将工具执行结果作为 Tool Role 下发并反馈给 LLM
				toolMsg := openai.ChatCompletionMessage{
					Role:       openai.ChatMessageRoleTool,
					Content:    toolResult,
					Name:       toolName,
					ToolCallID: toolCall.ID,
				}
				s.sessionManager.AddMessage(sessionID, toolMsg)
			}
			// 循环继续，LLM 会携带 Tool 执行结果的上下文，思考下一步或者生成最终回答
			continue
		}
		// 如果没有 toolCalls，说明模型认为任务完成或提出了澄清性问题，将这一轮最终内容返回给用户
		result := msg.Content
		log.Printf("多轮对话已经获取结果! result: \n%s", result)
		return result, nil
	}

	log.Printf("由于执行步骤过多，已自动中断对话以保护系统资源。您可以尝试提出一个更确切的问题。")
	return "由于执行步骤过多，已自动中断对话以保护系统资源。您可以尝试提出一个更确切的问题。", nil
}

// ClearSession 会话清理接口
func (s *AgentService) ClearSession(sessionID string) {
	log.Printf("开始调用ClearSession方法，params={sessionID: %s}", sessionID)
	defer func() { log.Printf("调用ClearSession方法结束，result={}") }()
	s.sessionManager.ClearSession(sessionID)
}

// ChatWithHistory 接收完整的用户历史列表、当次最新对话。
// 内部包含了对历史对话的管理：过长则请求摘要，向量化历史存入库，拉取最相关历史。
func (s *AgentService) ChatWithHistory(ctx context.Context, sessionID string, query string, history []dto.ChatMessage, options config.LLMRequestOptions) (string, error) {
	log.Printf("开始调用ChatWithHistory, sessionID=%s, 历史对话长度=%d, 当前提问=%s", sessionID, len(history), query)

	systemMsgContent := "你是一个高级智能企业助手。你有能力调用多种外部工具来帮助用户找图或查询数据。"

	// 从Weaviate里搜索并补充历史上下文（不包括当前传进来的这轮）
	if s.vectorService != nil {
		relatedHistory, err := s.vectorService.SearchRelatedChatHistory(ctx, sessionID, query, 3)
		if err != nil {
			log.Printf("Failed to retrieve chat history from VectorService: %v", err)
		}
		if err == nil && len(relatedHistory) > 0 {
			systemMsgContent += "\n\n[检索出的相关历史对话片段]:\n"
			for i, fragment := range relatedHistory {
				systemMsgContent += fmt.Sprintf("%d. %s\n", i+1, fragment)
			}
		}
	}

	// 历史对话过长，触发摘要并异步存储当前已满载的数据，防止继续占用大空间
	if len(history) > 10 {
		log.Printf("历史记录多于10轮，触发异步摘要与向量化...")

		// 1. 发起异步摘要（提取所有核心点并存为记忆）
		if s.vectorService != nil {
			s.vectorService.AsyncGenerateSummary(ctx, sessionID, history, options)

			// 模拟将多轮内容保存一条 Segment（这里也可以每个单独保存，如果不需要就跳过等待下次摘要）
			var comboText string
			for _, h := range history {
				comboText += h.Role + ": " + h.Content + "\n"
			}
			// 2. 将当前的历史对话以分段的形式存入向量库（这里简单模拟成一条，实际可以设计更细粒度的分段策略）
			go s.vectorService.StoreChatSegment(context.Background(), sessionID, "【分段原话记忆】:"+comboText)
		}

		systemMsgContent += "\n\n[提示]: 该用户的历史会话过长已被自动折叠或交由向量记忆接管。可直接回答当前的提问或查阅上方向量片段。\n"
	}

	// 初始化或者重置 Session（因为 history 是客户端抛过来的，可以选择使用端上的上下文或者服务端的内存Session）
	s.sessionManager.ClearSession(sessionID)
	_ = s.sessionManager.GetSession(sessionID)

	// 系统 Prompt
	s.sessionManager.AddMessage(sessionID, openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleSystem,
		Content: systemMsgContent,
	})

	// 将提取出的最近几轮/相关几轮放到上下文中（这里简单把传入的history放入，作为基础回退）
	for _, h := range history {
		s.sessionManager.AddMessage(sessionID, openai.ChatCompletionMessage{
			Role:    h.Role,
			Content: h.Content,
		})
	}

	// 调用原生底层Chat接口（已去除了自带系统prompt等初始化的 Chat 方法体，也可直接写内置通信逻辑）
	return s.Chat(ctx, sessionID, query, options)
}

// 兼容原先 Query 方法以避免外部调用直接报错，实质转发给 Chat，SessionID 写死或基于配置均可
func (s *AgentService) Query(ctx context.Context, query string, options config.LLMRequestOptions) (string, error) {
	log.Printf("开始调用Query方法，params={query: %s, options: %v}", query, options)
	// 调用新形态的 Chat 逻辑，这里给出一个默认的 default_session 用于兼容
	return s.Chat(ctx, "default_session", query, options)
}
