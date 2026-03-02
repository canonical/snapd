#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd -- "$SCRIPT_DIR/../../.." && pwd)

usage() {
    cat <<'EOF'
Usage: build-deb-podman.sh [options]

Build snapd Debian packages in a fresh Debian unstable container using podman.
Default invocation (no arguments) runs all stages in order:
  1) source package
  2) arch-all binary packages
  3) arch-any binary packages

Options:
  --source-dir <dir>   Source tree to build (default: repository root)
  --output <dir>       Output base directory (default: <repo>/_artifacts/deb-builds)
  --arch <arch>        Debian architecture (default: derived from host)
  --image <image>      Container image (default: docker.io/library/debian:unstable)
  --keep-container     Keep the build container after completion/failure
  -h, --help           Show this help
EOF
}

map_uname_to_deb_arch() {
    case "$(uname -m)" in
        x86_64) echo "amd64" ;;
        aarch64) echo "arm64" ;;
        armv7l|armv7*) echo "armhf" ;;
        ppc64le) echo "ppc64el" ;;
        s390x) echo "s390x" ;;
        riscv64) echo "riscv64" ;;
        *)
            echo "cannot map host architecture '$(uname -m)' to a Debian architecture; pass --arch explicitly" >&2
            return 1
            ;;
    esac
}

SOURCE_DIR="$REPO_ROOT"
OUTPUT_DIR="$REPO_ROOT/_artifacts/deb-builds"
BUILD_ARCH="$(map_uname_to_deb_arch)"
IMAGE="docker.io/library/debian:unstable"
KEEP_CONTAINER=0

while [ "$#" -gt 0 ]; do
    case "$1" in
        --source-dir)
            SOURCE_DIR="$2"
            shift 2
            ;;
        --output)
            OUTPUT_DIR="$2"
            shift 2
            ;;
        --arch)
            BUILD_ARCH="$2"
            shift 2
            ;;
        --image)
            IMAGE="$2"
            shift 2
            ;;
        --keep-container)
            KEEP_CONTAINER=1
            shift
            ;;
        -h|--help)
            usage
            exit 0
            ;;
        *)
            echo "unknown option: $1" >&2
            usage >&2
            exit 1
            ;;
    esac
done

if ! command -v podman >/dev/null 2>&1; then
    echo "cannot find podman in PATH" >&2
    exit 1
fi

HOST_UID=$(id -u)
HOST_GID=$(id -g)

if [ ! -d "$SOURCE_DIR/packaging/debian-sid" ]; then
    echo "cannot find packaging/debian-sid in $SOURCE_DIR" >&2
    exit 1
fi

if ! command -v git >/dev/null 2>&1; then
    echo "cannot find git in PATH" >&2
    exit 1
fi

if ! git -C "$SOURCE_DIR" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    echo "source directory is not a git repository: $SOURCE_DIR" >&2
    exit 1
fi

FULL_VERSION=$(git -C "$SOURCE_DIR" show HEAD:packaging/debian-sid/changelog | sed -n '1s/^snapd (\([^)]*\)).*/\1/p')
if [ -z "$FULL_VERSION" ]; then
    echo "cannot determine package version from packaging/debian-sid/changelog" >&2
    exit 1
fi
UPSTREAM_VERSION=${FULL_VERSION%%-*}

STAGE_DIR=$(mktemp -d)
cleanup_stage() {
    if [ "$KEEP_CONTAINER" -eq 0 ]; then
        rm -rf "$STAGE_DIR"
    fi
}
trap cleanup_stage EXIT

if ! git -C "$SOURCE_DIR" archive --format=tar --prefix="snapd-${UPSTREAM_VERSION}/" HEAD | xz -T0 > "$STAGE_DIR/snapd_${UPSTREAM_VERSION}.orig.tar.xz"; then
    echo "cannot export orig tarball from git" >&2
    exit 1
fi

if ! git -C "$SOURCE_DIR" archive --format=tar HEAD packaging/debian-sid | tar -xf - -C "$STAGE_DIR"; then
    echo "cannot export packaging/debian-sid from git" >&2
    exit 1
fi

mkdir -p "$OUTPUT_DIR/source" "$OUTPUT_DIR/arch-all" "$OUTPUT_DIR/arch-any"
if [ ! -w "$OUTPUT_DIR/source" ] || [ ! -w "$OUTPUT_DIR/arch-all" ] || [ ! -w "$OUTPUT_DIR/arch-any" ]; then
    echo "output directory is not writable, attempting ownership repair: $OUTPUT_DIR" >&2
    if podman unshare chown -R "$HOST_UID:$HOST_GID" "$OUTPUT_DIR" 2>/dev/null; then
        echo "repaired output directory ownership" >&2
    else
        echo "cannot repair output directory ownership automatically: $OUTPUT_DIR" >&2
        echo "try: podman unshare chown -R $HOST_UID:$HOST_GID '$OUTPUT_DIR'" >&2
        echo "or choose a writable location with --output (for example /tmp/snapd-deb-builds)" >&2
        exit 1
    fi

    if [ ! -w "$OUTPUT_DIR/source" ] || [ ! -w "$OUTPUT_DIR/arch-all" ] || [ ! -w "$OUTPUT_DIR/arch-any" ]; then
        echo "output directory is still not writable after repair: $OUTPUT_DIR" >&2
        exit 1
    fi
