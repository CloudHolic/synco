package pipeline

import (
	"synco/model"
	"time"
)

func Debounce(inCh <-chan model.FileEvent, delay time.Duration) <-chan model.FileEvent {
	outCh := make(chan model.FileEvent, cap(inCh))

	go func() {
		defer close(outCh)

		timers := make(map[string]*time.Timer)
		events := make(map[string]model.FileEvent)

		for event := range inCh {
			path := event.Path

			if t, ok := timers[path]; ok {
				t.Stop()
			}

			events[path] = event

			timers[path] = time.AfterFunc(delay, func() {
				outCh <- events[path]
				delete(timers, path)
				delete(events, path)
			})
		}

		for path, t := range timers {
			t.Stop()
			outCh <- events[path]
		}
	}()

	return outCh
}
