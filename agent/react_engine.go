package agent

import (
	"echo-core/remote"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
)

// Tool 工具定义
type Tool struct {
	Name        string                                              `json:"name"`
	Description string                                              `json:"description"`
	Parameters  map[string]interface{}                              `json:"parameters"`
	Handler     func(params map[string]interface{}) (string, error) `json:"-"`
}

// ReActEngine ReAct模式引擎
type ReActEngine struct {
	aiClient *remote.AIClient
	tools    map[string]*Tool
	maxSteps int
}

// NewReActEngine 创建ReAct引擎
func NewReActEngine(aiClient *remote.AIClient, tools []Tool) *ReActEngine {
	toolMap := make(map[string]*Tool)
	for i := range tools {
		t := tools[i]
		toolMap[t.Name] = &t
	}
	log.Printf("[ReActEngine] 引擎创建完成 | tools_count: %d", len(toolMap))
	return &ReActEngine{
		aiClient: aiClient,
		tools:    toolMap,
		maxSteps: 10,
	}
}

// AddTool 添加工具
func (e *ReActEngine) AddTool(tool Tool) {
	e.tools[tool.Name] = &tool
	log.Printf("[ReActEngine] 工具添加 | tool_name: %s", tool.Name)
}

// Execute 执行ReAct循环
func (e *ReActEngine) Execute(systemPrompt string, messages []remote.AIChatMessage) (string, error) {
	log.Printf("[ReActEngine] Execute开始 | messages_count: %d", len(messages))

	if len(messages) == 0 {
		log.Printf("[ReActEngine] 消息为空")
		return "", errors.New("messages cannot be empty")
	}

	// 确保最后一条是用户消息
	lastMsg := messages[len(messages)-1]
	if lastMsg.Role != "user" {
		log.Printf("[ReActEngine] 最后一条不是用户消息 | role: %s", lastMsg.Role)
		return "", errors.New("last message must be user role")
	}

	// 构建初始上下文
	log.Printf("[ReActEngine] 构建初始上下文")
	context := make([]remote.AIChatMessage, 0, len(messages)+10)
	context = append(context, remote.AIChatMessage{Role: "system", Content: systemPrompt})

	// 添加历史消息
	for i := 0; i < len(messages)-1; i++ {
		context = append(context, messages[i])
	}
	log.Printf("[ReActEngine] 上下文构建完成 | context_size: %d", len(context))

	// 收集工具定义
	var toolDefs []remote.AITool
	for _, t := range e.tools {
		toolDefs = append(toolDefs, remote.AITool{
			Type: "function",
			Function: remote.AIFunctionDef{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Parameters,
			},
		})
	}
	log.Printf("[ReActEngine] 工具定义收集完成 | tools_count: %d", len(toolDefs))

	// ReAct循环
	step := 0
	var finalReply string

	for step < e.maxSteps {
		step++
		log.Printf("[ReActEngine] ReAct循环 Step %d/%d", step, e.maxSteps)

		// 构建当前轮次的用户消息
		currentUserMsg := messages[len(messages)-1]
		if step > 1 {
			// 后续轮次，把新问题作为用户消息追加
			currentUserMsg = remote.AIChatMessage{
				Role:    "user",
				Content: fmt.Sprintf("请继续，上一轮你说了: %s", finalReply),
			}
			log.Printf("[ReActEngine] 后续轮次，构建继续消息")
		}

		// 添加上下文
		execMessages := make([]remote.AIChatMessage, len(context))
		copy(execMessages, context)
		execMessages = append(execMessages, currentUserMsg)

		log.Printf("[ReActEngine] 调用AI | execMessages: %d", len(execMessages))
		// 调用AI
		resp, err := e.aiClient.Chat(execMessages, toolDefs)
		if err != nil {
			log.Printf("[ReActEngine] AI调用失败 | error: %v", err)
			return "", fmt.Errorf("AI call failed: %w", err)
		}
		log.Printf("[ReActEngine] AI调用成功")

		if len(resp.Choices) == 0 {
			log.Printf("[ReActEngine] AI响应choices为空")
			return "", errors.New("no response choices")
		}

		choice := resp.Choices[0]
		assistantMsg := choice.Message

		// 记录助手消息
		context = append(context, assistantMsg)
		log.Printf("[ReActEngine] 助手消息已记录 | tool_calls_count: %d", len(assistantMsg.ToolCalls))

		// 检查是否有工具调用
		if len(assistantMsg.ToolCalls) > 0 {
			log.Printf("[ReActEngine] 检测到工具调用 | tool_calls_count: %d", len(assistantMsg.ToolCalls))
			// 执行工具调用
			for _, tc := range assistantMsg.ToolCalls {
				toolName := tc.Function.Name
				args := tc.Function.Arguments
				log.Printf("[ReActEngine] 执行工具调用 | tool_name: %s | args: %s", toolName, args)

				tool, exists := e.tools[toolName]
				if !exists {
					obs := fmt.Sprintf("Error: tool %s not found", toolName)
					log.Printf("[ReActEngine] 工具不存在 | tool_name: %s", toolName)
					context = append(context, remote.AIChatMessage{
						Role:      "tool",
						Content:   obs,
						ToolCalls: []remote.AIToolCall{{ID: tc.ID, Type: "function", Function: remote.AIFunction{Name: toolName}}},
					})
					continue
				}

				// 解析参数
				var params map[string]interface{}
				if err := json.Unmarshal([]byte(args), &params); err != nil {
					obs := fmt.Sprintf("Error: failed to parse arguments for %s: %v", toolName, err)
					log.Printf("[ReActEngine] 参数解析失败 | tool_name: %s | error: %v", toolName, err)
					context = append(context, remote.AIChatMessage{
						Role:      "tool",
						Content:   obs,
						ToolCalls: []remote.AIToolCall{{ID: tc.ID, Type: "function", Function: remote.AIFunction{Name: toolName}}},
					})
					continue
				}

				// 执行工具
				log.Printf("[ReActEngine] 执行工具 | tool_name: %s | params: %v", toolName, params)
				result, err := tool.Handler(params)
				if err != nil {
					log.Printf("[ReActEngine] 工具执行出错 | tool_name: %s | error: %v", toolName, err)
					result = fmt.Sprintf("Error executing %s: %v", toolName, err)
				}
				log.Printf("[ReActEngine] 工具执行完成 | tool_name: %s | result_len: %d", toolName, len(result))

				// 将结果注入上下文
				context = append(context, remote.AIChatMessage{
					Role:      "tool",
					Content:   result,
					ToolCalls: []remote.AIToolCall{{ID: tc.ID, Type: "function", Function: remote.AIFunction{Name: toolName}}},
				})
			}
			log.Printf("[ReActEngine] 工具调用循环完成，继续ReAct")
			continue
		}

		// 没有工具调用，返回最终回复
		if content, ok := assistantMsg.Content.(string); ok {
			log.Printf("[ReActEngine] 无工具调用，返回最终回复 | reply_len: %d", len(content))
			finalReply = content
			return finalReply, nil
		}

		// 检查finish_reason
		if choice.FinishReason == "stop" || choice.FinishReason == "length" {
			if content, ok := assistantMsg.Content.(string); ok {
				log.Printf("[ReActEngine] finish_reason: %s | reply_len: %d", choice.FinishReason, len(content))
				return content, nil
			}
		}
	}

	log.Printf("[ReActEngine] 达到最大循环次数 | max_steps: %d", e.maxSteps)
	return finalReply, errors.New("max steps reached")
}

