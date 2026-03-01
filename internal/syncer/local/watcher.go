package local

import (
	"fmt"
	"os"
	"path/filepath"
	"synco/internal/logger"
	"synco/internal/model"
	"time"

	"github.com/fsnotify/fsnotify"
	"go.uber.org/zap"
)

type Watcher struct {
	fw      *fsnotify.Watcher
	eventCh chan model.FileEvent
	doneCh  chan struct{}
}

func New(bufferSize int) (*Watcher, error) {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create watcher: %w", err)
	}

	return &Watcher{
		fw:      fw,
		eventCh: make(chan model.FileEvent, bufferSize),
		doneCh:  make(chan struct{}),
	}, nil
}

func (w *Watcher) Watch(dir string) error {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("failed to resolve path: %w", err)
	}

	if _, err := os.Stat(absDir); err != nil {
		return fmt.Errorf("soiurce directory not found: %w", err)
	}

	if err := w.addRecursive(absDir); err != nil {
		return err
	}

	go w.run()

	logger.Log.Info("watcher started",
		zap.String("dir", absDir))
	return nil
}

func (w *Watcher) addRecursive(dir string) error {
	return filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			if err := w.fw.Add(path); err != nil {
				return fmt.Errorf("failed to watch %s: %w", path, err)
			}
			logger.Log.Debug("watching directory",
				zap.String("path", path))
		}

		return nil
	})
}

func (w *Watcher) run() {
	defer close(w.eventCh)

	for {
		select {
		case <-w.doneCh:
			logger.Log.Info("watcher stopping")
			return

		case fsEvent, ok := <-w.fw.Events:
			if !ok {
				return
			}

			eventType := toEventType(fsEvent.Op)
			if eventType == "" {
				continue
			}

			if fsEvent.Op.Has(fsnotify.Create) {
				if info, err := os.Stat(fsEvent.Name); err == nil && info.IsDir() {
					if err := w.fw.Add(fsEvent.Name); err != nil {
						logger.Log.Warn("failed to watch new directory",
							zap.String("path", fsEvent.Name),
							zap.Error(err))
					} else {
						logger.Log.Debug("added new directory to watch",
							zap.String("path", fsEvent.Name))
					}
				}
			}

			event := model.FileEvent{
				Type:      eventType,
				Path:      fsEvent.Name,
				Timestamp: time.Now(),
			}

			select {
			case w.eventCh <- event:
			default:
				logger.Log.Warn("event channel is full, dropping event",
					zap.String("path", fsEvent.Name))
			}

		case err, ok := <-w.fw.Errors:
			if !ok {
				return
			}

			logger.Log.Error("watcher error",
				zap.Error(err))
		}
	}
}

func (w *Watcher) Events() <-chan model.FileEvent {
	return w.eventCh
}

func (w *Watcher) Stop() {
	close(w.doneCh)
	_ = w.fw.Close()
}

func toEventType(op fsnotify.Op) model.EventType {
	switch {
	case op.Has(fsnotify.Create):
		return model.EventCreate
	case op.Has(fsnotify.Write):
		return model.EventWrite
	case op.Has(fsnotify.Remove):
		return model.EventRemove
	case op.Has(fsnotify.Rename):
		return model.EventRename
	default:
		return ""
	}
}
