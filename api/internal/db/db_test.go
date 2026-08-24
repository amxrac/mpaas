package db

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/amxrac/mpaas/api/internal/models"
	"github.com/ncruces/go-sqlite3/gormlite"
	"gorm.io/gorm"
)

func newTestDB(t *testing.T) *DB {
	t.Helper()

	conn, err := gorm.Open(
		gormlite.Open("file::memory:?cache=shared"),
		&gorm.Config{},
	)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}

	db := NewDB(conn)

	if err := db.Migrate(
		&models.Deployment{},
		&models.LogEntry{},
	); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}

	t.Cleanup(func() {
		_ = db.CloseDB()
	})

	return db
}

func testDeployment() *models.Deployment {
	return &models.Deployment{
		GithubURL: "https://github.com/test/repo",
		Status:    models.StatusPending,
	}
}

func TestDBPing(t *testing.T) {
	db := newTestDB(t)

	err := db.Ping()
	if err != nil {
		t.Fatalf("Ping() error = %v", err)
	}
}

func TestInsertAndGetDeployment(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	want := testDeployment()

	err := db.InsertDeployment(ctx, want)
	if err != nil {
		t.Fatalf("InsertDeployment() error = %v", err)
	}

	got, err := db.GetDeploymentByID(ctx, want.ID)
	if err != nil {
		t.Fatalf("GetDeploymentByID() error = %v", err)
	}

	if got.ID != want.ID {
		t.Errorf("ID = %q, want %q", got.ID, want.ID)
	}

	if got.GithubURL != want.GithubURL {
		t.Errorf("GithubURL = %q, want %q", got.GithubURL, want.GithubURL)
	}

	if got.Status != want.Status {
		t.Errorf("Status = %q, want %q", got.Status, want.Status)
	}
}

func TestGetDeploymentByIDNotFound(t *testing.T) {
	db := newTestDB(t)

	_, err := db.GetDeploymentByID(context.Background(), "does-not-exist")
	if err == nil {
		t.Fatal("GetDeploymentByID() expected error, got nil")
	}

	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Errorf("error = %v, want gorm.ErrRecordNotFound", err)
	}
}

func TestListDeployments(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	first := testDeployment()
	first.GithubURL = "https://github.com/test/first"

	err := db.InsertDeployment(ctx, first)
	if err != nil {
		t.Fatalf("InsertDeployment(first) error = %v", err)
	}

	time.Sleep(time.Millisecond)

	second := testDeployment()
	second.GithubURL = "https://github.com/test/second"

	err = db.InsertDeployment(ctx, second)
	if err != nil {
		t.Fatalf("InsertDeployment(second) error = %v", err)
	}

	got, err := db.ListDeployments(ctx)
	if err != nil {
		t.Fatalf("ListDeployments() error = %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("len(deployments) = %d, want 2", len(got))
	}

	if got[0].ID != second.ID {
		t.Errorf("first deployment = %q, want %q", got[0].ID, second.ID)
	}

	if got[1].ID != first.ID {
		t.Errorf("second deployment = %q, want %q", got[1].ID, first.ID)
	}
}

func TestUpdateDeployment(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	dep := testDeployment()

	err := db.InsertDeployment(ctx, dep)
	if err != nil {
		t.Fatalf("InsertDeployment() error = %v", err)
	}

	dep.Status = models.StatusRunning
	dep.ImageName = "test/image"
	dep.ContainerName = "test-container"
	dep.ContainerPort = 8080
	dep.CaddyRoute = "test-repo.localhost"

	err = db.UpdateDeployment(ctx, dep)
	if err != nil {
		t.Fatalf("UpdateDeployment() error = %v", err)
	}

	got, err := db.GetDeploymentByID(ctx, dep.ID)
	if err != nil {
		t.Fatalf("GetDeploymentByID() error = %v", err)
	}

	if got.Status != models.StatusRunning {
		t.Errorf("Status = %q, want %q", got.Status, models.StatusRunning)
	}

	if got.ImageName != dep.ImageName {
		t.Errorf("ImageName = %q, want %q", got.ImageName, dep.ImageName)
	}

	if got.ContainerName != dep.ContainerName {
		t.Errorf("ContainerName = %q, want %q", got.ContainerName, dep.ContainerName)
	}

	if got.ContainerPort != dep.ContainerPort {
		t.Errorf("ContainerPort = %d, want %d", got.ContainerPort, dep.ContainerPort)
	}

	if got.CaddyRoute != dep.CaddyRoute {
		t.Errorf("CaddyRoute = %q, want %q", got.CaddyRoute, dep.CaddyRoute)
	}
}

func TestDeleteDeployment(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	dep := testDeployment()

	err := db.InsertDeployment(ctx, dep)
	if err != nil {
		t.Fatalf("InsertDeployment() error = %v", err)
	}

	err = db.DeleteDeployment(ctx, dep.ID)
	if err != nil {
		t.Fatalf("DeleteDeployment() error = %v", err)
	}

	_, err = db.GetDeploymentByID(ctx, dep.ID)
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("GetDeploymentByID() error = %v, want gorm.ErrRecordNotFound", err)
	}
}