// ExecuteWithHistory 基于历史消息执行
func (e *ReActEngine) ExecuteWithHistory(history []remote.AIChatMessage, currentInput string) (string, []remote.AIChatMessage, error) {
	log.Printf("[ReActEngine] ExecuteWithHistory | history_count: %d | input_len: %d", len(history), len(currentInput))

	// 构建带历史的完整消息列表
	messages := make([]remote.AIChatMessage, 0, len(history)+1)
	for _, h := range history {
		messages = append(messages, h)
	}
	messages = append(messages, remote.AIChatMessage{Role: "user", Content: currentInput})

	// 执行
	reply, err := e.Execute("", messages)
	if err != nil {
		log.Printf("[ReActEngine] ExecuteWithHistory Execute失败 | error: %v", err)
		return "", nil, err
	}

	// 更新历史
	history = append(history, remote.AIChatMessage{Role: "user", Content: currentInput})
	history = append(history, remote.AIChatMessage{Role: "assistant", Content: reply})

	log.Printf("[ReActEngine] ExecuteWithHistory完成 | reply_len: %d", len(reply))
	return reply, history, nil
}

// Agent Agent定义
type Agent struct {
	Name        string
	Description string
	Prompt      string
	Tools       []Tool
	Engine      *ReActEngine
}

// NewAgent 创建Agent
func NewAgent(name, description, prompt string, aiClient *remote.AIClient, tools []Tool) *Agent {
	log.Printf("[Agent] 创建Agent | name: %s | tools_count: %d", name, len(tools))
	return &Agent{
		Name:        name,
		Description: description,
		Prompt:      prompt,
		Tools:       tools,
		Engine:      NewReActEngine(aiClient, tools),
	}
}

// Run 运行Agent
func (a *Agent) Run(messages []remote.AIChatMessage) (string, error) {
	log.Printf("[Agent] Run | name: %s | messages_count: %d", a.Name, len(messages))
	reply, err := a.Engine.Execute(a.Prompt, messages)
	if err != nil {
		log.Printf("[Agent] Run失败 | name: %s | error: %v", a.Name, err)
		return "", err
	}
	log.Printf("[Agent] Run完成 | name: %s | reply_len: %d", a.Name, len(reply))
	return reply, nil
}

