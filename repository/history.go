package repository

import (
	"synco/db"
	"synco/model"
	"time"
)

type HistoryRepository struct{}

func NewHistoryRepository() *HistoryRepository {
	return &HistoryRepository{}
}

func (r *HistoryRepository) Save(result model.SyncResult) error {
	status := model.StatusSuccess
	errMsg := ""
	if result.Err != nil {
		status = model.StatusFailed
		errMsg = result.Err.Error()
	}

	history := model.History{
		EventType: status,
		SrcPath:   result.SrcPath,
		DstPath:   result.DstPath,
		FileEvent: string(result.Event.Type),
		ErrMsg:    errMsg,
		SyncedAt:  time.Now(),
	}

	return db.DB.Create(&history).Error
}

type Stats struct {
	Total   int64
	Success int64
	Failed  int64
}

func (r *HistoryRepository) GetStats() (Stats, error) {
	var stats Stats
	if err := db.DB.Model(&model.History{}).Count(&stats.Total).Error; err != nil {
		return stats, err
	}

	if err := db.DB.Model(&model.History{}).
		Where("event_type = ?", model.StatusSuccess).
		Count(&stats.Success).Error; err != nil {
		return stats, err
	}

	stats.Failed = stats.Total - stats.Success
	return stats, nil
}

func (r *HistoryRepository) GetRecent(limit int) ([]model.History, error) {
	var histories []model.History
	result := db.DB.
		Order("synced_at desc").
		Limit(limit).
		Find(&histories)

	return histories, result.Error
}

func (r *HistoryRepository) GetFailed() ([]model.History, error) {
	var histories []model.History
	result := db.DB.
		Where("event_type = ?", model.StatusFailed).
		Order("synced_at desc").
		Find(&histories)

	return histories, result.Error
}
