package model

import "gorm.io/gorm"

type EndpointType string

const (
	EndpointLocal     EndpointType = "LOCAL"
	EndpointRemoteTCP EndpointType = "REMOTE_TCP"
	EndpointGDrive    EndpointType = "GOOGLE_DRIVE"
	EndpointDropbox   EndpointType = "DROPBOX"
)

type JobStatus string

const (
	JobStatusActive  JobStatus = "ACTIVE"
	JobStatusPaused  JobStatus = "PAUSED"
	JobStatusStopped JobStatus = "STOPPED"
)

type Job struct {
	gorm.Model
	SrcType  EndpointType `gorm:"not null"`
	SrcPath  string       `gorm:"not null"`
	DstType  EndpointType `gorm:"not null"`
	DstPath  string       `gorm:"not null"`
	Status   JobStatus    `gorm:"not null;default:'ACTIVE'"`
	RecvPort int
}
