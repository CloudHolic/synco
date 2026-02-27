package daemon

import (
	"sync"
	"synco/internal/model"
	"time"
)

type JobState struct {
	mu        sync.RWMutex
	JobID     uint
	Src       string
	Dst       string
	Status    model.JobStatus
	StartedAt time.Time
	Synced    int
	Failed    int
	LastSync  *time.Time
	PauseCh   chan struct{}
	ResumeCh  chan struct{}
	StopCh    chan struct{}
}

func NewJobState(job model.Job) *JobState {
	return &JobState{
		JobID:     job.ID,
		Src:       job.Src,
		Dst:       job.Dst,
		Status:    model.JobStatusActive,
		StartedAt: time.Now(),
		PauseCh:   make(chan struct{}, 1),
		ResumeCh:  make(chan struct{}, 1),
		StopCh:    make(chan struct{}, 1),
	}
}

func (s *JobState) RecordSync(result model.SyncResult) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.LastSync = new(time.Now())
	if result.Err != nil {
		s.Failed++
	} else {
		s.Synced++
	}
}

func (s *JobState) SetStatus(status model.JobStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Status = status
}

type JobSnapshot struct {
	JobID     uint            `json:"job_id"`
	Src       string          `json:"src"`
	Dst       string          `json:"dst"`
	Status    model.JobStatus `json:"status"`
	StartedAt time.Time       `json:"started_at"`
	Synced    int             `json:"synced"`
	Failed    int             `json:"failed"`
	LastSync  *time.Time      `json:"last_sync"`
}

func (s *JobState) Snapshot() JobSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return JobSnapshot{
		JobID:     s.JobID,
		Src:       s.Src,
		Dst:       s.Dst,
		Status:    s.Status,
		StartedAt: s.StartedAt,
		Synced:    s.Synced,
		Failed:    s.Failed,
		LastSync:  s.LastSync,
	}
}
