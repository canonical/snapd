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

The `BASH_XTRACEFD` environment variable is preserved, along with the file
descriptor. This allows the outer script to differentiate trace output from
stderr for better clarity.

Several named volumes are used to avoid network operations on subsequent runs.
- `snapd-opensuse-zypper-cache` is mapped to `/var/cache/zypp`
- `snapd-gomod-cache` is mapped to `/var/cache/gomod`

`GOMODCACHE` is exported early in the container script so every subsequent `go`
invocation picks it up. This volume is shared with other distributions that use
the same approach.

Zypper discards downloaded packages after installation by default, so all
repositories are modified to set the keep packages flag.


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
    -v "snapd-opensuse-zypper-cache:/var/cache/zypp" \
    -v "snapd-gomod-cache:/var/cache/gomod" \
    -w /build \
    registry.opensuse.org/opensuse/tumbleweed:latest \
    /bin/bash -x -e -u
```

## Host Script

The small host script creates the .build directory with the structure expected
by `rpmbuild`. This directory is shared with the container and can be used to
access resulting packages and build logs.

```sh
rm -rf .build
mkdir -p .build/{SRPMS,RPMS,SOURCES,SPECS}
```

## Container Script

The build script has several sections. The pre-created source tarball from
`packaging/.build/` is combined with the `snapd.spec` file from this directory.

```sh
# Show the sizes of persistent caches to verify volumes are populated across runs.
echo "Zypper cache:              $(du -sh /var/cache/zypp              2>/dev/null | cut -f1 || echo empty)"
echo "Go module cache:           $(du -sh /var/cache/gomod             2>/dev/null | cut -f1 || echo empty)"

# Allow both root and the builder user to read and write the Go module cache.
chmod 1777 /var/cache/gomod
export GOMODCACHE=/var/cache/gomod

# Configure zypper to retain downloaded packages in the cache volume.
zypper modifyrepo --keep-packages --all

# Install bootstrap packages.
BASH_XTRACEFD= zypper --non-interactive install --no-recommends \
    bash coreutils findutils gawk gzip make rpm-build \
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
version=$(rpmspec -q --qf "%{VERSION}\n" /build/SPECS/snapd.spec | head -n1);

# Refresh archive index.
zypper --non-interactive refresh

# Discover and install build-dependencies.
rpmspec -q --buildrequires /build/SPECS/snapd.spec > /tmp/buildreqs.txt
BASH_XTRACEFD= xargs -r -d "\n" zypper --non-interactive install --no-recommends < /tmp/buildreqs.txt

# Copy the pre-created source tarball from the packaging directory.
install /src/packaging/.build/snapd_"$version".vendor.tar.xz /build/SOURCES/

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