fi

ROOTLESS=$(podman info --format '{{.Host.Security.Rootless}}' 2>/dev/null || echo "unknown")
if [ "$ROOTLESS" = "true" ]; then
    echo "note: running podman in rootless mode" >&2
fi

CONTAINER_NAME="snapd-deb-build-$$"
PODMAN_RM_ARG="--rm"
if [ "$KEEP_CONTAINER" -eq 1 ]; then
    PODMAN_RM_ARG=""
fi

echo "Build configuration:"
echo "  source dir: $SOURCE_DIR"
echo "  output dir: $OUTPUT_DIR"
echo "  arch:       $BUILD_ARCH"
echo "  image:      $IMAGE"
echo "  version:    $FULL_VERSION"

set +e
podman run -i $PODMAN_RM_ARG --name "$CONTAINER_NAME" \
    --security-opt label=disable \
    --mount type=bind,src="$STAGE_DIR",dst=/in,ro \
    --mount type=bind,src="$OUTPUT_DIR",dst=/out \
    -e BUILD_ARCH="$BUILD_ARCH" \
    -e FULL_VERSION="$FULL_VERSION" \
    -e UPSTREAM_VERSION="$UPSTREAM_VERSION" \
    "$IMAGE" \
    bash -s <<'EOF'
set -euo pipefail

export DEBIAN_FRONTEND=noninteractive

apt-get update
apt-get install -y eatmydata devscripts git equivs ca-certificates fakeroot

mkdir -p /work
cp -a "/in/snapd_${UPSTREAM_VERSION}.orig.tar.xz" /work/
tar -xJf "/work/snapd_${UPSTREAM_VERSION}.orig.tar.xz" -C /work/
SRC_DIR="/work/snapd-${UPSTREAM_VERSION}"
cp -a /in/packaging/debian-sid "$SRC_DIR/debian"

cd "$SRC_DIR"

apt-get build-dep -y ./

if ! id -u builder >/dev/null 2>&1; then
    useradd -m builder
fi

chown -R builder:builder /work

su - builder -c "cd $SRC_DIR && dpkg-buildpackage -S -sa -uc -us"

find /work -maxdepth 1 -type f \( \
    -name 'snapd_*.dsc' -o \
    -name 'snapd_*.debian.tar.*' -o \
    -name 'snapd_*.orig.tar.*' -o \
    -name 'snapd-*.tar.*' -o \
    -name 'snapd_*.changes' \
\) -exec cp -a {} /out/source/ \;

DSC=$(find /work -maxdepth 1 -type f -name 'snapd_*.dsc' | sort | tail -n 1)
if [ -z "$DSC" ]; then
    echo "cannot locate generated .dsc file" >&2
    exit 1
fi

su - builder -c "cd $SRC_DIR && dpkg-buildpackage -A -uc -us"

find /work -maxdepth 1 -type f \( \
    -name '*_all.deb' -o \
    -name '*.buildinfo' -o \
    -name '*.changes' \
\) -exec cp -a {} /out/arch-all/ \;

su - builder -c "cd $SRC_DIR && dpkg-buildpackage -B -uc -us -a${BUILD_ARCH}"

find /work -maxdepth 1 -type f \( \
    -name "*_${BUILD_ARCH}.deb" -o \
    -name '*.buildinfo' -o \
    -name '*.changes' \
\) -exec cp -a {} /out/arch-any/ \;
EOF
STATUS=$?
set -e

if [ "$STATUS" -ne 0 ]; then
    echo "build failed (exit code $STATUS)" >&2
    if [ "$KEEP_CONTAINER" -eq 1 ]; then
        echo "container kept: $CONTAINER_NAME" >&2
    fi
    exit "$STATUS"
fi

if ! find "$OUTPUT_DIR/source" -mindepth 1 -type f | grep -q .; then
    echo "build failed: source stage produced no artifacts in $OUTPUT_DIR/source" >&2
    exit 1
fi

if ! find "$OUTPUT_DIR/arch-all" -mindepth 1 -type f | grep -q .; then
    echo "build failed: arch-all stage produced no artifacts in $OUTPUT_DIR/arch-all" >&2
    exit 1
fi

if ! find "$OUTPUT_DIR/arch-any" -mindepth 1 -type f | grep -q .; then
    echo "build failed: arch-any stage produced no artifacts in $OUTPUT_DIR/arch-any" >&2
    exit 1
fi

echo "Build complete. Artifacts:"
echo "  source:   $OUTPUT_DIR/source"
echo "  arch-all: $OUTPUT_DIR/arch-all"
echo "  arch-any: $OUTPUT_DIR/arch-any"

if [ "$KEEP_CONTAINER" -eq 1 ]; then
    echo "container kept: $CONTAINER_NAME"
fi
