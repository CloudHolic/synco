package model

import "time"

type EventType string

const (
	EventCreate EventType = "CREATE"
	EventWrite  EventType = "WRITE"
	EventRemove EventType = "REMOVE"
	EventRename EventType = "RENAME"
)

type FileEvent struct {
	Type      EventType
	Path      string
	Timestamp time.Time
}

type SyncResult struct {
	Event   FileEvent
	SrcPath string
	DstPath string
	Err     error
}
