package syncer

import (
	"synco/internal/model"
)

type EventSource interface {
	Events() <-chan model.FileEvent
	Start() error
	Stop()
}

type Syncer interface {
	Run(inCh <-chan model.FileEvent) <-chan model.SyncResult
}

func RunLoop(inCh <-chan model.FileEvent, handle func(model.FileEvent) model.SyncResult) <-chan model.SyncResult {
	outCh := make(chan model.SyncResult, cap(inCh))

	go func() {
		defer close(outCh)
		for event := range inCh {
			outCh <- handle(event)
		}
	}()

	return outCh
}
