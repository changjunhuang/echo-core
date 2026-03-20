package vector

import (
	"context"
	"errors"
	"fmt"
	"github.com/weaviate/weaviate-go-client/v5/weaviate"
	"github.com/weaviate/weaviate-go-client/v5/weaviate/graphql"

	"github.com/weaviate/weaviate/entities/models"
	"go-start/models/vector"
	"os"
)

// WeaviateRepository 定义仓储接口
type WeaviateRepository interface {
	// EnsureSchema 确保类存在，不存在则创建
	EnsureSchema(ctx context.Context, className string) error
	// InsertDocument 插入单个文档向量
	InsertDocument(ctx context.Context, className string, doc vector.DocumentVector) error
	// BatchInsertDocuments 批量插入文档向量
	BatchInsertDocuments(ctx context.Context, className string, docs []vector.DocumentVector) error
	// SearchByVector 根据向量相似性搜索（可选，供后续使用）
	SearchByVector(ctx context.Context, className string, vector []float32, limit int) ([]vector.DocumentVector, error)
}

// weaviateRepository 是 WeaviateRepository 的具体实现
type weaviateRepository struct {
	client *weaviate.Client
}

// NewWeaviateRepository 创建仓储实例。
// 真实修复点：初始化失败时直接返回 error，而不是返回一个内部 client 为 nil 的对象。
func NewWeaviateRepository() (WeaviateRepository, error) {
	host := os.Getenv("WEAVIATE_HOST")
	if host == "" {
		return nil, errors.New("WEAVIATE_HOST environment variable not set")
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
		return nil, fmt.Errorf("create weaviate client: %w", err)
	}
	return &weaviateRepository{client: client}, nil
}

// helper to check client availability
func (r *weaviateRepository) ensureClient() error {
	if r == nil || r.client == nil {
		return errors.New("weaviate client not initialized")
	}
	return nil
}

// EnsureSchema 实现接口
func (r *weaviateRepository) EnsureSchema(ctx context.Context, className string) error {
	if err := r.ensureClient(); err != nil {
		return err
	}
	exists, err := r.client.Schema().ClassExistenceChecker().
		WithClassName(className).
		Do(ctx)
	if err != nil {
		return fmt.Errorf("check class existence: %w", err)
	}
	if exists {
		return nil
	}

	// 定义类结构，根据业务需求调整属性
	class := &models.Class{
		Class: className,
		Properties: []*models.Property{
			{
				Name:     "fileId",
				DataType: []string{"string"},
			},
			{
				Name:     "filename",
				DataType: []string{"string"},
			},
		},
		Vectorizer: "none",
		VectorIndexConfig: map[string]interface{}{
			"distance": "cosine",
		},
	}

	err = r.client.Schema().ClassCreator().WithClass(class).Do(ctx)
	if err != nil {
		return fmt.Errorf("create class: %w", err)
	}
	return nil
}

// InsertDocument 实现接口
func (r *weaviateRepository) InsertDocument(ctx context.Context, className string, doc vector.DocumentVector) error {
	if err := r.ensureClient(); err != nil {
		return err
	}
	properties := map[string]interface{}{
		"fileId":   doc.FileID,
		"filename": doc.Filename,
	}
	for k, v := range doc.Metadata {
		properties[k] = v
	}

	_, err := r.client.Data().Creator().
		WithClassName(className).
		WithID(doc.FileID).
		WithProperties(properties).
		WithVector(doc.Vector).
		Do(ctx)
	if err != nil {
		return fmt.Errorf("insert document: %w", err)
	}
	return nil
}

// BatchInsertDocuments 实现接口
func (r *weaviateRepository) BatchInsertDocuments(ctx context.Context, className string, docs []vector.DocumentVector) error {
	if err := r.ensureClient(); err != nil {
		return err
	}
	batcher := r.client.Batch().ObjectsBatcher()
	for _, doc := range docs {
		properties := map[string]interface{}{
			"fileId":   doc.FileID,
			"filename": doc.Filename,
		}
		for k, v := range doc.Metadata {
			properties[k] = v
		}
		obj := &models.Object{
			Class:      className,
			Properties: properties,
			Vector:     doc.Vector,
		}
		batcher = batcher.WithObject(obj)
	}

	resp, err := batcher.Do(ctx)
	if err != nil {
		return fmt.Errorf("batch insert: %w", err)
	}
	for _, objRes := range resp {
		if objRes.Result.Errors != nil {
			return fmt.Errorf("batch insert object error: %v", objRes.Result.Errors)
		}
	}
	return nil
}

// SearchByVector 实现接口（示例，可能需要根据实际 GraphQL 响应解析）
func (r *weaviateRepository) SearchByVector(ctx context.Context, className string, vector []float32, limit int) ([]vector.DocumentVector, error) {
	if err := r.ensureClient(); err != nil {
		return nil, err
	}
	_, err := r.client.GraphQL().Get().
		WithClassName(className).
		WithFields(
			graphql.Field{Name: "fileId"},
			graphql.Field{Name: "filename"},
		).
		WithNearVector(r.client.GraphQL().NearVectorArgBuilder().
			WithVector(vector).
			WithCertainty(0.7),
		).
		WithLimit(limit).
		Do(ctx)
	if err != nil {
		return nil, err
	}
	return nil, nil
}
