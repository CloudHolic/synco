package tcp

import (
	"bufio"
	"bytes"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"synco/internal/logger"
	"synco/internal/model"
	"synco/internal/syncer/conflict"
	"synco/internal/util"

	"go.uber.org/zap"
)

type Server struct {
	dst      string
	addr     string
	nodeID   string
	vc       *Vclock
	resolver *conflict.Resolver
	listener net.Listener
	doneCh   chan struct{}
}

func NewServer(dst, addr, nodeID string, strategy model.ConflictStrategy) (*Server, error) {
	absDst, err := filepath.Abs(dst)
	if err != nil {
		return nil, fmt.Errorf("invalid dst path: %w", err)
	}

	if err := os.MkdirAll(absDst, 0755); err != nil {
		return nil, fmt.Errorf("failed to create dst dir: %w", err)
	}

	return &Server{
		dst:      absDst,
		addr:     addr,
		nodeID:   nodeID,
		vc:       NewVclock(),
		resolver: conflict.NewResolver(strategy),
		doneCh:   make(chan struct{}),
	}, nil
}

func (s *Server) Start() error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", s.addr, err)
	}

	s.listener = ln

	logger.Log.Info("tcp server started",
		zap.String("addr", s.addr),
		zap.String("dst", s.dst))

	go s.accept()
	return nil
}

func (s *Server) Stop() {
	close(s.doneCh)
	if s.listener != nil {
		_ = s.listener.Close()
	}
}

func (s *Server) accept() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.doneCh:
				return
			default:
				logger.Log.Error("accept error", zap.Error(err))
				continue
			}
		}

		go s.handleConn(conn)
	}
}

func (s *Server) handleConn(conn net.Conn) {
	defer func(conn net.Conn) {
		_ = conn.Close()
	}(conn)

	reader := bufio.NewReader(conn)
	msg, err := ReadMessage(reader)
	if err != nil {
		logger.Log.Error("failed to read message", zap.Error(err))
		_ = WriteResponse(conn, Response{
			Code: ResponseErr,
			Msg:  err.Error(),
		})
		return
	}

	switch msg.Type {
	case MessageSync:
		s.handleSync(conn, msg)
	case MessageDelete:
		s.handleDelete(conn, msg)
	case MessagePing:
		_ = WriteResponse(conn, Response{Code: ResponseOK})
	default:
		_ = WriteResponse(conn, Response{
			Code: ResponseErr,
			Msg:  fmt.Sprintf("unknown message type: %d", msg.Type),
		})
	}
}

func (s *Server) handleSync(conn net.Conn, msg Message) {
	dstPath := filepath.Join(s.dst, filepath.FromSlash(msg.Path))

	if existing, err := FileChecksum(dstPath); err == nil {
		if ChecksumEqual(existing, msg.Checksum) {
			_ = WriteResponse(conn, Response{Code: ResponseSkip})
			return
		}
	}

	localVC := s.vc.Snapshot()
	relation := Compare(msg.VClock, localVC)

	switch relation {
	case Before:
		logger.Log.Debug("skipping outdated message",
			zap.String("path", msg.Path))

		_ = WriteResponse(conn, Response{Code: ResponseSkip})
		return

	case Concurrent:
		if dstInfo, err := os.Stat(dstPath); err == nil {
			conflictInfo := &model.ConflictInfo{
				Path:       msg.Path,
				SrcModTime: msg.ModTime,
				DstModTime: dstInfo.ModTime(),
				Strategy:   s.resolver.Strategy(),
			}
			proceed, err := s.resolver.Resolve(conflictInfo, "", dstPath)
			if err != nil || !proceed {
				_ = WriteResponse(conn, Response{
					Code: ResponseSkip,
					Msg:  "conflict: resolved as skip",
				})
				return
			}
		}

	case After:
	}

	s.vc.Merge(msg.VClock)
	s.vc.Tick(s.nodeID)

	if err := ValidateChecksum(msg.Data, msg.Checksum); err != nil {
		_ = WriteResponse(conn, Response{
			Code: ResponseErr,
			Msg:  "checksum validation failed",
		})
		return
	}

	if err := util.AtomicWrite(dstPath, bytes.NewReader(msg.Data)); err != nil {
		_ = WriteResponse(conn, Response{
			Code: ResponseErr,
			Msg:  err.Error(),
		})
		return
	}

	logger.Log.Info("file synced",
		zap.String("path", dstPath),
		zap.Int("size", len(msg.Data)),
		zap.String("origin", msg.OriginID))

	_ = WriteResponse(conn, Response{Code: ResponseOK})
}

func (s *Server) handleDelete(conn net.Conn, msg Message) {
	dstPath := filepath.Join(s.dst, msg.Path)

	if err := os.Remove(dstPath); err != nil && !os.IsNotExist(err) {
		_ = WriteResponse(conn, Response{
			Code: ResponseErr,
			Msg:  err.Error(),
		})

		return
	}

	logger.Log.Info("file deleted",
		zap.String("path", dstPath))

	_ = WriteResponse(conn, Response{Code: ResponseOK})
}
