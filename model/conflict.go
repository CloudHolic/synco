package model

import "time"

type ConflictStrategy string

const (
	StrategyNewerWins  ConflictStrategy = "NEWER_WINS"
	StrategySourceWins ConflictStrategy = "SOURCE_WINS"
	StrategyBackup     ConflictStrategy = "BACKUP"
	StrategySkip       ConflictStrategy = "SKIP"
)

type ConflictInfo struct {
	Path       string
	SrcModTime time.Time
	DstModTime time.Time
	Strategy   ConflictStrategy
	Resolved   bool
	BackupPath string
}
