# Arch Linux Packaging

This directory contains packaging for the Arch Linux distribution.

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

The `BASH_XTRACEFD` environment variable is preserved, along with the file
descriptor. This allows the outer script to differentiate trace output from
stderr for better clarity.

Several named volumes are used to avoid network operations on subsequent runs.
- `snapd-arch-pacman-cache` is mapped to `/var/cache/pacman/pkg`
- `snapd-gomod-cache` is mapped to `/var/cache/gomod`

`GOMODCACHE` is exported early in the container script so every subsequent `go`
invocation picks it up. This volume is shared with other distributions that use
the same approach.

Pacman retains downloaded packages by default; the `--needed` flag prevents
reinstalling already-cached packages.

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
    -v "snapd-arch-pacman-cache:/var/cache/pacman/pkg" \
    -v "snapd-gomod-cache:/var/cache/gomod" \
    -w /build \
    docker.io/archlinux/archlinux:latest \
    /bin/bash -x -e -u
```

## Host Script

The small host script creates the .build directory with the structure expected
by `makepkg`. This directory is shared with the container and can be used to
access resulting packages and build logs.

```sh
rm -rf .build
mkdir -p .build
```

## Container Script

The build script has several sections. As a part of the process we are creating
a source tarball, and combining it with the `PKGBUILD` file from this
directory.

```sh
# Show the sizes of persistent caches to verify volumes are populated across runs.
echo "Pacman cache:    $(du -sh /var/cache/pacman/pkg  2>/dev/null | cut -f1 || echo empty)"
echo "Go module cache: $(du -sh /var/cache/gomod       2>/dev/null | cut -f1 || echo empty)"

# Allow both root and the builder user to read and write the Go module cache.
chmod 1777 /var/cache/gomod
export GOMODCACHE=/var/cache/gomod

# Install bootstrap packages.
(
    source /src/packaging/arch/PKGBUILD
    # -n makes this a "variable name" reference.
    declare -n arch_checkdepends="checkdepends_$(uname -m | tr - _)"
    pacman -Syu --needed --noconfirm \
        ${makedepends[@]} \
        ${checkdepends[@]} \
        ${arch_checkdepends[@]+${arch_checkdepends[@]}} \
        base-devel
)

# Determine the version of the package.
version=$(bash -c '. /src/packaging/arch/PKGBUILD; echo "$pkgver"')

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

# Create single (-s) source archive with bundled vendored sources.
( cd /src-rw && ./packaging/pack-source -s -v "$version" -o /build )

# Copy packaging files to the build directory.
install -t /build /src/packaging/arch/PKGBUILD /src/packaging/arch/snapd.install

# Create a non-root build user.
useradd -m builder

# Transfer ownership of the work directory to the build user.
# When exiting, restore root ownership. Root in the container
# is mapped to the calling host user.
chown -R builder /build
trap 'chown -R root /build' EXIT

# Build the binary package.
su builder -c 'cd /build && GOMODCACHE=/var/cache/gomod makepkg${SKIP_TESTS:+ --nocheck} --force'
```
