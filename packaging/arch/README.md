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

We are using the Docker Hub Arch Linux image.

The `BASH_XTRACEFD` environment variable is preserved, along with the file
descriptor. This allows the outer script to differentiate trace output from
stderr for better clarity.

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
    -v "../../:/src:ro" \
    -v ".build/:/build" \
    -w /build \
    docker.io/archlinux/archlinux:latest \
    /bin/bash -x -u
```

## Host Script

The small host script creates the .build directory with the structure expected
by `makepkg`. This directory is shared with the container and can be used to
access resulting packages and build logs.

```sh
mkdir -p .build
```

## Container Script

The build script has several sections. As a part of the process we are creating
a source tarball, and combining it with the `PKGBUILD` file from this
directory.

```sh
# Install bootstrap packages.
pacman --noconfirm -Sy --needed \
    base-devel git go go-tools libseccomp libcap systemd \
    xfsprogs python-docutils apparmor autoconf-archive m4 \
    python squashfs-tools shellcheck openssh lib32-glibc

# Determine the version of the package.
version=$(bash -c '. /src/packaging/arch/PKGBUILD; echo "$pkgver"')

# Copy the source tree to a temporary location, so that we can call go mod vendor.
mkdir -p /src-rw
tar -C /src -c . | tar -C /src-rw -x

# Vendor Go modules that are needed.
( cd /src-rw && go mod vendor )

# Create single (-s) source archive with bundled vendored sources.
( cd /src-rw && ./packaging/pack-source -s -v "$version" -o /build )

# Copy packaging files to the build directory.
install -t /build /src/packaging/arch/PKGBUILD /src/packaging/arch/snapd.install

# Create a non-root build user required.
useradd -m builder

# Transfer ownership of the work directory to the build user.
chown -R builder /build

# Build the binary package.
su builder -c 'cd /build && makepkg --nocheck --force'
```
