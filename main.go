package main

import (
	"log"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"

	"go-start/config"
	"go-start/routes"
)

func main() {
	// 初始化配置文件
	initConfig()
	// 初始化数据库
	config.InitDB()
	// 初始化 Weaviate
	if err := config.InitWeaviate(); err != nil {
		log.Fatalf("init weaviate failed: %v", err)
	}

	// 现在可以用 os.Getenv() 读取了
	port := os.Getenv("APP_PORT")
	if port == "" {
		port = "8080"
	}

	// 设置路由
	r := gin.Default()
	if err := routes.SetupRoutes(r); err != nil {
		log.Fatalf("setup routes failed: %v", err)
	}

	r.GET("/", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"message": "Hello from Gin!",
		})
	})

	// 启动
	log.Println("服务启动在 :" + port)
	if err := r.Run(":" + port); err != nil {
		log.Fatalf("server run failed: %v", err)
	}
}

func initConfig() {
	// 加载 .env 文件（必须在最开始加载）
	if err := godotenv.Load(); err != nil {
		log.Println("警告: 未找到 .env 文件，使用系统环境变量")
	}
}
