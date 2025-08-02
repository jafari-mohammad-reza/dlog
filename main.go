package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"path"
	"regexp"
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

	eventChan, errChan := dc.Events(ctx, events.ListOptions{})

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
						go streamClog(containerCtx, dc, StreamOpts{
							Name: event.Actor.Attributes["name"],
							ID:   event.Actor.ID,
						})
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
	go func() {
		tk := time.NewTicker(time.Minute * 10)
		for {
			select {
			case <-tk.C:
				cleanupOldFiles(openedFiles)
			}
		}
	}()
	for record := range recordChan {
		var file *os.File
		f, ok := openedFiles[fmt.Sprintf("%s-%s", time.Now().Format(time.DateTime), record.ContainerName)]
		if !ok {
			if err := os.MkdirAll("logs", 0755); err != nil {
				log.Printf("Failed to create logs directory: %v\n", err)
				continue
			}
			file, err := os.OpenFile(path.Join("logs", fmt.Sprintf("%s-%s.log", time.Now().Format(time.DateTime), record.ContainerName)), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if err != nil {
				log.Printf("Failed to open log file for %s: %v\n", record.ContainerName, err)
				continue
			}
			openedFiles[fmt.Sprintf("%s-%s", time.Now().Format(time.DateTime), record.ContainerName)] = file
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

		ftTime, err := time.Parse(time.DateTime, ft)
		fmt.Printf("ftTime: %v\n", ftTime)
		if err != nil {
			fmt.Printf("failed to parse %s date %s\n", key, err.Error())
			continue
		}

		if ftTime.Before(time.Now().Add(-15 * time.Minute)) {
			fmt.Printf("%s is deprecated\n", key)
			if err := file.Close(); err != nil {
				fmt.Printf("failed to close %s file\n", key)
				continue
			}
			delete(openedFiles, key)
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
