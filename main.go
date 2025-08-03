package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"path"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/client"
)

type RecordLog struct {
	ContainerName string
	Log           string
}
type StreamOpts struct {
	Name string
	ID   string
}

var recordChan chan RecordLog

func streamClog(ctx context.Context, dc *client.Client, cn StreamOpts) {
	containerID := cn.ID
	name := cn.Name
	logOptions := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Timestamps: true,
		Follow:     true,
		Tail:       "0",
	}

	logReader, err := dc.ContainerLogs(ctx, containerID, logOptions)
	if err != nil {
		log.Printf("Failed to get logs for container %s: %v\n", name, err)
		return
	}
	defer logReader.Close()

	scanner := bufio.NewScanner(logReader)
	for scanner.Scan() {
		line := scanner.Text()
		recordChan <- RecordLog{
			ContainerName: name,
			Log:           line,
		}
	}
	if err := scanner.Err(); err != nil {
		log.Printf("Error reading logs for %s: %v\n", name, err)
	}
}

func watchContainers(ctx context.Context, dc *client.Client) {
	tracked := make(map[string]context.CancelFunc)
	var mu sync.Mutex
	// already running containers
	cl, err := dc.ContainerList(ctx, container.ListOptions{
		Latest: true,
		Before: time.Now().Format(time.DateTime),
	})
	if err != nil {
		fmt.Printf("failed to fetch containers list %s", err.Error())
		os.Exit(1)
	}
	mu.Lock()
	for _, c := range cl {
		if _, exists := tracked[c.ID]; !exists {
			containerCtx, cancel := context.WithCancel(ctx)
			tracked[c.ID] = cancel

			go func(containerID, containerName string) {
				defer func() {
					if r := recover(); r != nil {
						fmt.Printf("panic in streamClog for container %s: %v", containerID, r)
					}
				}()

				streamClog(containerCtx, dc, StreamOpts{
					Name: containerName,
					ID:   containerID,
				})
			}(c.ID, c.Names[0])
		}
	}
	mu.Unlock()
	eventChan, errChan := dc.Events(ctx, events.ListOptions{})
	// new containers activity
	for {
		select {
		case err := <-errChan:
			fmt.Printf("failed to process docker event %s", err.Error())
			continue
		case event := <-eventChan:
			if event.Type == "container" {
				mu.Lock()
				switch event.Action {
				case "start":
					if _, ok := tracked[event.Actor.ID]; !ok {
						containerCtx, cancel := context.WithCancel(ctx)
						tracked[event.Actor.ID] = cancel
						go func() {
							if r := recover(); r != nil {
								fmt.Printf("panic in streamClog for container %s: %v", event.Actor.ID, r)
							}
							streamClog(containerCtx, dc, StreamOpts{
								Name: event.Actor.Attributes["name"],
								ID:   event.Actor.ID,
							})
						}()
					}
				case "die", "stop", "kill":
					if cancel, exists := tracked[event.Actor.ID]; exists {
						fmt.Printf("container %s log stopped", event.Actor.ID)
						cancel()
						delete(tracked, event.Actor.ID)
					}
				}
				mu.Unlock()
			}

		case <-ctx.Done():
			mu.Lock()
			for _, cancel := range tracked {
				cancel()
			}
			tracked = make(map[string]context.CancelFunc)
			mu.Unlock()
			fmt.Println("Context cancelled, stopping event watching")
			return
		}
	}
}

func recordLogs() {
	openedFiles := make(map[string]*os.File)
	loadLog(openedFiles)
	go func() {
		tk := time.NewTicker(time.Hour * 12)
		for {
			select {
			case <-tk.C:
				cleanupOldFiles(openedFiles)
			}
		}
	}()
	for record := range recordChan {
		var file *os.File
		f, ok := openedFiles[fmt.Sprintf("%s-%s", time.Now().Format(time.DateOnly), record.ContainerName)]
		if !ok {
			if err := os.MkdirAll("logs", 0755); err != nil {
				log.Printf("Failed to create logs directory: %v\n", err)
				continue
			}
			file, err := os.OpenFile(path.Join("logs", fmt.Sprintf("%s-%s.log", time.Now().Format(time.DateOnly), record.ContainerName)), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if err != nil {
				log.Printf("Failed to open log file for %s: %v\n", record.ContainerName, err)
				continue
			}
			openedFiles[fmt.Sprintf("%s-%s", time.Now().Format(time.DateOnly), record.ContainerName)] = file
			f = file
		}
		file = f
		if _, err := fmt.Fprintf(file, "[%s] %s\n", time.Now().Format(time.DateTime), record.Log); err != nil {
			log.Printf("Failed to write log for %s: %v\n", record.ContainerName, err)
			continue
		}
		if err := file.Sync(); err != nil {
			log.Printf("Failed to sync log file for %s: %v\n", record.ContainerName, err)
			continue
		}
	}
	for name, file := range openedFiles {
		if err := file.Close(); err != nil {
			log.Printf("Failed to close log file for %s: %v\n", name, err)
		}
	}
}

func loadLog(openedFiles map[string]*os.File) error {
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
		openedFiles[container] = f
	}

	return nil
}
func cleanupOldFiles(openedFiles map[string]*os.File) {
	fmt.Println("ticked")
	for key, file := range openedFiles {

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
			delete(openedFiles, key)
		}
	}
}

func healthCheck() {
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("healthy"))
	})
	if err := http.ListenAndServe(":8080", nil); err != nil {
		fmt.Println("failed to start healthpeak")
	}
}

func main() {
	recordChan = make(chan RecordLog, 1000)
	dc, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		log.Fatalf("Failed to create Docker client: %v\n", err)
	}

	ctx := context.Background()
	go recordLogs()
	go healthCheck()
	watchContainers(ctx, dc)
}
