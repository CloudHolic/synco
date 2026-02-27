package pipeline

import (
	"path/filepath"
	"strings"
	"synco/internal/model"
)

func Filter(inCh <-chan model.FileEvent, ignoreList []string) <-chan model.FileEvent {
	outCh := make(chan model.FileEvent, cap(inCh))

	go func() {
		defer close(outCh)

		for event := range inCh {
			if shouldIgnore(event.Path, ignoreList) {
				continue
			}
			outCh <- event
		}
	}()

	return outCh
}

func shouldIgnore(path string, ignoreList []string) bool {
	parts := strings.Split(filepath.ToSlash(path), "/")

	for _, part := range parts {
		for _, pattern := range ignoreList {
			matched, err := filepath.Match(pattern, part)
			if err == nil && matched {
				return true
			}
		}
	}

	return false
}
