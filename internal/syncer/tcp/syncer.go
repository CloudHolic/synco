package tcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"synco/internal/logger"
	"synco/internal/model"
	"synco/internal/retry"
	"synco/internal/syncer"
	"time"

	"go.uber.org/zap"
)

type Syncer struct {
	src      string
	addr     string
	dst      string
	nodeID   string
	vc       *Vclock
	strategy model.ConflictStrategy
	pull     bool
}

func NewSyncer(src, addr, nodeID string, vc *Vclock) (*Syncer, error) {
	absSrc, err := filepath.Abs(src)
	if err != nil {
		return nil, fmt.Errorf("invalid src path: %w", err)
	}

	return &Syncer{
		src:    absSrc,
		addr:   addr,
		nodeID: nodeID,
		vc:     vc,
	}, nil
}

func NewPullSyncer(src, addr, dst, nodeID string, strategy model.ConflictStrategy) (*Syncer, error) {
	absDst, err := filepath.Abs(dst)
	if err != nil {
		return nil, fmt.Errorf("invalid dst path: %w", err)
	}

	return &Syncer{
		src:      src,
		addr:     addr,
		dst:      absDst,
		nodeID:   nodeID,
		vc:       NewVclock(),
		pull:     true,
		strategy: strategy,
	}, nil
}

func (s *Syncer) Run(inCh <-chan model.FileEvent) <-chan model.SyncResult {
	if s.pull {
		out := make(chan model.SyncResult)
		close(out)
		return out
	}

	return syncer.RunLoop(inCh, s.handle)
}

func (s *Syncer) FullSync() ([]model.SyncResult, error) {
	if s.pull {
		srv, err := NewServer(s.dst, ":0", s.nodeID, s.strategy)
		if err != nil {
			return nil, err
		}

		if err := srv.Start(); err != nil {
			return nil, err
		}
		defer srv.Stop()

		ip, err := GetOutboundIP()
		if err != nil {
			return nil, err
		}

		pushTo := fmt.Sprintf("%s:%d", ip, srv.Port())

		return requestPushOnce(s.addr, s.src, pushTo, s.nodeID)
	}

	var results []model.SyncResult

	err := filepath.WalkDir(s.src, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}

		results = append(results, s.handle(model.FileEvent{
			Type:      model.EventWrite,
			Path:      path,
			Timestamp: time.Now(),
		}))

		return nil
	})

	return results, err
}

func (s *Syncer) handle(event model.FileEvent) model.SyncResult {
	result := model.SyncResult{
		Event:   event,
		SrcPath: event.Path,
		DstPath: s.addr,
	}

	err := retry.Do(context.Background(), retry.Default, func(attempt int) error {
		if attempt > 1 {
			logger.Log.Warn("tcp: retrying connection",
				zap.String("addr", s.addr),
				zap.Int("attempt", attempt))
		}

		return s.send(event)
	})

	if err != nil {
		logger.Log.Warn("tcp: sync failed after retries",
			zap.String("path", event.Path),
			zap.Error(err))

		result.Err = err
	} else {
		logger.Log.Info("tcp: synced",
			zap.String("type", string(event.Type)),
			zap.String("path", event.Path))
	}

	return result
}

func (s *Syncer) send(event model.FileEvent) error {
	conn, err := net.DialTimeout("tcp", s.addr, 5*time.Second)
	if err != nil {
		return fmt.Errorf("failed to connect to %s: %w", s.addr, err)
	}

	defer func(conn net.Conn) {
		_ = conn.Close()
	}(conn)

	reader := bufio.NewReader(conn)

	switch event.Type {
	case model.EventCreate, model.EventWrite:
		return s.sendFile(conn, reader, event.Path)
	case model.EventRemove, model.EventRename:
		return s.sendDelete(conn, reader, event.Path)
	}

	return nil
}

func (s *Syncer) sendFile(conn net.Conn, reader *bufio.Reader, path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("failed to stat file: %w", err)
	}

	checksum, err := FileChecksum(path)
	if err != nil {
		return fmt.Errorf("failed to compute checksum: %w", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	relPath, err := filepath.Rel(s.src, path)
	if err != nil || strings.HasPrefix(relPath, "..") {
		relPath = filepath.Base(path)
	}

	s.vc.Tick(s.nodeID)

	msg := Message{
		Type:     MessageSync,
		OriginID: s.nodeID,
		VClock:   s.vc.Snapshot(),
		Path:     filepath.ToSlash(relPath),
		ModTime:  info.ModTime(),
		Checksum: checksum,
		Data:     data,
	}

	if err := WriteMessage(conn, msg); err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}

	resp, err := ReadResponse(reader)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	if resp.Code == ResponseOK {
		s.vc.Merge(resp.VClock)
	}

	switch resp.Code {
	case ResponseOK:
		return nil
	case ResponseSkip:
		logger.Log.Debug("server skipped (checksum match)",
			zap.String("path", path))
		return nil
	case ResponseErr:
		return fmt.Errorf("server error :%s", resp.Msg)
	default:
		return fmt.Errorf("unknown response code: %d", resp.Code)
	}
}

func (s *Syncer) sendDelete(conn net.Conn, reader *bufio.Reader, path string) error {
	relPath, err := filepath.Rel(s.src, path)
	if err != nil || strings.HasPrefix(relPath, "..") {
		relPath = filepath.Base(path)
	}

	msg := Message{
		Type: MessageDelete,
		Path: filepath.ToSlash(relPath),
	}

	if err := WriteMessage(conn, msg); err != nil {
		return fmt.Errorf("failed to send delete: %w", err)
	}

	resp, err := ReadResponse(reader)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	if resp.Code == ResponseErr {
		return fmt.Errorf("server error: %s", resp.Msg)
	}

	return nil
}

func requestPushOnce(addr, src, pushTo, nodeID string) ([]model.SyncResult, error) {
	body := fmt.Sprintf(`{"src":"%s","push_to":"%s","node_id":"%s"}`, src, pushTo, nodeID)
	resp, err := PostRemoteDaemon(addr, "/jobs/push-once", nodeID, body)
	if err != nil {
		return nil, fmt.Errorf("failed to reach remote daemon at %s: %w", addr, err)
	}

	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	if resp.StatusCode != http.StatusOK {
		var errResp map[string]string
		_ = json.NewDecoder(resp.Body).Decode(&errResp)
		return nil, fmt.Errorf("remote rejected: %s", errResp["error"])
	}

	var response struct {
		Results []model.SyncResult `json:"results"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return response.Results, nil
}
