package pipeline

import (
	"crypto/sha256"
	"io"
	"os"
	"sync"
	"synco/logger"
	"synco/model"

	"go.uber.org/zap"
)

type ChecksumFilter struct {
	mu    sync.Mutex
	cache map[string][]byte
}

func NewChecksumFilter() *ChecksumFilter {
	return &ChecksumFilter{
		cache: make(map[string][]byte),
	}
}

func (cf *ChecksumFilter) Run(inCh <-chan model.FileEvent) <-chan model.FileEvent {
	outCh := make(chan model.FileEvent, cap(inCh))

	go func() {
		defer close(outCh)

		for event := range inCh {
			if event.Type == model.EventRemove || event.Type == model.EventRename {
				cf.mu.Lock()
				delete(cf.cache, event.Path)
				cf.mu.Unlock()
				outCh <- event
				continue
			}

			sum, err := checksum(event.Path)
			if err != nil {
				logger.Log.Debug("checksum failed, skipping",
					zap.String("path", event.Path),
					zap.Error(err))
				continue
			}

			cf.mu.Lock()
			prev, exists := cf.cache[event.Path]
			changed := !exists || !equal(prev, sum)
			if changed {
				cf.cache[event.Path] = sum
			}
			cf.mu.Unlock()

			if changed {
				outCh <- event
			} else {
				logger.Log.Debug("checksum unchanged, skipping",
					zap.String("path", event.Path))
			}
		}
	}()

	return outCh
}

func checksum(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	defer func(f *os.File) {
		_ = f.Close()
	}(f)

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, err
	}

	return h.Sum(nil), nil
}

func equal(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}
