# Stage 1: Build Stage
FROM golang:1.22-alpine AS builder
ENV CGO_ENABLED=0
ENV GOOS=linux

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -ldflags="-w -s" -o /mihomo-exporter .


# Stage 2: Final Image
FROM alpine:latest
RUN addgroup -S appgroup && adduser -S appuser -G appgroup
COPY --from=builder /mihomo-exporter /mihomo-exporter
RUN chown appuser:appgroup /mihomo-exporter
USER appuser

EXPOSE 9188
ENTRYPOINT ["/mihomo-exporter"]