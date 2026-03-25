# Fedora Packaging

This directory contains packaging for the Fedora family of distributions.

## Build Container

The package can be built using a rootless podman container.

The entire snapd tree is exposed as `/src` inside the container, this is done
with the first `-v` switch. The `.build` directory is exposed as the `/build`
directory. This is where most of the build actually happens. This is where we
copy the built packages from the container back to the container host. The `Z`
option applies the right SELinux context.

The `--rm` switch removes the container so that it doesn't linger after each
build.  The `--interactive` switch allows us to pass a script to bash on stdin.
The `--userns host` option maps the ID of the calling user to root inside the
container.

The `BASH_XTRACEFD` environment variable is preserved, along with the file
descriptor. This allows the outer script to differentiate trace output from
stderr for better clarity.

Several named volumes are used to avoid network operations on subsequent runs.
- `snapd-fedora-dnf-cache` is mapped to `/var/cache/libdnf5`
- `snapd-gomod-cache` is mapped to `/var/cache/gomod`

`GOMODCACHE` is exported early in the container script so every subsequent `go`
invocation picks it up. This volume is shared with other distributions that use
the same approach.

DNF discards downloaded packages after installation by default, so all `dnf
install` calls pass `--setopt=keepcache=True` to retain the RPMs in the volume.

```sh
podman run \
    --rm \
    --interactive \
    --attach stdin \
    --attach stdout \
    --attach stderr \
    --preserve-fd="${BASH_XTRACEFD-}" \
    --userns host \
    -e BASH_XTRACEFD="${BASH_XTRACEFD-}" \
    -e SKIP_TESTS="${SKIP_TESTS-}" \
    -v "../../:/src:ro,Z" \
    -v ".build/:/build:Z" \
    -v "snapd-fedora-dnf-cache:/var/cache/libdnf5:Z" \
    -v "snapd-gomod-cache:/var/cache/gomod:Z" \
    -w /build \
    registry.fedoraproject.org/fedora:latest \
    /bin/bash -x -u
```

## Host Script

The small host script creates the `.build` directory with the structure expected
by `rpmbuild`. This directory is shared with the container and can be used to
access resulting packages and build logs.

```sh
rm -rf .build
mkdir -p .build/{SRPMS,RPMS,SOURCES,SPECS}
```

## Container Script

The build script has several sections. As a part of the process we are creating
two source tarballs (one without vendored dependencies and one with only the
vendored dependencies), and combining them with the `snapd.spec` file from this
directory.

```sh
# Show the sizes of persistent caches to verify volumes are populated across runs.
echo "DNF cache:       $(du -sh /var/cache/libdnf5  2>/dev/null | cut -f1 || echo empty)"
echo "Go module cache: $(du -sh /var/cache/gomod    2>/dev/null | cut -f1 || echo empty)"

# Allow both root and the builder user to read and write the Go module cache.
chmod 1777 /var/cache/gomod
export GOMODCACHE=/var/cache/gomod

# Install bootstrap packages.
dnf --assumeyes install --setopt=install_weak_deps=False --setopt=keepcache=True \
    bash coreutils findutils gawk git gzip make rpm-build \
    rpm-devel systemd-rpm-macros tar xz golang

# Determine the version of the package.
version=$(rpmspec -q --qf "%{VERSION}\n" /build/SPECS/snapd.spec | head -n1)

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

# Create the no-vendor and only-vendor source archives.
( cd /src-rw && ./packaging/pack-source -v "$version" -o /build/SOURCES )

# Copy packaging files to the build directory.
ls -lh /build
install -t /build/SPECS/ /src/packaging/fedora/snapd.spec

# Discover and install build dependencies.
rpmspec -q --buildrequires /build/SPECS/snapd.spec >/tmp/buildreqs.txt
xargs -r -d "\n" dnf --assumeyes install --setopt=keepcache=True </tmp/buildreqs.txt

# Create a non-root build user.
useradd -m builder

# Transfer ownership of the work directory to the build user.
# When exiting, restore root ownership. Root in the container
# is mapped to the calling host user.
chown -R builder /build
trap 'chown -R root /build' EXIT

# Build the binary package.
su builder -c 'cd /build && BASH_XTRACEFD= GOMODCACHE=/var/cache/gomod rpmbuild -ba /build/SPECS/snapd.spec --define "_topdir /build"'"${SKIP_TESTS:+ --nocheck}"
```
