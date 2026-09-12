#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BUILD_DIR="${BUILD_DIR:-$ROOT_DIR/build}"
TARGET_ARCH="${TARGET_ARCH:-amd64}"
IMAGE_NAME="${IMAGE_NAME:-localhost/leanclash}"
MIHOMO_BIN="${MIHOMO_BIN:-$BUILD_DIR/mihomo}"
VERSION="${VERSION:-$("$MIHOMO_BIN" -v 2>&1 | grep -oP 'v\K[0-9]+\.[0-9]+\.[0-9]+' | head -1 || true)}"
VERSION="${VERSION:-latest}"

usage() {
    cat <<EOF
Usage: TARGET_ARCH=amd64|arm64 $0 [binary|container|all]

  binary     Build the native LeanClash binary into build/leanclash
  container  Build the container binary, image, and tar.gz archive
  all        Build binary and container archive (default)

Environment:
  TARGET_ARCH  Target architecture for builds (default: amd64)
  IMAGE_NAME   Container image name (default: localhost/leanclash)
  VERSION      Image/archive version (default: detected Mihomo version)
  MIHOMO_BIN   Mihomo runtime binary used in the image (default: build/mihomo)
EOF
}

log() { printf '[INFO] %s\n' "$*"; }
die() { printf '[ERROR] %s\n' "$*" >&2; exit 1; }
need_cmd() { command -v "$1" >/dev/null 2>&1 || die "$1 is required"; }

check_arch() {
    case "$TARGET_ARCH" in
        amd64|arm64) ;;
        *) die "TARGET_ARCH must be amd64 or arm64" ;;
    esac
}

git_value() {
    git -C "$ROOT_DIR" "$@" 2>/dev/null || printf 'unknown'
}

build_time="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
commit="$(git_value rev-parse --short HEAD)"
ensure_frontend() {
    if [ ! -f "$ROOT_DIR/web/dist/index.html" ] || [ ! -d "$ROOT_DIR/web/dist/assets" ]; then
        if command -v npm >/dev/null 2>&1 && [ -f "$ROOT_DIR/web/package.json" ]; then
            log "Building web frontend..."
            (
                cd "$ROOT_DIR/web"
                [ -d node_modules ] || npm install
                npm run build
            )
        else
            log "Warning: web frontend assets not found and npm not available; building with fallback placeholder"
        fi
    fi
}

build_binary() {
    need_cmd go
    check_arch
    ensure_frontend
    mkdir -p "$BUILD_DIR"
    log "Building LeanClash binary for linux/$TARGET_ARCH"
    (
        cd "$ROOT_DIR"
        CGO_ENABLED=0 GOOS=linux GOARCH="$TARGET_ARCH" \
            go build -trimpath \
            -ldflags "-s -w -X main.version=$VERSION -X main.commit=$commit -X main.buildTime=$build_time" \
            -o "$BUILD_DIR/leanclash" .
    )
    chmod 0755 "$BUILD_DIR/leanclash"
    "$BUILD_DIR/leanclash" info
}

build_container_binary() {
    need_cmd go
    check_arch
    ensure_frontend
    mkdir -p "$BUILD_DIR"
    log "Building container LeanClash binary for linux/$TARGET_ARCH"
    (
        cd "$ROOT_DIR"
        CGO_ENABLED=0 GOOS=linux GOARCH="$TARGET_ARCH" \
            go build -tags container -trimpath \
            -ldflags "-s -w -X main.version=$VERSION -X main.commit=$commit -X main.buildTime=$build_time" \
            -o "$BUILD_DIR/leanclash-container" .
    )
    chmod 0755 "$BUILD_DIR/leanclash-container"
}

check_container_inputs() {
    need_cmd docker
    check_arch
    [ -f "$MIHOMO_BIN" ] || die "Mihomo binary not found: $MIHOMO_BIN"
    build_container_binary
}

build_container() {
    check_container_inputs
    local tag="${IMAGE_NAME}:${VERSION}"
    local alt_image="localhost/leanclash-container"
    if [ "$IMAGE_NAME" = "localhost/leanclash-container" ]; then
        alt_image="localhost/leanclash"
    fi

    log "Building container image $tag for linux/$TARGET_ARCH"
    local build_args=(
        --platform "linux/$TARGET_ARCH"
        --tag "$tag"
        --tag "${IMAGE_NAME}:latest"
        --tag "${alt_image}:${VERSION}"
        --tag "${alt_image}:latest"
    )
    docker build "${build_args[@]}" "$ROOT_DIR"

    local stamp out tmp
    stamp="$(date +%Y%m%d_%H%M%S)"
    out="$BUILD_DIR/leanclash-${VERSION}-${TARGET_ARCH}-${stamp}.tar.gz"
    tmp="${out}.partial"
    local tags=("$tag" "${IMAGE_NAME}:latest" "${alt_image}:${VERSION}" "${alt_image}:latest")
    log "Saving ${tags[*]} -> $out"
    docker save "${tags[@]}" | gzip -c >"$tmp"
    mv "$tmp" "$out"
    log "Container archive: $out ($(du -h "$out" | cut -f1))"
}

case "${1:-all}" in
    binary)
        build_binary
        ;;
    container)
        build_container
        ;;
    all)
        build_binary
        build_container
        ;;
    -h|--help|help)
        usage
        ;;
    *)
        usage >&2
        exit 2
        ;;
esac
