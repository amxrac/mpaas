package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/amxrac/mpaas/internal/models"
	"github.com/ncruces/go-sqlite3/gormlite"
	"gorm.io/gorm"
)

type DB struct {
	conn *gorm.DB
}

func ConnectDB(dsn string) (*gorm.DB, error) {
	conn, err := gorm.Open(gormlite.Open(dsn+"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)"), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("connect db: %w", err)
	}
	return conn, nil
}

func (db *DB) CloseDB() error {
	sqlDB, err := db.conn.DB()
	if err != nil {
		return fmt.Errorf("db close: %w", err)
	}
	return sqlDB.Close()

}

func NewDB(conn *gorm.DB) *DB {
	return &DB{conn: conn}
}

func (db *DB) Migrate(models ...any) error {
	if err := db.conn.AutoMigrate(models...); err != nil {
		return fmt.Errorf("db migration error: %w", err)
	}
	return nil
}

func (db *DB) InsertDeployment(ctx context.Context, dep *models.Deployment) error {
	err := db.conn.WithContext(ctx).Create(&dep).Error
	if err != nil {
		return fmt.Errorf("insert deployment: %w", err)
	}
	return nil
}

func (db *DB) GetDeploymentByID(ctx context.Context, id string) (*models.Deployment, error) {
	var dep models.Deployment
	err := db.conn.WithContext(ctx).First(&dep, "id = ?", id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("get deployment %q: %w", id, err)
		}
		return nil, err
	}
	return &dep, nil
}

func (db *DB) ListDeployments(ctx context.Context) ([]models.Deployment, error) {
	deps := make([]models.Deployment, 0)
	err := db.conn.WithContext(ctx).Order("created_at DESC").Limit(100).Find(&deps).Error
	if err != nil {
		return nil, fmt.Errorf("list deployments: %w", err)
	}

	return deps, nil
}

func (db *DB) UpdateDeployment(ctx context.Context, dep models.Deployment) error {
	err := db.conn.WithContext(ctx).Model(&models.Deployment{}).Where("id = ?", dep.ID).Updates(map[string]any{
		"status":         dep.Status,
		"image_tag":      dep.ImageTag,
		"container_name": dep.ContainerName,
		"container_port": dep.ContainerPort,
		"caddy_route":    dep.CaddyRoute,
	}).Error
	if err != nil {
		return fmt.Errorf("update deployment %q: %w", dep.ID, err)
	}
	return nil
}

func (db *DB) DeleteDeployment(ctx context.Context, id string) error {
	err := db.conn.WithContext(ctx).Delete(&models.Deployment{}, "id = ?", id).Error
	if err != nil {
		return fmt.Errorf("delete deployment %q: %w", id, err)
	}

	return nil
}

func (db *DB) InsertLogEntry(ctx context.Context, message, deploymentID string) error {
	entry := models.LogEntry{
		DeploymentID: deploymentID,
		Message:      message,
	}

	err := db.conn.WithContext(ctx).Create(&entry).Error
	if err != nil {
		return fmt.Errorf("insert log entry: %w", err)
	}

	return nil
}

func (db *DB) GetLogEntryByDeploymentID(ctx context.Context, deploymentID string) ([]models.LogEntry, error) {
	var entries []models.LogEntry
	err := db.conn.WithContext(ctx).
		Where("deployment_id = ?", deploymentID).
		Order("created_at ASC").
		Find(&entries).Error
	if err != nil {
		return nil, fmt.Errorf("get logs for deployment %q: %w", deploymentID, err)
	}
	return entries, nil

}
