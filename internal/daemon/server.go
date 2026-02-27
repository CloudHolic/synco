package daemon

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"synco/internal/logger"
	"synco/internal/model"
	"synco/internal/repository"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"go.uber.org/zap"
)

type Server struct {
	echo     *echo.Echo
	manager  *JobManager
	jobRepo  *repository.JobRepository
	histRepo *repository.HistoryRepository
	port     int
	stopCh   chan struct{}
}

func NewServer(manager *JobManager, port int) *Server {
	e := echo.New()
	e.HideBanner = true
	e.Use(middleware.Recover())

	s := &Server{
		echo:     e,
		manager:  manager,
		jobRepo:  repository.NewJobRepository(),
		histRepo: repository.NewHistoryRepository(),
		port:     port,
		stopCh:   make(chan struct{}, 1),
	}
	s.registerRoutes()
	return s
}

func (s *Server) registerRoutes() {
	// For the entire daemon
	s.echo.GET("/status", s.handleStatus)
	s.echo.POST("/stop", s.handleStop)

	// For a specific job
	g := s.echo.Group("/jobs")
	g.GET("", s.handleListJobs)
	g.POST("", s.handleAddJob)
	g.POST("/delegate", s.handleDelegate)
	g.DELETE("/:id", s.handleRemoveJob)
	g.POST("/:id/pause", s.handlePauseJob)
	g.POST("/:id/resume", s.handleResumeJob)

	// History
	s.echo.GET("/history", s.handleHistory)
}

func (s *Server) Start() {
	go func() {
		addr := ":" + strconv.Itoa(s.port)
		logger.Log.Info("daemon server started",
			zap.String("addr", addr))

		if err := s.echo.Start(addr); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Log.Error("daemon server error", zap.Error(err))
		}
	}()
}

func (s *Server) Stop(ctx context.Context) error {
	s.manager.StopAll()
	return s.echo.Shutdown(ctx)
}

func (s *Server) StopCh() <-chan struct{} {
	return s.stopCh
}

func (s *Server) handleStatus(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]any{
		"jobs": s.manager.Snapshots(),
	})
}

func (s *Server) handleStop(c echo.Context) error {
	s.stopCh <- struct{}{}
	return c.JSON(http.StatusOK, map[string]string{"status": "stopping"})
}

func (s *Server) handleListJobs(c echo.Context) error {
	jobs, err := s.jobRepo.GetAll()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	snaps := make(map[uint]JobSnapshot)
	for _, snap := range s.manager.Snapshots() {
		snaps[snap.JobID] = snap
	}

	return c.JSON(http.StatusOK, map[string]any{
		"jobs":    jobs,
		"running": snaps,
	})
}

type addJobRequest struct {
	Src     string             `json:"src"`
	SrcType model.EndpointType `json:"src_type"`
	Dst     string             `json:"dst"`
	DstType model.EndpointType `json:"dst_type"`
}

func (s *Server) handleAddJob(c echo.Context) error {
	var req addJobRequest
	if err := c.Bind(&req); err != nil || req.Src == "" || req.Dst == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "src and dst required"})
	}

	job, err := s.jobRepo.Add(req.Src, req.SrcType, req.Dst, req.DstType)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	if err := s.manager.StartJob(job); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	return c.JSON(http.StatusCreated, job)
}

type delegateRequest struct {
	Src    string `json:"src"`
	PushTo string `json:"push_to"`
	NodeID string `json:"node_id"`
}

func (s *Server) handleDelegate(c echo.Context) error {
	var req delegateRequest
	if err := c.Bind(&req); err != nil || req.Src == "" || req.PushTo == "" || req.NodeID == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "src and push_to required"})
	}

	jobs, err := s.jobRepo.GetAll()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	for _, j := range jobs {
		if j.SrcPath == req.Src && j.DstPath == req.PushTo {
			return c.JSON(http.StatusOK, map[string]string{"status": "already exists"})
		}
	}

	job, err := s.jobRepo.Add(req.Src, model.EndpointLocal, req.PushTo, model.EndpointRemoteTCP)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	if err := s.manager.StartJob(job); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	logger.Log.Info("delegated job started",
		zap.String("src", req.Src),
		zap.String("push_to", req.PushTo),
		zap.String("requested_by", req.NodeID))

	return c.JSON(http.StatusCreated, job)
}

func (s *Server) handleRemoveJob(c echo.Context) error {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid id"})
	}

	_ = s.manager.StopJob(uint(id))

	if err := s.jobRepo.Delete(uint(id)); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	return c.NoContent(http.StatusNoContent)
}

func (s *Server) handlePauseJob(c echo.Context) error {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid id"})
	}

	if err := s.manager.PauseJob(uint(id)); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, map[string]string{"status": "paused"})
}

func (s *Server) handleResumeJob(c echo.Context) error {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid id"})
	}

	if err := s.manager.ResumeJob(uint(id)); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, map[string]string{"status": "resumed"})
}

func (s *Server) handleHistory(c echo.Context) error {
	n := 20
	if nStr := c.QueryParam("n"); nStr != "" {
		if parsed, err := strconv.Atoi(nStr); err == nil {
			n = parsed
		}
	}

	histories, err := s.histRepo.GetRecent(n)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, histories)
}
