package aggregator

import (
	"context"
	"dlog/internal/conf"
	"os"
	"testing"
)

func TestAggregator(t *testing.T) {
	cfg := &conf.Config{
		HealthCheckPort: 8080,
	}
	err := os.Mkdir("logs", 0644)
	if err != nil && !os.IsExist(err) {
		t.Fatalf("failed to create logs directory: %v", err)
	}
	defer os.RemoveAll("logs")
	trackedChan := make(chan conf.TrackedOption)
	recordChan := make(chan conf.RecordLog)
	ag := NewAggregatorService(cfg, trackedChan, recordChan)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errChan := ag.Start(ctx)
	for err := range errChan {
		if err != nil {
			t.Errorf("Aggregator encountered an error: %v", err)
		}
	}
}
