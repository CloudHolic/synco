package server

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"synco/logger"
	"synco/protocol"

	"go.uber.org/zap"
)

type TCPServer struct {
	dst      string
	addr     string
	listener net.Listener
	doneCh   chan struct{}
}

func New(dst, addr string) (*TCPServer, error) {
	absDst, err := filepath.Abs(dst)
	if err != nil {
		return nil, fmt.Errorf("invalid dst path: %w", err)
	}

	if err := os.MkdirAll(absDst, 0755); err != nil {
		return nil, fmt.Errorf("failed to create dst dir: %w", err)
	}

	return &TCPServer{
		dst:    absDst,
		addr:   addr,
		doneCh: make(chan struct{}),
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

	if existing, err := protocol.FileChecksum(dstPath); err != nil {
		if protocol.ChecksumEqual(existing, msg.Checksum) {
			_ = protocol.WriteResponse(conn, protocol.Response{Code: protocol.ResponseSkip})
			return
		}
	}

	if err := protocol.ValidateChecksum(msg.Data, msg.Checksum); err != nil {
		_ = protocol.WriteResponse(conn, protocol.Response{
			Code: protocol.ResponseErr,
			Msg:  "checksum validation failed",
		})
		return
	}

	if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
		_ = protocol.WriteResponse(conn, protocol.Response{
			Code: protocol.ResponseErr,
			Msg:  err.Error(),
		})
		return
	}

	tmpPath := dstPath + ".synco.tmp"
	if err := os.WriteFile(tmpPath, msg.Data, 0644); err != nil {
		_ = protocol.WriteResponse(conn, protocol.Response{
			Code: protocol.ResponseErr,
			Msg:  err.Error(),
		})
		return
	}

	if err := os.Rename(tmpPath, dstPath); err != nil {
		_ = os.Remove(tmpPath)
		_ = protocol.WriteResponse(conn, protocol.Response{
			Code: protocol.ResponseErr,
			Msg:  err.Error(),
		})
		return
	}

	logger.Log.Info("file synced",
		zap.String("path", dstPath),
		zap.Int("size", len(msg.Data)))

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
