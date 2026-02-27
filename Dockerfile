# Multi-stage build for triage CLI
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Download dependencies first for layer caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build
COPY . .

# Build with version info via ldflags
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w -X github.com/jacklau/triage/cmd.version=${VERSION}" -o /triage .

# Runtime stage
FROM alpine:3.19

# Add ca-certificates for HTTPS calls (GitHub API, webhooks)
RUN apk --no-cache add ca-certificates

COPY --from=builder /triage /usr/local/bin/triage

# Config can be mounted as a volume
VOLUME ["/root/.triage"]

ENTRYPOINT ["triage"]
