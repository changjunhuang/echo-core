package config

import (
	"echo-core/models"
	"echo-core/utils"
	"fmt"
	"gorm.io/gorm/schema"
	"log"
	"os"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var DB *gorm.DB

// DatabaseConfig 数据库配置结构
type DatabaseConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	DBName   string
	Charset  string
}

// InitDB 初始化数据库连接
func InitDB() {
	config := DatabaseConfig{
		Host:     utils.GetEnv("DB_HOST", "localhost"),
		Port:     utils.GetEnv("DB_PORT", "3306"),
		User:     utils.GetEnv("DB_USER", "root"),
		Password: utils.GetEnv("DB_PASSWORD", ""),
		DBName:   utils.GetEnv("DB_NAME", "testdb"),
		Charset:  "utf8mb4",
	}

	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=%s&parseTime=True&loc=Local",
		config.User,
		config.Password,
		config.Host,
		config.Port,
		config.DBName,
		config.Charset,
	)

	// 配置 GORM
	gormConfig := &gorm.Config{
		Logger: logger.New(
			log.New(os.Stdout, "\r\n", log.LstdFlags),
			logger.Config{
				SlowThreshold: time.Second, // 慢 SQL 阈值
				LogLevel:      logger.Info, // 日志级别
				Colorful:      true,        // 彩色打印
			},
		),
		NamingStrategy: schema.NamingStrategy{
			SingularTable: true, // 禁用复数表名
		},
	}

	var err error
	DB, err = gorm.Open(mysql.Open(dsn), gormConfig)
	if err != nil {
		log.Fatalf("数据库连接失败: %v", err)
	}

	// 获取底层 sql.DB 对象设置连接池
	sqlDB, err := DB.DB()
	if err != nil {
		log.Fatalf("获取底层 DB 失败: %v", err)
	}

	// 连接池配置
	sqlDB.SetMaxIdleConns(10)           // 最大空闲连接
	sqlDB.SetMaxOpenConns(100)          // 最大打开连接
	sqlDB.SetConnMaxLifetime(time.Hour) // 连接最大存活时间

	log.Println("数据库连接成功")

	// 自动迁移表结构
	if err := autoMigrate(DB); err != nil {
		log.Printf("自动迁移失败: %v", err)
	}
}

// GetDB 获取数据库实例（供其他包使用）
func GetDB() *gorm.DB {
	return DB
}

// autoMigrate 自动迁移表结构
func autoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(
		&models.Department{},
		&models.File{},
		&models.SessionMessage{},
		&models.UserMemory{},
		&models.ConversationSummary{},
		&models.AgentConfig{},
	)
}
