package tcp

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"synco/internal/logger"
	"synco/internal/model"
	"time"

	"go.uber.org/zap"
)

type Syncer struct {
	src    string
	addr   string
	nodeID string
	vc     *Vclock
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

func (s *Syncer) Run(inCh <-chan model.FileEvent) <-chan model.SyncResult {
	outCh := make(chan model.SyncResult, cap(inCh))

	go func() {
		defer close(outCh)

		for event := range inCh {
			result := s.handle(event)

			if result.Err != nil {
				logger.Log.Error("remote sync failed",
					zap.String("path", event.Path),
					zap.Error(result.Err))
			} else {
				logger.Log.Info("remote synced",
					zap.String("type", string(event.Type)),
					zap.String("path", event.Path))
			}

			outCh <- result
		}
	}()

	return outCh
}

func (s *Syncer) handle(event model.FileEvent) model.SyncResult {
	result := model.SyncResult{
		Event:   event,
		SrcPath: event.Path,
		DstPath: s.addr,
	}

	conn, err := net.DialTimeout("tcp", s.addr, 5*time.Second)
	if err != nil {
		result.Err = fmt.Errorf("failed to connect to %s: %w", s.addr, err)
		return result
	}

	defer func(conn net.Conn) {
		_ = conn.Close()
	}(conn)

	reader := bufio.NewReader(conn)

	switch event.Type {
	case model.EventCreate, model.EventWrite:
		result.Err = s.sendFile(conn, reader, event.Path)
	case model.EventRemove, model.EventRename:
		result.Err = s.sendDelete(conn, reader, event.Path)
	}

	return result
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
