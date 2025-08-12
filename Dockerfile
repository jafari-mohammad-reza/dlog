FROM golang:1.24.1-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o dlog ./cmd/main.go

# Final image
FROM alpine:3.19

WORKDIR /app
COPY --from=builder /app/dlog .
COPY --from=builder /app/config.yaml ./config.yaml 

RUN apk add --no-cache ca-certificates

EXPOSE 8080

ENTRYPOINT ["./dlog"]