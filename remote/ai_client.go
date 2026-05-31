package remote

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
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
type AIChatMessage struct {
	Role      string       `json:"role"`
	Content   interface{}  `json:"content"`
	ToolCalls []AIToolCall `json:"tool_calls,omitempty"`
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

// Chat 实现聊天功能
func (c *AIClient) Chat(messages []AIChatMessage, tools []AITool) (*AIResponse, error) {
	log.Printf("[AIClient] Chat开始 | messages_count: %d | tools_count: %d", len(messages), len(tools))

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
func (c *AIClient) ChatStream(messages []AIChatMessage, tools []AITool, handler func(string) error) error {
	log.Printf("[AIClient] ChatStream开始 | messages_count: %d", len(messages))

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

	log.Printf("[AIClient] 开始读取流式响应")
	reader := resp.Body
	buf := make([]byte, 1024)
	totalRead := 0
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			totalRead += n
			chunk := string(buf[:n])
			// 简单处理SSE格式
			if len(chunk) > 6 {
				// 去掉 data: 前缀
				content := chunk
				if len(content) > 5 && content[:5] == "data:" {
					content = content[5:]
				}
				if content == "[DONE]" {
					break
				}
				content = trimSpace(content)
				if content != "" {
					if err := handler(content); err != nil {
						log.Printf("[AIClient] 流式处理回调失败 | error: %v", err)
					}
				}
			}
		}
		if err != nil {
			break
		}
	}

	log.Printf("[AIClient] ChatStream完成 | total_read: %d", totalRead)
	return nil
}

// trimSpace 去除首尾空白
func trimSpace(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\n' || s[0] == '\r' || s[0] == '\t') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\n' || s[len(s)-1] == '\r' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	return s
}

// GenerateSummary 生成摘要
func (c *AIClient) GenerateSummary(messages []AIChatMessage) (string, error) {
	log.Printf("[AIClient] GenerateSummary开始 | messages_count: %d", len(messages))

	if len(messages) == 0 {
		log.Printf("[AIClient] 消息为空")
		return "", nil
	}

	// 构建摘要提示
	systemMsg := AIChatMessage{
		Role:    "system",
		Content: "请总结以下对话的核心内容，返回一个简洁的摘要（不超过200字）。只返回摘要内容，不要其他解释。",
	}

	// 收集所有用户消息
	userContent := ""
	for _, msg := range messages {
		if msg.Role == "user" {
			if content, ok := msg.Content.(string); ok {
				userContent += content + "\n"
			}
		}
	}
	log.Printf("[AIClient] 收集用户消息完成 | user_content_len: %d", len(userContent))

	userMsg := AIChatMessage{
		Role:    "user",
		Content: userContent,
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
