package models

import "time"

type Status string

const (
	StatusPending   Status = "pending"
	StatusBuilding  Status = "building"
	StatusDeploying Status = "deploying"
	StatusRunning   Status = "running"
	StatusFailed    Status = "failed"
)

type Deployment struct {
	ID            string    `json:"id" gorm:"type:cuid;primaryKey;default:gen_random_cuid()"`
	Name          string    `json:"name"`
	Status        Status    `json:"status" gorm:"default:pending"`
	GithubURL     string    `gorm:"not null" json:"github_url"`
	ImageTag      string    `json:"image_tag"`
	ContainerName string    `json:"container_name"`
	ContainerPort string    `json:"container_port"`
	CaddyRoute    string    `json:"caddy_route"`
	CreatedAt     time.Time `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt     time.Time `json:"updated_at" gorm:"autoUpdateTime"`
}

type LogEntry struct {
	ID           string      `gorm:"type:cuid;primaryKey;default:gen_random_cuid()" json:"id"`
	DeploymentID string      `gorm:"type:cuid;not null;index" json:"deployment_id"`
	Message      string      `gorm:"not null" json:"message"`
	CreatedAt    time.Time   `gorm:"autoCreateTime" json:"created_at"`
	Deployment   *Deployment `gorm:"foreignKey:DeploymentID;constraint:OnDelete:CASCADE"`
}
