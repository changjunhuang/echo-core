package handlers

import (
	"echo-core/service"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// ChatStreamHandler 流式聊天处理器
// 统一承载 SSE 与 WebSocket 两种流式通道
type ChatStreamHandler struct {
	svc *service.ChatService
}

// NewChatStreamHandler 创建流式聊天处理器
func NewChatStreamHandler(svc *service.ChatService) *ChatStreamHandler {
	return &ChatStreamHandler{svc: svc}
}

// ChatHandleSSE SSE流式聊天接口
// POST /api/chat
// Content-Type: text/event-stream
// 事件帧格式：
//
//	event: start   | data: {"sessionId":"..."}
//	event: delta   | data: {"delta":"片段文本","reply":"累计文本"}
//	event: finish  | data: {"reply":"完整回复","sessionId":"..."}
//	event: error   | data: {"error":"错误信息"}
func (h *ChatStreamHandler) ChatHandleSSE(c *gin.Context) {
	log.Printf("[ChatSSE] 收到SSE聊天请求 | IP: %s", c.ClientIP())

	var req service.ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("[ChatSSE] 请求参数解析失败: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.UserID == "" || req.SessionID == "" || req.Message == "" {
		log.Printf("[ChatSSE] 参数缺失 | userId: %s | sessionId: %s", req.UserID, req.SessionID)
		c.JSON(http.StatusBadRequest, gin.H{"error": "userId, sessionId and message are required"})
		return
	}

	// SSE 响应头
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Writer.WriteHeader(http.StatusOK)

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		log.Printf("[ChatSSE] 响应写入器不支持Flusher")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "streaming unsupported"})
		return
	}

	// start 帧
	h.writeSSEEvent(c, "start", map[string]string{"sessionId": req.SessionID})
	flusher.Flush()

	log.Printf("[ChatSSE] 开始流式输出 | userId: %s | sessionId: %s", req.UserID, req.SessionID)

	h.svc.ChatStream(req, func(chunk service.StreamChunk) {
		if chunk.Err != nil {
			log.Printf("[ChatSSE] 流式错误 | error: %v", chunk.Err)
			h.writeSSEEvent(c, "error", map[string]string{"error": chunk.Err.Error()})
			flusher.Flush()
			return
		}
		if !chunk.Done {
			h.writeSSEEvent(c, "delta", map[string]string{
				"delta": chunk.Delta,
				"reply": chunk.Reply,
			})
			flusher.Flush()
			return
		}
		// 结束帧
		h.writeSSEEvent(c, "finish", map[string]string{
			"reply":     chunk.Reply,
			"sessionId": req.SessionID,
		})
		flusher.Flush()
	})

	log.Printf("[ChatSSE] 流式输出完成 | userId: %s | sessionId: %s", req.UserID, req.SessionID)
}

// writeSSEEvent 写入一个 SSE 事件帧
func (h *ChatStreamHandler) writeSSEEvent(c *gin.Context, event string, payload interface{}) {
	data, err := json.Marshal(payload)
	if err != nil {
		log.Printf("[ChatSSE] 事件序列化失败 | event: %s | error: %v", event, err)
		return
	}
	if _, err := fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", event, data); err != nil {
		log.Printf("[ChatSSE] 事件写入失败 | event: %s | error: %v", event, err)
	}
}

// WebSocket 升级器
// CheckOrigin 始终放行：内网服务，部署时建议收敛
var wsUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 4096,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// WSIncomingMessage 客户端→服务端的 WebSocket 消息
type WSIncomingMessage struct {
	Type      string `json:"type"`                // 消息类型：chat / ping
	UserID    string `json:"userId,omitempty"`    // 用户ID
	SessionID string `json:"sessionId,omitempty"` // 会话ID
	Message   string `json:"message,omitempty"`   // 聊天内容
}

// WSOutgoingMessage 服务端→客户端的 WebSocket 消息
type WSOutgoingMessage struct {
	Type      string `json:"type"`                // start / delta / finish / error / pong
	Delta     string `json:"delta,omitempty"`     // 本次新增文本
	Reply     string `json:"reply,omitempty"`     // 累计回复
	SessionID string `json:"sessionId,omitempty"` // 会话ID
	Error     string `json:"error,omitempty"`     // 错误信息
	Timestamp int64  `json:"timestamp,omitempty"` // 服务器时间戳（毫秒）
}

