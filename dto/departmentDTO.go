package dto

import "time"

// 列表查询请求
type DepartmentRequest struct {
	Id               uint      `json:"id"`
	Name             string    `json:"name"`
	CreatedTimeStart time.Time `json:"created_time_start"`
	CreatedTimeEnd   time.Time `json:"created_time_end"`
	UpdatedTimeStart time.Time `json:"updated_time_start"`
	UpdatedTimeEnd   time.Time `json:"updated_time_end"`

	Page     int `form:"page" binding:"min=1"`        // 页码，默认1
	PageSize int `form:"page_size" binding:"max=100"` // 每页数量，默认10
}

// 单条响应
type DepartmentResponse struct {
	ID          uint      `json:"id"`
	Name        string    `json:"name"`
	CreatedTime time.Time `json:"created_time"`
	UpdatedTime time.Time `json:"updated_time"`
}

// 列表响应
type DepartmentListResponse struct {
	Total int                  `json:"total"`
	Page  int                  `json:"page"`
	Data  []DepartmentResponse `json:"data"`
}

// 创建请求
type DepartmentCreateRequest struct {
	Name        string    `json:"name" binding:"required,min=2,max=100"`
	CreatedTime time.Time `json:"created_time"`
	UpdatedTime time.Time `json:"updated_time"`
}

// 更新请求
type DepartmentUpdateRequest struct {
	Id   uint   `json:"id"`
	Name string `json:"name" binding:"omitempty,min=2,max=100"`
}
