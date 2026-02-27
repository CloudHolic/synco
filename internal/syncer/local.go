package syncer

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"synco/internal/conflict"
	"synco/internal/logger"
	"synco/internal/model"

	"go.uber.org/zap"
)

type LocalSyncer struct {
	src      string
	dst      string
	resolver *conflict.Resolver
}

func NewLocalSyncer(src, dst string, strategy model.ConflictStrategy) (*LocalSyncer, error) {
	absSrc, err := filepath.Abs(src)
	if err != nil {
		return nil, fmt.Errorf("invalid src path: %w", err)
	}
	absDst, err := filepath.Abs(dst)
	if err != nil {
		return nil, fmt.Errorf("invalid dst path: %w", err)
	}

	if err := os.MkdirAll(absDst, 0755); err != nil {
		return nil, fmt.Errorf("failed to create dst dir: %w", err)
	}

	return &LocalSyncer{
		src:      absSrc,
		dst:      absDst,
		resolver: conflict.NewResolver(strategy),
	}, nil
}

func (s *LocalSyncer) Run(inCh <-chan model.FileEvent) <-chan model.SyncResult {
	outCh := make(chan model.SyncResult, cap(inCh))

	go func() {
		defer close(outCh)

		for event := range inCh {
			result := s.handle(event)

			if result.Err != nil {
				logger.Log.Error("sync failed",
					zap.String("type", string(event.Type)),
					zap.String("path", event.Path),
					zap.Error(result.Err))
			} else {
				logger.Log.Info("synced",
					zap.String("type", string(event.Type)),
					zap.String("src", result.SrcPath),
					zap.String("dst", result.DstPath))
			}

			outCh <- result
		}
	}()

	return outCh
}

func (s *LocalSyncer) FullSync() ([]model.SyncResult, error) {
	var results []model.SyncResult

	err := filepath.WalkDir(s.src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		dstPath := s.toDst(path)

		if d.IsDir() {
			return os.MkdirAll(dstPath, 0755)
		}

		event := model.FileEvent{
			Type: model.EventWrite,
			Path: path,
		}
		result := s.handle(event)
		results = append(results, result)
		return nil
	})

	return results, err
}

func (s *LocalSyncer) handle(event model.FileEvent) model.SyncResult {
	dstPath := s.toDst(event.Path)
	result := model.SyncResult{
		Event:   event,
		SrcPath: event.Path,
		DstPath: dstPath,
	}

	switch event.Type {
	case model.EventCreate, model.EventWrite:
		conflictInfo, err := s.resolver.DetectConflict(event.Path, dstPath)
		if err != nil {
			result.Err = err
			return result
		}

		if conflictInfo != nil {
			proceed, err := s.resolver.Resolve(conflictInfo, event.Path, dstPath)
			if err != nil {
				result.Err = err
				return result
			}

			if !proceed {
				result.Conflict = conflictInfo
				return result
			}
		}

		result.Err = s.copyFile(event.Path, dstPath)

	case model.EventRemove:
		result.Err = s.removeFile(dstPath)

	case model.EventRename:
		// Rename의 경우 이전 경로 삭제 + 새 경로 복사로 처리
		// fsnotify는 rename 시에 이전 경로만 알려주므로 해당 경로는 삭제만 수행함
		result.Err = s.removeFile(dstPath)
	}

	return result
}

func (s *LocalSyncer) copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return fmt.Errorf("failed to create parent dir: %w", err)
	}

	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open src: %w", err)
	}

	defer func(srcFile *os.File) {
		_ = srcFile.Close()
	}(srcFile)

	info, err := srcFile.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat src: %w", err)
	}

	tmpPath := dst + ".synco.tmp"
	dstFile, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
	if err != nil {
		return fmt.Errorf("failed to open dst: %w", err)
	}

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		_ = dstFile.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to copy: %w", err)
	}

	if err := dstFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to close dst: %w", err)
	}

	if err := os.Rename(tmpPath, dst); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to rename tmp: %w", err)
	}

	return nil
}

func (s *LocalSyncer) removeFile(dst string) error {
	if err := os.Remove(dst); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove: %w", err)
	}

	return nil
}

func (s *LocalSyncer) toDst(srcPath string) string {
	rel, err := filepath.Rel(s.src, srcPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		return filepath.Join(s.dst, filepath.Base(srcPath))
	}

	return filepath.Join(s.dst, rel)
}
