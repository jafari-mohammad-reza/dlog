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

func streamClog(ctx context.Context, wg *sync.WaitGroup, dc *client.Client, containerID, name string) {
	defer wg.Done()

	logOptions := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Timestamps: true,
		Follow:     true,
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
			All:    true,
			Latest: true,
		})
		if err != nil {
			log.Printf("Failed to list containers: %v\n", err)
			time.Sleep(5 * time.Second)
			continue
		}

		mu.Lock()
		wg := sync.WaitGroup{}

		for _, cn := range containers {
			if !tracked[cn.ID] {
				tracked[cn.ID] = true
				wg.Add(1)
				go streamClog(ctx, &wg, dc, cn.ID, cn.Names[0])
			}
		}
		mu.Unlock()
		fmt.Println("new containers tracked")
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
