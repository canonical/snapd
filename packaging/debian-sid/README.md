# Debian Packaging

This directory contains packaging for the Debian family of distributions.

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

We are using the Debian repository, with the latest sid image.

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
    docker.io/debian:sid \
    /bin/bash -x -u
```

## Host Script

The small host script creates the input and output directories that allow
exchanging data with the container. The `.build/` directory can be inspected
to debug failures.

```sh
mkdir -p .build
```

## Container Script

The build script has several sections. The source tree is copied to a writable
location, the `debian/` directory is populated from `packaging/debian-sid/`,
Go modules are vendored, and then `dpkg-buildpackage` is invoked.

```sh
# Install bootstrap packages.
apt-get update
apt-get --yes install --no-install-recommends \
    bash coreutils dpkg-dev fakeroot findutils git golang gzip make tar xz-utils ca-certificates

# Determine the version of the package.
# The cut there is to extract the upstream component of the non-native package.
version=$(dpkg-parsechangelog --file /src/packaging/debian-sid/changelog --show-field Version | cut -d - -f 1)

# Copy the source tree to a temporary location, so that we can call go mod vendor.
mkdir -p /src-rw
tar -C /src -c . | tar -C /src-rw -x

# Vendor Go modules that are needed.
( cd /src-rw && go mod vendor )

# Create two separate source archives, one with upstream bits and one with vendored dependencies.
( cd /src-rw && ./packaging/pack-source -v "$version" -o /build )

# Copy packaging files to the build directory.
tar -Jxf /build/snapd_"$version".no-vendor.tar.xz -C /build
cp -a /src/packaging/debian-sid /build/snapd-"$version"/debian

# Discover and install build dependencies.
apt-get --yes build-dep /build/snapd-"$version"

# Create a non-root build user required.
useradd -m builder

# Transfer ownership of the work directory to the build user.
chown -R builder /build

# Build the binary package.
su builder -c 'cd /build && BASH_XTRACEFD= rpmbuild -ba /build/SPECS/snapd.spec --define "_topdir /build" --nocheck'
```
