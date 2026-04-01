package vector

import (
	"context"
	"errors"
	"fmt"
	"github.com/weaviate/weaviate-go-client/v5/weaviate"
	"github.com/weaviate/weaviate-go-client/v5/weaviate/graphql"
	"github.com/weaviate/weaviate/entities/models"
	"go-start/models/vector"
	"log"
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
	// SearchByVector 根据向量相似性搜索
	SearchByVector(ctx context.Context, className string, vector []float32, limit int) ([]vector.DocumentVector, error)
	// NearText an object
	NearText(ctx context.Context, className string, concepts []string, limit int) ([]vector.DocumentVector, error)
}

// weaviateRepository 是 WeaviateRepository 的具体实现
type weaviateRepository struct {
	client *weaviate.Client
}

// NewWeaviateRepository 创建仓储实例
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

func (r *weaviateRepository) ensureClient() error {
	if r == nil || r.client == nil {
		return errors.New("weaviate client not initialized")
	}
	return nil
}

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
		WithProperties(properties).
		WithVector(doc.Vector).
		Do(ctx)
	if err != nil {
		return fmt.Errorf("insert document: %w", err)
	}
	return nil
}

func (r *weaviateRepository) BatchInsertDocuments(ctx context.Context, className string, docs []vector.DocumentVector) error {
	if err := r.ensureClient(); err != nil {
		return err
	}
	var batch []*models.Object
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
		batch = append(batch, obj)
	}

	batchResult, err := r.client.Batch().ObjectsBatcher().WithObjects(batch...).Do(ctx)
	if err != nil {
		return fmt.Errorf("batch insert documents: %w", err)
	}
	for _, res := range batchResult {
		if res.Result.Errors != nil {
			return fmt.Errorf("batch insert error: %v", res.Result.Errors.Error)
		}
	}
	return nil
}

func (r *weaviateRepository) SearchByVector(ctx context.Context, className string, queryVector []float32, limit int) ([]vector.DocumentVector, error) {
	if err := r.ensureClient(); err != nil {
		return nil, err
	}

	fields := []graphql.Field{
		{Name: "fileId"},
		{Name: "filename"},
		{
			Name: "_additional",
			Fields: []graphql.Field{
				{Name: "distance"},
			},
		},
	}

	nearVector := r.client.GraphQL().NearVectorArgBuilder().WithVector(queryVector)
	result, err := r.client.GraphQL().Get().
		WithClassName(className).
		WithFields(fields...).
		WithNearVector(nearVector).
		WithLimit(1). // 强制只获取分数最高的1条结果
		Do(ctx)
	if err != nil {
		return nil, fmt.Errorf("search by vector: %w", err)
	}

	// 安全地处理GraphQL响应
	if result == nil {
		// 查询结果本身为nil，直接返回空，避免panic
		return []vector.DocumentVector{}, nil
	}
	if result.Errors != nil && len(result.Errors) > 0 {
		return nil, fmt.Errorf("graphql search error: %v", result.Errors)
	}

	get, ok := result.Data["Get"].(map[string]interface{})
	if !ok || get == nil {
		// Get字段不存在或为nil，说明没有匹配项
		return []vector.DocumentVector{}, nil
	}

	rawItems, ok := get[className].([]interface{})
	if !ok || rawItems == nil {
		// 对应的class没有内容
		return []vector.DocumentVector{}, nil
	}

	documents := make([]vector.DocumentVector, 0, 1) // 最多返回1条
	for _, item := range rawItems {
		itemMap, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		fileID, _ := itemMap["fileId"].(string)
		filename, _ := itemMap["filename"].(string)

		// 检查距离(distance)，过滤掉差距过远的数据
		// 余弦距离(cosine)范围是 0 (完全相同) 到 2 (完全相反)。通常0.25是一个合理的阈值。
		if additional, ok := itemMap["_additional"].(map[string]interface{}); ok {
			if dist, ok := additional["distance"].(float64); ok {
				if dist > 0.6 {
					// 距离过大，说明不符合条件，直接丢弃
					continue
				}
			}
		}

		documents = append(documents, vector.DocumentVector{
			FileID:   fileID,
			Filename: filename,
		})

		// 保证只返回唯一的最符合结果，拿到后就跳出循环
		break
	}

	return documents, nil
}

func (r *weaviateRepository) NearText(ctx context.Context, className string, concepts []string, limit int) ([]vector.DocumentVector, error) {
	if err := r.ensureClient(); err != nil {
		return nil, err
	}

	fields := []graphql.Field{
		{Name: "fileId"},
		{Name: "filename"},
	}

	nearText := r.client.GraphQL().NearTextArgBuilder().WithConcepts(concepts)

	result, err := r.client.GraphQL().Get().
		WithClassName(className).
		WithFields(fields...).
		WithNearText(nearText).
		WithLimit(limit).
		Do(ctx)
	if err != nil {
		// 如果 schema 的 vectorizer 为 "none"，这里会报错 "Unknown argument 'nearText'..."
		return nil, fmt.Errorf("search by nearText: %w", err)
	}

	// 安全地处理GraphQL响应
	if result == nil {
		return []vector.DocumentVector{}, nil
	}
	if result.Errors != nil && len(result.Errors) > 0 {
		log.Println("graphql search error: %v,message:", result.Errors, result.Errors[0].Message)
		return nil, fmt.Errorf("graphql search error: %v,message:", result.Errors, result.Errors[0].Message)
	}

	get, ok := result.Data["Get"].(map[string]interface{})
	if !ok || get == nil {
		return []vector.DocumentVector{}, nil
	}

	items, ok := get[className].([]interface{})
	if !ok || items == nil {
		return []vector.DocumentVector{}, nil
	}

	var documents []vector.DocumentVector
	for _, item := range items {
		itemMap, ok := item.(map[string]interface{})
		if !ok || itemMap == nil {
			continue
		}
		fileID, _ := itemMap["fileId"].(string)
		filename, _ := itemMap["filename"].(string)
		documents = append(documents, vector.DocumentVector{
			FileID:   fileID,
			Filename: filename,
		})
	}

	return documents, nil
}
