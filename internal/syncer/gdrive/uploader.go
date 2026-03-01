package gdrive

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"synco/internal/auth"
	"synco/internal/logger"
	"synco/internal/model"
	"synco/internal/syncer"

	"go.uber.org/zap"
	"google.golang.org/api/drive/v3"
)

type Uploader struct {
	mu         sync.RWMutex
	src        string
	folderPath string
	svc        *drive.Service
	rootID     string
	idCache    map[string]string
}

func NewUploader(src, folderPath string) (*Uploader, error) {
	absSrc, err := filepath.Abs(src)
	if err != nil {
		return nil, fmt.Errorf("invalid src path: %w", err)
	}

	ctx := context.Background()
	svc, err := auth.NewDriveService(ctx)
	if err != nil {
		return nil, err
	}

	s := &Uploader{
		src:        absSrc,
		folderPath: strings.TrimPrefix(folderPath, "/"),
		svc:        svc,
		idCache:    make(map[string]string),
	}

	rootID, err := s.ensureFolderPath(folderPath)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare gdrive folder: %w", err)
	}
	s.rootID = rootID

	logger.Log.Info("gdrive syncer ready",
		zap.String("src", absSrc),
		zap.String("folder", folderPath),
		zap.String("folder_id", rootID))

	return s, nil
}

func (s *Uploader) Run(inCh <-chan model.FileEvent) <-chan model.SyncResult {
	return syncer.RunLoop(inCh, s.handle)
}

func (s *Uploader) handle(event model.FileEvent) model.SyncResult {
	result := model.SyncResult{
		Event:   event,
		SrcPath: event.Path,
		DstPath: "gdrive:" + s.folderPath,
	}

	switch event.Type {
	case model.EventCreate, model.EventWrite:
		result.Err = s.uploadFile(event.Path)
	case model.EventRemove, model.EventRename:
		result.Err = s.deleteFile(event.Path)
	}

	if result.Err != nil {
		logger.Log.Error("gdrive sync failed",
			zap.String("path", event.Path),
			zap.Error(result.Err))
	} else {
		logger.Log.Info("gdrive synced",
			zap.String("type", string(event.Type)),
			zap.String("path", event.Path))
	}

	return result
}

func (s *Uploader) uploadFile(localPath string) error {
	relPath := s.relPath(localPath)
	parentID, err := s.ensureParentFolders(relPath)
	if err != nil {
		return fmt.Errorf("failed to create parent folders: %w", err)
	}

	f, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}

	defer func(f *os.File) {
		_ = f.Close()
	}(f)

	fileName := filepath.Base(localPath)

	existingID := s.getCachedID(relPath)
	if existingID == "" {
		existingID, _ = s.findFile(fileName, parentID)
	}

	if existingID != "" {
		_, err = s.svc.Files.Update(existingID, &drive.File{}).Media(f).Do()
		if err != nil {
			return fmt.Errorf("failed to update file: %w", err)
		}

		s.setCachedID(relPath, existingID)
	} else {
		driveFile := &drive.File{
			Name:    fileName,
			Parents: []string{parentID},
		}

		created, err := s.svc.Files.Create(driveFile).Media(f).Fields("id").Do()
		if err != nil {
			return fmt.Errorf("failed to create file: %w", err)
		}

		s.setCachedID(relPath, created.Id)
	}

	return nil
}

func (s *Uploader) deleteFile(localPath string) error {
	relPath := s.relPath(localPath)

	fileID := s.getCachedID(relPath)
	if fileID == "" {
		fileName := filepath.Base(localPath)
		parentPath := filepath.Dir(localPath)
		parentID, err := s.findFolderByPath(parentPath)
		if err != nil || parentID == "" {
			return nil // 부모 폴더가 없으면 이미 삭제된 것
		}

		fileID, _ = s.findFile(fileName, parentID)
	}

	if fileID == "" {
		return nil
	}

	if err := s.svc.Files.Delete(fileID).Do(); err != nil {
		if isNotFound(err) {
			return nil
		}

		return fmt.Errorf("failed to delete file: %w", err)
	}

	s.deleteCachedID(relPath)
	return nil
}

func (s *Uploader) ensureFolderPath(folderPath string) (string, error) {
	parts := splitPath(folderPath)
	if len(parts) == 0 {
		return "root", nil
	}

	parentID := "root"
	for _, part := range parts {
		id, err := s.findFolder(part, parentID)
		if err != nil {
			return "", err
		}

		if id == "" {
			id, err = s.createFolder(part, parentID)
			if err != nil {
				return "", err
			}
		}

		parentID = id
	}

	return parentID, nil
}

func (s *Uploader) ensureParentFolders(relPath string) (string, error) {
	dir := filepath.ToSlash(filepath.Dir(relPath))
	if dir == "." || dir == "" {
		return s.rootID, nil
	}

	parts := strings.Split(dir, "/")
	parentID := s.rootID

	for i, part := range parts {
		cacheKey := strings.Join(parts[:i+1], "/")

		id := s.getCachedID("__dir__" + cacheKey)
		if id != "" {
			parentID = id
			continue
		}

		id, err := s.findFolder(part, parentID)
		if err != nil {
			return "", err
		}

		if id == "" {
			id, err = s.createFolder(part, parentID)
			if err != nil {
				return "", err
			}
		}

		s.setCachedID("__dir__/"+cacheKey, id)
		parentID = id
	}

	return parentID, nil
}

func (s *Uploader) findFolder(name, parentID string) (string, error) {
	q := fmt.Sprintf("name='%s' and '%s' in parents and mimeType='application/vnd.google-apps.folder' and trashed=false", escapeName(name), parentID)

	list, err := s.svc.Files.List().Q(q).Fields("files(id)").Do()
	if err != nil {
		return "", err
	}
	if len(list.Files) == 0 {
		return "", nil
	}

	return list.Files[0].Id, nil
}

func (s *Uploader) findFolderByPath(relPath string) (string, error) {
	if relPath == "." || relPath == "" {
		return s.rootID, nil
	}

	parts := strings.Split(filepath.ToSlash(relPath), "/")
	parentID := s.rootID
	for _, part := range parts {
		id, err := s.findFolder(part, parentID)
		if err != nil || id == "" {
			return "", err
		}

		parentID = id
	}

	return parentID, nil
}

func (s *Uploader) findFile(name, parentID string) (string, error) {
	q := fmt.Sprintf("name='%s' and '%s' in parents and mimeType!='application/vnd.google-apps.folder' and trahsed=false", escapeName(name), parentID)

	list, err := s.svc.Files.List().Q(q).Fields("files(id)").Do()
	if err != nil {
		return "", err
	}

	if len(list.Files) == 0 {
		return "", nil
	}

	return list.Files[0].Id, nil
}

func (s *Uploader) createFolder(name, parentID string) (string, error) {
	f := &drive.File{
		Name:     name,
		MimeType: "application/vnd.google-apps.folder",
		Parents:  []string{parentID},
	}

	created, err := s.svc.Files.Create(f).Fields("id").Do()
	if err != nil {
		return "", fmt.Errorf("failed to create folder %s: %w", name, err)
	}

	return created.Id, nil
}

func (s *Uploader) relPath(localPath string) string {
	rel, err := filepath.Rel(s.src, localPath)
	if err != nil {
		return filepath.Base(localPath)
	}

	return filepath.ToSlash(rel)
}

func (s *Uploader) getCachedID(key string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.idCache[key]
}

func (s *Uploader) setCachedID(key, id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.idCache[key] = id
}

func (s *Uploader) deleteCachedID(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.idCache, key)
}
