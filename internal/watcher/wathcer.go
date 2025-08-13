package watcher

import (
	"bufio"
	"context"
	"dlog/internal/aggregator"
	"dlog/internal/conf"
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
	Host        conf.Host
	dc          *client.Client
	tracked     map[string]context.CancelFunc
	trackedChan chan conf.TrackedOption
	recordChan  chan conf.RecordLog
	crashedChan chan conf.RecordLog
	mu          sync.Mutex
	ag          *aggregator.AggregatorService
}

func NewWatcher(cfg *conf.Config, host conf.Host) *Watcher {
	trackedChan := make(chan conf.TrackedOption, 100)
	recordChan := make(chan conf.RecordLog, 1000)
	crashChan := make(chan conf.RecordLog, 1000)
	return &Watcher{
		Host:        host,
		tracked:     make(map[string]context.CancelFunc),
		trackedChan: trackedChan,
		recordChan:  recordChan,
		crashedChan: crashChan,
		mu:          sync.Mutex{},
		ag:          aggregator.NewAggregatorService(cfg, host.Name, trackedChan, recordChan, crashChan),
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
	for {
		eventChan, errChan := w.dc.Events(ctx, events.ListOptions{})

		for {
			select {
			case err := <-errChan:
				if err != nil {
					fmt.Printf("failed to process docker event: %v\n", err)
				} else {
					fmt.Println("docker event channel closed")
				}
				goto reconnect

			case event := <-eventChan:
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
								fmt.Printf("failed to get container logs for %s: %v\n", name, err)
								continue
							}
							cl, err := io.ReadAll(clog)
							if err != nil {
								fmt.Printf("failed to read container logs for %s: %v\n", name, err)
								continue
							}
							w.crashedChan <- conf.RecordLog{
								ContainerName: name,
								Log:           string(cl),
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

	reconnect:
		time.Sleep(2 * time.Second)
		fmt.Println("Reconnecting to Docker events...")
	}
}

func (w *Watcher) trackResourceUsage(ctx context.Context) error {
	// for _, cn := range w.tracked {
	// 	// w.dc.ContainerStats(ctx context.Context, containerID string, stream bool)
	// 	// log current cpu , memory and network usage
	// 	// show maximum usage
	// 	// show average usage
	// }
	return nil
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