// MultiAgentOrchestrator 多Agent编排器
type MultiAgentOrchestrator struct {
	agents     map[string]*Agent
	orchPrompt string
}

// NewMultiAgentOrchestrator 创建编排器
func NewMultiAgentOrchestrator(orchPrompt string) *MultiAgentOrchestrator {
	log.Printf("[Orchestrator] 创建编排器 | prompt_len: %d", len(orchPrompt))
	return &MultiAgentOrchestrator{
		agents:     make(map[string]*Agent),
		orchPrompt: orchPrompt,
	}
}

// RegisterAgent 注册Agent
func (o *MultiAgentOrchestrator) RegisterAgent(agent *Agent) {
	o.agents[agent.Name] = agent
	log.Printf("[Orchestrator] Agent注册 | name: %s | total_agents: %d", agent.Name, len(o.agents))
}

// GetAgent 获取Agent
func (o *MultiAgentOrchestrator) GetAgent(name string) *Agent {
	return o.agents[name]
}

// ListAgents 列出所有Agent
func (o *MultiAgentOrchestrator) ListAgents() []string {
	var names []string
	for name := range o.agents {
		names = append(names, name)
	}
	return names
}

// Orchestrate 编排执行
func (o *MultiAgentOrchestrator) Orchestrate(userInput string, history []remote.AIChatMessage) (string, string, error) {
	log.Printf("[Orchestrator] Orchestrate开始 | user_input: %s | history_count: %d | available_agents: %d", userInput, len(history), len(o.agents))

	if len(o.agents) == 0 {
		log.Printf("[Orchestrator] 无可用Agent")
		return "", "", errors.New("no agents registered")
	}

	// 构建路由提示
	agentList := make([]string, 0, len(o.agents))
	for name, agent := range o.agents {
		agentList = append(agentList, fmt.Sprintf("- %s: %s", name, agent.Description))
	}
	routingPrompt := o.orchPrompt + "\n\n可用Agent:\n" + strings.Join(agentList, "\n") + "\n\n用户问题: " + userInput

	// 路由到Agent
	messages := make([]remote.AIChatMessage, 0, len(history)+1)
	messages = append(messages, history...)
	messages = append(messages, remote.AIChatMessage{Role: "user", Content: routingPrompt})

	// 调用主AI做路由（使用第一个Agent的client）
	var firstAgent *Agent
	for _, agent := range o.agents {
		firstAgent = agent
		break
	}

	// 使用路由LLM选择Agent
	log.Printf("[Orchestrator] 开始路由 | first_agent: %s", firstAgent.Name)
	choice, err := o.routeToAgent(userInput, history, firstAgent)
	if err != nil {
		log.Printf("[Orchestrator] 路由失败 | error: %v", err)
		return "", "", err
	}

	log.Printf("[Orchestrator] Orchestrate完成 | reply_len: %d", len(choice))
	return choice, "", nil
}

// routeToAgent 路由到具体Agent
func (o *MultiAgentOrchestrator) routeToAgent(userInput string, history []remote.AIChatMessage, defaultAgent *Agent) (string, error) {
	log.Printf("[Orchestrator] routeToAgent | input: %s | default_agent: %s", userInput, defaultAgent.Name)

	// 路由逻辑：根据关键词选择Agent
	input := strings.ToLower(userInput)
	searchKeywords := []string{"搜索", "查找", "查询", "信息", "知道", "了解", "介绍", "什么", "如何", "怎么", "为什么", "哪里", "多少", "search", "find", "query", "info"}

	for _, keyword := range searchKeywords {
		if strings.Contains(input, keyword) {
			if agent, ok := o.agents["search"]; ok {
				log.Printf("[Orchestrator] 匹配到search Agent | keyword: %s", keyword)
				// 构建包含当前用户输入的完整消息列表
				fullMessages := make([]remote.AIChatMessage, len(history)+1)
				copy(fullMessages, history)
				fullMessages[len(history)] = remote.AIChatMessage{Role: "user", Content: userInput}
				reply, err := agent.Run(fullMessages)
				return reply, err
			}
		}
	}

	// 默认使用第一个Agent
	log.Printf("[Orchestrator] 使用默认Agent | agent: %s", defaultAgent.Name)
	reply, err := defaultAgent.Run(history)
	if err != nil {
		log.Printf("[Orchestrator] Agent.Run失败 | error: %v", err)
		return "", err
	}

	log.Printf("[Orchestrator] routeToAgent完成 | reply_len: %d", len(reply))
	return reply, nil
}
