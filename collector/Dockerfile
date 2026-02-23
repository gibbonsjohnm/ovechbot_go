FROM golang:1.21-alpine AS builder
WORKDIR /app
RUN apk add --no-cache ca-certificates
COPY go.mod ./
COPY . .
RUN go mod tidy && CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /collector ./cmd/collector

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
USER nobody
COPY --from=builder /collector /collector
ENTRYPOINT ["/collector"]
