package aggregator

import (
	"bytes"
	"context"
	"dlog/internal/conf"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/fsnotify/fsnotify"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/transform"
)

type AggregatorService struct {
	cfg         *conf.Config
	host        string
	openedFiles map[string]*os.File
	trackedChan chan conf.TrackedOption
	recordChan  chan conf.RecordLog
}

func NewAggregatorService(cfg *conf.Config, host string, trackedChan chan conf.TrackedOption, recordChan chan conf.RecordLog) *AggregatorService {
	return &AggregatorService{
		cfg:         cfg,
		host:        host,
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
		err := a.mergeClosedLogs(ctx)
		if err != nil {
			errChan <- err
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
			if err := os.MkdirAll(fmt.Sprintf("%s-logs", a.host), 0755); err != nil {
				log.Printf("Failed to create logs directory: %v\n", err)
				return err
			}
			file, err := os.OpenFile(path.Join(fmt.Sprintf("%s-logs", a.host), fmt.Sprintf("%s-%s.log", time.Now().Format(time.DateOnly), record.ContainerName)), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
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

func ensureUTF8(b []byte) []byte {
    if utf8.Valid(b) {
        return b
    }

    reader := transform.NewReader(bytes.NewReader(b), charmap.Windows1252.NewDecoder())
    converted, err := io.ReadAll(reader)
    if err != nil {
        out := make([]rune, 0, len(b))
        for len(b) > 0 {
            r, size := utf8.DecodeRune(b)
            if r == utf8.RuneError && size == 1 {
                out = append(out, '�')
                b = b[1:]
            } else {
                out = append(out, r)
                b = b[size:]
            }
        }
        return []byte(string(out))
    }

    out := make([]byte, 0, len(converted))
    for _, c := range converted {
        if (c >= 32 || c == 10 || c == 9) && c != 127 {
            out = append(out, c)
        }
    }
    return out
}

func (a *AggregatorService) mergeClosedLogs(ctx context.Context) error {
    errChan := make(chan error, 1)
    ticker := time.NewTicker(time.Hour * 12)

    go func() {
        defer ticker.Stop()

        for {
            select {
            case <-ticker.C:
                dirName := fmt.Sprintf("%s-logs", a.host)
                entries, err := os.ReadDir(dirName)
                if err != nil {
                    select { case errChan <- fmt.Errorf("failed to read %s entries: %w", dirName, err): default: }
                    continue
                }

                filesByContainer := make(map[string][]string)

                for _, entry := range entries {
                    if entry.IsDir() {
                        continue
                    }
                    if _, ok := a.openedFiles[entry.Name()]; ok {
                        continue
                    }

                    cleanName := strings.TrimSuffix(entry.Name(), ".log")
                    parts := strings.Split(cleanName, "-")
                    if len(parts) < 4 {
                        fmt.Printf("skipping invalid log name: %s", entry.Name())
                        continue
                    }

                    cname := strings.TrimSuffix(parts[3], ".")
                    filesByContainer[cname] = append(filesByContainer[cname],
                        filepath.Join(dirName, entry.Name()))
                }

                for cname, paths := range filesByContainer {
                    var mergedContent []byte

                    for _, fullPath := range paths {
                        data, err := os.ReadFile(fullPath)
                        if err != nil {
                            select { case errChan <- fmt.Errorf("failed to read %s: %w", fullPath, err): default: }
                            continue
                        }

                        utf8Data := ensureUTF8(data)
                        mergedContent = append(mergedContent, utf8Data...)

                        if err := os.Remove(fullPath); err != nil {
                            select { case errChan <- fmt.Errorf("failed to remove %s: %w", fullPath, err): default: }
                        }
                    }

                    mergedFile := fmt.Sprintf("%s-logs/%s-%s.log", a.host, time.Now().Format(time.DateOnly), cname)

                    f, err := os.OpenFile(mergedFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
                    if err != nil {
                        select { case errChan <- fmt.Errorf("failed to open merged file %s: %w", mergedFile, err): default: }
                        continue
                    }
                    if _, err := f.Write(mergedContent); err != nil {
                        select { case errChan <- fmt.Errorf("failed to write to %s: %w", mergedFile, err): default: }
                    }
                    f.Close()
                }
                fmt.Println("merged successfully with UTF-8 encoding")

            case <-ctx.Done():
                fmt.Println("Stopping mergeClosedLogs")
                return
            }
        }
    }()

    for {
        select {
        case err := <-errChan:
            if err != nil {
                return err
            }
        case <-ctx.Done():
            return ctx.Err()
        }
    }
}

func (a *AggregatorService) loadLog() error {
	entries, err := os.ReadDir(fmt.Sprintf("%s-logs", a.host))
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

		if len(filename) < 16 {
			continue
		}

		datePart := filename[:10]
		timestamp, err := time.Parse(time.DateOnly, datePart)
		if err != nil {
			fmt.Printf("Warning: skipping file %s - invalid date: %v\n", filename, err)
			continue
		}

		rest := filename[11 : len(filename)-4]
		containerName := rest
		if idx := strings.LastIndex(rest, "-"); idx != -1 {
			containerName = rest[idx+1:]
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
		fullPath := path.Join(fmt.Sprintf("%s-logs", a.host), fileInfo.filename)
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
		re := regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})`)
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
		fmt.Printf("failed to init watcher %s\n", err.Error())
		return err
	}
	defer watcher.Close()
	if _, err := os.Stat(fmt.Sprintf("%s-logs", a.host)); os.IsNotExist(err) {
		if err := os.Mkdir(fmt.Sprintf("%s-logs", a.host), 0755); err != nil {
			return err
		}
	}
	if err := watcher.Add(fmt.Sprintf("%s-logs", a.host)); err != nil {
		fmt.Printf("failed to add logs to watcher: %s\n", err.Error())
		return err
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
