package conflict

import (
	"fmt"
	"os"
	"path/filepath"
	"synco/logger"
	"synco/model"
	"time"

	"go.uber.org/zap"
)

type Resolver struct {
	strategy model.ConflictStrategy
}

func NewResolver(strategy model.ConflictStrategy) *Resolver {
	if strategy == "" {
		strategy = model.StrategyNewerWins
	}

	return &Resolver{strategy: strategy}
}

func (r *Resolver) DetectConflict(srcPath, dstPath string) (*model.ConflictInfo, error) {
	srcInfo, err := os.Stat(srcPath)
	if err != nil {
		return nil, nil // src가 없으면 충돌이 아님
	}

	dstInfo, err := os.Stat(dstPath)
	if err != nil {
		return nil, nil // dst가 없어도 충돌이 아님
	}

	// dst가 src보다 더 최근에 수정되었으면 충돌로 간주
	if dstInfo.ModTime().After(srcInfo.ModTime()) {
		return &model.ConflictInfo{
			Path:       srcPath,
			SrcModTime: srcInfo.ModTime(),
			DstModTime: dstInfo.ModTime(),
			Strategy:   r.strategy,
		}, nil
	}

	return nil, nil
}

func (r *Resolver) Resolve(conflict *model.ConflictInfo, srcPath, dstPath string) (bool, error) {
	logger.Log.Warn("conflict detected",
		zap.String("path", srcPath),
		zap.String("strategy", string(r.strategy)),
		zap.Time("src_mod", conflict.SrcModTime),
		zap.Time("dst_mod", conflict.DstModTime))

	switch r.strategy {
	case model.StrategyNewerWins:
		return r.resolveNewerWins(conflict)

	case model.StrategySourceWins:
		conflict.Resolved = true
		return true, nil

	case model.StrategyBackup:
		if err := r.backup(dstPath, conflict); err != nil {
			return false, err
		}
		conflict.Resolved = true
		return true, nil

	case model.StrategySkip:
		logger.Log.Info("conflict skipped",
			zap.String("path", srcPath))
		conflict.Resolved = false
		return false, nil

	default:
		return false, fmt.Errorf("unknown strategy: %s", r.strategy)
	}
}

func (r *Resolver) resolveNewerWins(conflict *model.ConflictInfo) (bool, error) {
	if conflict.SrcModTime.After(conflict.DstModTime) {
		conflict.Resolved = true
		logger.Log.Info("conflict resolved: src wins (newer)",
			zap.String("path", conflict.Path))
		return true, nil
	}

	logger.Log.Info("conflict resolved: dst wins (newer)",
		zap.String("path", conflict.Path))
	conflict.Resolved = false
	return false, nil
}

func (r *Resolver) backup(dstPath string, conflict *model.ConflictInfo) error {
	timestamp := time.Now().Format("20060102_150405")
	ext := filepath.Ext(dstPath)
	base := dstPath[:len(dstPath)-len(ext)]
	backupPath := fmt.Sprintf("%s.conflict_%s%s", base, timestamp, ext)

	if err := os.Rename(dstPath, backupPath); err != nil {
		return fmt.Errorf("failed to backup %s: %w", dstPath, err)
	}

	conflict.BackupPath = backupPath
	logger.Log.Info("conflict backup created",
		zap.String("original", dstPath),
		zap.String("backup", backupPath))

	return nil
}

func (r *Resolver) Strategy() model.ConflictStrategy {
	return r.strategy
}
