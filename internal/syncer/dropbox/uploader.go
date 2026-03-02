package dropbox

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"synco/internal/auth"
	"synco/internal/logger"
	"synco/internal/model"
	"synco/internal/retry"
	"synco/internal/syncer"
	"time"

	"github.com/dropbox/dropbox-sdk-go-unofficial/v6/dropbox"
	"github.com/dropbox/dropbox-sdk-go-unofficial/v6/dropbox/files"
	"go.uber.org/zap"
)

const (
	sessionThreshold = 150 * 1024 * 1024 // 150MB
	chunkSize        = 100 * 1024 * 1024 // 100MB
)

type Uploader struct {
	src        string
	folderPath string
	client     files.Client
}

func NewUploader(src, folderPath string) (*Uploader, error) {
	absSrc, err := filepath.Abs(src)
	if err != nil {
		return nil, fmt.Errorf("invalid src path: %w", err)
	}

	client, err := auth.Dropbox.NewClient()
	if err != nil {
		return nil, err
	}

	folderPath = normalizePath(folderPath)
	if err := ensureFolder(client, folderPath); err != nil {
		return nil, fmt.Errorf("failed to prepare dropbox folder: %w", err)
	}

	logger.Log.Info("dropbox syncer ready",
		zap.String("src", absSrc),
		zap.String("folder", folderPath))

	return &Uploader{
		src:        absSrc,
		folderPath: folderPath,
		client:     client,
	}, nil
}

func (s *Uploader) Run(inCh <-chan model.FileEvent) <-chan model.SyncResult {
	return syncer.RunLoop(inCh, s.handle)
}

func (s *Uploader) FullSync() ([]model.SyncResult, error) {
	var results []model.SyncResult

	err := filepath.WalkDir(s.src, func(path string, d fs.DirEntry, err error) error {
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

func (s *Uploader) handle(event model.FileEvent) model.SyncResult {
	result := model.SyncResult{
		Event:   event,
		SrcPath: event.Path,
		DstPath: "dropbox:" + s.folderPath,
	}

	switch event.Type {
	case model.EventCreate, model.EventWrite:
		result.Err = s.uploadFile(event.Path)
	case model.EventRemove, model.EventRename:
		result.Err = s.deleteFile(event.Path)
	}

	if result.Err != nil {
		logger.Log.Error("dropbox sync failed",
			zap.String("path", event.Path),
			zap.Error(result.Err))
	} else {
		logger.Log.Info("dropbox synced",
			zap.String("type", string(event.Type)),
			zap.String("path", event.Path))
	}

	return result
}

func (s *Uploader) uploadFile(localPath string) error {
	info, err := os.Stat(localPath)
	if err != nil {
		return fmt.Errorf("failed to stat file: %w", err)
	}

	if info.Size() < sessionThreshold {
		return s.uploadSmallFile(localPath)
	}

	return s.uploadLargeFile(localPath, info.Size())
}

func (s *Uploader) uploadSmallFile(localPath string) error {
	dropboxPath := s.folderPath + "/" + s.relPath(localPath)

	f, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}

	defer func(f *os.File) {
		_ = f.Close()
	}(f)

	arg := files.NewUploadArg(dropboxPath)
	arg.Mode = &files.WriteMode{Tagged: dropbox.Tagged{Tag: "overwrite"}}
	arg.Autorename = false

	if _, err := s.client.Upload(arg, f); err != nil {
		return fmt.Errorf("failed to upload to dropbox: %w", err)
	}

	return nil
}

func (s *Uploader) uploadLargeFile(localPath string, totalSize int64) error {
	dropboxPath := s.folderPath + "/" + s.relPath(localPath)

	logger.Log.Info("starting upload session",
		zap.String("file", filepath.Base(localPath)),
		zap.Int64("size_mb", totalSize/1024/1024))

	f, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}

	defer func(f *os.File) {
		_ = f.Close()
	}(f)

	firstChunk := io.LimitReader(f, chunkSize)
	startArg := files.NewUploadSessionStartArg()
	startArg.Close = false

	session, err := s.client.UploadSessionStart(startArg, firstChunk)
	if err != nil {
		return fmt.Errorf("failed to start upload session: %w", err)
	}

	sessionID := session.SessionId
	offset := uint64(chunkSize)
	remaining := totalSize - chunkSize

	for remaining > chunkSize {
		chunk := io.LimitReader(f, chunkSize)
		cursor := files.NewUploadSessionCursor(sessionID, offset)
		appendArg := files.NewUploadSessionAppendArg(cursor)
		appendArg.Close = false

		if err := retry.Do(nil, retry.Config{
			MaxAttempts: 3,
			BaseDelay:   2 * time.Second,
			MaxDelay:    30 * time.Second,
		}, func(attempt int) error {
			if attempt > 1 {
				// 실패시 다시 읽어서 제공해야 함
				if _, err := f.Seek(int64(offset), io.SeekStart); err != nil {
					return fmt.Errorf("failed to seek: %w", err)
				}
				chunk = io.LimitReader(f, chunkSize)
			}

			return s.client.UploadSessionAppendV2(appendArg, chunk)
		}); err != nil {
			return fmt.Errorf("failed to append chunk at offset %d: %w", offset, err)
		}

		offset += uint64(chunkSize)
		remaining -= chunkSize
	}

	lastChunk := io.LimitReader(f, remaining)
	cursor := files.NewUploadSessionCursor(sessionID, offset)
	commitInfo := files.NewCommitInfo(dropboxPath)
	commitInfo.Mode = &files.WriteMode{Tagged: dropbox.Tagged{Tag: "overwrite"}}
	commitInfo.Autorename = false
	finishArg := files.NewUploadSessionFinishArg(cursor, commitInfo)

	if err := retry.Do(nil, retry.Config{
		MaxAttempts: 3,
		BaseDelay:   2 * time.Second,
		MaxDelay:    30 * time.Second,
	}, func(attempt int) error {
		if attempt > 1 {
			if _, err := f.Seek(int64(offset), io.SeekStart); err != nil {
				return fmt.Errorf("failed to seek: %w", err)
			}

			lastChunk = io.LimitReader(f, remaining)
		}

		_, err := s.client.UploadSessionFinish(finishArg, lastChunk)
		return err
	}); err != nil {
		return fmt.Errorf("failed to finish upload session: %w", err)
	}

	logger.Log.Info("upload session complete",
		zap.String("file", filepath.Base(localPath)))

	return nil
}

func (s *Uploader) deleteFile(localPath string) error {
	relPath := s.relPath(localPath)
	dropboxPath := s.folderPath + "/" + relPath

	arg := files.NewDeleteArg(dropboxPath)
	if _, err := s.client.DeleteV2(arg); err != nil {
		if isNotFound(err) {
			return nil
		}

		return fmt.Errorf("failed to delete from dropbox: %w", err)
	}

	return nil
}

func (s *Uploader) relPath(localPath string) string {
	rel, err := filepath.Rel(s.src, localPath)
	if err != nil {
		return filepath.Base(localPath)
	}

	return filepath.ToSlash(rel)
}
