package dropbox

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"synco/internal/auth"
	"synco/internal/logger"
	"synco/internal/model"

	"github.com/dropbox/dropbox-sdk-go-unofficial/v6/dropbox"
	"github.com/dropbox/dropbox-sdk-go-unofficial/v6/dropbox/files"
	"go.uber.org/zap"
)

type DropboxSyncer struct {
	src        string
	folderPath string
	client     files.Client
}

func NewDropboxSyncer(src, folderPath string) (*DropboxSyncer, error) {
	absSrc, err := filepath.Abs(src)
	if err != nil {
		return nil, fmt.Errorf("invalid src path: %w", err)
	}

	token, err := auth.NewDropboxToken()
	if err != nil {
		return nil, err
	}

	cfg := dropbox.Config{Token: token.AccessToken}
	client := files.New(cfg)

	folderPath = normalizeDropboxPath(folderPath)
	if err := ensureDropboxFolder(client, folderPath); err != nil {
		return nil, fmt.Errorf("failed to prepare dropbox folder: %w", err)
	}

	logger.Log.Info("dropbox syncer ready",
		zap.String("src", absSrc),
		zap.String("folder", folderPath))

	return &DropboxSyncer{
		src:        absSrc,
		folderPath: folderPath,
		client:     client,
	}, nil
}

func (s *DropboxSyncer) Run(inCh <-chan model.FileEvent) <-chan model.SyncResult {
	outCh := make(chan model.SyncResult, cap(inCh))

	go func() {
		defer close(outCh)

		for event := range inCh {
			result := s.handle(event)

			if result.Err != nil {
				logger.Log.Error("dropbox sync failed",
					zap.String("path", event.Path),
					zap.Error(result.Err))
			} else {
				logger.Log.Info("dropbox synced",
					zap.String("type", string(event.Type)),
					zap.String("path", event.Path))
			}

			outCh <- result
		}
	}()

	return outCh
}

func (s *DropboxSyncer) handle(event model.FileEvent) model.SyncResult {
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

	return result
}

func (s *DropboxSyncer) uploadFile(localPath string) error {
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

func (s *DropboxSyncer) deleteFile(localPath string) error {
	relPath := s.relPath(localPath)
	dropboxPath := s.folderPath + "/" + relPath

	arg := files.NewDeleteArg(dropboxPath)
	if _, err := s.client.DeleteV2(arg); err != nil {
		if isDropboxNotFound(err) {
			return nil
		}

		return fmt.Errorf("failed to delete from dropbox: %w", err)
	}

	return nil
}

func (s *DropboxSyncer) relPath(localPath string) string {
	rel, err := filepath.Rel(s.src, localPath)
	if err != nil {
		return filepath.Base(localPath)
	}

	return filepath.ToSlash(rel)
}

func ensureDropboxFolder(client files.Client, path string) error {
	arg := files.NewCreateFolderArg(path)
	arg.Autorename = false

	if _, err := client.CreateFolderV2(arg); err != nil {
		if isDropboxConflict(err) {
			return nil
		}

		return err
	}

	return nil
}

func normalizeDropboxPath(p string) string {
	p = "/" + strings.Trim(filepath.ToSlash(p), "/")
	return p
}

func isDropboxNotFound(err error) bool {
	if apiErr, ok := errors.AsType[files.DeleteV2APIError](err); ok {
		return apiErr.EndpointError != nil &&
			apiErr.EndpointError.PathLookup != nil &&
			apiErr.EndpointError.PathLookup.Tag == "not_found"
	}

	return false
}

func isDropboxConflict(err error) bool {
	if apiErr, ok := errors.AsType[files.CreateFolderV2APIError](err); ok {
		return apiErr.EndpointError != nil &&
			apiErr.EndpointError.Path != nil &&
			apiErr.EndpointError.Path.Tag == "conflict"
	}

	return false
}
