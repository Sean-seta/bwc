package model

import "time"

type Video struct {
	ID       string `json:"id"`
	Title    string
	URL      string      `json:"url"`
	Height   int         `json:"height"`
	Duration string      `json:"duration"`
	Prefix   VideoPrefix `json:"prefix"`
	Status   VideoStatus `json:"status" default:"pending"`
	StartAt  time.Time   `json:"start_at"`
	EndAt    time.Time   `json:"end_at"`
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
