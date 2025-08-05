package aggregator

import (
	"context"
	"dlog/internal/conf"
	"os"
	"path"
	"testing"
	"time"
)

func TestAggregator_recordLogs(t *testing.T) {
	cfg := &conf.Config{}
	recordChan := make(chan conf.RecordLog, 1)
	ag := NewAggregatorService(cfg, nil, recordChan)

	go func() {
		recordChan <- conf.RecordLog{
			ContainerName: "test-record",
			Log:           "log entry",
		}
		close(recordChan)
	}()

	err := ag.recordLogs()
	if err != nil {
		t.Fatalf("recordLogs failed: %v", err)
	}

	logFile := path.Join("logs", time.Now().Format(time.DateOnly)+"-test-record.log")
	defer os.Remove(logFile)
	content, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}
	if len(content) == 0 {
		t.Error("log file is empty")
	}
}

func TestAggregator_loadLog(t *testing.T) {
	cfg := &conf.Config{}
	ag := NewAggregatorService(cfg, nil, nil)

	os.Mkdir("logs", 0777)
	defer os.RemoveAll("logs")

	logFile := path.Join("logs", time.Now().Format(time.DateOnly)+"-test-test.log")
	os.WriteFile(logFile, []byte("test log"), 0644)

	err := ag.loadLog()
	if err != nil {
		t.Fatalf("loadLog failed: %v", err)
	}
	if len(ag.openedFiles) == 0 {
		t.Error("openedFiles should not be empty after loadLog")
	}
}

func TestAggregator_cleanup(t *testing.T) {
	cfg := &conf.Config{}
	ag := NewAggregatorService(cfg, nil, nil)

	oldDate := time.Now().Add(-48 * time.Hour).Format(time.DateOnly)
	fileName := oldDate + "-test-cleanup"
	os.Mkdir("logs", 0777)
	defer os.RemoveAll("logs")
	f, _ := os.Create(path.Join("logs", fileName+".log"))
	ag.openedFiles[fileName] = f

	ctx := context.Background()
	ag.cleanup(ctx)

	if _, exists := ag.openedFiles[fileName]; exists {
		t.Error("cleanup did not remove old file from openedFiles")
	}
}

func TestAggregator_watchDirs_cancel(t *testing.T) {
	cfg := &conf.Config{}
	trackedChan := make(chan conf.TrackedOption)
	ag := NewAggregatorService(cfg, "localhost", trackedChan, nil)

	os.Mkdir("logs", 0777)
	defer os.RemoveAll("logs")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := ag.watchDirs(ctx)
	if err == nil || err == context.Canceled {

	} else {
		t.Errorf("watchDirs returned unexpected error: %v", err)
	}
}
