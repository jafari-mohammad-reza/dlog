package aggregator

import (
	"context"
	"dlog/internal/conf"
	"os"
	"path"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewAggregatorService(t *testing.T) {
	cfg := &conf.Config{}
	trackedChan := make(chan conf.TrackedOption)
	recordChan := make(chan conf.RecordLog)
	crashChan := make(chan conf.RecordLog)
	statChan := make(chan conf.StatLog)

	ag := NewAggregatorService(cfg, "test-host", trackedChan, recordChan, crashChan, statChan)

	assert.NotNil(t, ag)
	assert.Equal(t, cfg, ag.cfg)
	assert.Equal(t, "test-host", ag.host)
	assert.NotNil(t, ag.openedFiles)
	assert.NotNil(t, ag.statMap)
	assert.Equal(t, trackedChan, ag.trackedChan)
	assert.Equal(t, recordChan, ag.recordChan)
	assert.Equal(t, crashChan, ag.crashChan)
	assert.Equal(t, statChan, ag.statChan)
}

func TestAggregatorService_Start(t *testing.T) {
	cfg := &conf.Config{}
	ag := NewAggregatorService(cfg, "localhost", nil, nil, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	errChan := ag.Start(ctx)
	assert.NotNil(t, errChan)

	// Wait for context to be cancelled
	<-ctx.Done()
}

func TestAggregator_recordCrashes(t *testing.T) {
	cfg := &conf.Config{}
	crashChan := make(chan conf.RecordLog, 1)
	ag := NewAggregatorService(cfg, "localhost", nil, nil, crashChan, nil)

	defer os.RemoveAll("localhost-logs")

	go func() {
		crashChan <- conf.RecordLog{
			ContainerName: "test-container",
			Log:           []byte("error occurred\n"),
		}
		close(crashChan)
	}()

	err := ag.recordCrashes()
	require.NoError(t, err)

	// Verify crash log was created
	crashFile := path.Join("localhost-logs", time.Now().Format(time.DateOnly)+"-test-container-crashes.log")
	content, err := os.ReadFile(crashFile)
	require.NoError(t, err)
	assert.Contains(t, string(content), "error occurred")
}

func TestAggregator_recordStats(t *testing.T) {
	cfg := &conf.Config{}
	statChan := make(chan conf.StatLog, 1)
	ag := NewAggregatorService(cfg, "localhost", nil, nil, nil, statChan)

	defer os.RemoveAll("localhost_stats")

	go func() {
		statChan <- conf.StatLog{
			ContainerName: "test-container",
			CpuUsage:      1000,
			MemUsage:      2000,
			ContainerID:   "test-id",
		}
		close(statChan)
	}()

	err := ag.recordStats()
	require.NoError(t, err)

	// Verify stats file was created
	statsFile := path.Join("localhost_stats", time.Now().Format(time.DateOnly)+"-test-container.log")
	content, err := os.ReadFile(statsFile)
	require.NoError(t, err)
	assert.Contains(t, string(content), "Mem usage: 2000")
	assert.Contains(t, string(content), "CPU usage: 1000")
}

// TODO: fix failing test and write test for crash listener
func TestAggregator_recordLogs(t *testing.T) {
	cfg := &conf.Config{}
	recordChan := make(chan conf.RecordLog, 1)
	ag := NewAggregatorService(cfg, "localhost", nil, recordChan, nil, nil)

	defer os.RemoveAll("localhost-logs")

	go func() {
		recordChan <- conf.RecordLog{
			ContainerName: "test-record",
			Log:           []byte("log entry"),
		}
		close(recordChan)
	}()

	err := ag.recordLogs()
	require.NoError(t, err)

	logFile := path.Join("localhost-logs", time.Now().Format(time.DateOnly)+"-test-record.log")
	content, err := os.ReadFile(logFile)
	require.NoError(t, err)
	assert.NotEmpty(t, content)
	assert.Contains(t, string(content), "log entry")
}

func TestStripANSIAndControlChars(t *testing.T) {
	testCases := []struct {
		input    string
		expected string
	}{
		{
			input:    "\x1b[31mRed text\x1b[0m",
			expected: "Red text",
		},
		{
			input:    "Normal text",
			expected: "Normal text",
		},
		{
			input:    "Text with\ttab\nand newline",
			expected: "Text with\ttab\nand newline",
		},
		{
			input:    "Text with \x07 bell character",
			expected: "Text with  bell character",
		},
		{
			input:    "\x1b[1;32mBold Green\x1b[0m Normal",
			expected: "Bold Green Normal",
		},
	}

	for _, tc := range testCases {
		t.Run("", func(t *testing.T) {
			result := stripANSIAndControlChars(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestEnsureUTF8(t *testing.T) {
	testCases := []struct {
		name     string
		input    []byte
		expected string
	}{
		{
			name:     "valid UTF-8",
			input:    []byte("Hello, 世界"),
			expected: "Hello, 世界",
		},
		{
			name:     "invalid UTF-8 bytes",
			input:    []byte{0xFF, 0xFE, 0x41, 0x42},
			expected: "ÿþAB",
		},
		{
			name:     "mixed valid and invalid",
			input:    []byte("Hello" + string([]byte{0xFF}) + "World"),
			expected: "HelloÿWorld",
		},
		{
			name:     "empty input",
			input:    []byte{},
			expected: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := ensureUTF8(tc.input)
			assert.Equal(t, tc.expected, string(result))
		})
	}
}

func TestAggregator_loadLog(t *testing.T) {
	cfg := &conf.Config{}
	ag := NewAggregatorService(cfg, "localhost", nil, nil, nil, nil)

	os.Mkdir("localhost-logs", 0777)
	defer os.RemoveAll("localhost-logs")

	logFile := path.Join("localhost-logs", time.Now().Format(time.DateOnly)+"-test-test.log")
	os.WriteFile(logFile, []byte("test log"), 0644)

	err := ag.loadLog()
	require.NoError(t, err)
	assert.NotEmpty(t, ag.openedFiles)
}

func TestAggregator_loadLog_NoDirectory(t *testing.T) {
	cfg := &conf.Config{}
	ag := NewAggregatorService(cfg, "nonexistent", nil, nil, nil, nil)

	err := ag.loadLog()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "error reading logs dir")
}

func TestAggregator_loadLog_InvalidFiles(t *testing.T) {
	cfg := &conf.Config{}
	ag := NewAggregatorService(cfg, "localhost", nil, nil, nil, nil)

	os.Mkdir("localhost-logs", 0777)
	defer os.RemoveAll("localhost-logs")

	// Create files with invalid names
	os.WriteFile(path.Join("localhost-logs", "invalid.txt"), []byte("test"), 0644)
	os.WriteFile(path.Join("localhost-logs", "short.log"), []byte("test"), 0644)
	os.WriteFile(path.Join("localhost-logs", "invalid-date-format.log"), []byte("test"), 0644)

	err := ag.loadLog()
	require.NoError(t, err)
	assert.Empty(t, ag.openedFiles) // Should be empty due to invalid files
}

func TestAggregator_cleanup(t *testing.T) {
	cfg := &conf.Config{}
	ag := NewAggregatorService(cfg, "localhost", nil, nil, nil, nil)

	oldDate := time.Now().Add(-48 * time.Hour).Format(time.DateOnly)
	fileName := oldDate + "-test-cleanup"
	os.Mkdir("localhost-logs", 0777)
	defer os.RemoveAll("localhost-logs")
	f, _ := os.Create(path.Join("localhost-logs", fileName+".log"))
	ag.openedFiles[fileName] = f

	ctx := context.Background()
	ag.cleanup(ctx)

	_, exists := ag.openedFiles[fileName]
	assert.False(t, exists, "cleanup should remove old file from openedFiles")
}

func TestAggregator_cleanup_InvalidDateFormat(t *testing.T) {
	cfg := &conf.Config{}
	ag := NewAggregatorService(cfg, "localhost", nil, nil, nil, nil)

	f, _ := os.Create("test.log")
	defer f.Close()
	ag.openedFiles["invalid-date-format"] = f

	ctx := context.Background()
	ag.cleanup(ctx)

	// File should still exist because date parsing failed
	_, exists := ag.openedFiles["invalid-date-format"]
	assert.True(t, exists)
}

func TestAggregator_cleanup_RecentFile(t *testing.T) {
	cfg := &conf.Config{}
	ag := NewAggregatorService(cfg, "localhost", nil, nil, nil, nil)

	recentDate := time.Now().Format(time.DateOnly)
	fileName := recentDate + "-recent-file"
	f, _ := os.Create("test.log")
	defer f.Close()
	ag.openedFiles[fileName] = f

	ctx := context.Background()
	ag.cleanup(ctx)

	// Recent file should not be removed
	_, exists := ag.openedFiles[fileName]
	assert.True(t, exists)
}

func TestAggregator_mergeClosedLogs(t *testing.T) {
	cfg := &conf.Config{}
	ag := NewAggregatorService(cfg, "localhost", nil, nil, nil, nil)

	os.Mkdir("localhost-logs", 0777)
	defer os.RemoveAll("localhost-logs")

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := ag.mergeClosedLogs(ctx)
	assert.Error(t, err) // Should return context.DeadlineExceeded or Canceled
}

func TestAggregator_mergeClosedLogs_NoDirectory(t *testing.T) {
	cfg := &conf.Config{}
	ag := NewAggregatorService(cfg, "nonexistent", nil, nil, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := ag.mergeClosedLogs(ctx)
	assert.Error(t, err)
}

func TestAggregator_watchDirs_cancel(t *testing.T) {
	cfg := &conf.Config{}
	trackedChan := make(chan conf.TrackedOption)
	ag := NewAggregatorService(cfg, "localhost", trackedChan, nil, nil, nil)

	os.Mkdir("localhost-logs", 0777)
	defer os.RemoveAll("localhost-logs")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := ag.watchDirs(ctx)
	assert.Error(t, err)
	assert.Equal(t, context.Canceled, err)
}

func TestAggregator_watchDirs_CreateDirectory(t *testing.T) {
	cfg := &conf.Config{}
	trackedChan := make(chan conf.TrackedOption)
	ag := NewAggregatorService(cfg, "testhost", trackedChan, nil, nil, nil)

	defer os.RemoveAll("testhost-logs")

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := ag.watchDirs(ctx)
	assert.Error(t, err) // Should timeout

	// Verify directory was created
	_, err = os.Stat("testhost-logs")
	assert.NoError(t, err)
}
