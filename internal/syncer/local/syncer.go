package local

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"synco/internal/logger"
	"synco/internal/model"
	"synco/internal/syncer"
	"synco/internal/syncer/conflict"
	"synco/internal/util"

	"go.uber.org/zap"
)

type Syncer struct {
	src      string
	dst      string
	resolver *conflict.Resolver
}

func NewSyncer(src, dst string, strategy model.ConflictStrategy) (*Syncer, error) {
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

	return &Syncer{
		src:      absSrc,
		dst:      absDst,
		resolver: conflict.NewResolver(strategy),
	}, nil
}

func (s *Syncer) Run(inCh <-chan model.FileEvent) <-chan model.SyncResult {
	return syncer.RunLoop(inCh, s.handle)
}

func (s *Syncer) FullSync() ([]model.SyncResult, error) {
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

func (s *Syncer) handle(event model.FileEvent) model.SyncResult {
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
		result.Err = util.RemoveIfExists(dstPath)

	case model.EventRename:
		// Rename의 경우 이전 경로 삭제 + 새 경로 복사로 처리
		// fsnotify는 rename 시에 이전 경로만 알려주므로 해당 경로는 삭제만 수행함
		result.Err = util.RemoveIfExists(dstPath)
	}

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

	return result
}

func (s *Syncer) copyFile(src, dst string) error {
	f, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open src: %w", err)
	}

	defer func(f *os.File) {
		_ = f.Close()
	}(f)

	return util.AtomicWrite(dst, f)
}

func (s *Syncer) toDst(srcPath string) string {
	rel, err := filepath.Rel(s.src, srcPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		return filepath.Join(s.dst, filepath.Base(srcPath))
	}

	return filepath.Join(s.dst, rel)
}
