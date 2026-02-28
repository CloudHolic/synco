package gdrive

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"synco/internal/auth"
	"synco/internal/logger"
	"synco/internal/model"
	"time"

	"go.uber.org/zap"
	"google.golang.org/api/drive/v3"
)

type GDrivePoller struct {
	folderPath string
	folderID   string
	knownDirs  map[string]bool
	svc        *drive.Service
	interval   time.Duration
	tokenPath  string
	stopCh     chan struct{}
	eventCh    chan model.FileEvent
}

func NewGdrivePoller(jobID uint, folderPath string, interval time.Duration) (*GDrivePoller, error) {
	ctx := context.Background()
	svc, err := auth.NewDriveService(ctx)
	if err != nil {
		return nil, err
	}

	tmp := &GDriveSyncer{svc: svc, idCache: make(map[string]string)}
	folderID, err := tmp.ensureFolderPath(folderPath)
	if err != nil {
		return nil, fmt.Errorf("failed to find gdrive folder: %w", err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	tokenPath := filepath.Join(home, ".synco", fmt.Sprintf("gdrive_pagetoken_%d", jobID))

	p := &GDrivePoller{
		folderPath: folderPath,
		folderID:   folderID,
		knownDirs:  make(map[string]bool),
		svc:        svc,
		interval:   interval,
		tokenPath:  tokenPath,
		stopCh:     make(chan struct{}),
		eventCh:    make(chan model.FileEvent, 100),
	}

	p.knownDirs[folderID] = true
	if err := p.indexSubFolders(folderID); err != nil {
		logger.Log.Warn("failed to index subfolders",
			zap.Error(err))
	}

	logger.Log.Info("gdrive poller ready",
		zap.String("folder", folderPath),
		zap.String("folder_id", folderID),
		zap.Duration("interval", interval))

	return p, nil
}

func (p *GDrivePoller) Events() <-chan model.FileEvent {
	return p.eventCh
}

func (p *GDrivePoller) Start() error {
	token, err := p.loadPageToken()
	if err != nil {
		resp, err := p.svc.Changes.GetStartPageToken().Do()
		if err != nil {
			return fmt.Errorf("failed to get start page token: %w", err)
		}

		token = resp.StartPageToken
		if err := p.savePageToken(token); err != nil {
			return err
		}

		logger.Log.Info("gdrive polling started (new)",
			zap.String("folder", p.folderPath))
	} else {
		logger.Log.Info("gdrive polling resumed",
			zap.String("folder", p.folderPath))
	}

	go p.poll(token)
	return nil
}

func (p *GDrivePoller) Stop() {
	close(p.stopCh)
}

func (p *GDrivePoller) poll(pageToken string) {
	defer close(p.eventCh)

	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	for {
		select {
		case <-p.stopCh:
			return
		case <-ticker.C:
			newToken, err := p.fetchChanges(pageToken)
			if err != nil {
				logger.Log.Warn("gdrive poll error",
					zap.Error(err))
				continue
			}

			if newToken != pageToken {
				pageToken = newToken
				_ = p.savePageToken(pageToken)
			}
		}
	}
}

func (p *GDrivePoller) fetchChanges(pageToken string) (string, error) {
	for {
		resp, err := p.svc.Changes.List(pageToken).
			Fields("nextPageToken, newStartPageToken, changes(fileId, removed, file(name, parents, mimeType, modifiedTime))").
			Do()
		if err != nil {
			return pageToken, err
		}

		for _, change := range resp.Changes {
			p.handleChange(change)
		}

		if resp.NextPageToken != "" {
			pageToken = resp.NextPageToken
			continue
		}

		return resp.NewStartPageToken, nil
	}
}

func (p *GDrivePoller) handleChange(change *drive.Change) {
	if change.Removed || change.File == nil {
		logger.Log.Debug("gdrive file removed",
			zap.String("id", change.FileId))
		return
	}

	file := change.File

	if file.MimeType == "application/vnd.google-apps.folder" {
		if p.isUnderTarget(file.Parents) {
			p.knownDirs[change.FileId] = true
		}

		return
	}

	if !p.isUnderTarget(file.Parents) {
		return
	}

	relPath, err := p.resolveRelPath(change.FileId, file)
	if err != nil {
		logger.Log.Warn("failed to resolve path",
			zap.String("file", file.Name),
			zap.Error(err))
		return
	}

	event := model.FileEvent{
		Type:      model.EventWrite,
		Path:      relPath,
		Timestamp: time.Now(),
	}

	select {
	case p.eventCh <- event:
	case <-p.stopCh:
	}
}

func (p *GDrivePoller) isUnderTarget(parents []string) bool {
	for _, parentID := range parents {
		if p.knownDirs[parentID] {
			return true
		}
	}

	return false
}

func (p *GDrivePoller) resolveRelPath(fileID string, file *drive.File) (string, error) {
	parts := []string{file.Name}
	parentID := ""
	if len(file.Parents) > 0 {
		parentID = file.Parents[0]
	}

	for parentID != "" && parentID != p.folderID {
		parent, err := p.svc.Files.Get(parentID).
			Fields("id, name, parents").Do()
		if err != nil {
			return "", err
		}

		parts = append([]string{parent.Name}, parts...)
		if len(parent.Parents) > 0 {
			parentID = parent.Parents[0]
		} else {
			break
		}
	}

	return strings.Join(parts, "/"), nil
}

func (p *GDrivePoller) indexSubFolders(parentID string) error {
	q := fmt.Sprintf("'%s' in parents and mimeType='application/vnd.google-apps.folder' and trashed=false", parentID)

	list, err := p.svc.Files.List().Q(q).Fields("files(id)").Do()
	if err != nil {
		return err
	}

	for _, f := range list.Files {
		p.knownDirs[f.Id] = true
		_ = p.indexSubFolders(f.Id)
	}

	return nil
}

func (p *GDrivePoller) savePageToken(token string) error {
	return os.WriteFile(p.tokenPath, []byte(token), 0600)
}

func (p *GDrivePoller) loadPageToken() (string, error) {
	b, err := os.ReadFile(p.tokenPath)
	if err != nil {
		return "", err
	}

	return string(b), nil
}
