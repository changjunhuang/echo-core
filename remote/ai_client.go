package remote

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// AIRequest OpenAI兼容的请求结构
type AIRequest struct {
	Model       string          `json:"model"`
	Messages    []AIChatMessage `json:"messages"`
	Tools       []AITool        `json:"tools,omitempty"`
	ToolChoice  interface{}     `json:"tool_choice,omitempty"`
	Temperature float64         `json:"temperature,omitempty"`
	Stream      bool            `json:"stream,omitempty"`
}

// AIChatMessage 聊天消息
//   - assistant 角色：填 ToolCalls，描述模型决定调用哪些工具
//   - tool 角色：填 ToolCallID，引用 assistant 那条消息里某个 tool_call.id
//   - 其它角色：仅 Role + Content
type AIChatMessage struct {
	Role       string       `json:"role"`
	Content    interface{}  `json:"content,omitempty"`
	ToolCallID string       `json:"tool_call_id,omitempty"`
	ToolCalls  []AIToolCall `json:"tool_calls,omitempty"`
}

// AIToolCall 工具调用
type AIToolCall struct {
	ID       string     `json:"id"`
	Type     string     `json:"type"`
	Function AIFunction `json:"function"`
}

// AIFunction 函数定义
type AIFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// AITool 工具定义
type AITool struct {
	Type     string        `json:"type"`
	Function AIFunctionDef `json:"function"`
}

// AIFunctionDef 函数定义
type AIFunctionDef struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

