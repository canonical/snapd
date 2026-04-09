# Debian Packaging

This directory contains packaging for the Debian family of distributions.

## Build Container

The package can be built using a rootless podman container.

The entire snapd tree is exposed as a read-only `/src` inside the container.
The `.build` directory is exposed as the `/build` directory.

The `--rm` switch removes the container so that it doesn't linger after each
build.  The `--interactive` switch allows us to pass a script to bash on stdin.
The `--userns host` option maps the ID of the calling user to root inside the
container.

The `BASH_XTRACEFD` environment variable is preserved, along with the file
descriptor. This allows the outer script to differentiate trace output from
stderr for better clarity.

Several named volumes are used to avoid network operations on subsequent runs.
- `snapd-debian-apt-cache` is mapped to `/var/cache/apt`
- `snapd-debian-apt-lists` is mapped to `/var/lib/apt/lists`
- `snapd-gomod-cache` is mapped to `/var/cache/gomod`

`GOMODCACHE` is exported early in the container script so every subsequent `go`
invocation picks it up. This volume is shared with other distributions that use
the same approach.

To make apt cache work docker hook is removed from
`/etc/apt/apt.conf.d/docker-clean`. The hook registers a `DPkg::Post-Invoke`
command that deletes all `.deb` files from the archive after every `dpkg` run.
This wipes the volume from inside the container, causing a full re-download on
every build.

```sh
podman run \
    --rm \
    --interactive \
    --attach stdin \
    --attach stdout \
    --attach stderr \
    --preserve-fd="${BASH_XTRACEFD-}" \
    --userns host \
    --security-opt label=disable \
    -e BASH_XTRACEFD="${BASH_XTRACEFD-}" \
    -e SKIP_TESTS="${SKIP_TESTS-}" \
    -v "../../:/src:ro" \
    -v ".build/:/build" \
    -v "snapd-debian-apt-cache:/var/cache/apt" \
    -v "snapd-debian-apt-lists:/var/lib/apt/lists" \
    -v "snapd-gomod-cache:/var/cache/gomod" \
    -w /build \
    docker.io/debian:sid \
    /bin/bash -x -e -u
```

## Host Script

The small host script creates the input and output directories that allow
exchanging data with the container. The `.build/` directory can be inspected to
debug failures.

```sh
rm -rf .build
mkdir -p .build
```

## Container Script

The build script has several sections. The source tree is copied to a writable
location, the `debian/` directory is populated from `packaging/debian-sid/`, Go
modules are vendored, and then `dpkg-buildpackage` is invoked.

```sh
# Show the sizes of persistent caches to verify volumes are populated across runs.
echo "APT cache:       $(du -sh /var/cache/apt       2>/dev/null | cut -f1 || echo empty)"
echo "Go module cache: $(du -sh /var/cache/gomod     2>/dev/null | cut -f1 || echo empty)"

# Allow both root and the builder user to read and write the Go module cache.
chmod 1777 /var/cache/gomod
export GOMODCACHE=/var/cache/gomod

# The official Debian container image ships a post-invoke hook that deletes all
# .deb files after every dpkg run. Remove it so the archive cache is preserved.
rm -f /etc/apt/apt.conf.d/docker-clean

# Install the base build-dependencies as well as golang needed
# to export the source tarball.
apt-get update
DEBIAN_FRONTEND=noninteractive apt-get --yes install --no-install-recommends \
    devscripts build-essential golang

# Determine the version of the package.
# The cut there is to extract the upstream component of the non-native package.
version=$(dpkg-parsechangelog --file /src/packaging/debian-sid/changelog --show-field Version | cut -d - -f 1)

# Copy the source tree to a temporary location, so that we can call go mod vendor.
mkdir -p /src-rw
tar -C /src -c \
    --exclude='./vendor/*' \
    --exclude='./c-vendor/squashfuse' \
    --exclude='.git' \
    --exclude='.git/*' \
    --exclude='.image-garden/*' \
    --exclude='./packaging/*/.build/*' \
    --exclude='./built-snap/*' \
    --exclude='./*.snap' \
. | tar -C /src-rw -x

# Vendor Go modules that are needed.
( cd /src-rw && go mod vendor )

# Create two separate source archives, one with upstream bits and one with vendored dependencies.
( cd /src-rw && ./packaging/pack-source -v "$version" -o /build )

# Copy packaging files to the build directory.
tar -Jxf /build/snapd_"$version".no-vendor.tar.xz -C /build
# The debian-sid directory contains .build/ which would be copied recursively, exclude it by using a glob.
mkdir /build/snapd-"$version"/debian
cp -a /src/packaging/debian-sid/* /build/snapd-"$version"/debian

# Discover and install build dependencies.
apt-get --yes build-dep /build/snapd-"$version"

# Create a non-root build user.
useradd -m builder

# Transfer ownership of the work directory to the build user.
# When exiting, restore root ownership. Root in the container
# is mapped to the calling host user.
chown -R builder /build
trap 'chown -R root /build' EXIT

# Build the binary package.
su builder -c 'cd /build/snapd-'"$version"' && DEB_BUILD_OPTIONS="${SKIP_TESTS:+nocheck}" GOMODCACHE=/var/cache/gomod dpkg-buildpackage -us -uc -b'
```
