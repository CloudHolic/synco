package dropbox

import (
	"fmt"
	"os"
	"path/filepath"
	"synco/internal/auth"
	"synco/internal/logger"
	"synco/internal/model"
	"synco/internal/syncer"

	"github.com/dropbox/dropbox-sdk-go-unofficial/v6/dropbox"
	"github.com/dropbox/dropbox-sdk-go-unofficial/v6/dropbox/files"
	"go.uber.org/zap"
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
	relPath := s.relPath(localPath)
	dropboxPath := s.folderPath + "/" + relPath

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