// AIResponse AI响应结构
type AIResponse struct {
	ID      string   `json:"id"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

// Choice 选择
type Choice struct {
	Index        int           `json:"index"`
	Message      AIChatMessage `json:"message"`
	FinishReason string        `json:"finish_reason"`
}

// Usage 使用量
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// StreamChunk 流式响应块
type StreamChunk struct {
	Choices []StreamChoice `json:"choices"`
}

// StreamChoice 流式选择
type StreamChoice struct {
	Index        int         `json:"index"`
	Delta        StreamDelta `json:"delta"`
	FinishReason string      `json:"finish_reason,omitempty"`
}

// StreamDelta 流式增量
type StreamDelta struct {
	Role      string       `json:"role,omitempty"`
	Content   string       `json:"content,omitempty"`
	ToolCalls []AIToolCall `json:"tool_calls,omitempty"`
}

// StreamToolChunk 流式回调载荷（增强版，带工具调用感知）
// 一次回调 Content 与 ToolCalls 互斥（与上游 SSE 帧保持一致）：
//   - Content: 按到达顺序连续触发的文本片段
//   - ToolCalls: 仅在 Finish=true 且 finish_reason="tool_calls" 时一次性携带累积结果
//   - Finish:   true 表示本次流已结束（[DONE] 或 finish_reason 已到）
type StreamToolChunk struct {
	Content   string
	ToolCalls []AIToolCall
	Finish    bool
}

// AIClient AI客户端
type AIClient struct {
	baseURL string
	apiKey  string
	model   string
	client  *http.Client
	timeout time.Duration
}

// NewAIClient 创建AI客户端
func NewAIClient(baseURL, apiKey, model string) *AIClient {
	log.Printf("[AIClient] 创建AI客户端 | baseURL: %s | model: %s", baseURL, model)
	return &AIClient{
		baseURL: baseURL,
		apiKey:  apiKey,
		model:   model,
		client: &http.Client{
			Timeout: 120 * time.Second,
		},
		timeout: 120 * time.Second,
	}
}

// ModelName 暴露当前客户端使用的模型名（用于 prefix cache key 等场景）
func (c *AIClient) ModelName() string {
	return c.model
}

// Chat 实现聊天功能
func (c *AIClient) Chat(messages []AIChatMessage, tools []AITool) (*AIResponse, error) {
	return c.ChatWithToolChoice(messages, tools, nil)
}

// ChatWithToolChoice 实现聊天功能，并允许指定 tool_choice（"auto" / "required" / "none" / 强制指定 function）。
// 多数调用方应使用 Chat；当需要强制模型走某条工具路径时（如 Agent 路由器）使用本方法。
func (c *AIClient) ChatWithToolChoice(messages []AIChatMessage, tools []AITool, toolChoice interface{}) (*AIResponse, error) {
	log.Printf("[AIClient] Chat开始 | messages_count: %d | tools_count: %d | tool_choice: %v", len(messages), len(tools), toolChoice)

	if len(messages) == 0 {
		log.Printf("[AIClient] 消息为空")
		return nil, errors.New("messages cannot be empty")
	}

	// 确保最后一条消息是user角色
	lastMsg := messages[len(messages)-1]
	if lastMsg.Role != "user" {
		log.Printf("[AIClient] 最后一条不是user消息 | role: %s", lastMsg.Role)
		return nil, errors.New("last message must be from user")
	}

	req := AIRequest{
		Model:       c.model,
		Messages:    messages,
		Tools:       tools,
		ToolChoice:  toolChoice,
		Temperature: 0.7,
	}

	log.Printf("[AIClient] 序列化请求")
	jsonData, err := json.Marshal(req)
	if err != nil {
		log.Printf("[AIClient] 请求序列化失败 | error: %v", err)
		return nil, fmt.Errorf("marshal request failed: %w", err)
	}
	log.Printf("[AIClient] 请求序列化完成 | request_size: %d", len(jsonData))

	log.Printf("[AIClient] 发送HTTP请求 | url: %s/chat/completions", c.baseURL)
	httpReq, err := http.NewRequest("POST", c.baseURL+"/chat/completions", bytes.NewBuffer(jsonData))
	if err != nil {
		log.Printf("[AIClient] 创建请求失败 | error: %v", err)
		return nil, fmt.Errorf("create request failed: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	startTime := time.Now()
	resp, err := c.client.Do(httpReq)
	elapsed := time.Since(startTime)
	log.Printf("[AIClient] HTTP请求完成 | elapsed: %v", elapsed)

	if err != nil {
		log.Printf("[AIClient] 请求失败 | error: %v", err)
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	log.Printf("[AIClient] 响应状态 | status: %d", resp.StatusCode)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("[AIClient] AI服务返回错误状态 | status: %d | body: %s", resp.StatusCode, string(body))
		return nil, fmt.Errorf("AI service returned status %d: %s", resp.StatusCode, string(body))
	}

	log.Printf("[AIClient] 解析响应")
	var aiResp AIResponse
	if err := json.NewDecoder(resp.Body).Decode(&aiResp); err != nil {
		log.Printf("[AIClient] 响应解析失败 | error: %v", err)
		return nil, fmt.Errorf("decode response failed: %w", err)
	}

	log.Printf("[AIClient] Chat完成 | choices_count: %d | usage: %+v | elapsed: %v", len(aiResp.Choices), aiResp.Usage, elapsed)
	return &aiResp, nil
}

// ChatStream 流式聊天
// 按 OpenAI 兼容的 SSE 协议逐行解析 data: {...} 帧，提取 delta.content 后回调 handler
// handler 收到的是纯文本片段；返回错误时立即终止流
func (c *AIClient) ChatStream(messages []AIChatMessage, tools []AITool, handler func(string) error) error {
	log.Printf("[AIClient] ChatStream开始 | messages_count: %d | tools_count: %d", len(messages), len(tools))

	if len(messages) == 0 {
		log.Printf("[AIClient] 消息为空")
		return errors.New("messages cannot be empty")
	}

	req := AIRequest{
		Model:       c.model,
		Messages:    messages,
		Tools:       tools,
		Temperature: 0.7,
		Stream:      true,
	}

	jsonData, err := json.Marshal(req)
	if err != nil {
		log.Printf("[AIClient] 请求序列化失败 | error: %v", err)
		return fmt.Errorf("marshal request failed: %w", err)
	}

	httpReq, err := http.NewRequest("POST", c.baseURL+"/chat/completions", bytes.NewBuffer(jsonData))
	if err != nil {
		log.Printf("[AIClient] 创建请求失败 | error: %v", err)
		return fmt.Errorf("create request failed: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Accept", "text/event-stream")

	log.Printf("[AIClient] 发送流式请求")
	resp, err := c.client.Do(httpReq)
	if err != nil {
		log.Printf("[AIClient] 请求失败 | error: %v", err)
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("[AIClient] AI服务返回错误状态 | status: %d | body: %s", resp.StatusCode, string(body))
		return fmt.Errorf("AI service returned status %d: %s", resp.StatusCode, string(body))
	}

	log.Printf("[AIClient] 开始解析SSE流式响应")
	scanner := bufio.NewScanner(resp.Body)
	// 适当扩大缓冲，避免单行超长被截断
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	chunkCount := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		// 兼容注释行与结束标记
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			if payload == "[DONE]" {
				break
			}
			continue
		}

		var chunk StreamChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			log.Printf("[AIClient] SSE帧解析失败 | error: %v | payload: %s", err, payload)
			continue
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		delta := chunk.Choices[0].Delta.Content
		if delta == "" {
			continue
		}
		chunkCount++
		if err := handler(delta); err != nil {
			log.Printf("[AIClient] 流式处理回调失败 | error: %v", err)
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		log.Printf("[AIClient] 读取SSE流失败 | error: %v", err)
		return fmt.Errorf("read SSE stream failed: %w", err)
	}

	log.Printf("[AIClient] ChatStream完成 | chunks: %d", chunkCount)
	return nil
}

// ChatStreamWithTools 流式聊天，支持工具调用。
// 与 ChatStream 行为一致，但额外按 index 累积 delta.tool_calls，
// 在 finish_reason="tool_calls" 时通过 handler 一次性回吐累积结果，
// 其它场景下仅以 Finish=true 通知流结束。
func (c *AIClient) ChatStreamWithTools(messages []AIChatMessage, tools []AITool, handler func(StreamToolChunk) error) error {
	log.Printf("[AIClient] ChatStreamWithTools开始 | messages_count: %d | tools_count: %d", len(messages), len(tools))

	if len(messages) == 0 {
		log.Printf("[AIClient] 消息为空")
		return errors.New("messages cannot be empty")
	}

	req := AIRequest{
		Model:       c.model,
		Messages:    messages,
		Tools:       tools,
		Temperature: 0.7,
		Stream:      true,
	}

	jsonData, err := json.Marshal(req)
	if err != nil {
		log.Printf("[AIClient] 请求序列化失败 | error: %v", err)
		return fmt.Errorf("marshal request failed: %w", err)
	}

	httpReq, err := http.NewRequest("POST", c.baseURL+"/chat/completions", bytes.NewBuffer(jsonData))
	if err != nil {
		log.Printf("[AIClient] 创建请求失败 | error: %v", err)
		return fmt.Errorf("create request failed: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Accept", "text/event-stream")

	log.Printf("[AIClient] 发送流式请求")
	resp, err := c.client.Do(httpReq)
	if err != nil {
		log.Printf("[AIClient] 请求失败 | error: %v", err)
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("[AIClient] AI服务返回错误状态 | status: %d | body: %s", resp.StatusCode, string(body))
		return fmt.Errorf("AI service returned status %d: %s", resp.StatusCode, string(body))
	}

	log.Printf("[AIClient] 开始解析SSE流式响应")
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	// 按 index 累积 tool_calls
	toolAcc := make(map[int]*AIToolCall)
	var lastFinishReason string
	chunkCount := 0
	finishEmitted := false

	emitFinish := func() error {
		if finishEmitted {
			return nil
		}
		finishEmitted = true
		var toolCalls []AIToolCall
		if lastFinishReason == "tool_calls" && len(toolAcc) > 0 {
			toolCalls = make([]AIToolCall, 0, len(toolAcc))
			for i := 0; i < len(toolAcc); i++ {
				if tc, ok := toolAcc[i]; ok {
					toolCalls = append(toolCalls, *tc)
				}
			}
			log.Printf("[AIClient] ChatStreamWithTools 累积工具调用 | count: %d", len(toolCalls))
		}
		return handler(StreamToolChunk{
			ToolCalls: toolCalls,
			Finish:    true,
		})
	}

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" {
			continue
		}
		if payload == "[DONE]" {
			if err := emitFinish(); err != nil {
				log.Printf("[AIClient] 流式处理回调失败 | error: %v", err)
				return err
			}
			break
		}

		var chunk StreamChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			log.Printf("[AIClient] SSE帧解析失败 | error: %v | payload: %s", err, payload)
			continue
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		delta := chunk.Choices[0].Delta
		if chunk.Choices[0].FinishReason != "" {
			lastFinishReason = chunk.Choices[0].FinishReason
		}

		// 文本片段优先外抛
		if delta.Content != "" {
			chunkCount++
			if err := handler(StreamToolChunk{Content: delta.Content}); err != nil {
				log.Printf("[AIClient] 流式处理回调失败 | error: %v", err)
				return err
			}
		}

		// 累积 tool_calls（同一 id 跨多帧）
		// 上游 OpenAI 协议里：首次出现携带 id/type/name，后续帧只携带 arguments 片段
		// 兼容两种情况：
		//  1) 增量带 id 且与已有 id 相同 → 合并
		//  2) 增量 id 为空（arguments 增量）→ 合并到 map 末尾最近一次添加的 call
		//  3) 增量带新 id → 新开一个槽
		for _, tc := range delta.ToolCalls {
			placed := false
			if tc.ID != "" {
				for k, v := range toolAcc {
					if v.ID == tc.ID {
						mergeToolCall(v, tc)
						toolAcc[k] = v
						placed = true
						break
					}
				}
			} else {
				// 找最近一次添加的非空 call 合并
				for k := len(toolAcc) - 1; k >= 0; k-- {
					if toolAcc[k] != nil {
						mergeToolCall(toolAcc[k], tc)
						placed = true
						break
					}
				}
			}
			if !placed {
				idx := len(toolAcc)
				merged := tc // copy
				toolAcc[idx] = &merged
			}
		}

		// 出现 finish_reason 立即回吐（OpenAI 协议里 [DONE] 之后才会再有 finish_reason，
		// 但部分实现可能省略 [DONE]，需在这里兜底）
		if chunk.Choices[0].FinishReason != "" {
			if err := emitFinish(); err != nil {
				log.Printf("[AIClient] 流式处理回调失败 | error: %v", err)
				return err
			}
			// finish_reason 之后可能还有 [DONE]，循环继续由 [DONE] 兜底；
			// 若无 [DONE]，浏览器/客户端也会以收到 Finish 终止
		}
	}
	if err := scanner.Err(); err != nil {
		log.Printf("[AIClient] 读取SSE流失败 | error: %v", err)
		return fmt.Errorf("read SSE stream failed: %w", err)
	}

	log.Printf("[AIClient] ChatStreamWithTools完成 | chunks: %d | tool_call_count: %d", chunkCount, len(toolAcc))
	return nil
}

// mergeToolCall 合并同一 tool_call 的增量（function.arguments 跨帧累积）
func mergeToolCall(dst *AIToolCall, src AIToolCall) {
	if src.ID != "" {
		dst.ID = src.ID
	}
	if src.Type != "" {
		dst.Type = src.Type
	}
	if src.Function.Name != "" {
		dst.Function.Name = src.Function.Name
	}
	if src.Function.Arguments != "" {
		dst.Function.Arguments += src.Function.Arguments
	}
}

// GenerateSummary 生成摘要（支持增量：旧摘要 + 新消息 → 新摘要）
func (c *AIClient) GenerateSummary(prevSummary string, messages []AIChatMessage) (string, error) {
	log.Printf("[AIClient] GenerateSummary开始 | prev_summary_len: %d | messages_count: %d", len(prevSummary), len(messages))

	if len(messages) == 0 {
		log.Printf("[AIClient] 消息为空")
		return "", nil
	}

	// 构建系统提示：要求增量合并，输出不超 300 字
	systemMsg := AIChatMessage{
		Role: "system",
		Content: `你是对话摘要生成器，负责把【已有摘要】与【新增对话】合并成一份"累计摘要"。

【规则】
1. 若已有摘要为空：从零生成一份简洁摘要，覆盖所有关键信息。
2. 若已有摘要非空：在保留历史要点基础上，融入新增对话中的新事实/偏好/决策/结论，丢弃已过时的细节。
3. 摘要不超过 300 字，使用中文（除非对话为其它语种）。
4. 摘要以条目化短句为主，便于下游 LLM 快速消费。
5. 只返回摘要正文，不要任何前缀/解释/标题。`,
	}

	// 收集本次对话（包含 user / assistant / tool 角色，避免漏掉关键信息）
	var b strings.Builder
	if strings.TrimSpace(prevSummary) != "" {
		b.WriteString("【已有摘要】\n")
		b.WriteString(prevSummary)
		b.WriteString("\n\n【新增对话】\n")
	} else {
		b.WriteString("【对话内容】\n")
	}
	for _, msg := range messages {
		role := msg.Role
		if content, ok := msg.Content.(string); ok && content != "" {
			b.WriteString("[")
			b.WriteString(role)
			b.WriteString("] ")
			b.WriteString(content)
			b.WriteString("\n")
		}
	}
	log.Printf("[AIClient] 收集对话内容完成 | content_len: %d", b.Len())

	userMsg := AIChatMessage{
		Role:    "user",
		Content: b.String(),
	}

	log.Printf("[AIClient] 调用Chat生成摘要")
	summaryResp, err := c.Chat([]AIChatMessage{systemMsg, userMsg}, nil)
	if err != nil {
		log.Printf("[AIClient] 生成摘要失败 | error: %v", err)
		return "", err
	}

	if len(summaryResp.Choices) == 0 {
		log.Printf("[AIClient] AI无响应")
		return "", errors.New("no response from AI")
	}

	content, ok := summaryResp.Choices[0].Message.Content.(string)
	if !ok {
		log.Printf("[AIClient] 内容类型无效")
		return "", errors.New("invalid content type")
	}

	log.Printf("[AIClient] GenerateSummary完成 | summary_len: %d", len(content))
	return content, nil
}

// GetTextEmbedding 获取文本向量
func (c *AIClient) GetTextEmbedding(text string) ([]float32, error) {
	log.Printf("[AIClient] GetTextEmbedding | text_len: %d", len(text))

	reqBody := map[string]string{"text": text}
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		log.Printf("[AIClient] 请求序列化失败 | error: %v", err)
		return nil, err
	}

	req, err := http.NewRequest("POST", c.baseURL+"/embeddings", bytes.NewBuffer(jsonData))
	if err != nil {
		log.Printf("[AIClient] 创建请求失败 | error: %v", err)
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.client.Do(req)
	if err != nil {
		log.Printf("[AIClient] 请求失败 | error: %v", err)
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("[AIClient] embedding服务返回错误 | status: %d | body: %s", resp.StatusCode, string(body))
		return nil, fmt.Errorf("embedding service returned %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Printf("[AIClient] 解析响应失败 | error: %v", err)
		return nil, err
	}

	if len(result.Data) == 0 {
		log.Printf("[AIClient] 无embedding返回")
		return nil, errors.New("no embedding returned")
	}

	log.Printf("[AIClient] GetTextEmbedding完成 | embedding_dim: %d", len(result.Data[0].Embedding))
	return result.Data[0].Embedding, nil
}
