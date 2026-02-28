package dropbox

import (
	"fmt"
	"io"
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

type DropboxDownloadSyncer struct {
	folderPath string
	dst        string
	client     files.Client
}

func NewDropboxDownloadSyncer(folderPath, dst string) (*DropboxDownloadSyncer, error) {
	absDst, err := filepath.Abs(dst)
	if err != nil {
		return nil, fmt.Errorf("invalid dst path: %w", err)
	}

	if err := os.MkdirAll(absDst, 0755); err != nil {
		return nil, fmt.Errorf("failed to create dst dir: %w", err)
	}

	token, err := auth.NewDropboxToken()
	if err != nil {
		return nil, err
	}

	cfg := dropbox.Config{Token: token.AccessToken}
	client := files.New(cfg)

	return &DropboxDownloadSyncer{
		folderPath: normalizeDropboxPath(folderPath),
		dst:        absDst,
		client:     client,
	}, nil
}

func (s *DropboxDownloadSyncer) Run(inCh <-chan model.FileEvent) <-chan model.SyncResult {
	outCh := make(chan model.SyncResult, cap(inCh))

	go func() {
		defer close(outCh)

		for event := range inCh {
			result := s.handle(event)

			if result.Err != nil {
				logger.Log.Error("dropbox download failed",
					zap.String("path", event.Path),
					zap.Error(result.Err))
			} else {
				logger.Log.Info("dropbox downloaded",
					zap.String("path", event.Path),
					zap.String("dst", result.DstPath))
			}

			outCh <- result
		}
	}()

	return outCh
}

func (s *DropboxDownloadSyncer) handle(event model.FileEvent) model.SyncResult {
	localPath := filepath.Join(s.dst, filepath.FromSlash(event.Path))
	result := model.SyncResult{
		Event:   event,
		SrcPath: "dropbox:" + event.Path,
		DstPath: localPath,
	}

	switch event.Type {
	case model.EventWrite, model.EventCreate:
		result.Err = s.downloadFile(event.Path, localPath)
	case model.EventRemove:
		result.Err = s.removeLocal(localPath)
	}

	return result
}

func (s *DropboxDownloadSyncer) downloadFile(relPath, localPath string) error {
	dropboxPath := s.folderPath + "/" + strings.TrimPrefix(relPath, "/")

	arg := files.NewDownloadArg(dropboxPath)
	_, content, err := s.client.Download(arg)
	if err != nil {
		return fmt.Errorf("failed to download from dropbox: %w", err)
	}

	defer func(content io.ReadCloser) {
		_ = content.Close()
	}(content)

	if err := os.MkdirAll(filepath.Dir(localPath), 0755); err != nil {
		return fmt.Errorf("failed to create dir: %w", err)
	}

	tmpPath := localPath + ".synco.tmp"
	f, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}

	if _, err := io.Copy(f, content); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to write file: %w", err)
	}

	_ = f.Close()

	return os.Rename(tmpPath, localPath)
}

func (s *DropboxDownloadSyncer) removeLocal(localPath string) error {
	if err := os.Remove(localPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove: %w", err)
	}
	return nil
}
