# dlog

**dlog** is a lightweight Docker container log aggregator and watcher written in Go.  
It monitors Docker containers, collects their logs, and writes them to timestamped files for each container.

## Features

- Watches Docker containers for start/stop events.
- Streams logs from running containers.
- Aggregates logs into per-container files in a `logs/` directory.
- Cleans up old log files automatically.
- Simple health check endpoint.

## Usage

1. **Configuration:**  
   Create a `config.yaml` in the project root (optional, default port is 8080):

   ```yaml
   health_port: 8080
   ```

2. **Build and Run:**

   ```sh
   make build_linux_amd_64 # for linux amd 64
   make build_linux_arm_64 # for linux arm 64
   ```

3. **Health Check:**  
   Visit [http://localhost:8080/health](http://localhost:8080/health) to check if the service is running.

4. **Logs:**  
   Aggregated logs are stored in the `logs/` directory, named as `YYYY-MM-DD-<container-name>.log`.

## Requirements

- Go 1.20+
- Docker daemon running locally

## Project Structure

- `cmd/main.go` — Entry point
- `internal/aggregator/` — Log aggregation logic
- `internal/watcher/` — Docker event watcher
- `internal/conf/` — Configuration
