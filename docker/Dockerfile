# =============================================================================
# Build Stage - Compile Go binary
# =============================================================================
FROM casjaysdev/go:latest AS builder

RUN apk add --no-cache git bash

ARG TARGETARCH
ARG VERSION=dev
ARG BUILD_EPOCH
ARG COMMIT_ID
ARG OFFICIAL_SITE

WORKDIR /build

# Copy go.mod first for layer caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build
# BuildDate is NOT embedded - it is derived from BuildEpoch at process start
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} GOFLAGS=-buildvcs=false go build \
    -buildvcs=false -trimpath \
    -ldflags "-s -w -X 'main.Version=${VERSION}' -X 'main.CommitID=${COMMIT_ID}' -X 'main.BuildEpoch=${BUILD_EPOCH}' -X 'main.OfficialSite=${OFFICIAL_SITE}'" \
    -o /build/binary/api ./src

# =============================================================================
# Runtime Stage - Minimal Alpine image
# =============================================================================
FROM alpine:latest

# ARGs for build-time values (set by docker build --build-arg)
ARG VERSION=dev
ARG BUILD_DATE
ARG BUILD_EPOCH
ARG COMMIT_ID
ARG LICENSE=MIT

# No LABEL blocks — all image metadata is applied as OCI annotations at build time.
# Pass --annotation flags to docker buildx build (or use docker/metadata-action
# annotations: output in GitHub Actions). Annotations attach to the manifest index
# and are visible on multiarch pulls. LABEL applies only to per-platform layers.
# See docker-rules.md → OCI Meta Labels.

# Upgrade preinstalled base packages first so runtime libs (openssl/libssl3/
# libcrypto3 etc.) carry the latest security patches, not whatever shipped
# in the cached alpine:latest base layer at last mirror sync
RUN apk upgrade --no-cache

# Install required packages including Tor
# NOTE: Tor binary installed but NOT configured here - binary handles all Tor setup (see PART 31)
RUN apk add --no-cache \
    git \
    curl \
    bash \
    tini \
    tor

# NO USER directive and no user/group creation - the container starts as root
# and the binary creates its own user/group, sets permissions, then drops
# privileges after initialization (PART 26 "Privilege drop, not Dockerfile users")

# NO directory creation - binary handles ALL setup based on env vars
# This allows users to override CONFIG_DIR, DATA_DIR, etc.
# Docker volume mounts auto-create mount points

# Copy binary from builder stage (multi-stage build)
COPY --from=builder /build/binary/api /usr/local/bin/api

# Copy entrypoint script from ./docker/rootfs/
COPY docker/rootfs/ /

# Copy Dockerfile to image (for reference and backup)
COPY docker/Dockerfile /root/Dockerfile

# Make all binaries/scripts in /usr/local/bin executable (755)
RUN chmod 755 /usr/local/bin/*

# Set environment
# Note: Tor available (binary installed) but server binary controls Tor startup (see PART 31)
# MODE is never baked into the image — binary defaults to production; compose sets MODE explicitly
ENV TZ=America/New_York \
    DATABASE_DIR=/data/db/sqlite

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
