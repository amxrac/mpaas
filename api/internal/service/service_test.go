package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/amxrac/mpaas/api/internal/db"
	"github.com/amxrac/mpaas/api/internal/models"
	"github.com/amxrac/mpaas/api/internal/stream"
	"github.com/ncruces/go-sqlite3/gormlite"
	"gorm.io/gorm"
)

func TestParseGitHubRepoURL(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantURL   string
		wantOwner string
		wantRepo  string
		wantErr   bool
	}{
		{
			name:      "full URL",
			input:     "https://github.com/amxrac/mpaas",
			wantURL:   "https://github.com/amxrac/mpaas",
			wantOwner: "amxrac",
			wantRepo:  "mpaas",
		},
		{
			name:      "without scheme",
			input:     "github.com/amxrac/mpaas",
			wantURL:   "https://github.com/amxrac/mpaas",
			wantOwner: "amxrac",
			wantRepo:  "mpaas",
		},
		{
			name:      "trailing slash",
			input:     "https://github.com/amxrac/mpaas/",
			wantURL:   "https://github.com/amxrac/mpaas/",
			wantOwner: "amxrac",
			wantRepo:  "mpaas",
		},
		{
			name:    "wrong scheme",
			input:   "http://github.com/amxrac/mpaas",
			wantErr: true,
		},
		{
			name:    "wrong host",
			input:   "https://gitlab.com/amxrac/mpaas",
			wantErr: true,
		},
		{
			name:    "query",
			input:   "https://github.com/amxrac/mpaas?foo=bar",
			wantErr: true,
		},
		{
			name:    "fragment",
			input:   "https://github.com/amxrac/mpaas#readme",
			wantErr: true,
		},
		{
			name:    "missing owner",
			input:   "https://github.com/mpaas",
			wantErr: true,
		},
		{
			name:    "too many path components",
			input:   "https://github.com/amxrac/mpaas/foo",
			wantErr: true,
		},
		{
			name:    "invalid owner",
			input:   "https://github.com/foo/bar/baz",
			wantErr: true,
		},
		{
			name:    "invalid repo characters",
			input:   "https://github.com/amxrac/mpaas!",
			wantErr: true,
		},
		{
			name:    "dot owner",
			input:   "https://github.com/./mpaas",
			wantErr: true,
		},
		{
			name:    "dot dot repo",
			input:   "https://github.com/amxrac/..",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u, owner, repo, err := ParseGitHubRepoURL(tt.input)

			if tt.wantErr {
				if !errors.Is(err, errInvalidGitHubRepoURL) {
					t.Fatalf("got error %v, want %v", err, errInvalidGitHubRepoURL)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if u.String() != tt.wantURL {
				t.Errorf("URL = %q, want %q", u.String(), tt.wantURL)
			}

			if owner != tt.wantOwner {
				t.Errorf("owner = %q, want %q", owner, tt.wantOwner)
			}

			if repo != tt.wantRepo {
				t.Errorf("repo = %q, want %q", repo, tt.wantRepo)
			}
		})
	}
}

func TestValidateContainerPort(t *testing.T) {
	tests := []struct {
		port    int
		wantErr bool
	}{
		{port: 0},
		{port: 1},
		{port: 80},
		{port: 8080},
		{port: 65535},
		{port: -1, wantErr: true},
		{port: 65536, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(string(rune(tt.port)), func(t *testing.T) {
			err := validateContainerPort(tt.port)

			if tt.wantErr && err == nil {
				t.Fatalf("expected error for port %d", tt.port)
			}

			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error for port %d: %v", tt.port, err)
			}
		})
	}
}

