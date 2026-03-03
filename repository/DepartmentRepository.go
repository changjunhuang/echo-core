package repository

import (
	"go-start/config"
	"go-start/dto"
	"go-start/models"
	"gorm.io/gorm"
)

type DepartmentRepository struct {
	db *gorm.DB
}

func NewProductRepository() *DepartmentRepository {
	return &DepartmentRepository{db: config.GetDB()}
}

// GetByID 根据ID获取单条记录
func (r *DepartmentRepository) GetByID(id uint) (*models.Department, error) {
	var product models.Department
	if err := r.db.First(&product, id).Error; err != nil {
		return nil, err
	}
	return &product, nil
}

// GetList 获取列表（支持分页、筛选、排序）
func (r *DepartmentRepository) GetList(req dto.DepartmentRequest) ([]models.Department, int64, error) {
	var products []models.Department
	var total int64

	query := r.db.Model(&models.Department{})

	// 动态构建查询条件
	if req.Id >= 0 {
		query = query.Where("id = ?", req.Id)
	}
	if req.Name != "" {
		query = query.Where("name LIKE ?", "%"+req.Name+"%")
	}
	if !req.CreatedTimeStart.IsZero() {
		query = query.Where("created_at >= ?", req.CreatedTimeStart)
	}
	if !req.CreatedTimeEnd.IsZero() {
		query = query.Where("created_at <= ?", req.CreatedTimeEnd)
	}

	// 获取总数（必须在分页前）
	query.Count(&total)

	// 排序
	sortBy := "created_time"
	order := "desc"
	query = query.Order(sortBy + " " + order)

	// 分页
	page := req.Page
	if page == 0 {
		page = 1
	}
	pageSize := req.PageSize
	if pageSize == 0 {
		pageSize = 10
	}
	offset := (page - 1) * pageSize

	if err := query.Limit(pageSize).Offset(offset).Find(&products).Error; err != nil {
		return nil, 0, err
	}

	return products, total, nil
}

// GetByCategory 按名称获取
func (r *DepartmentRepository) GetByName(name string) ([]models.Department, error) {
	var products []models.Department
	err := r.db.Where("name = ?", name).Find(&products).Error
	return products, err
}

// Create 创建
func (r *DepartmentRepository) Create(product *models.Department) error {
	return r.db.Create(product).Error
}

// Update 更新
func (r *DepartmentRepository) Update(id uint, updates map[string]interface{}) error {
	return r.db.Model(&models.Department{}).Where("id = ?", id).Updates(updates).Error
}

// Delete 软删除
func (r *DepartmentRepository) Delete(id uint) error {
	return r.db.Delete(&models.Department{}, id).Error
}

// HardDelete 硬删除
func (r *DepartmentRepository) HardDelete(id uint) error {
	return r.db.Unscoped().Delete(&models.Department{}, id).Error
}
