# openSUSE Packaging

This directory contains packaging for openSUSE family of distributions.

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

We are using the openSUSE registry, with the latest tumbleweed image.

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
    registry.opensuse.org/opensuse/tumbleweed:latest \
    /bin/bash -x -u
```

## Host Script

The small host script creates the .build directory with the structure expected
by `rpmbuild`. This directory is shared with the container and can be used to
access resulting packages and build logs.

```sh
mkdir -p .build/{SRPMS,RPMS,SOURCES,SPECS}
```

## Container Script

The build script has several sections. As a part of the process we are creating
a source tarball, and combining it with the `snapd.spec` file from this
directory.

```sh
# Install bootstrap packages.
BASH_XTRACEFD= zypper --non-interactive install --no-recommends \
    bash coreutils findutils gawk git gzip make rpm-build \
    rpm-config-SUSE systemd-rpm-macros tar xz


# Copy packaging files to the build directory.
install -t /build/SPECS/ \
    /src/packaging/opensuse/snapd.spec \
    /src/packaging/opensuse/snapd.changes \

# Copy extra files to SOURCES as they are referenced from the spec file.
install -t /build/SOURCES/ \
    /src/packaging/opensuse/snapd-rpmlintrc \
    /src/packaging/opensuse/permissions.*


# Discover package version.
version=$(rpmspec -q --qf "%{VERSION}\n" /build/SPECS/snapd.spec | head -n1); \

# Refresh archive index.
zypper --non-interactive refresh

# Discover and install build-dependencies.
rpmspec -q --buildrequires /build/SPECS/snapd.spec > /tmp/buildreqs.txt
BASH_XTRACEFD= xargs -r -d "\n" zypper --non-interactive install --no-recommends < /tmp/buildreqs.txt

# Copy the source tree to a temporary location, so that we can call go mod vendor.
mkdir -p /src-rw
tar -C /src -c . | tar -C /src-rw -x

# Vendor Go modules that are needed.
( cd /src-rw && go mod vendor )

# Create source archive with vendored sources.
( cd /src-rw && ./packaging/pack-source -s -v "$version" -o /build/SOURCES )

# Create a non-root build user required.
useradd -m builder

# Transfer ownership of the work directory to the build user.
chown -R builder /build

# Build the binary package.
su builder -c 'cd /build && BASH_XTRACEFD= rpmbuild -ba /build/SPECS/snapd.spec --define "_topdir /build" --nocheck'
```
