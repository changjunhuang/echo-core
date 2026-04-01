package service

import (
	"context"
	"errors"
	"fmt"
	vectorModel "go-start/models/vector"
	repositoryVector "go-start/repository/vector"
)

// WeaviateService 处理文档相关的业务逻辑
type WeaviateService struct {
	weaviateRepo repositoryVector.WeaviateRepository
	className    string
}

// NewWeaviateService 创建文档服务，初始化失败时直接返回 error，避免把无效依赖注入进来。
func NewWeaviateService(className string) (*WeaviateService, error) {
	if className == "" {
		return nil, errors.New("weaviate className cannot be empty")
	}

	repo, err := repositoryVector.NewWeaviateRepository()
	if err != nil {
		return nil, fmt.Errorf("create weaviate repository: %w", err)
	}

	return &WeaviateService{
		weaviateRepo: repo,
		className:    className,
	}, nil
}

// helper to ensure repository is initialized
func (s *WeaviateService) ensureRepo() error {
	if s == nil || s.weaviateRepo == nil {
		return errors.New("weaviate repository not initialized")
	}
	return nil
}

// EnsureSchema 确保向量存储的 schema 存在
func (s *WeaviateService) EnsureSchema(ctx context.Context) error {
	if err := s.ensureRepo(); err != nil {
		return err
	}
	return s.weaviateRepo.EnsureSchema(ctx, s.className)
}

// StoreDocumentVector 存储单个文档向量
func (s *WeaviateService) StoreDocumentVector(ctx context.Context, fileID, filename string, vectorData []float32, metadata map[string]interface{}) error {
	if err := s.ensureRepo(); err != nil {
		return err
	}
	doc := vectorModel.DocumentVector{
		FileID:   fileID,
		Filename: filename,
		Vector:   vectorData,
		Metadata: metadata,
	}
	return s.weaviateRepo.InsertDocument(ctx, s.className, doc)
}

// BatchStoreDocumentVectors 批量存储文档向量
func (s *WeaviateService) BatchStoreDocumentVectors(ctx context.Context, docs []struct {
	FileID   string
	Filename string
	Vector   []float32
	Metadata map[string]interface{}
}) error {
	if err := s.ensureRepo(); err != nil {
		return err
	}

	var repoDocs []vectorModel.DocumentVector
	for _, d := range docs {
		repoDocs = append(repoDocs, vectorModel.DocumentVector{
			FileID:   d.FileID,
			Filename: d.Filename,
			Vector:   d.Vector,
			Metadata: d.Metadata,
		})
	}
	return s.weaviateRepo.BatchInsertDocuments(ctx, s.className, repoDocs)
}

// SearchByVector conducts a search by vector.
func (s *WeaviateService) SearchByVector(ctx context.Context, vector []float32, limit int) ([]vectorModel.DocumentVector, error) {
	if err := s.ensureRepo(); err != nil {
		return nil, err
	}
	return s.weaviateRepo.SearchByVector(ctx, s.className, vector, limit)
}

// NearText searches for objects by text.
func (s *WeaviateService) NearText(ctx context.Context, concepts []string, limit int) ([]vectorModel.DocumentVector, error) {
	if err := s.ensureRepo(); err != nil {
		return nil, err
	}
	return s.weaviateRepo.NearText(ctx, s.className, concepts, limit)
}
