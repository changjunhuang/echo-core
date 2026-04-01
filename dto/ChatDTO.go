package dto

// ChatRequest is the request body for a chat message.
type ChatRequest struct {
	Message  string `json:"message"`
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
	BaseURL  string `json:"base_url,omitempty"`
	APIKey   string `json:"api_key,omitempty"`
}

// ChatResponse is the response body for a chat message.
type ChatResponse struct {
	Reply string `json:"reply"`
}
