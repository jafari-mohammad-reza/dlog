build:
	GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -trimpath -o dlog
run:
	@air -c .air.toml