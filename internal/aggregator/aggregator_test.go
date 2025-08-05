package aggregator

import (
	"context"
	"dlog/internal/conf"
	"fmt"
	"os"
	"path"
	"testing"
	"time"
)

func TestAggregator(t *testing.T) {
	cfg := &conf.Config{
		HealthCheckPort: 8080,
	}
	if err := os.Mkdir("logs", 0777); err != nil && !os.IsExist(err) {
		t.Fatalf("failed to create logs directory: %v", err)
	}
	defer os.RemoveAll("logs")

	trackedChan := make(chan conf.TrackedOption)
	recordChan := make(chan conf.RecordLog)
	ag := NewAggregatorService(cfg, trackedChan, recordChan)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		recordChan <- conf.RecordLog{
			ContainerName: "test-container",
			Log:           "some test log",
		}
		time.Sleep(500 * time.Millisecond)

		content, err := os.ReadFile(path.Join("logs", fmt.Sprintf("%s-test-container.log", time.Now().Format(time.DateOnly))))
		if err != nil {
			t.Errorf("failed to read log file: %v", err)
		} else if string(content) == "" {
			t.Error("log file is empty")
		}
		close(done)
	}()

	errChan := ag.Start(ctx)

	select {
	case <-done:

	case err := <-errChan:
		t.Fatalf("Aggregator encountered an error: %v", err)
	case <-ctx.Done():
		t.Fatal("test timed out")
	}
}
