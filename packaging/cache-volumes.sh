#!/bin/bash
# Helper script to import/export podman/docker named volumes to/from .cache/ directories
# Used by GitHub Actions CI for volume caching between builds
# Keeps volumes isolated and prevents host filesystem pollution with complex permissions

set -e

# This script is only intended to run in CI environments where Docker/podman
# named volumes need to be cached. It's not useful for local development.
if [ -z "${CI:-}" ]; then
    cat >&2 << 'EOF'
This script is only needed in CI environments (GitHub Actions, etc.) where
Docker/podman named volumes are cached between builds via GitHub Actions.

Local development doesn't need this - you can run kulturysta directly.

To run this script anyway, set the CI environment variable:
  CI=true ./cache-volumes.sh <distro> [import|export]

Or use it within GitHub Actions workflows (CI is automatically set there).
EOF
    exit 0
fi

DISTRO="${1:?Usage: $0 <distro> [import|export]}"
MODE="${2:-import}"  # Default to import

PACKAGING_DIR="$(cd "$(dirname "$0")" && pwd)"
CACHE_DIR="$PACKAGING_DIR/$DISTRO/.cache"

# Volume mappings: VOLUME_NAME:CONTAINER_PATH:CACHE_SUBDIR
declare -A VOLUME_MAPPINGS

case "$DISTRO" in
    arch)
        VOLUME_MAPPINGS=(
            [snapd-arch-pacman-cache]="/var/cache/pacman/pkg:pacman"
            [snapd-gomod-cache]="/var/cache/gomod:gomod"
        )
        ;;
    debian-*|ubuntu-*)
        VOLUME_MAPPINGS=(
            [snapd-debian-apt-cache]="/var/cache/apt:apt"
            [snapd-debian-apt-lists]="/var/lib/apt/lists:apt-lists"
            [snapd-gomod-cache]="/var/cache/gomod:gomod"
        )
        ;;
    fedora-*)
        VOLUME_MAPPINGS=(
            [snapd-fedora-dnf-cache]="/var/cache/libdnf5:dnf"
            [snapd-gomod-cache]="/var/cache/gomod:gomod"
        )
        ;;
    opensuse-*)
        VOLUME_MAPPINGS=(
            [snapd-opensuse-zypper-cache]="/var/cache/zypp:zypper"
            [snapd-gomod-cache]="/var/cache/gomod:gomod"
        )
        ;;
    *)
        echo "ERROR: Unknown distro '$DISTRO'" >&2
        exit 1
        ;;
esac

# Determine container engine (podman or docker)
if command -v podman &>/dev/null; then
    ENGINE="podman"
elif command -v docker &>/dev/null; then
    ENGINE="docker"
else
    echo "ERROR: Neither podman nor docker found" >&2
    exit 1
fi

case "$MODE" in
    import)
        # Import cache from .cache/ into named volumes
        echo "[$DISTRO] Importing cache from $CACHE_DIR into volumes..."
        
        for volume_info in "${!VOLUME_MAPPINGS[@]}"; do
            volume_name="$volume_info"
            # shellcheck disable=SC2034
            IFS=':' read -r _ cache_subdir <<< "${VOLUME_MAPPINGS[$volume_info]}"
            
            cache_path="$CACHE_DIR/$cache_subdir"
            
            # Skip if cache dir doesn't exist (first run)
            if [[ ! -d "$cache_path" ]]; then
                echo "  ⊘ $volume_name: cache not found (first run?), skipping"
                continue
            fi
            
            # Use podman/docker to run a temporary container and copy cache contents
            # This approach works with both podman and docker named volumes
            echo "  → Importing $volume_name from $cache_path..."
            
            # Create volume if it doesn't exist
            "$ENGINE" volume create "$volume_name" 2>/dev/null || true
            
            # Copy cache contents into volume using tar through a temporary container
            tar -C "$cache_path" -cf - . | "$ENGINE" run --rm -i -v "$volume_name:/mnt" busybox tar -C /mnt -xf -
        done
        ;;
        
    export)
        # Export cache from named volumes to .cache/
        echo "[$DISTRO] Exporting cache from volumes to $CACHE_DIR..."
        
        mkdir -p "$CACHE_DIR"
        
        for volume_info in "${!VOLUME_MAPPINGS[@]}"; do
            volume_name="$volume_info"
            # shellcheck disable=SC2034
            IFS=':' read -r _ cache_subdir <<< "${VOLUME_MAPPINGS[$volume_info]}"
            
            cache_path="$CACHE_DIR/$cache_subdir"
            
            echo "  ← Exporting $volume_name to $cache_subdir..."
            
            # Skip if volume doesn't exist
            if ! "$ENGINE" volume inspect "$volume_name" &>/dev/null; then
                echo "    ⊘ Volume $volume_name doesn't exist, skipping"
                continue
            fi
            
            # Create cache directory
            mkdir -p "$cache_path"
            
            # Export volume contents using tar through temporary container
            "$ENGINE" run --rm -v "$volume_name:/mnt" busybox tar -C /mnt -cf - . | tar -C "$cache_path" -xf -
        done
        ;;
        
    *)
        echo "ERROR: Unknown mode '$MODE'. Use 'import' or 'export'" >&2
        exit 1
        ;;
esac

echo "[$DISTRO] Cache operation '$MODE' completed"
