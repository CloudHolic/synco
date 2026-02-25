package model

import (
	"time"

	"gorm.io/gorm"
)

type SyncStatus string

const (
	StatusSuccess SyncStatus = "SUCCESS"
	StatusFailed  SyncStatus = "FAILED"
)

type History struct {
	gorm.Model
	EventType SyncStatus `gorm:"not null"`
	SrcPath   string     `gorm:"not null"`
	DstPath   string     `gorm:"not null"`
	FileEvent string     `gorm:"not null"`
	ErrMsg    string
	SyncedAt  time.Time `gorm:"not null"`
}
