package watcher

import (
	"bufio"
	"context"
	"dlog/internal/aggregator"
	"dlog/internal/conf"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
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
	cfg         *conf.Config
	Host        conf.Host
	dc          *client.Client
	tracked     map[string]context.CancelFunc
	trackedChan chan conf.TrackedOption
	recordChan  chan conf.RecordLog
	crashedChan chan conf.RecordLog
	statChan    chan conf.StatLog
	mu          sync.Mutex
	ag          *aggregator.AggregatorService
}

func NewWatcher(cfg *conf.Config, host conf.Host) *Watcher {
	trackedChan := make(chan conf.TrackedOption, 100)
	recordChan := make(chan conf.RecordLog, 1000)
	crashChan := make(chan conf.RecordLog, 1000)
	statChan := make(chan conf.StatLog, 1000)
	return &Watcher{
		cfg:         cfg,
		Host:        host,
		tracked:     make(map[string]context.CancelFunc),
		trackedChan: trackedChan,
		recordChan:  recordChan,
		crashedChan: crashChan,
		statChan:    statChan,
		mu:          sync.Mutex{},
		ag:          aggregator.NewAggregatorService(cfg, host.Name, trackedChan, recordChan, crashChan, statChan),
	}
}
func (w *Watcher) Start(ctx context.Context) error {
	fmt.Printf("%s host started \n", w.Host.Name)
	dc, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		log.Fatalf("Failed to create Docker client: %v", err)
	}
	pctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()
	if _, err := dc.Ping(pctx); err != nil {
		return fmt.Errorf("failed to connect to %s", w.Host.Name)
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
		w.closeDc()
		return err
	case <-ctx.Done():
		w.closeDc()
		return ctx.Err()
	}
}
func (w *Watcher) closeDc() error {
	if w.dc != nil {
		return w.dc.Close()
	}
	return nil
}

func (w *Watcher) watchContainers(ctx context.Context) error {
outer:
	for {
		eventChan, errChan := w.dc.Events(ctx, events.ListOptions{})

		for {
			select {
			case err, ok := <-errChan:
				if !ok {
					fmt.Println("error channel closed")
					break outer
				}
				if err != nil {
					fmt.Printf("failed to process docker event: %v", err)
				} else {
					fmt.Println("docker event channel closed")
				}
				break outer

			case event, ok := <-eventChan:
				if !ok {
					fmt.Println("event channel closed")
					break outer
				}

				if event.Type == "container" {
					w.mu.Lock()
					name := event.Actor.Attributes["name"]
					switch event.Action {
					case "start":
						if _, ok := w.tracked[name]; !ok {
							containerCtx, cancel := context.WithCancel(ctx)
							w.tracked[name] = cancel
							go func(name, id string) {
								defer func() {
									if r := recover(); r != nil {
										fmt.Printf("panic in streamClog for container %s: %v", name, r)
									}
								}()
								go func() {
									err := w.trackResourceUsage(containerCtx)
									if err != nil {
										fmt.Printf("failed to track resource usage for container %s: %v", name, err)
									}
								}()
								w.streamClog(containerCtx, conf.StreamOpts{
									Name: name,
									ID:   id,
								})
							}(name, event.Actor.ID)
						}
					case "die", "stop", "kill":
						if event.Action == "die" || event.Action == "kill" {
							clog, err := w.dc.ContainerLogs(ctx, event.Actor.ID, container.LogsOptions{
								Tail:       "10",
								ShowStdout: true,
								ShowStderr: true,
							})
							if err != nil {
								fmt.Printf("failed to get container logs for %s: %v", name, err)
								w.mu.Unlock()
								continue
							}
							cl, err := io.ReadAll(clog)
							clog.Close()
							if err != nil {
								fmt.Printf("failed to read container logs for %s: %v", name, err)
								w.mu.Unlock()
								break outer
							}
							w.crashedChan <- conf.RecordLog{
								ContainerName: name,
								Log:           cl,
							}
						}
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
	time.Sleep(2 * time.Second)
	fmt.Println("Reconnecting to Docker events...")
	return nil
}

func (w *Watcher) trackResourceUsage(ctx context.Context) error {
	for id, _ := range w.tracked {
		stats, err := w.dc.ContainerStats(ctx, id, true)
		if err != nil {
			fmt.Printf("failed to fetch container stats for %s: %s\n", id, err.Error())
			continue
		}
		scanner := bufio.NewScanner(stats.Body)
		for scanner.Scan() {
			var stat conf.ContainerStats
			err := json.Unmarshal(scanner.Bytes(), &stat)
			if err != nil {
				fmt.Printf("failed to unmarshal container stats for %s: %s\n", id, err.Error())
				continue
			}
			statLog := conf.StatLog{
				CpuUsage:      stat.CPUStats.CPUUsage.TotalUsage,
				MemUsage:      stat.MemoryStats.Usage,
				ContainerName: id,
				ContainerID:   stat.ID,
			}
			w.statChan <- statLog
			time.Sleep(time.Minute * time.Duration(w.cfg.StatInterval))
		}
	}
	return nil
}
func (w *Watcher) registerCns(ctx context.Context) error {
	cl, err := w.dc.ContainerList(ctx, container.ListOptions{
		Latest: true,
		Before: time.Now().Format(time.DateTime),
	})
	if err != nil {
		fmt.Printf("failed to fetch containers list %s\n", err.Error())
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
						fmt.Printf("panic in streamClog for container %s: %v\n", containerID, r)
					}
				}()
				go func() {
					err := w.trackResourceUsage(containerCtx)
					if err != nil {
						fmt.Printf("failed to track resource usage for container %s: %v\n", name, err)
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
	var scanner *bufio.Scanner
	if w.Host.Method == conf.Socket {
		logReader, err := w.dc.ContainerLogs(ctx, containerID, logOptions)
		if err != nil {
			log.Printf("Failed to get logs for container %s: %v\n", name, err)
			return
		}
		defer logReader.Close()
		scanner = bufio.NewScanner(logReader)
	} else if w.Host.Method == conf.File {
		insp, err := w.dc.ContainerInspect(ctx, cn.ID)
		if err != nil {
			fmt.Printf("failed to get %s inspects: %s\n", cn.Name, err.Error())
			return
		}
		f, err := os.OpenFile(insp.LogPath, os.O_WRONLY, 0644)
		if err != nil {
			fmt.Printf("failed to open %s log file: %s\n", cn.Name, err.Error())
			return
		}
		defer f.Close()
		scanner = bufio.NewScanner(f)
	} else {
		fmt.Printf("invalid log scanning method for %s\n", cn.Name)
		return
	}

	for scanner.Scan() {
		line := scanner.Bytes()
		w.recordChan <- conf.RecordLog{
			ContainerName: name,
			Log:           line,
		}
	}
	if err := scanner.Err(); err != nil {
		log.Printf("Error reading logs for %s: %v\n", name, err)
	}
}
