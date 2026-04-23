# Ubuntu 26.04 Packaging

This directory contains packaging for the Ubuntu distribution.

## Build Container

The package can be built using a rootless podman container.

The entire snapd tree is exposed as `/src` inside the container, this is done
with the first `-v` switch. The `.build` directory is exposed as the `/build`
directory. This is where most of the build actually happens. This is where we
copy the built packages from the container back to the container host.

The `--rm` switch removes the container so that it doesn't linger after each
build.  The `--interactive` switch allows us to pass a script to bash on stdin.
The `--userns host` option maps the ID of the calling user to root inside the
container.

We are using a Ubuntu 24.04 container (noble) for the build.

The `BASH_XTRACEFD` environment variable is preserved, along with the file
descriptor. This allows the outer script to differentiate trace output from
stderr for better clarity.

Several named volumes are used to avoid network operations on subsequent runs.
- `snapd-ubuntu-apt-cache` is mapped to `/var/cache/apt`
- `snapd-ubuntu-apt-lists` is mapped to `/var/lib/apt/lists`
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
    -v "snapd-ubuntu-apt-cache:/var/cache/apt" \
    -v "snapd-ubuntu-apt-lists:/var/lib/apt/lists" \
    -v "snapd-gomod-cache:/var/cache/gomod" \
    -w /build \
    docker.io/ubuntu:resolute \
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
location, the `debian/` directory is populated from `packaging/ubuntu-26.04/`,
Go modules are vendored, and then `dpkg-buildpackage` is invoked.

This is a native package (`3.0 (native)` format), so the version has no
upstream/revision split and there is only a single source archive. In the
future this will go away, but for now let's go with the status quo.

```sh
# Show the sizes of persistent caches to verify volumes are populated across runs.
echo "APT cache:       $(du -sh /var/cache/apt       2>/dev/null | cut -f1 || echo empty)"
echo "Go module cache: $(du -sh /var/cache/gomod     2>/dev/null | cut -f1 || echo empty)"

# Allow both root and the builder user to read and write the Go module cache.
chmod 1777 /var/cache/gomod
export GOMODCACHE=/var/cache/gomod

# The official Ubuntu container image ships a post-invoke hook that deletes all
# .deb files after every dpkg run. Remove it so the archive cache is preserved.
rm -f /etc/apt/apt.conf.d/docker-clean

# Install the base build-dependencies as well as golang, git and ca-certificates
# needed to export the source tarball with vendored packages.
apt-get update
DEBIAN_FRONTEND=noninteractive apt-get --yes install --no-install-recommends \
    devscripts build-essential golang ca-certificates git

# Determine the version of the package.
# Native package: no upstream/revision split, use the full version as-is.
version=$(dpkg-parsechangelog --file /src/packaging/ubuntu-26.04/changelog --show-field Version)

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

# Vendor C pieces that are needed.
# Note that we run this from the /src directory so that it doesn't have
# to be copied into the build tree as an exception.
( cd /src-rw/c-vendor && /src/c-vendor/vendor.sh )

# Create a source archive with bundled vendored sources.
# NOTE: This is still online and is not cached anywhere.
( cd /src-rw && ./packaging/pack-source -v "$version" -o /build )

# Unpack the source archive and install the packaging directory.
tar -Jxf /build/snapd_"$version".no-vendor.tar.xz -C /build
tar -Jxf /build/snapd_"$version".only-vendor.tar.xz -C /build
# The ubuntu-26.04 directory contains .build/ which would be copied recursively, exclude it by using a glob.
mkdir /build/snapd-"$version"/debian
cp -a /src/packaging/ubuntu-26.04/* /build/snapd-"$version"/debian

# Discover and install build dependencies.
DEBIAN_FRONTEND=noninteractive apt-get --yes build-dep /build/snapd-"$version"

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
