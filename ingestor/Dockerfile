# Build stage
FROM golang:1.21-alpine AS builder
WORKDIR /app

RUN apk add --no-cache ca-certificates

COPY go.mod ./
COPY . .
RUN go mod tidy && CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /ingestor ./cmd/ingestor

# Run stage
FROM alpine:3.20
RUN apk add --no-cache ca-certificates
USER nobody
COPY --from=builder /ingestor /ingestor
ENTRYPOINT ["/ingestor"]
