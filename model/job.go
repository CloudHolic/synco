package model

import "gorm.io/gorm"

type JobStatus string

const (
	JobStatusActive  JobStatus = "ACTIVE"
	JobStatusPaused  JobStatus = "PAUSED"
	JobStatusStopped JobStatus = "STOPPED"
)

type Job struct {
	gorm.Model
	Src    string    `gorm:"not null"`
	Dst    string    `gorm:"not null"`
	Status JobStatus `gorm:"not null;default:'ACTIVE'"`
}
