package daemon

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"synco/internal/config"
	"synco/internal/logger"
	"synco/internal/model"
	"synco/internal/pipeline"
	"synco/internal/repository"
	"synco/internal/syncer"
	"synco/internal/syncer/dropbox"
	"synco/internal/syncer/gdrive"
	"synco/internal/syncer/local"
	"synco/internal/syncer/tcp"
	"time"

	"go.uber.org/zap"
)

type JobManager struct {
	mu      sync.RWMutex
	jobs    map[uint]*JobState
	cfg     *config.Config
	repo    *repository.HistoryRepository
	jobRepo *repository.JobRepository
	nodeID  string
}

func NewJobManager(cfg *config.Config) (*JobManager, error) {
	nodeID, err := model.LoadOrCreateNodeID()
	if err != nil {
		return nil, fmt.Errorf("failed to load node ID: %w", err)
	}

	return &JobManager{
		jobs:    make(map[uint]*JobState),
		cfg:     cfg,
		repo:    repository.NewHistoryRepository(),
		jobRepo: repository.NewJobRepository(),
		nodeID:  nodeID,
	}, nil
}

func (m *JobManager) StartJob(job model.Job) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.jobs[job.ID]; exists {
		return fmt.Errorf("job %d already running", job.ID)
	}

	state := NewJobState(job)

	if job.SrcType == model.EndpointRemoteTCP {
		if err := m.startDelegatedJob(job, state); err != nil {
			return err
		}

		m.jobs[job.ID] = state
		return nil
	}

	src, err := m.newSource(job)
	if err != nil {
		return err
	}

	s, err := m.newSyncer(job)
	if err != nil {
		return err
	}

	if err := src.Start(); err != nil {
		return fmt.Errorf("failed to start source: %w", err)
	}

	m.jobs[job.ID] = state
	go m.runPipeline(state, src, s)

	logger.Log.Info("job started",
		zap.Uint("id", job.ID),
		zap.String("src", job.SrcPath),
		zap.String("dst", job.DstPath))

	return nil
}

func (m *JobManager) newSource(job model.Job) (syncer.EventSource, error) {
	switch job.SrcType {
	case model.EndpointLocal:
		return local.NewSource(job.SrcPath, m.cfg.BufferSize)
	case model.EndpointGDrive:
		path := strings.TrimPrefix(job.SrcPath, "gdrive:")
		return gdrive.NewSource(job.ID, path, 30*time.Second)
	case model.EndpointDropbox:
		path := strings.TrimPrefix(job.SrcPath, "dropbox:")
		return dropbox.NewSource(job.ID, path)
	default:
		return nil, fmt.Errorf("unsupported src type: %s", job.SrcType)
	}
}

func (m *JobManager) newSyncer(job model.Job) (syncer.Syncer, error) {
	switch {
	case job.DstType == model.EndpointLocal && job.SrcType == model.EndpointLocal:
		return local.NewSyncer(job.SrcPath, job.DstPath, m.cfg.ConflictStrategy)

	case job.DstType == model.EndpointLocal && job.SrcType == model.EndpointGDrive:
		path := strings.TrimPrefix(job.SrcPath, "gdrive:")
		return gdrive.NewDownloader(path, job.DstPath)

	case job.DstType == model.EndpointLocal && job.SrcType == model.EndpointDropbox:
		path := strings.TrimPrefix(job.SrcPath, "dropbox:")
		return dropbox.NewDownloader(path, job.DstPath)

	case job.DstType == model.EndpointRemoteTCP:
		return tcp.NewSyncer(job.SrcPath, job.DstPath, m.nodeID, tcp.NewVclock())

	case job.DstType == model.EndpointGDrive:
		path := strings.TrimPrefix(job.DstPath, "gdrive:")
		return gdrive.NewUploader(job.SrcPath, path)

	case job.DstType == model.EndpointDropbox:
		path := strings.TrimPrefix(job.DstPath, "dropbox:")
		return dropbox.NewUploader(job.SrcPath, path)

	default:
		return nil, fmt.Errorf("unsupported job type: %s → %s", job.SrcType, job.DstType)
	}
}

