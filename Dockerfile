# Build stage
FROM golang:1.23-alpine AS builder

WORKDIR /build

# Install build dependencies
RUN apk add --no-cache git ca-certificates

# Copy dependency manifests first for optimal layer caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code and assets
COPY . .

# Compile static binary without CGO
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /build/bin/server ./cmd/server

# Final minimal runtime stage
FROM alpine:3.20

WORKDIR /app

# Install runtime dependencies and setup unprivileged user
RUN apk --no-cache add ca-certificates tzdata && \
    addgroup -g 10001 -S appgroup && \
    adduser -u 10001 -S appuser -G appgroup

# Copy compiled binary and templates
COPY --from=builder /build/bin/server /app/server
COPY --from=builder /build/web /app/web

USER appuser:appgroup

EXPOSE 8080

ENTRYPOINT ["/app/server"]
