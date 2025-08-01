package main

import (
	"bufio"
	"context"
	"fmt"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"io"
	"log"
	"os"
	"sync"
)

type LogRec struct {
	Name string
	Logs io.ReadCloser
}

func streamClog(wg *sync.WaitGroup, dc *client.Client, cn container.Summary) {
	defer wg.Done()

	logOptions := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Timestamps: true,
		Follow:     true,
	}

	logReader, err := dc.ContainerLogs(context.Background(), cn.ID, logOptions)
	name := cn.Names[0]
	if err != nil {
		log.Printf("Failed to get logs for container %s: %v\n", name, err)
		return
	}
	defer logReader.Close()

	scanner := bufio.NewScanner(logReader)
	for scanner.Scan() {
		fmt.Printf("[%s] %s\n", name, scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		log.Printf("Error reading logs for %s: %v\n", name, err)
	}
}
func main() {
	dc, err := client.NewClientWithOpts()
	if err != nil {
		fmt.Println("failed to create client: ", err.Error())
		os.Exit(1)
	}
	containers, err := dc.ContainerList(context.Background(), container.ListOptions{
		All:    true,
		Latest: true,
	})
	if err != nil {
		fmt.Printf("failed to get contaienr list %s", err.Error())
		os.Exit(1)
	}
	wg := sync.WaitGroup{}
	for _, cn := range containers {
		wg.Add(1)
		go streamClog(&wg, dc, cn)
	}
	wg.Wait()
}
