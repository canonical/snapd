# Fedora Packaging

This directory contains packaging for the Fedora family of distributions.

## Build Container

The package can be built using a rootless podman container.

The entire snapd tree is exposed as `/src` inside the container, this is done
with the first `-v` switch. The `.build/rpmbuild` directory is exposed as the
`/work` directory. This is where most of the build actually happens. Lastly the
`out` directory is exposed as `/out` inside the container. This is where we
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
    -v "../../:/src:ro,Z" \
    -v ".build/rpmbuild:/work:Z" \
    -v "out:/out:Z" \
    -w /work \
    registry.fedoraproject.org/fedora:latest \
    /bin/bash -x -u
```

## Host Script

The small host script creates the input and output directories that allow
exchanging data with the container. The `.build/rpmbuild` directory can be
inspected to debug failures.

```sh
mkdir -p .build/rpmbuild
mkdir -p .build/rpmbuild/{SRPMS,RPMS,SOURCES,SPECS}
mkdir -p out
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

# Copy the spec file to the SPECS directory.
install -t /work/SPECS/ /src/packaging/fedora/snapd.spec

# Refresh package metadata.
dnf --assumeyes makecache

# Discover build-dependencies.
rpmspec -q --buildrequires /work/SPECS/snapd.spec \
    | sed "/^rpmlib(/d;/^$/d" \
    | sort -u \
> /tmp/buildreqs.txt

# Install build dependencies.
if [ -s /tmp/buildreqs.txt ]; then
    xargs -r -d "\n" dnf --assumeyes install < /tmp/buildreqs.txt
fi

# Discover package version.
version=$(rpmspec -q --qf "%{VERSION}\n" /work/SPECS/snapd.spec | head -n1)

# Copy the source tree to a temporary location, so that we can call go mod vendor.
mkdir -p /src-rw
tar -C /src -c . | tar -C /src-rw -x

# Vendor Go modules that are needed.
( cd /src-rw && go mod vendor )

# Create the no-vendor and only-vendor source archives.
( cd /src-rw && ./packaging/pack-source -v "$version" -o /work/SOURCES )

# Build the binary package.
BASH_XTRACEFD= rpmbuild -ba /work/SPECS/snapd.spec --define "_topdir /work"

# Copy source and binary packages.
cp -a /work/SRPMS/. /out/
cp -a /work/RPMS/. /out/
```