func TestInsertLog(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	dep := testDeployment()

	err := db.InsertDeployment(ctx, dep)
	if err != nil {
		t.Fatalf("InsertDeployment() error = %v", err)
	}

	entry, err := db.InsertLog(ctx, "building image", dep.ID)
	if err != nil {
		t.Fatalf("InsertLog() error = %v", err)
	}

	if entry.ID == "" {
		t.Error("InsertLog() returned empty ID")
	}

	if entry.Message != "building image" {
		t.Errorf("Message = %q, want %q", entry.Message, "building image")
	}

	if entry.DeploymentID != dep.ID {
		t.Errorf("DeploymentID = %q, want %q", entry.DeploymentID, dep.ID)
	}
}

func TestGetLogsAfterID(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	dep := testDeployment()

	err := db.InsertDeployment(ctx, dep)
	if err != nil {
		t.Fatalf("InsertDeployment() error = %v", err)
	}

	first, err := db.InsertLog(ctx, "first", dep.ID)
	if err != nil {
		t.Fatalf("InsertLog(first) error = %v", err)
	}

	second, err := db.InsertLog(ctx, "second", dep.ID)
	if err != nil {
		t.Fatalf("InsertLog(second) error = %v", err)
	}

	_, err = db.InsertLog(ctx, "third", dep.ID)
	if err != nil {
		t.Fatalf("InsertLog(third) error = %v", err)
	}

	got, err := db.GetLogsAfterID(ctx, dep.ID, first.ID)
	if err != nil {
		t.Fatalf("GetLogsAfterID() error = %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("len(logs) = %d, want 2", len(got))
	}

	if got[0].ID != second.ID {
		t.Errorf("first result ID = %q, want %q", got[0].ID, second.ID)
	}

	if got[0].Message != "second" {
		t.Errorf("first result message = %q, want %q", got[0].Message, "second")
	}

	if got[1].Message != "third" {
		t.Errorf("second result message = %q, want %q", got[1].Message, "third")
	}
}

func TestGetLogsAfterIDIsolatedByDeployment(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	dep1 := testDeployment()
	dep2 := testDeployment()

	err := db.InsertDeployment(ctx, dep1)
	if err != nil {
		t.Fatalf("InsertDeployment(dep1) error = %v", err)
	}

	err = db.InsertDeployment(ctx, dep2)
	if err != nil {
		t.Fatalf("InsertDeployment(dep2) error = %v", err)
	}

	first, err := db.InsertLog(ctx, "dep1-first", dep1.ID)
	if err != nil {
		t.Fatalf("InsertLog() error = %v", err)
	}

	if _, err := db.InsertLog(ctx, "dep1-second", dep1.ID); err != nil {
		t.Fatalf("InsertLog() error = %v", err)
	}

	if _, err := db.InsertLog(ctx, "dep2-log", dep2.ID); err != nil {
		t.Fatalf("InsertLog() error = %v", err)
	}

	got, err := db.GetLogsAfterID(ctx, dep1.ID, first.ID)
	if err != nil {
		t.Fatalf("GetLogsAfterID() error = %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("len(logs) = %d, want 1", len(got))
	}

	if got[0].Message != "dep1-second" {
		t.Errorf("Message = %q, want %q", got[0].Message, "dep1-second")
	}
}

func TestListDeploymentsLimit(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	for i := range 101 {
		dep := testDeployment()
		dep.GithubURL = fmt.Sprintf("https://github.com/test/repo-%d", i)

		err := db.InsertDeployment(ctx, dep)
		if err != nil {
			t.Fatalf("InsertDeployment(%d) error = %v", i, err)
		}
	}

	got, err := db.ListDeployments(ctx)
	if err != nil {
		t.Fatalf("ListDeployments() error = %v", err)
	}

	if len(got) != 100 {
		t.Fatalf("len(deployments) = %d, want 100", len(got))
	}
}
