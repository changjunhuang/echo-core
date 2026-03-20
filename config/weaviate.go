package config

import (
	"context"
	"fmt"
	"github.com/weaviate/weaviate-go-client/v5/weaviate"
	"log"
	"os"
)

func InitWeaviate() error {
	host := os.Getenv("WEAVIATE_HOST")
	if host == "" {
		return fmt.Errorf("WEAVIATE_HOST environment variable not set")
	}

	scheme := os.Getenv("WEAVIATE_SCHEME")
	if scheme == "" {
		scheme = "http"
	}

	cfg := weaviate.Config{
		Host:   host,
		Scheme: scheme,
	}
	client, err := weaviate.NewClient(cfg)
	if err != nil {
		return fmt.Errorf("创建 Weaviate 客户端失败: %w", err)
	}
	log.Println("成功连接到 Weaviate !")

	meta, err := client.Misc().MetaGetter().Do(context.Background())
	if err != nil {
		return fmt.Errorf("获取 Weaviate 元信息失败: %w", err)
	}
	log.Println("连接成功！Weaviate 版本: , 主机名: ", meta.Version, meta.Hostname)
	return nil
}
