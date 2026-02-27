package server

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"synco/internal/conflict"
	"synco/internal/logger"
	"synco/internal/model"
	"synco/internal/protocol"
	"synco/internal/vclock"

	"go.uber.org/zap"
)

type TCPServer struct {
	dst      string
	addr     string
	nodeID   string
	vc       *vclock.Vclock
	resolver *conflict.Resolver
	listener net.Listener
	doneCh   chan struct{}

	peerAddr string
	peerSrc  string
}

func New(dst, addr, nodeID string, strategy model.ConflictStrategy) (*TCPServer, error) {
	absDst, err := filepath.Abs(dst)
	if err != nil {
		return nil, fmt.Errorf("invalid dst path: %w", err)
	}

	if err := os.MkdirAll(absDst, 0755); err != nil {
		return nil, fmt.Errorf("failed to create dst dir: %w", err)
	}

	return &TCPServer{
		dst:      absDst,
		addr:     addr,
		nodeID:   nodeID,
		vc:       vclock.New(),
		resolver: conflict.NewResolver(strategy),
		doneCh:   make(chan struct{}),
	}, nil
}

func (s *TCPServer) Start() error {
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

func (s *TCPServer) Stop() {
	close(s.doneCh)
	if s.listener != nil {
		_ = s.listener.Close()
	}
}

func (s *TCPServer) accept() {
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

func (s *TCPServer) handleConn(conn net.Conn) {
	defer func(conn net.Conn) {
		_ = conn.Close()
	}(conn)

	reader := bufio.NewReader(conn)
	msg, err := protocol.ReadMessage(reader)
	if err != nil {
		logger.Log.Error("failed to read message", zap.Error(err))
		_ = protocol.WriteResponse(conn, protocol.Response{
			Code: protocol.ResponseErr,
			Msg:  err.Error(),
		})
		return
	}

	switch msg.Type {
	case protocol.MessageSync:
		s.handleSync(conn, msg)
	case protocol.MessageDelete:
		s.handleDelete(conn, msg)
	case protocol.MessagePing:
		_ = protocol.WriteResponse(conn, protocol.Response{Code: protocol.ResponseOK})
	default:
		_ = protocol.WriteResponse(conn, protocol.Response{
			Code: protocol.ResponseErr,
			Msg:  fmt.Sprintf("unknown message type: %d", msg.Type),
		})
	}
}

func (s *TCPServer) handleSync(conn net.Conn, msg protocol.Message) {
	dstPath := filepath.Join(s.dst, filepath.FromSlash(msg.Path))

	if existing, err := protocol.FileChecksum(dstPath); err == nil {
		if protocol.ChecksumEqual(existing, msg.Checksum) {
			_ = protocol.WriteResponse(conn, protocol.Response{Code: protocol.ResponseSkip})
			return
		}
	}

	localVC := s.vc.Snapshot()
	relation := vclock.Compare(msg.VClock, localVC)

	switch relation {
	case vclock.Before:
		logger.Log.Debug("skipping outdated message",
			zap.String("path", msg.Path))

		_ = protocol.WriteResponse(conn, protocol.Response{Code: protocol.ResponseSkip})
		return

	case vclock.Concurrent:
		if dstInfo, err := os.Stat(dstPath); err == nil {
			conflictInfo := &model.ConflictInfo{
				Path:       msg.Path,
				SrcModTime: msg.ModTime,
				DstModTime: dstInfo.ModTime(),
				Strategy:   s.resolver.Strategy(),
			}
			proceed, err := s.resolver.Resolve(conflictInfo, "", dstPath)
			if err != nil || !proceed {
				_ = protocol.WriteResponse(conn, protocol.Response{
					Code: protocol.ResponseSkip,
					Msg:  "conflict: resolved as skip",
				})
				return
			}
		}

	case vclock.After:
	}

	s.vc.Merge(msg.VClock)
	s.vc.Tick(s.nodeID)

	if err := protocol.ValidateChecksum(msg.Data, msg.Checksum); err != nil {
		_ = protocol.WriteResponse(conn, protocol.Response{
			Code: protocol.ResponseErr,
			Msg:  "checksum validation failed",
		})
		return
	}

	if err := s.writeFile(dstPath, msg.Data); err != nil {
		_ = protocol.WriteResponse(conn, protocol.Response{
			Code: protocol.ResponseErr,
			Msg:  err.Error(),
		})
		return
	}

	logger.Log.Info("file synced",
		zap.String("path", dstPath),
		zap.Int("size", len(msg.Data)),
		zap.String("origin", msg.OriginID))

	_ = protocol.WriteResponse(conn, protocol.Response{Code: protocol.ResponseOK})
}

func (s *TCPServer) handleDelete(conn net.Conn, msg protocol.Message) {
	dstPath := filepath.Join(s.dst, msg.Path)

	if err := os.Remove(dstPath); err != nil && !os.IsNotExist(err) {
		_ = protocol.WriteResponse(conn, protocol.Response{
			Code: protocol.ResponseErr,
			Msg:  err.Error(),
		})

		return
	}

	logger.Log.Info("file deleted",
		zap.String("path", dstPath))

	_ = protocol.WriteResponse(conn, protocol.Response{Code: protocol.ResponseOK})
}

func (s *TCPServer) writeFile(dstPath string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
		return fmt.Errorf("failed to create parent dir: %w", err)
	}

	tmpPath := dstPath + ".synco.tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return err
	}

	if err := os.Rename(tmpPath, dstPath); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}

	return nil
}

func (s *TCPServer) SetPeer(peerAddr, peerSrc string) {
	s.peerAddr = peerAddr
	s.peerSrc = peerSrc
}
