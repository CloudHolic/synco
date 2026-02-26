package syncer

import "synco/model"

type Syncer interface {
	Run(inCh <-chan model.FileEvent) <-chan model.SyncResult
}
