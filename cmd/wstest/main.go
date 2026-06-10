package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"time"

	"github.com/gorilla/websocket"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8080", "echo-core 服务地址")
	userID := flag.String("user", "u_test_ws", "userId")
	sessionID := flag.String("session", "s_test_ws", "sessionId")
	message := flag.String("msg", "用一句话告诉我1+1等于几", "聊天内容")
	flag.Parse()

	u := url.URL{Scheme: "ws", Host: *addr, Path: "/api/chat/ws"}
	log.Printf("正在连接 %s", u.String())

	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		log.Fatalf("连接失败: %v", err)
	}
	defer conn.Close()
	log.Printf("连接已建立")

	// 1. 发送 ping 测试心跳
	if err := conn.WriteJSON(map[string]string{"type": "ping"}); err != nil {
		log.Fatalf("发送 ping 失败: %v", err)
	}
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		log.Fatalf("读取 pong 失败: %v", err)
	}
	log.Printf("[PING响应] %s", string(msg))

	// 2. 发送聊天消息
	payload := map[string]string{
		"type":      "chat",
		"userId":    *userID,
		"sessionId": *sessionID,
		"message":   *message,
	}
	if err := conn.WriteJSON(payload); err != nil {
		log.Fatalf("发送聊天消息失败: %v", err)
	}
	log.Printf("聊天消息已发送，等待流式响应...")

	// 3. 读取流式响应
	deadline := time.Now().Add(60 * time.Second)
	conn.SetReadDeadline(deadline)
	deltaCount := 0
	var lastReply string
	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			log.Fatalf("读取消息失败: %v", err)
		}
		var out map[string]interface{}
		if err := json.Unmarshal(raw, &out); err != nil {
			log.Printf("消息解析失败: %v | raw=%s", err, string(raw))
			continue
		}
		switch out["type"] {
		case "start":
			log.Printf("[START] sessionId=%v", out["sessionId"])
		case "delta":
			deltaCount++
			if reply, ok := out["reply"].(string); ok {
				lastReply = reply
			}
			delta, _ := out["delta"].(string)
			fmt.Printf("[DELTA #%d] %q\n", deltaCount, delta)
		case "finish":
			reply, _ := out["reply"].(string)
			log.Printf("[FINISH] 总delta=%d | reply_len=%d", deltaCount, len(reply))
			log.Printf("[REPLY] %s", reply)
			if reply != lastReply {
				log.Printf("[WARN] finish.reply 与累计 reply 不一致")
			}
			log.Printf("测试成功 ✅")
			os.Exit(0)
		case "error":
			log.Printf("[ERROR] %v", out["error"])
			os.Exit(1)
		default:
			log.Printf("[UNKNOWN] %s", string(raw))
		}
	}
}
