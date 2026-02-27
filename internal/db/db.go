package db

import (
	"fmt"
	"synco/internal/model"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var DB *gorm.DB

func Init(dbPath string) error {
	var err error
	DB, err = gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return fmt.Errorf("failed to open db: %w", err)
	}

	if err := DB.AutoMigrate(&model.History{}, &model.Job{}); err != nil {
		return fmt.Errorf("failed to migrate: %w", err)
	}

	return nil
}
