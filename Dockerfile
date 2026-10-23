# Multi-stage build for ultra-small, secure production image
FROM golang:1.24-alpine AS builder

WORKDIR /app

# Install git and ca-certificates
RUN apk add --no-cache git ca-certificates

# Cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source code and build statically
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o anthology-dav .

# Final minimal scratch/alpine runtime image
FROM alpine:3.20

WORKDIR /app

RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /app/anthology-dav /app/anthology-dav

# Expose standard port
EXPOSE 8085

ENV PORT=8085

CMD ["/app/anthology-dav"]
