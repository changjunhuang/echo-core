package models

import (
	"time"
)

type Department struct {
	Id          uint      `json:"id"`
	Name        string    `json:"name"`
	CreatedTime time.Time `json:"created_time"`
	UpdatedTime time.Time `json:"updated_time"`
}
