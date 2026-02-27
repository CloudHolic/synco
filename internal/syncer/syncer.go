package syncer

import (
	"synco/internal/model"
)

type Syncer interface {
	Run(inCh <-chan model.FileEvent) <-chan model.SyncResult
}
