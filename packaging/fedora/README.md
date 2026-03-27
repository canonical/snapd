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

We are using the Fedora registry, with the latest Fedora image.

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
    -e BASH_XTRACEFD="${BASH_XTRACEFD-}" \
    -v "../../:/src:ro" \
    -v ".build/:/build" \
    -w /build \
    registry.fedoraproject.org/fedora:latest \
    /bin/bash -x -u
```

## Host Script

The small host script creates the `.build` directory with the structure expected
by `rpmbuild`. This directory is shared with the container and can be used to
access resulting packages and build logs.

```sh
mkdir -p .build/{SRPMS,RPMS,SOURCES,SPECS}
```

## Container Script

The build script has several sections. As a part of the process we are creating
two source tarballs (one without vendored dependencies and one with only the
vendored dependencies), and combining them with the `snapd.spec` file from this
directory.

```sh
# Install bootstrap packages.
dnf --assumeyes install --setopt=install_weak_deps=False \
    bash coreutils findutils gawk git gzip make rpm-build \
    rpm-devel systemd-rpm-macros tar xz golang

# Determine the version of the package.
version=$(rpmspec -q --qf "%{VERSION}\n" /build/SPECS/snapd.spec | head -n1)

# Copy the source tree to a temporary location, so that we can call go mod vendor.
mkdir -p /src-rw
tar -C /src -c . | tar -C /src-rw -x

# Vendor Go modules that are needed.
( cd /src-rw && go mod vendor )

# Create the no-vendor and only-vendor source archives.
( cd /src-rw && ./packaging/pack-source -v "$version" -o /build/SOURCES )

# Copy packaging files to the build directory.
ls -lh /build
install -t /build/SPECS/ /src/packaging/fedora/snapd.spec

# Discover and install build dependencies.
rpmspec -q --buildrequires /build/SPECS/snapd.spec >/tmp/buildreqs.txt
xargs -r -d "\n" dnf --assumeyes install </tmp/buildreqs.txt

# Create a non-root build user required.
useradd -m builder

# Transfer ownership of the work directory to the build user.
chown -R builder /build

# Build the binary package.
su builder -c 'cd /build && BASH_XTRACEFD= rpmbuild -ba /build/SPECS/snapd.spec --define "_topdir /build" --nocheck'
```
