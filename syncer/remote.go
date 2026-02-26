package syncer

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"synco/logger"
	"synco/model"
	"synco/protocol"
	"time"

	"go.uber.org/zap"
)

type RemoteSyncer struct {
	src  string
	addr string
}

func NewRemoteSyncer(src, addr string) (*RemoteSyncer, error) {
	absSrc, err := filepath.Abs(src)
	if err != nil {
		return nil, fmt.Errorf("invalid src path: %w", err)
	}

	return &RemoteSyncer{src: absSrc, addr: addr}, nil
}

func (s *RemoteSyncer) Run(inCh <-chan model.FileEvent) <-chan model.SyncResult {
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

func (s *RemoteSyncer) handle(event model.FileEvent) model.SyncResult {
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

func (s *RemoteSyncer) sendFile(conn net.Conn, reader *bufio.Reader, path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("failed to stat file: %w", err)
	}

	checksum, err := protocol.FileChecksum(path)
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

	msg := protocol.Message{
		Type:     protocol.MessageSync,
		Path:     filepath.ToSlash(relPath),
		ModTime:  info.ModTime(),
		Checksum: checksum,
		Data:     data,
	}

	if err := protocol.WriteMessage(conn, msg); err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}

	resp, err := protocol.ReadResponse(reader)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	switch resp.Code {
	case protocol.ResponseOK:
		return nil
	case protocol.ResponseSkip:
		logger.Log.Debug("server skipped (checksum match)",
			zap.String("path", path))
		return nil
	case protocol.ResponseErr:
		return fmt.Errorf("server error :%s", resp.Msg)
	default:
		return fmt.Errorf("unknown response code: %d", resp.Code)
	}
}

func (s *RemoteSyncer) sendDelete(conn net.Conn, reader *bufio.Reader, path string) error {
	relPath, err := filepath.Rel(s.src, path)
	if err != nil || strings.HasPrefix(relPath, "..") {
		relPath = filepath.Base(path)
	}

	msg := protocol.Message{
		Type: protocol.MessageDelete,
		Path: filepath.ToSlash(relPath),
	}

	if err := protocol.WriteMessage(conn, msg); err != nil {
		return fmt.Errorf("failed to send delete: %w", err)
	}

	resp, err := protocol.ReadResponse(reader)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	if resp.Code == protocol.ResponseErr {
		return fmt.Errorf("server error: %s", resp.Msg)
	}

	return nil
}
