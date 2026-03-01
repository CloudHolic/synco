package local

import (
	"synco/internal/model"
)

type Source struct {
	w *Watcher
}

func NewSource(path string, bufSize int) (*Source, error) {
	w, err := New(bufSize)
	if err != nil {
		return nil, err
	}

	if err := w.Watch(path); err != nil {
		return nil, err
	}

	return &Source{w: w}, nil
}

func (s *Source) Events() <-chan model.FileEvent {
	return s.w.Events()
}

func (s *Source) Start() error {
	return nil
}

func (s *Source) Stop() {
	s.w.Stop()
}
