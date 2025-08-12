package main

import (
	"context"
	"dlog/internal/conf"
	"dlog/internal/watcher"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("Recovered in main:", r)
		}
	}()
	cfg, err := conf.NewConfig()
	if err != nil {
		fmt.Printf("failed to load config: %v\n", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer func() {
		fmt.Println("terminate main context")
		stop()
	}()
	if cfg.TimeoutDuration > 0 {
		go func() {
			timeout := time.NewTimer(cfg.TimeoutDuration)
			select {
			case <-ctx.Done():
				timeout.Stop()
			case <-timeout.C:
				fmt.Println("timeout")
				stop()
			}
		}()
	}

	go func() {
		if err := healthCheck(cfg); err != nil {
			os.Exit(1)
		}
	}()
	watcher := watcher.NewWatcherService(cfg)
	if err := watcher.Start(ctx); err != nil {
		os.Exit(1)
	}
}

func healthCheck(cfg *conf.Config) error {
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("healthy"))
	})
	if err := http.ListenAndServe(fmt.Sprintf(":%d", cfg.HealthCheckPort), nil); err != nil {
		fmt.Println("failed to start healthpeak")
		return err
	}
	return nil
}
