package models

import (
	"time"

	"github.com/oklog/ulid/v2"
	"gorm.io/gorm"
)

type Status string

const (
	StatusPending   Status = "pending"
	StatusBuilding  Status = "building"
	StatusDeploying Status = "deploying"
	StatusRunning   Status = "running"
	StatusFailed    Status = "failed"
	StatusStopped   Status = "stopped"
)

type Deployment struct {
	ID            string    `json:"id" gorm:"primaryKey"`
	Name          string    `json:"name"`
	Status        Status    `json:"status" gorm:"default:pending"`
	GithubURL     string    `gorm:"not null" json:"github_url"`
	ImageName     string    `json:"image_name"`
	ContainerName string    `json:"container_name"`
	ContainerPort int       `json:"container_port"`
	CaddyRoute    string    `json:"caddy_route"`
	CreatedAt     time.Time `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt     time.Time `json:"updated_at" gorm:"autoUpdateTime"`
}

type LogEntry struct {
	ID           string      `gorm:"primaryKey" json:"id"`
	DeploymentID string      `gorm:"not null;index" json:"deployment_id"`
	Message      string      `gorm:"not null" json:"message"`
	CreatedAt    time.Time   `gorm:"autoCreateTime" json:"created_at"`
	Deployment   *Deployment `gorm:"foreignKey:DeploymentID;constraint:OnDelete:CASCADE"`
}

func (d *Deployment) BeforeCreate(tx *gorm.DB) error {
	if d.ID == "" {
		d.ID = ulid.Make().String()
	}
	return nil
}

func (l *LogEntry) BeforeCreate(tx *gorm.DB) error {
	if l.ID == "" {
		l.ID = ulid.Make().String()
	}
	return nil
}
