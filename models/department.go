package models

import (
	"time"
)

type Department struct {
	Id         uint      `json:"id"`
	Name       string    `json:"name"`
	CreateTime time.Time `json:"create_time" gorm:"autoCreateTime"`
	UpdateTime time.Time `json:"update_time" gorm:"autoUpdateTime"`
}

func (Department) TableName() string {
	return "Department"
}
