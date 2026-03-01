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
	"synco/internal/syncer"
	"synco/internal/util"

	"github.com/dropbox/dropbox-sdk-go-unofficial/v6/dropbox"
	"github.com/dropbox/dropbox-sdk-go-unofficial/v6/dropbox/files"
	"go.uber.org/zap"
)

type Downloader struct {
	folderPath string
	dst        string
	client     files.Client
}

func NewDownloader(folderPath, dst string) (*Downloader, error) {
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

	return &Downloader{
		folderPath: normalizePath(folderPath),
		dst:        absDst,
		client:     client,
	}, nil
}

func (s *Downloader) Run(inCh <-chan model.FileEvent) <-chan model.SyncResult {
	return syncer.RunLoop(inCh, s.handle)
}

func (s *Downloader) handle(event model.FileEvent) model.SyncResult {
	localPath := filepath.Join(s.dst, filepath.FromSlash(event.Path))
	result := model.SyncResult{
		Event:   event,
		SrcPath: "dropbox:" + event.Path,
		DstPath: localPath,
	}

	switch event.Type {
	case model.EventWrite, model.EventCreate:
		result.Err = s.downloadFile(event.Path, localPath)
	case model.EventRemove, model.EventRename:
		result.Err = util.RemoveIfExists(localPath)
	}

	if result.Err != nil {
		logger.Log.Error("dropbox download failed",
			zap.String("path", event.Path),
			zap.Error(result.Err))
	} else {
		logger.Log.Info("dropbox downloaded",
			zap.String("path", event.Path),
			zap.String("dst", result.DstPath))
	}

	return result
}

func (s *Downloader) downloadFile(relPath, localPath string) error {
	dropboxPath := s.folderPath + "/" + strings.TrimPrefix(relPath, "/")

	arg := files.NewDownloadArg(dropboxPath)
	_, content, err := s.client.Download(arg)
	if err != nil {
		return fmt.Errorf("failed to download from dropbox: %w", err)
	}

	defer func(content io.ReadCloser) {
		_ = content.Close()
	}(content)

	return util.AtomicWrite(localPath, content)
}
