build:
	GOOS=linux GOARCH=amd64 go build -o dlog
run:
	@air -c .air.toml