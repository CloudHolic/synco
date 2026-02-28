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

	"go.uber.org/zap"
	"google.golang.org/api/drive/v3"
)

type GDriveDownloadSyncer struct {
	folderID string
	dst      string
	svc      *drive.Service
	helper   *GDriveSyncer
}

func NewGDriveDownloadSyncer(jobID uint, folderPath, dst string) (*GDriveDownloadSyncer, error) {
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

	helper := &GDriveSyncer{svc: svc, idCache: make(map[string]string)}
	folderID, err := helper.ensureFolderPath(folderPath)
	if err != nil {
		return nil, fmt.Errorf("failed to find gdrive folder: %w", err)
	}
	helper.rootID = folderID

	return &GDriveDownloadSyncer{
		folderID: folderID,
		dst:      absDst,
		svc:      svc,
		helper:   helper,
	}, nil
}

func (s *GDriveDownloadSyncer) Run(inCh <-chan model.FileEvent) <-chan model.SyncResult {
	outCh := make(chan model.SyncResult, cap(inCh))

	go func() {
		defer close(outCh)

		for event := range inCh {
			result := s.handle(event)

			if result.Err != nil {
				logger.Log.Error("gdrive download failed",
					zap.String("path", event.Path),
					zap.Error(result.Err))
			} else {
				logger.Log.Info("gdrive downloaded",
					zap.String("path", event.Path),
					zap.String("dst", result.DstPath))
			}

			outCh <- result
		}
	}()

	return outCh
}

func (s *GDriveDownloadSyncer) handle(event model.FileEvent) model.SyncResult {
	localPath := filepath.Join(s.dst, filepath.FromSlash(event.Path))
	result := model.SyncResult{
		Event:   event,
		SrcPath: "gdrive:" + event.Path,
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

func (s *GDriveDownloadSyncer) downloadFile(relPath, localPath string) error {
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

	if err := os.MkdirAll(filepath.Dir(localPath), 0755); err != nil {
		return fmt.Errorf("failed to create dir: %w", err)
	}

	tmpPath := localPath + ".synco.tmp"
	f, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}

	if _, err := io.Copy(f, resp.Body); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to write to temp file: %w", err)
	}

	_ = f.Close()

	return os.Rename(tmpPath, localPath)
}

func (s *GDriveDownloadSyncer) removeLocal(localPath string) error {
	if err := os.Remove(localPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove: %w", err)
	}

	return nil
}
