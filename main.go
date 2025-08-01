package main

import (
	"bufio"
	"context"
	"fmt"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"io"
	"log"
	"sync"
	"time"
)

func streamClog(ctx context.Context, wg *sync.WaitGroup, dc *client.Client, cn container.Summary, cancel context.CancelFunc) {
	defer func() {
		wg.Done()
		cancel()
	}()
	containerID := cn.ID
	name := cn.Names[0]
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
	defer func(logReader io.ReadCloser) {
		if err := logReader.Close(); err != nil {
			log.Printf("Failed to close log reader for %s: %s", name, err.Error())
		}
	}(logReader)

	scanner := bufio.NewScanner(logReader)
	for scanner.Scan() {
		fmt.Printf("[%s] %s: %s\n", time.Now().Format(time.RFC3339Nano), name, scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		log.Printf("Error reading logs for %s: %v\n", name, err)
	}
}

func watchContainers(ctx context.Context, dc *client.Client) {
	tracked := make(map[string]bool)
	var mu sync.Mutex

	for {
		containers, err := dc.ContainerList(ctx, container.ListOptions{
			All:    false,
			Latest: true,
		})
		if err != nil {
			log.Printf("Failed to list containers: %v\n", err)
			time.Sleep(5 * time.Second)
			continue
		}

		wg := sync.WaitGroup{}

		for _, cn := range containers {
			mu.Lock()
			if _, exists := tracked[cn.ID]; !exists {
				containerCtx, cancel := context.WithCancel(context.Background())
				wg.Add(1)

				go streamClog(containerCtx, &wg, dc, cn, cancel)
				tracked[cn.ID] = true
				go func(id, name string, ctx context.Context) {
					<-ctx.Done()
					mu.Lock()
					delete(tracked, id)
					mu.Unlock()
					log.Printf("Removed container from tracking: %s\n", name)
				}(cn.ID, cn.Names[0], containerCtx)
			}
			mu.Unlock()
		}
		fmt.Printf("tracking new containers, current containers count %d \n", len(tracked))
		time.Sleep(5 * time.Second)
	}
}

func main() {
	dc, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		log.Fatalf("Failed to create Docker client: %v\n", err)
	}

	ctx := context.Background()
	watchContainers(ctx, dc)
}
