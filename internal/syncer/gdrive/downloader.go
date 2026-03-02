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
	svc, err := auth.GDrive.NewService(ctx)
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

func (s *Downloader) FullSync() ([]model.SyncResult, error) {
	files, err := s.listAllFiles(s.folderID, "")
	if err != nil {
		return nil, fmt.Errorf("failed to list gdrive files: %w", err)
	}

	var results []model.SyncResult
	for _, f := range files {
		localPath := filepath.Join(s.dst, filepath.FromSlash(f.relPath))
		result := model.SyncResult{
			SrcPath: "gdrive:" + f.relPath,
			DstPath: localPath,
		}
		result.Err = s.downloadFile(f.relPath, localPath)
		results = append(results, result)
	}

	return results, nil
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

type gdriveFileEntry struct {
	fileID  string
	relPath string
}

func (s *Downloader) listAllFiles(parentID, prefix string) ([]gdriveFileEntry, error) {
	q := fmt.Sprintf("'%s' in parents and trashed=false", parentID)

	var entries []gdriveFileEntry
	pageToken := ""

	for {
		call := s.svc.Files.List().Q(q).Fields("nextPageToken, files(id, name, mimeType)")
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}

		resp, err := call.Do()
		if err != nil {
			return nil, err
		}

		for _, f := range resp.Files {
			relPath := f.Name
			if prefix != "" {
				relPath = prefix + "/" + f.Name
			}

			if f.MimeType == "application/vnd.google-apps.folder" {
				sub, err := s.listAllFiles(f.Id, relPath)
				if err != nil {
					return nil, err
				}

				entries = append(entries, sub...)
			} else {
				entries = append(entries, gdriveFileEntry{
					fileID:  f.Id,
					relPath: relPath,
				})
			}
		}

		if resp.NextPageToken == "" {
			break
		}

		pageToken = resp.NextPageToken
	}

	return entries, nil
}