// ChatHandleWS WebSocket 聊天接口
// GET /api/chat/ws
// 协议：
// 客户端发送 {"type":"chat","userId":"...","sessionId":"...","message":"..."}
// 服务端推送：start -> delta*N -> finish / error
// 心跳：客户端可发 {"type":"ping"}，服务端回 {"type":"pong"}
func (h *ChatStreamHandler) ChatHandleWS(c *gin.Context) {
	log.Printf("[ChatWS] 收到WebSocket升级请求 | IP: %s", c.ClientIP())

	conn, err := wsUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("[ChatWS] WebSocket升级失败: %v", err)
		return
	}
	defer conn.Close()
	log.Printf("[ChatWS] WebSocket连接建立 | remote: %s", conn.RemoteAddr())

	// 读取循环：阻塞读取客户端消息
	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			log.Printf("[ChatWS] 读取消息失败，连接关闭 | error: %v", err)
			return
		}

		var in WSIncomingMessage
		if err := json.Unmarshal(raw, &in); err != nil {
			log.Printf("[ChatWS] 消息解析失败 | error: %v", err)
			_ = conn.WriteJSON(WSOutgoingMessage{
				Type:      "error",
				Error:     "invalid message format: " + err.Error(),
				Timestamp: time.Now().UnixMilli(),
			})
			continue
		}

		switch in.Type {
		case "ping":
			_ = conn.WriteJSON(WSOutgoingMessage{Type: "pong", Timestamp: time.Now().UnixMilli()})
		case "chat":
			h.handleChatMessage(conn, in)
		default:
			_ = conn.WriteJSON(WSOutgoingMessage{
				Type:      "error",
				Error:     "unknown message type: " + in.Type,
				Timestamp: time.Now().UnixMilli(),
			})
		}
	}
}

// handleChatMessage 处理单条聊天消息
func (h *ChatStreamHandler) handleChatMessage(conn *websocket.Conn, in WSIncomingMessage) {
	log.Printf("[ChatWS] 收到聊天消息 | userId: %s | sessionId: %s | messageLen: %d", in.UserID, in.SessionID, len(in.Message))

	if in.UserID == "" || in.SessionID == "" || in.Message == "" {
		log.Printf("[ChatWS] 参数缺失 | userId: %s | sessionId: %s", in.UserID, in.SessionID)
		_ = conn.WriteJSON(WSOutgoingMessage{
			Type:      "error",
			Error:     "userId, sessionId and message are required",
			Timestamp: time.Now().UnixMilli(),
		})
		return
	}

	// start
	_ = conn.WriteJSON(WSOutgoingMessage{
		Type:      "start",
		SessionID: in.SessionID,
		Timestamp: time.Now().UnixMilli(),
	})

	req := service.ChatRequest{
		UserID:    in.UserID,
		SessionID: in.SessionID,
		Message:   in.Message,
	}
	h.svc.ChatStream(req, func(chunk service.StreamChunk) {
		if chunk.Err != nil {
			log.Printf("[ChatWS] 流式错误 | error: %v", chunk.Err)
			_ = conn.WriteJSON(WSOutgoingMessage{
				Type:      "error",
				Error:     chunk.Err.Error(),
				Timestamp: time.Now().UnixMilli(),
			})
			return
		}
		if chunk.Done {
			_ = conn.WriteJSON(WSOutgoingMessage{
				Type:      "finish",
				Reply:     chunk.Reply,
				SessionID: in.SessionID,
				Timestamp: time.Now().UnixMilli(),
			})
			return
		}
		_ = conn.WriteJSON(WSOutgoingMessage{
			Type:      "delta",
			Delta:     chunk.Delta,
			Reply:     chunk.Reply,
			SessionID: in.SessionID,
			Timestamp: time.Now().UnixMilli(),
		})
	})

	log.Printf("[ChatWS] 聊天消息处理完成 | userId: %s | sessionId: %s", in.UserID, in.SessionID)
}
