package routes

import (
	"echo-core/handlers"
	"echo-core/remote"
	"echo-core/service"
	"fmt"
	"os"

	"github.com/gin-gonic/gin"
)

// SetupRoutes 设置所有路由
func SetupRoutes(r *gin.Engine) error {
	api := r.Group("/api")
	departmentRegisterRoutes(api)
	if err := fileRegisterRoutes(api); err != nil {
		return err
	}
	if err := chatRegisterRoutes(api); err != nil {
		return err
	}
	userRegisterRoutes(api)
	return nil
}

// demo 注册部门相关的路由
func departmentRegisterRoutes(api *gin.RouterGroup) {
	departmentHandler := handlers.NewDepartmentHandler()
	{
		Departments := api.Group("/department")
		{
			Departments.GET("", departmentHandler.GetDepartmentList)       // 列表查询
			Departments.POST("", departmentHandler.CreateDepartment)       // 创建
			Departments.GET("/:id", departmentHandler.GetDepartment)       // 详情
			Departments.PUT("/:id", departmentHandler.UpdateDepartment)    // 更新
			Departments.DELETE("/:id", departmentHandler.DeleteDepartment) // 删除
		}
	}
}

// 文件相关的路由
func fileRegisterRoutes(api *gin.RouterGroup) error {
	fileHandler, err := handlers.NewFileHandler()
	if err != nil {
		return fmt.Errorf("create file handler: %w", err)
	}
	{
		file := api.Group("/file")
		{
			file.POST("/token", fileHandler.GetUploadTokenHandler)  // 获取上传token
			file.POST("/register", fileHandler.RegisterFileHandler) // 注册文件信息
		}
	}
	return nil
}

// 注册聊天相关的路由
func chatRegisterRoutes(api *gin.RouterGroup) error {
	// 创建AI客户端
	baseURL := os.Getenv("LLM_BASE_URL")
	apiKey := os.Getenv("LLM_API_KEY")
	model := os.Getenv("LLM_MODEL")

	aiClient := remote.NewAIClient(baseURL, apiKey, model)

	// 创建聊天服务
	chatSvc := service.NewChatService(aiClient)
	// 创建聊天处理器（传统 JSON 同步接口）
	chatHandler := handlers.NewChatHandler(chatSvc)
	// 创建流式聊天处理器（SSE + WebSocket）
	chatStreamHandler := handlers.NewChatStreamHandler(chatSvc)

	chat := api.Group("/chat")
	{
		// POST /api/chat         流式聊天（SSE，返回 text/event-stream）
		chat.POST("", chatStreamHandler.ChatHandleSSE)
		// GET  /api/chat/ws      WebSocket 聊天（全双工通道入口）
		chat.GET("/ws", chatStreamHandler.ChatHandleWS)
		// 其它辅助接口维持 JSON 同步形态
		chat.GET("/history", chatHandler.GetHistoryHandle)      // 获取历史
		chat.GET("/summary", chatHandler.GetSummaryHandle)      // 获取摘要
		chat.GET("/memory", chatHandler.GetUserMemoryHandle)    // 获取用户记忆
		chat.POST("/memory", chatHandler.SaveUserMemoryHandle)  // 保存用户记忆
		chat.GET("/agents", chatHandler.GetAgentsHandle)        // 获取Agent列表
		chat.DELETE("/session", chatHandler.ClearSessionHandle) // 清理会话
	}
	return nil
}

// 用户管理相关路由
func userRegisterRoutes(api *gin.RouterGroup) {
	userHandler := handlers.NewUserHandler()
	auth := api.Group("/auth")
	{
		auth.POST("/login", userHandler.Login)               // 登录
		auth.POST("/register", userHandler.Register)         // 注册
		auth.POST("/checkAccount", userHandler.CheckAccount) // 账号占用校验
		auth.POST("/check", userHandler.Check)               // 校验会话是否有效
		auth.POST("/logout", userHandler.Logout)             // 注销会话
	}
}
