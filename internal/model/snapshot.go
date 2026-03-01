package model

import "time"

type JobSnapshot struct {
	JobID     uint       `json:"job_id"`
	Src       string     `json:"src"`
	Dst       string     `json:"dst"`
	Status    JobStatus  `json:"status"`
	StartedAt time.Time  `json:"started_at"`
	Synced    int        `json:"synced"`
	Failed    int        `json:"failed"`
	LastSync  *time.Time `json:"last_sync"`
}
