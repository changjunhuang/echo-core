package main

import (
	"context"
	"go-start/config"
	"go-start/dto"
	"go-start/service"
	"log"
	"os"
	"time"

	"github.com/joho/godotenv"
)

func main() {
	// 加载环境变量
	_ = godotenv.Load(".env")

	log.Println("=== Starting VectorService Test ===")

	// 初始化 VectorService
	os.Setenv("WEAVIATE_HOST", "121.43.145.179:8080")
	os.Setenv("WEAVIATE_SCHEME", "http")

	vs := service.NewVectorService()

	sessionID := "test_session_123"

	// 1. 构建一组对话记录
	history := []dto.ChatMessage{
		{Role: "user", Content: "你好，请问你是谁？"},
		{Role: "assistant", Content: "我是企业智能助手，有什么可以帮你的吗？"},
		{Role: "user", Content: "我想查一下公司财务部门的照片和最近的财务报表。"},
		{Role: "assistant", Content: "好的，请问你有具体的照片名称或报表日期吗？"},
		{Role: "user", Content: "只要是今年的或者名字里带财务的就行。"},
	}

	log.Println("1. 测试直接存储对话片段 VectorStore...")
	err := vs.StoreChatSegment(context.Background(), sessionID, "【历史片段】：用户想要找今年财务部门的照片和财务报表。")
	if err != nil {
		log.Printf("Store directly failed: %v", err)
	}

	log.Println("1.5 测试异步生成摘要功能...")
	vs.AsyncGenerateSummary(context.Background(), sessionID, history, config.LLMRequestOptions{})

	// 等待一小会儿确保异步任务完成
	log.Println("Waiting for async API (up to 15s)...")
	time.Sleep(15 * time.Second)

	log.Println("2. 测试检索历史对话功能...")
	results, err := vs.SearchRelatedChatHistory(context.Background(), sessionID, "财务有哪些记录", 2)
	if err != nil {
		log.Fatalf("Search failed: %v", err)
	}

	log.Printf("Found %d fragments in vector space for session %s.", len(results), sessionID)
	for i, r := range results {
		log.Printf("Fragment %d: %s", i+1, r)
	}

	log.Println("=== VectorService Test Completed ===")
}