func TestLogWriter(t *testing.T) {
	db := testDB(t)
	hub := stream.NewHub()

	deployment := &models.Deployment{
		GithubURL: "https://github.com/amxrac/mpaas",
		Status:    models.StatusPending,
	}

	err := db.InsertDeployment(context.Background(), deployment)
	if err != nil {
		t.Fatalf("insert deployment: %v", err)
	}

	s := &Service{
		db:     db,
		stream: hub,
	}

	ch := hub.Subscribe(deployment.ID)
	defer hub.Unsubscribe(deployment.ID, ch)

	w := newLogWriter(context.Background(), s, deployment.ID)

	n, err := w.Write([]byte("hello\nworld"))
	if err != nil {
		t.Fatalf("write: %v", err)
	}

	if n != len("hello\nworld") {
		t.Fatalf("Write returned %d, want %d", n, len("hello\nworld"))
	}

	// "hello" should have been emitted. "world" is still buffered
	event := <-ch
	if event.Message != "hello" {
		t.Fatalf("message = %q, want %q", event.Message, "hello")
	}

	w.Write([]byte("\n"))

	event = <-ch
	if event.Message != "world" {
		t.Fatalf("message = %q, want %q", event.Message, "world")
	}
}

func TestLogWriterFlush(t *testing.T) {
	db := testDB(t)
	hub := stream.NewHub()

	deployment := &models.Deployment{
		GithubURL: "https://github.com/amxrac/mpaas",
		Status:    models.StatusPending,
	}

	err := db.InsertDeployment(context.Background(), deployment)
	if err != nil {
		t.Fatalf("insert deployment: %v", err)
	}

	s := &Service{
		db:     db,
		stream: hub,
	}

	ch := hub.Subscribe(deployment.ID)
	defer hub.Unsubscribe(deployment.ID, ch)

	w := newLogWriter(context.Background(), s, deployment.ID)

	_, _ = w.Write([]byte("partial line"))
	w.Flush()

	event := <-ch
	if event.Message != "partial line" {
		t.Fatalf("message = %q, want %q", event.Message, "partial line")
	}

	if w.partial != "" {
		t.Fatalf("partial = %q, want empty", w.partial)
	}
}

func TestLogWriterIgnoresEmptyLines(t *testing.T) {
	db := testDB(t)
	hub := stream.NewHub()

	deployment := &models.Deployment{
		GithubURL: "https://github.com/amxrac/mpaas",
		Status:    models.StatusPending,
	}

	err := db.InsertDeployment(context.Background(), deployment)
	if err != nil {
		t.Fatalf("insert deployment: %v", err)
	}

	s := &Service{
		db:     db,
		stream: hub,
	}

	ch := hub.Subscribe(deployment.ID)
	defer hub.Unsubscribe(deployment.ID, ch)

	w := newLogWriter(context.Background(), s, deployment.ID)

	_, _ = w.Write([]byte("\n   \n\t\n"))

	select {
	case event := <-ch:
		t.Fatalf("unexpected event: %+v", event)
	default:
	}
}

func TestFailDeployment(t *testing.T) {
	db := testDB(t)
	hub := stream.NewHub()

	deployment := &models.Deployment{
		GithubURL: "https://github.com/amxrac/mpaas",
		Status:    models.StatusPending,
	}

	err := db.InsertDeployment(context.Background(), deployment)
	if err != nil {
		t.Fatalf("insert deployment: %v", err)
	}

	s := &Service{
		db:     db,
		stream: hub,
	}

	s.failDeployment(
		context.Background(),
		deployment,
		"build failed",
	)

	got, err := db.GetDeploymentByID(context.Background(), deployment.ID)
	if err != nil {
		t.Fatalf("get deployment: %v", err)
	}

	if got.Status != models.StatusFailed {
		t.Fatalf("status = %q, want %q", got.Status, models.StatusFailed)
	}

	logs, err := db.GetLogsAfterID(context.Background(), deployment.ID, "")
	if err != nil {
		t.Fatalf("get logs: %v", err)
	}

	if len(logs) != 1 {
		t.Fatalf("got %d logs, want 1", len(logs))
	}

	if !strings.Contains(logs[0].Message, "build failed") {
		t.Fatalf("log = %q, want build failed", logs[0].Message)
	}
}

func testDB(t *testing.T) *db.DB {
	t.Helper()

	conn, err := gorm.Open(
		gormlite.Open("file::memory:?cache=shared"),
		&gorm.Config{},
	)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}

	database := db.NewDB(conn)

	err = database.Migrate(
		&models.Deployment{},
		&models.LogEntry{},
	)
	if err != nil {
		t.Fatalf("migrate test database: %v", err)
	}

	t.Cleanup(func() {
		sqlDB, err := conn.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})

	return database
}
