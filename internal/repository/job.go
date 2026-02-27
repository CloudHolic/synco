package repository

import (
	"synco/internal/db"
	"synco/internal/model"
)

type JobRepository struct{}

func NewJobRepository() *JobRepository {
	return &JobRepository{}
}

func (r *JobRepository) Add(src, dst string) (model.Job, error) {
	job := model.Job{
		Src:    src,
		Dst:    dst,
		Status: model.JobStatusActive,
	}

	return job, db.DB.Create(&job).Error
}

func (r *JobRepository) GetAll() ([]model.Job, error) {
	var jobs []model.Job
	return jobs, db.DB.Find(&jobs).Error
}

func (r *JobRepository) GetByID(id uint) (model.Job, error) {
	var job model.Job
	return job, db.DB.First(&job, id).Error
}

func (r *JobRepository) UpdateStatus(id uint, status model.JobStatus) error {
	return db.DB.Model(&model.Job{}).
		Where("id = ?", id).
		Update("status", status).Error
}

func (r *JobRepository) Delete(id uint) error {
	return db.DB.Delete(&model.Job{}, id).Error
}
