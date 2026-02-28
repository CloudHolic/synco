package dropbox

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"synco/internal/auth"
	"synco/internal/logger"
	"synco/internal/model"
	"time"

	"github.com/dropbox/dropbox-sdk-go-unofficial/v6/dropbox"
	"github.com/dropbox/dropbox-sdk-go-unofficial/v6/dropbox/files"
	"go.uber.org/zap"
)

type DropboxPoller struct {
	folderPath string
	client     files.Client
	cursor     string
	cursorPath string
	stopCh     chan struct{}
	eventCh    chan model.FileEvent
}

func NewDropboxPoller(jobID uint, folderPath string) (*DropboxPoller, error) {
	token, err := auth.NewDropboxToken()
	if err != nil {
		return nil, err
	}

	cfg := dropbox.Config{Token: token.AccessToken}
	client := files.New(cfg)

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	cursorPath := filepath.Join(home, ".synco", fmt.Sprintf("dropbox_cursor_%d", jobID))

	p := &DropboxPoller{
		folderPath: normalizeDropboxPath(folderPath),
		client:     client,
		cursorPath: cursorPath,
		stopCh:     make(chan struct{}),
		eventCh:    make(chan model.FileEvent, 100),
	}

	logger.Log.Info("dropbox poller ready",
		zap.String("folder", folderPath))

	return p, nil
}

func (p *DropboxPoller) Events() <-chan model.FileEvent {
	return p.eventCh
}

func (p *DropboxPoller) Start() error {
	cursor, err := p.loadCursor()
	if err != nil {
		c, err := p.getLatestCursor()
		if err != nil {
			return fmt.Errorf("failed to get dropbox cursor: %w", err)
		}

		cursor = c
		if err := p.saveCursor(cursor); err != nil {
			return err
		}

		logger.Log.Info("dropbox longpoll started (new)",
			zap.String("folder", p.folderPath))
	} else {
		logger.Log.Info("dropbox longpoll resumed",
			zap.String("folder", p.folderPath))
	}

	p.cursor = cursor
	go p.run()
	return nil
}

func (p *DropboxPoller) Stop() {
	close(p.stopCh)
}

func (p *DropboxPoller) run() {
	defer close(p.eventCh)

	for {
		select {
		case <-p.stopCh:
			return
		default:
		}

		changed, err := p.longpoll()
		if err != nil {
			logger.Log.Warn("dropbox longpoll error", zap.Error(err))

			select {
			case <-p.stopCh:
				return
			case <-time.After(10 * time.Second):
				continue
			}
		}

		if !changed {
			continue
		}

		if err := p.fetchChanges(); err != nil {
			logger.Log.Warn("dropbox fetch changes error",
				zap.Error(err))
		}
	}
}

func (p *DropboxPoller) longpoll() (bool, error) {
	arg := files.NewListFolderLongpollArg(p.cursor)
	arg.Timeout = 480

	resp, err := p.client.ListFolderLongpoll(arg)
	if err != nil {
		return false, err
	}

	if resp.Backoff > 0 {
		select {
		case <-p.stopCh:
			return false, nil
		case <-time.After(time.Duration(resp.Backoff) * time.Second):
		}
	}

	return resp.Changes, nil
}

func (p *DropboxPoller) fetchChanges() error {
	for {
		arg := files.NewListFolderContinueArg(p.cursor)
		resp, err := p.client.ListFolderContinue(arg)
		if err != nil {
			return err
		}

		for _, entry := range resp.Entries {
			p.handleEntry(entry)
		}

		p.cursor = resp.Cursor
		_ = p.saveCursor(p.cursor)

		if !resp.HasMore {
			break
		}
	}

	return nil
}

func (p *DropboxPoller) handleEntry(entry files.IsMetadata) {
	var event model.FileEvent

	switch e := entry.(type) {
	case *files.FileMetadata:
		relPath := p.toRelPath(e.PathDisplay)
		if relPath == "" {
			return
		}

		event = model.FileEvent{
			Type:      model.EventWrite,
			Path:      relPath,
			Timestamp: time.Now(),
		}

	case *files.DeletedMetadata:
		relPath := p.toRelPath(e.PathDisplay)
		if relPath == "" {
			return
		}
		event = model.FileEvent{
			Type:      model.EventRemove,
			Path:      relPath,
			Timestamp: time.Now(),
		}

	case *files.FolderMetadata:
		return
	}

	select {
	case p.eventCh <- event:
	case <-p.stopCh:
	}
}

func (p *DropboxPoller) toRelPath(dropboxPath string) string {
	prefix := strings.ToLower(p.folderPath)
	lower := strings.ToLower(dropboxPath)

	if !strings.HasPrefix(lower, prefix) {
		return ""
	}

	rel := dropboxPath[len(p.folderPath):]
	return strings.TrimPrefix(rel, "/")
}

func (p *DropboxPoller) getLatestCursor() (string, error) {
	arg := files.NewListFolderArg(p.folderPath)
	arg.Recursive = true

	resp, err := p.client.ListFolderGetLatestCursor(arg)
	if err != nil {
		return "", err
	}

	return resp.Cursor, nil
}

func (p *DropboxPoller) saveCursor(cursor string) error {
	return os.WriteFile(p.cursorPath, []byte(cursor), 0600)
}

func (p *DropboxPoller) loadCursor() (string, error) {
	b, err := os.ReadFile(p.cursorPath)
	if err != nil {
		return "", err
	}

	return string(b), nil
}
