package model

import "time"

type Video struct {
	ID      int64       `json:"id" gorm:"primaryKey;autoIncrement"`
	URL     string      `json:"url"`
	Version string      `json:"version"` // v1: add _v2, v2: keep logic
	Prefix  VideoPrefix `json:"prefix"`
	Status  VideoStatus `json:"status" default:"pending"`
	StartAt time.Time   `json:"start_at"`
	EndAt   time.Time   `json:"end_at"`
}

func (v Video) TableName() string {
	return "videos"
}

type VideoStatus string

const (
	VideoStatusPending   VideoStatus = "pending"
	VideoStatusCooking   VideoStatus = "cooking"
	VideoStatusFailed    VideoStatus = "failed"
	VideoStatusCompleted VideoStatus = "completed"
)

type VideoPrefix string

const (
	VideoPrefixNature   VideoPrefix = "31638062/Nature"
	VideoPrefixWildlife VideoPrefix = "31638062/Wildlife"
)
