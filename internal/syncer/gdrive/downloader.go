package gdrive

import (
	"context"
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

	"go.uber.org/zap"
	"google.golang.org/api/drive/v3"
)

type Downloader struct {
	folderID string
	dst      string
	svc      *drive.Service
	helper   *Uploader
}

func NewDownloader(folderPath, dst string) (*Downloader, error) {
	absDst, err := filepath.Abs(dst)
	if err != nil {
		return nil, fmt.Errorf("invalid dst path: %w", err)
	}
	if err := os.MkdirAll(absDst, 0755); err != nil {
		return nil, fmt.Errorf("failed to create dst dir: %w", err)
	}

	ctx := context.Background()
	svc, err := auth.NewDriveService(ctx)
	if err != nil {
		return nil, err
	}

	helper := &Uploader{svc: svc, idCache: make(map[string]string)}
	folderID, err := helper.ensureFolderPath(folderPath)
	if err != nil {
		return nil, fmt.Errorf("failed to find gdrive folder: %w", err)
	}
	helper.rootID = folderID

	return &Downloader{
		folderID: folderID,
		dst:      absDst,
		svc:      svc,
		helper:   helper,
	}, nil
}

func (s *Downloader) Run(inCh <-chan model.FileEvent) <-chan model.SyncResult {
	return syncer.RunLoop(inCh, s.handle)
}

func (s *Downloader) handle(event model.FileEvent) model.SyncResult {
	localPath := filepath.Join(s.dst, filepath.FromSlash(event.Path))
	result := model.SyncResult{
		Event:   event,
		SrcPath: "gdrive:" + event.Path,
		DstPath: localPath,
	}

	switch event.Type {
	case model.EventWrite, model.EventCreate:
		result.Err = s.downloadFile(event.Path, localPath)
	case model.EventRemove, model.EventRename:
		result.Err = util.RemoveIfExists(localPath)
	}

	if result.Err != nil {
		logger.Log.Error("gdrive download failed",
			zap.String("path", event.Path),
			zap.Error(result.Err))
	} else {
		logger.Log.Info("gdrive downloaded",
			zap.String("path", event.Path),
			zap.String("dst", result.DstPath))
	}

	return result
}

func (s *Downloader) downloadFile(relPath, localPath string) error {
	parts := strings.Split(relPath, "/")
	fileName := parts[len(parts)-1]
	dirParts := parts[:len(parts)-1]

	parentID := s.folderID
	for _, dir := range dirParts {
		id, err := s.helper.findFolder(dir, parentID)
		if err != nil || id == "" {
			return fmt.Errorf("folder not found: %s", dir)
		}
		parentID = id
	}

	fileID, err := s.helper.findFile(fileName, parentID)
	if err != nil || fileID == "" {
		return fmt.Errorf("file not found on gdrive: %s", relPath)
	}

	resp, err := s.svc.Files.Get(fileID).Download()
	if err != nil {
		return fmt.Errorf("failed to download: %w", err)
	}

	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	return util.AtomicWrite(localPath, resp.Body)
}
