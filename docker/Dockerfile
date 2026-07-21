# =============================================================================
# Build Stage - Compile Go binary
# =============================================================================
FROM casjaysdev/go:latest AS builder

RUN apk add --no-cache git bash

ARG TARGETARCH
ARG VERSION=dev
ARG BUILD_DATE
ARG COMMIT_ID

WORKDIR /build

# Copy go.mod first for layer caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go build \
    -ldflags "-s -w -X 'main.Version=${VERSION}' -X 'main.CommitID=${COMMIT_ID}' -X 'main.BuildDate=${BUILD_DATE}'" \
    -o /build/binary/api ./src

# =============================================================================
# Runtime Stage - Minimal Alpine image
# =============================================================================
FROM alpine:latest

# ARGs for build-time values (set by docker build --build-arg)
ARG VERSION=dev
ARG BUILD_DATE
ARG COMMIT_ID
ARG LICENSE=MIT

# Static Labels
LABEL maintainer="apimgr <apimgr@casjay.pro>" \
      org.opencontainers.image.vendor="apimgr" \
      org.opencontainers.image.authors="apimgr" \
      org.opencontainers.image.title="api" \
      org.opencontainers.image.base.name="api" \
      org.opencontainers.image.description="Containerized version of api" \
      org.opencontainers.image.url="https://github.com/apimgr/api" \
      org.opencontainers.image.source="https://github.com/apimgr/api" \
      org.opencontainers.image.documentation="https://github.com/apimgr/api" \
      org.opencontainers.image.vcs-type="Git" \
      com.github.containers.toolbox="false"

# Dynamic Labels (from ARGs)
LABEL org.opencontainers.image.licenses="${LICENSE}" \
      org.opencontainers.image.created="${BUILD_DATE}" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.schema-version="${VERSION}" \
      org.opencontainers.image.revision="${COMMIT_ID}"

# Install required packages including Tor
# libcap provides setcap, used below to grant the non-root user
# permission to bind the privileged port 80 (PART 26 exception:
# prefer setcap over running as root for privileged ports)
RUN apk add --no-cache \
    git \
    curl \
    bash \
    tini \
    tor \
    libcap

# Create non-root user/group to run the application
RUN addgroup -S app && adduser -S -G app app

# Create directories with proper structure
RUN mkdir -p /config /config/security /data/db /data/logs /data/tor /data/geoip /data/backup

# Copy binary from builder stage (multi-stage build)
COPY --from=builder /build/binary/api /usr/local/bin/api

# Copy entrypoint script from ./docker/rootfs/
COPY docker/rootfs/ /

# Copy Dockerfile to image (for reference and backup)
COPY docker/Dockerfile /root/Dockerfile

# Make all binaries/scripts in /usr/local/bin executable (755)
RUN chmod 755 /usr/local/bin/*

# Grant the api binary permission to bind port 80 without running as root
RUN setcap 'cap_net_bind_service=+ep' /usr/local/bin/api

# Hand ownership of config/data directories to the non-root user
RUN chown -R app:app /config /data

# Run as non-root user; setcap above allows binding port 80
USER app

# Set environment
# Note: Tor auto-enabled because tor binary is installed above
ENV MODE=development \
    TZ=America/New_York

# Expose internal port (always 80)
EXPOSE 80

# Stop signal for graceful shutdown
STOPSIGNAL SIGRTMIN+3

# Health check (long start period for services that need initialization)
HEALTHCHECK --start-period=10m --interval=5m --timeout=15s --retries=3 \
    CMD /usr/local/bin/api --status || exit 1

# Use tini as init with signal propagation
# -p SIGTERM: propagate SIGTERM to child processes
ENTRYPOINT [ "tini", "-p", "SIGTERM", "--", "/usr/local/bin/entrypoint.sh" ]
