package daemon

import (
	"fmt"
	"sync"
	"synco/config"
	"synco/logger"
	"synco/model"
	"synco/pipeline"
	"synco/repository"
	"synco/syncer"
	"synco/watcher"
	"time"

	"go.uber.org/zap"
)

type JobManager struct {
	mu      sync.RWMutex
	jobs    map[uint]*JobState
	cfg     *config.Config
	repo    *repository.HistoryRepository
	jobRepo *repository.JobRepository
}

func NewJobManager(cfg *config.Config) *JobManager {
	return &JobManager{
		jobs:    make(map[uint]*JobState),
		cfg:     cfg,
		repo:    repository.NewHistoryRepository(),
		jobRepo: repository.NewJobRepository(),
	}
}

func (m *JobManager) StartJob(job model.Job) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.jobs[job.ID]; exists {
		return fmt.Errorf("job %d already running", job.ID)
	}

	state := NewJobState(job)

	w, err := watcher.New(m.cfg.BufferSize)
	if err != nil {
		return fmt.Errorf("failed to create watcher: %w", err)
	}
	if err := w.Watch(job.Src); err != nil {
		return fmt.Errorf("failed to watch %s: %w", job.Src, err)
	}

	s, err := syncer.NewLocalSyncer(job.Src, job.Dst)
	if err != nil {
		w.Stop()
		return fmt.Errorf("failed to create syncer: %w", err)
	}

	m.jobs[job.ID] = state

	go m.runPipeline(state, w, s)

	logger.Log.Info("job started",
		zap.Uint("id", job.ID),
		zap.String("src", job.Src),
		zap.String("dst", job.Dst))

	return nil
}

func (m *JobManager) runPipeline(state *JobState, w *watcher.Watcher, s *syncer.LocalSyncer) {
	defer func() {
		w.Stop()

		m.mu.Lock()
		delete(m.jobs, state.JobID)
		m.mu.Unlock()

		logger.Log.Info("job stopped",
			zap.Uint("id", state.JobID))
	}()

	eventCh := w.Events()
	debouncedCh := pipeline.Debounce(eventCh, 100*time.Millisecond)
	filteredCh := pipeline.Filter(debouncedCh, m.cfg.IgnoreList)
	checksumCh := pipeline.NewChecksumFilter().Run(filteredCh)
	resultCh := s.Run(checksumCh)

	for {
		select {
		case result, ok := <-resultCh:
			if !ok {
				return
			}

			if state.Status == model.JobStatusPaused {
				continue
			}

			if err := m.repo.Save(result); err != nil {
				logger.Log.Warn("failed to save history",
					zap.Error(err))
			}

			state.RecordSync(result)

		case <-state.PauseCh:
			state.SetStatus(model.JobStatusPaused)
			_ = m.jobRepo.UpdateStatus(state.JobID, model.JobStatusPaused)
			logger.Log.Info("job paused",
				zap.Uint("id", state.JobID))

		case <-state.ResumeCh:
			state.SetStatus(model.JobStatusActive)
			_ = m.jobRepo.UpdateStatus(state.JobID, model.JobStatusActive)
			logger.Log.Info("job resumed",
				zap.Uint("id", state.JobID))

		case <-state.StopCh:
			return
		}
	}
}

func (m *JobManager) StopJob(id uint) error {
	m.mu.RLock()
	state, exists := m.jobs[id]
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf("job %d not found", id)
	}

	state.StopCh <- struct{}{}
	return nil
}

func (m *JobManager) StopAll() {
	m.mu.RLock()
	ids := make([]uint, 0, len(m.jobs))
	for id := range m.jobs {
		ids = append(ids, id)
	}
	m.mu.RUnlock()

	for _, id := range ids {
		_ = m.StopJob(id)
	}
}

func (m *JobManager) PauseJob(id uint) error {
	m.mu.RLock()
	state, exists := m.jobs[id]
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf("job %d not found", id)
	}

	state.PauseCh <- struct{}{}
	return nil
}

func (m *JobManager) ResumeJob(id uint) error {
	m.mu.RLock()
	state, exists := m.jobs[id]
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf("job %d not found", id)
	}

	state.ResumeCh <- struct{}{}
	return nil
}

func (m *JobManager) Snapshots() []JobSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()

	snaps := make([]JobSnapshot, 0, len(m.jobs))
	for _, state := range m.jobs {
		snaps = append(snaps, state.Snapshot())
	}

	return snaps
}
