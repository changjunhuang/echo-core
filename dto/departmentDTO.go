package dto

import "time"

// 列表查询请求
type DepartmentRequest struct {
	Id               uint      `form:"id" json:"id"`
	Name             string    `form:"name" json:"name"`
	CreatedTimeStart time.Time `form:"createdTimeStart" json:"createdTimeStart"`
	CreatedTimeEnd   time.Time `form:"createdTimeEnd" json:"createdTimeEnd"`
	UpdatedTimeStart time.Time `form:"updatedTimeStart" json:"updatedTimeStart"`
	UpdatedTimeEnd   time.Time `form:"updatedTimeEnd" json:"updatedTimeEnd"`

	Page     int `form:"page" binding:"min=1"`       // 页码，默认1
	PageSize int `form:"pageSize" binding:"max=100"` // 每页数量，默认10
}

// 单条响应
type DepartmentResponse struct {
	ID          uint      `json:"id"`
	Name        string    `json:"name"`
	CreatedTime time.Time `json:"createdTime"`
	UpdatedTime time.Time `json:"updatedTime"`
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
	CreatedTime time.Time `form:"createdTime" json:"createdTime"`
	UpdatedTime time.Time `form:"updatedTime" json:"updatedTime"`
}

// 更新请求
type DepartmentUpdateRequest struct {
	Id   uint   `json:"id"`
	Name string `json:"name" binding:"omitempty,min=2,max=100"`
}
