package dto

// ChatMessage represents a single historical chat message.
type ChatMessage struct {
	Role    string `json:"role"`    // "user" or "assistant"
	Content string `json:"content"`
}

// ChatRequest is the request body for a chat message.
type ChatRequest struct {
	SessionID string        `json:"session_id,omitempty"` // 用于关联多轮对话和记忆
	Message   string        `json:"message"`
	History   []ChatMessage `json:"history,omitempty"`    // 接收的多轮历史对话结果
	Provider  string        `json:"provider,omitempty"`
	Model     string        `json:"model,omitempty"`
	BaseURL   string        `json:"base_url,omitempty"`
	APIKey    string        `json:"api_key,omitempty"`
}

// ChatResponse is the response body for a chat message.
type ChatResponse struct {
	Reply string `json:"reply"`
}
