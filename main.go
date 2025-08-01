package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"path"
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

var recordChan chan RecordLog

func streamClog(ctx context.Context, dc *client.Client, cn container.Summary, cancel context.CancelFunc) {
	containerID := cn.ID
	name := strings.TrimPrefix(cn.Names[0], "/")
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
		fmt.Printf("[%s] %s: %s\n", time.Now().Format(time.RFC3339Nano), name, line)
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

	// Initial scan for running containers
	containers, err := dc.ContainerList(ctx, container.ListOptions{})
	if err != nil {
		log.Printf("Failed to list containers: %v\n", err)
	} else {
		for _, cn := range containers {
			mu.Lock()
			if _, exists := tracked[cn.ID]; !exists {
				name := cn.Names[0]
				name = strings.TrimPrefix(name, "/")
				containerCtx, cancel := context.WithCancel(context.Background())
				go streamClog(containerCtx, dc, container.Summary{
					ID:    cn.ID,
					Names: []string{name},
				}, cancel)
				tracked[cn.ID] = cancel
				log.Printf("Started tracking logs for running container: %s\n", name)
			}
			mu.Unlock()
		}
	}

	eventCh, errCh := dc.Events(ctx, events.ListOptions{})

	for {
		select {
		case event := <-eventCh:
			if event.Type == "container" && (event.Action == "start" || event.Action == "create") {
				containerID := event.Actor.ID
				mu.Lock()
				if _, exists := tracked[containerID]; !exists {
					containerJSON, err := dc.ContainerInspect(ctx, containerID)
					if err != nil {
						log.Printf("Failed to inspect container %s: %v\n", containerID, err)
						mu.Unlock()
						continue
					}
					name := containerJSON.Name
					name = strings.TrimPrefix(name, "/")
					containerCtx, cancel := context.WithCancel(context.Background())
					go streamClog(containerCtx, dc, container.Summary{
						ID:    containerID,
						Names: []string{name},
					}, cancel)
					tracked[containerID] = cancel
					log.Printf("Started tracking logs for container: %s\n", name)
				}
				mu.Unlock()
			}
			if event.Type == "container" && (event.Action == "die" || event.Action == "stop") {
				containerID := event.Actor.ID
				mu.Lock()
				if cancel, exists := tracked[containerID]; exists {
					cancel()
					delete(tracked, containerID)
					log.Printf("Stopped tracking logs for container: %s\n", containerID)
				}
				mu.Unlock()
			}
		case err := <-errCh:
			log.Printf("Error from Docker events stream: %v\n", err)
			time.Sleep(2 * time.Second)
		}
	}
}

func recordLogs() {
	openedFiles := make(map[string]*os.File)
	for record := range recordChan {
		log.Printf("Recording log for container %s\n", record.ContainerName)
		var file *os.File
		f, ok := openedFiles[record.ContainerName]
		if !ok {
			if err := os.MkdirAll("logs", 0755); err != nil {
				log.Printf("Failed to create logs directory: %v\n", err)
				continue
			}
			file, err := os.OpenFile(path.Join("logs", fmt.Sprintf("%s.log", record.ContainerName)), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if err != nil {
				log.Printf("Failed to open log file for %s: %v\n", record.ContainerName, err)
				continue
			}
			openedFiles[record.ContainerName] = file
			f = file
		}
		file = f
		if _, err := fmt.Fprintf(file, "[%s] %s\n", time.Now().Format(time.RFC3339Nano), record.Log); err != nil {
			log.Printf("Failed to write log for %s: %v\n", record.ContainerName, err)
			continue
		}
		if err := file.Sync(); err != nil {
			log.Printf("Failed to sync log file for %s: %v\n", record.ContainerName, err)
			continue
		}
		log.Printf("Log for container %s recorded successfully\n", record.ContainerName)
	}
	for name, file := range openedFiles {
		if err := file.Close(); err != nil {
			log.Printf("Failed to close log file for %s: %v\n", name, err)
		}
	}
}

func main() {
	recordChan = make(chan RecordLog, 1000)
	dc, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		log.Fatalf("Failed to create Docker client: %v\n", err)
	}

	ctx := context.Background()
	go watchContainers(ctx, dc)
	go recordLogs()
	select {}
}
