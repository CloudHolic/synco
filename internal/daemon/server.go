package daemon

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"synco/internal/logger"
	"synco/internal/model"
	"synco/internal/repository"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"go.uber.org/zap"
)

type Server struct {
	echo      *echo.Echo
	manager   *JobManager
	jobRepo   *repository.JobRepository
	histRepo  *repository.HistoryRepository
	port      int
	creds     *Credentials
	stopCh    chan struct{}
	startedAt time.Time
}

func NewServer(manager *JobManager, port int, creds *Credentials) *Server {
	e := echo.New()
	e.HideBanner = true
	e.Use(middleware.Recover())

	s := &Server{
		echo:      e,
		manager:   manager,
		jobRepo:   repository.NewJobRepository(),
		histRepo:  repository.NewHistoryRepository(),
		port:      port,
		creds:     creds,
		stopCh:    make(chan struct{}, 1),
		startedAt: time.Now(),
	}
	s.registerRoutes()
	return s
}

func (s *Server) registerRoutes() {
	authed := s.echo.Group("", s.authMiddleware())
	// For the entire daemon
	authed.GET("/status", s.handleStatus)
	authed.POST("/stop", s.handleStop)
	authed.GET("/history", s.handleHistory)

	// For a specific job
	jobs := authed.Group("/jobs")
	jobs.GET("", s.handleListJobs)
	jobs.POST("", s.handleAddJob)
	jobs.DELETE("/:id", s.handleRemoveJob)
	jobs.POST("/:id/pause", s.handlePauseJob)
	jobs.POST("/:id/resume", s.handleResumeJob)

	// Delegation은 원격에서 호출하므로 별도 로직 추가 적용
	jobs.POST("/delegate", s.handleDelegate, s.delegateMiddleware())
	jobs.POST("/push-once", s.handlePushOnce, s.delegateMiddleware())
}

func (s *Server) Start() {
	go func() {
		addr := fmt.Sprintf("127.0.0.1:%d", s.port)
		logger.Log.Info("daemon server started",
			zap.String("addr", addr))

		srv := &http.Server{
			Addr:      addr,
			Handler:   s.echo,
			TLSConfig: s.creds.TLSConfig,
		}

		if err := srv.ListenAndServeTLS("", ""); err != nil &&
			!errors.Is(err, http.ErrServerClosed) {
			logger.Log.Error("daemon server error",
				zap.Error(err))
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

func (s *Server) authMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			auth := c.Request().Header.Get("Authorization")
			if len(auth) < 8 || auth[:7] != "Bearer " {
				return echo.NewHTTPError(http.StatusUnauthorized, "missing token")
			}

			provided := auth[7:]

			// timing-safe 비교로 timing attack 방지
			if subtle.ConstantTimeCompare([]byte(provided), []byte(s.creds.Token)) != 1 {
				return echo.NewHTTPError(http.StatusUnauthorized, "invalid token")
			}

			return next(c)
		}
	}
}

func (s *Server) delegateMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			tsStr := c.Request().Header.Get("X-Synco-Timestamp")
			if tsStr == "" {
				return echo.NewHTTPError(http.StatusBadRequest, "missing timestamp")
			}

			ts, err := strconv.ParseInt(tsStr, 10, 64)
			if err != nil {
				return echo.NewHTTPError(http.StatusBadRequest, "invalid timestamp")
			}

			// 5분 이상 지난 요청은 거부
			age := time.Since(time.Unix(ts, 0))
			if age > 5*time.Minute || age < -1*time.Minute {
				return echo.NewHTTPError(http.StatusUnauthorized, "request expired")
			}

			return next(c)
		}
	}
}

func (s *Server) handleStatus(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]any{
		"daemon": map[string]any{
			"pid":        os.Getpid(),
			"started_at": s.startedAt,
		},
		"jobs": s.manager.Snapshots(),
	})
}

func (s *Server) handleStop(c echo.Context) error {
	s.stopCh <- struct{}{}
	return c.JSON(http.StatusOK, map[string]string{"status": "stopping"})
}

func (s *Server) handleHistory(c echo.Context) error {
	n := 20
	if nStr := c.QueryParam("n"); nStr != "" {
		if parsed, err := strconv.Atoi(nStr); err == nil {
			n = parsed
		}
	}

	var jobID uint
	if jStr := c.QueryParam("job_id"); jStr != "" {
		if parsed, err := strconv.ParseUint(jStr, 10, 64); err == nil {
			jobID = uint(parsed)
		}
	}

	histories, err := s.histRepo.GetRecent(n, jobID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, histories)
}

func (s *Server) handleListJobs(c echo.Context) error {
	jobs, err := s.jobRepo.GetAll()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	snaps := make(map[uint]model.JobSnapshot)
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

	go s.manager.RunInitialSync(job)

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

func (s *Server) handlePushOnce(c echo.Context) error {
	var req struct {
		Src    string `json:"src"`
		PushTo string `json:"push_to"`
		NodeID string `json:"node_id"`
	}

	if err := c.Bind(&req); err != nil || req.Src == "" || req.PushTo == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "src and push_to required")
	}

	results, err := s.manager.PushOnce(req.Src, req.PushTo, req.NodeID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, map[string]any{"results": results})
}
