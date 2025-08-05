package watcher

import (
	"bufio"
	"context"
	"dlog/internal/aggregator"
	"dlog/internal/conf"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/client"
)

type WatcherService struct {
	cfg      *conf.Config
	Watchers []*Watcher
}

func NewWatcherService(cfg *conf.Config) *WatcherService {
	return &WatcherService{
		cfg:      cfg,
		Watchers: make([]*Watcher, len(cfg.Hosts)),
	}
}

func (ws *WatcherService) Start(ctx context.Context) error {
	errChan := make(chan error, 1)
	for _, host := range ws.cfg.Hosts {
		w := NewWatcher(ws.cfg, host)
		ws.Watchers = append(ws.Watchers, w)
		go func() {
			if err := w.Start(ctx); err != nil {
				errChan <- err
			}
		}()
	}

	select {
	case err := <-errChan:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

type Watcher struct {
	Host        conf.Host
	dc          *client.Client
	tracked     map[string]context.CancelFunc
	trackedChan chan conf.TrackedOption
	recordChan  chan conf.RecordLog
	mu          sync.Mutex
	ag          *aggregator.AggregatorService
}

func NewWatcher(cfg *conf.Config, host conf.Host) *Watcher {
	trackedChan := make(chan conf.TrackedOption, 100)
	recordChan := make(chan conf.RecordLog, 1000)
	return &Watcher{
		Host:        host,
		tracked:     make(map[string]context.CancelFunc),
		trackedChan: trackedChan,
		recordChan:  recordChan,
		mu:          sync.Mutex{},
		ag:          aggregator.NewAggregatorService(cfg, host.Name, trackedChan, recordChan),
	}
}
func (w *Watcher) Start(ctx context.Context) error {
	fmt.Printf("%s host started \n", w.Host.Name)
	dc, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		log.Fatalf("Failed to create Docker client: %v", err)
	}
	w.dc = dc
	errChan := make(chan error, 1)

	go func() {
		if err := w.watchContainers(ctx); err != nil {
			errChan <- err
		}
	}()
	go func() {
		if err := w.registerCns(ctx); err != nil {
			errChan <- err
		}
	}()
	go func() {

		for err := range w.ag.Start(ctx) {
			errChan <- err
		}
	}()

	select {
	case err := <-errChan:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (w *Watcher) watchContainers(ctx context.Context) error {

	eventChan, errChan := w.dc.Events(ctx, events.ListOptions{})
	for {
		select {
		case err := <-errChan:
			fmt.Printf("failed to process docker event %s", err.Error())
			continue
		case event := <-eventChan:
			if event.Type == "container" {
				w.mu.Lock()
				name := event.Actor.Attributes["name"]
				switch event.Action {
				case "start":
					if _, ok := w.tracked[name]; !ok {
						containerCtx, cancel := context.WithCancel(ctx)
						w.tracked[name] = cancel
						go func() {
							if r := recover(); r != nil {
								fmt.Printf("panic in streamClog for container %s: %v", name, r)
							}
							w.streamClog(containerCtx, conf.StreamOpts{
								Name: name,
								ID:   event.Actor.ID,
							})
						}()
					}
				case "die", "stop", "kill":
					if cancel, exists := w.tracked[name]; exists {
						fmt.Printf("container %s log stopped", name)
						cancel()
						delete(w.tracked, name)
					}
				}
				w.mu.Unlock()
			}

		case <-ctx.Done():
			w.mu.Lock()
			for _, cancel := range w.tracked {
				cancel()
			}
			w.tracked = make(map[string]context.CancelFunc)
			w.mu.Unlock()
			fmt.Println("Context cancelled, stopping event watching")
			return nil
		}
	}
}

func (w *Watcher) registerCns(ctx context.Context) error {
	cl, err := w.dc.ContainerList(ctx, container.ListOptions{
		Latest: true,
		Before: time.Now().Format(time.DateTime),
	})
	if err != nil {
		fmt.Printf("failed to fetch containers list %s", err.Error())
		return err
	}

	for _, c := range cl {
		name := strings.TrimPrefix(c.Names[0], "/")
		_, exists := w.tracked[name]
		if !exists {
			w.mu.Lock()
			containerCtx, cancel := context.WithCancel(ctx)
			w.tracked[name] = cancel

			go func(containerID, containerName string) {
				defer func() {
					if r := recover(); r != nil {
						fmt.Printf("panic in streamClog for container %s: %v", containerID, r)
					}
				}()

				w.streamClog(containerCtx, conf.StreamOpts{
					Name: containerName,
					ID:   containerID,
				})
			}(c.ID, name)
			w.mu.Unlock()
		}
	}
	return nil
}

func (w *Watcher) streamClog(ctx context.Context, cn conf.StreamOpts) {
	containerID := cn.ID
	name := cn.Name
	logOptions := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
		Details:    false,
		Tail:       "0",
	}

	logReader, err := w.dc.ContainerLogs(ctx, containerID, logOptions)
	if err != nil {
		log.Printf("Failed to get logs for container %s: %v\n", name, err)
		return
	}
	defer logReader.Close()
	scanner := bufio.NewScanner(logReader)
	for scanner.Scan() {
		line := scanner.Text()
		w.recordChan <- conf.RecordLog{
			ContainerName: name,
			Log:           line,
		}
	}
	if err := scanner.Err(); err != nil {
		log.Printf("Error reading logs for %s: %v\n", name, err)
	}
}
