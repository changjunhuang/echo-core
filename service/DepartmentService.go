package service

import (
	"errors"
	"go-start/dto"
	"go-start/models"
	"go-start/repository"
	"gorm.io/gorm"
)

type DepartmentService struct {
	repo *repository.DepartmentRepository
}

func NewDepartmentService() *DepartmentService {
	return &DepartmentService{repo: repository.NewProductRepository()}
}

// GetDepartment 获取单个部门详情
func (s *DepartmentService) GetDepartment(id uint) (*dto.DepartmentResponse, error) {
	department, err := s.repo.GetByID(id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("部门不存在")
		}
		return nil, err
	}
	return s.toResponse(department), nil
}

// GetDepartmentList 获取部门列表
func (s *DepartmentService) GetDepartmentList(req dto.DepartmentRequest) (*dto.DepartmentListResponse, error) {
	departments, total, err := s.repo.GetList(req)
	if err != nil {
		return nil, err
	}

	var list []dto.DepartmentResponse
	for _, p := range departments {
		list = append(list, *s.toResponse(&p))
	}

	return &dto.DepartmentListResponse{
		Total: int(total),
		Page:  req.Page,
		Data:  list,
	}, nil
}

// CreateDepartment 创建部门
func (s *DepartmentService) CreateDepartment(req dto.DepartmentCreateRequest) (*dto.DepartmentResponse, error) {
	department := &models.Department{
		Name: req.Name,
	}

	if err := s.repo.Create(department); err != nil {
		return nil, err
	}

	return s.toResponse(department), nil
}

// UpdateDepartment 更新部门
func (s *DepartmentService) UpdateDepartment(id uint, req dto.DepartmentUpdateRequest) (*dto.DepartmentResponse, error) {
	// 检查存在
	if _, err := s.repo.GetByID(id); err != nil {
		return nil, errors.New("部门不存在")
	}

	updates := make(map[string]interface{})
	if req.Name != "" {
		updates["name"] = req.Name
	}

	if err := s.repo.Update(id, updates); err != nil {
		return nil, err
	}

	// 返回更新后的数据
	department, _ := s.repo.GetByID(id)
	return s.toResponse(department), nil
}

// Department 删除部门
func (s *DepartmentService) DeleteDepartment(id uint) error {
	return s.repo.Delete(id)
}

// 转换函数
func (s *DepartmentService) toResponse(p *models.Department) *dto.DepartmentResponse {
	return &dto.DepartmentResponse{
		ID:   p.Id,
		Name: p.Name,
	}
}