func (m *JobManager) startDelegatedJob(job model.Job, state *JobState) error {
	recvPort := job.RecvPort
	if recvPort == 0 {
		port, err := findAvailablePort()
		if err != nil {
			return fmt.Errorf("failed to find available port: %w", err)
		}
		recvPort = port

		if err := m.jobRepo.UpdateRecvPort(job.ID, recvPort); err != nil {
			return fmt.Errorf("failed to save receive port: %w", err)
		}
	}

	srv, err := tcp.NewServer(job.DstPath, fmt.Sprintf(":%d", recvPort), m.nodeID, m.cfg.ConflictStrategy)
	if err != nil {
		return fmt.Errorf("failed to create receive server: %w", err)
	}
	if err := srv.Start(); err != nil {
		return fmt.Errorf("failed to start receive server: %w", err)
	}

	state.RecvServer = srv

	myIP, err := getOutboundIP()
	if err != nil {
		return fmt.Errorf("failed to determine local IP: %w", err)
	}
	pushTo := fmt.Sprintf("%s:%d", myIP, recvPort)

	if err := m.requestDelegation(job, m.nodeID, pushTo); err != nil {
		srv.Stop()
		return fmt.Errorf("delegation failed: %w", err)
	}

	logger.Log.Info("receive job started",
		zap.Uint("id", job.ID),
		zap.String("src", job.SrcPath),
		zap.String("dst", job.DstPath),
		zap.Int("receive_port", recvPort),
		zap.String("push_to", pushTo))

	return nil
}

func (m *JobManager) requestDelegation(job model.Job, nodeID, pushTo string) error {
	ep := tcp.ParseEndpoint(job.SrcPath)
	if !ep.IsRemote() {
		return fmt.Errorf("src is not a remote endpoint")
	}

	body := fmt.Sprintf(`{"src":"%s","push_to":"%s","node_id":"%s"}`, ep.Path, pushTo, nodeID)
	url := fmt.Sprintf("http://%s/jobs/delegate", ep.Host)
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to reach remote daemon at %s: %w", ep.Host, err)
	}

	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		var result map[string]string
		_ = json.NewDecoder(resp.Body).Decode(&result)
		return fmt.Errorf("remote delegation rejected delegation: %s", result["error"])
	}

	logger.Log.Info("delegation accepted",
		zap.String("remote", ep.Host),
		zap.String("src", ep.Path),
		zap.String("push_to", pushTo))

	return nil
}

func (m *JobManager) runPipeline(state *JobState, src syncer.EventSource, s syncer.Syncer) {
	defer func() {
		src.Stop()

		m.mu.Lock()
		delete(m.jobs, state.JobID)
		m.mu.Unlock()

		logger.Log.Info("job stopped",
			zap.Uint("id", state.JobID))
	}()

	eventCh := src.Events()

	var processedCh <-chan model.FileEvent
	if _, ok := src.(*local.Source); ok {
		debouncedCh := pipeline.Debounce(eventCh, 100*time.Millisecond)
		filteredCh := pipeline.Filter(debouncedCh, m.cfg.IgnoreList)
		processedCh = pipeline.NewChecksumFilter().Run(filteredCh)
	} else {
		processedCh = pipeline.Filter(eventCh, m.cfg.IgnoreList)
	}

	resultCh := s.Run(processedCh)

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

	if state.RecvServer != nil {
		state.RecvServer.Stop()
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

func findAvailablePort() (int, error) {
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		return 0, err
	}

	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	return port, nil
}

func getOutboundIP() (string, error) {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "", err
	}

	defer func(conn net.Conn) {
		_ = conn.Close()
	}(conn)

	return conn.LocalAddr().(*net.UDPAddr).IP.String(), nil
}
