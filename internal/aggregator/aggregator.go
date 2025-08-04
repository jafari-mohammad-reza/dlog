package aggregator

import (
	"context"
	"dlog/internal/conf"
	"errors"
	"fmt"
	"log"
	"os"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

type AggregatorService struct {
	cfg         *conf.Config
	openedFiles map[string]*os.File
	trackedChan chan conf.TrackedOption
	recordChan  chan conf.RecordLog
}

func NewAggregatorService(cfg *conf.Config, trackedChan chan conf.TrackedOption, recordChan chan conf.RecordLog) *AggregatorService {
	return &AggregatorService{
		cfg:         cfg,
		openedFiles: make(map[string]*os.File),
		trackedChan: trackedChan,
		recordChan:  recordChan,
	}
}
func (a *AggregatorService) Start(ctx context.Context) chan error {
	go a.loadLog()
	errChan := make(chan error)
	go func() {
		tk := time.NewTicker(time.Hour * 12)
		for range tk.C {
			select {
			case <-ctx.Done():
				return
			default:
				a.cleanup(ctx)
			}
		}
	}()
	go func() {
		err := a.watchDirs(ctx)
		if err != nil {
			errChan <- err
		}
	}()
	go func() {
		err := a.recordLogs()
		if err != nil {
			errChan <- err
		}
	}()
	return errChan
}
func (a *AggregatorService) recordLogs() error {

	for record := range a.recordChan {
		var file *os.File
		f, ok := a.openedFiles[fmt.Sprintf("%s-%s", time.Now().Format(time.DateOnly), record.ContainerName)]
		startRecord := time.Now()
		if !ok {
			if err := os.MkdirAll("logs", 0755); err != nil {
				log.Printf("Failed to create logs directory: %v\n", err)
				return err
			}
			file, err := os.OpenFile(path.Join("logs", fmt.Sprintf("%s-%s.log", time.Now().Format(time.DateOnly), record.ContainerName)), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if err != nil {
				log.Printf("Failed to open log file for %s: %v\n", record.ContainerName, err)
				return err
			}
			a.openedFiles[fmt.Sprintf("%s-%s", time.Now().Format(time.DateOnly), record.ContainerName)] = file
			f = file
		}
		file = f
		if _, err := fmt.Fprintf(file, "[%s] %s\n", time.Now().Format(time.DateTime), record.Log); err != nil {
			log.Printf("Failed to write log for %s: %v\n", record.ContainerName, err)
			return err
		}
		if err := file.Sync(); err != nil {
			log.Printf("Failed to sync log file for %s: %v\n", record.ContainerName, err)
			return err
		}
		took := time.Since(startRecord)
		fmt.Printf("processing %s log took %v\n", record.ContainerName, took)
	}
	for name, file := range a.openedFiles {
		if err := file.Close(); err != nil {
			log.Printf("Failed to close log file for %s: %v\n", name, err)
		}
	}
	return nil
}

func (a *AggregatorService) loadLog() error {
	entries, err := os.ReadDir("logs")
	if err != nil {
		return fmt.Errorf("error reading logs dir: %w", err)
	}

	latestFiles := make(map[string]struct {
		filename  string
		timestamp time.Time
	})

	for _, entry := range entries {
		filename := entry.Name()

		if !strings.HasSuffix(filename, ".log") {
			continue
		}

		parts := strings.Split(filename, "-")
		if len(parts) < 4 {
			continue
		}

		containerName := strings.TrimSuffix(parts[len(parts)-1], ".log")

		timestampStr := strings.Split(filename, fmt.Sprintf("-%s.log", containerName))[0]
		timestamp, err := time.Parse(time.DateOnly, timestampStr)
		if err != nil {
			fmt.Printf("Warning: skipping file %s - invalid timestamp: %v", filename, err)
			continue
		}

		current, exists := latestFiles[containerName]
		if !exists || timestamp.After(current.timestamp) {
			latestFiles[containerName] = struct {
				filename  string
				timestamp time.Time
			}{filename, timestamp}
		}
	}

	for container, fileInfo := range latestFiles {
		fullPath := path.Join("logs", fileInfo.filename)
		f, err := os.OpenFile(fullPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return fmt.Errorf("failed to open %s: %w", fullPath, err)
		}
		a.openedFiles[container] = f
	}

	return nil
}
func (a *AggregatorService) cleanup(ctx context.Context) {
	for key, file := range a.openedFiles {
		re := regexp.MustCompile(`^(\d{4}-(0[1-9]|1[0-2])-(0[1-9]|[12]\d|3[01]) \d{2}:\d{2}:\d{2})`)
		matches := re.FindStringSubmatch(key)

		if len(matches) < 2 {
			fmt.Printf("no date found in key: %s\n", key)
			continue
		}

		ft := matches[1]
		fmt.Printf("ft: %v\n", ft)

		ftTime, err := time.Parse(time.DateOnly, ft)
		fmt.Printf("ftTime: %v\n", ftTime)
		if err != nil {
			fmt.Printf("failed to parse %s date %s\n", key, err.Error())
			continue
		}

		if ftTime.Before(time.Now().Add(-24 * time.Hour)) {
			fmt.Printf("%s is deprecated\n", key)
			if err := file.Close(); err != nil {
				fmt.Printf("failed to close %s file\n", key)
				continue
			}
			delete(a.openedFiles, key)
		}
	}
}
func (a *AggregatorService) watchDirs(ctx context.Context) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		fmt.Println("failed to init watcher")
		return err
	}
	defer watcher.Close()
	if err := watcher.Add("logs"); err != nil {
		fmt.Println("failed to add logs to watcher")
	}
	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return errors.New("failed to watch new docker events")
			}
			if event.Op.Has(fsnotify.Remove) || event.Op.Has(fsnotify.Rename) {
				if !strings.Contains(event.Name, "logs/") || !strings.HasSuffix(event.Name, ".log") {
					continue
				}

				logPath := strings.Split(event.Name, "logs/")[1]
				filename := strings.TrimSuffix(logPath, ".log")
				parts := strings.Split(filename, "-")

				if len(parts) < 4 {
					continue
				}

				cname := parts[3]

				delete(a.openedFiles, filename)
				a.trackedChan <- conf.TrackedOption{
					ID: cname,
					Op: conf.RemoveTracked,
				}
			}
		case err := <-watcher.Errors:
			return fmt.Errorf("watcher error: %w", err)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
