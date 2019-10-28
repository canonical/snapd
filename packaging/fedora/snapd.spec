# With Fedora, nothing is bundled. For everything else, bundling is used.
# To use bundled stuff, use "--with vendorized" on rpmbuild
%if 0%{?fedora}
%bcond_with vendorized
%else
%bcond_without vendorized
%endif

# With Amazon Linux 2+, we're going to provide the /snap symlink by default,
# since classic snaps currently require it... :(
%if 0%{?amzn} >= 2
%bcond_without snap_symlink
%else
%bcond_with snap_symlink
%endif

# A switch to allow building the package with support for testkeys which
# are used for the spread test suite of snapd.
%bcond_with testkeys

%global with_devel 1
%global with_debug 1
%global with_check 0
%global with_unit_test 0
%global with_test_keys 0
%global with_selinux 1

# For the moment, we don't support all golang arches...
%global with_goarches 0

# Set if multilib is enabled for supported arches
%ifarch x86_64 aarch64 %{power64} s390x
%global with_multilib 1
%endif

%if ! %{with vendorized}
%global with_bundled 0
%else
%global with_bundled 1
%endif

%if ! %{with testkeys}
%global with_test_keys 0
%else
%global with_test_keys 1
%endif

%if 0%{?with_debug}
%global _dwz_low_mem_die_limit 0
%else
%global debug_package   %{nil}
%endif

%global provider        github
%global provider_tld    com
%global project         snapcore
%global repo            snapd
# https://github.com/snapcore/snapd
%global provider_prefix %{provider}.%{provider_tld}/%{project}/%{repo}
%global import_path     %{provider_prefix}

%global snappy_svcs     snapd.service snapd.socket snapd.autoimport.service snapd.seeded.service
%global snappy_user_svcs snapd.session-agent.socket

# Until we have a way to add more extldflags to gobuild macro...
%if 0%{?fedora} || 0%{?rhel} >= 8
# buildmode PIE triggers external linker consumes -extldflags
%define gobuild_static(o:) go build -buildmode pie -compiler gc -tags=rpm_crashtraceback -ldflags "${LDFLAGS:-} -B 0x$(head -c20 /dev/urandom|od -An -tx1|tr -d ' \\n') -extldflags '%__global_ldflags -static'" -a -v -x %{?**};
%endif
%if 0%{?rhel} == 7
# trigger external linker manually, otherwise -extldflags have no meaning
%define gobuild_static(o:) go build -compiler gc -tags=rpm_crashtraceback -ldflags "${LDFLAGS:-} -B 0x$(head -c20 /dev/urandom|od -An -tx1|tr -d ' \\n') -linkmode external -extldflags '%__global_ldflags -static'" -a -v -x %{?**};
%endif

# These macros are not defined in RHEL 7
%if 0%{?rhel} == 7
%define gobuild(o:) go build -compiler gc -tags=rpm_crashtraceback -ldflags "${LDFLAGS:-} -B 0x$(head -c20 /dev/urandom|od -An -tx1|tr -d ' \\n') -linkmode external -extldflags '%__global_ldflags'" -a -v -x %{?**};
%define gotest() go test -compiler gc -ldflags "${LDFLAGS:-}" %{?**};
%endif

# Compat path macros
%{!?_environmentdir: %global _environmentdir %{_prefix}/lib/environment.d}
%{!?_systemdgeneratordir: %global _systemdgeneratordir %{_prefix}/lib/systemd/system-generators}
%{!?_systemd_system_env_generator_dir: %global _systemd_system_env_generator_dir %{_prefix}/lib/systemd/system-environment-generators}

# Fedora selinux-policy includes 'map' permission on a 'file' class. However,
# Amazon Linux 2 does not have the updated policy containing the fix for
# https://bugzilla.redhat.com/show_bug.cgi?id=1574383.
# For now disable SELinux on Amazon Linux 2 until it's fixed.
%if 0%{?amzn2} == 1
%global with_selinux 0
%endif

Name:           snapd
Version:        2.42
Release:        0%{?dist}
Summary:        A transactional software package manager
License:        GPLv3
URL:            https://%{provider_prefix}
Source0:        https://%{provider_prefix}/releases/download/%{version}/%{name}_%{version}.no-vendor.tar.xz
Source1:        https://%{provider_prefix}/releases/download/%{version}/%{name}_%{version}.only-vendor.tar.xz

%if 0%{?with_goarches}
# e.g. el6 has ppc64 arch without gcc-go, so EA tag is required
ExclusiveArch:  %{?go_arches:%{go_arches}}%{!?go_arches:%{ix86} x86_64 %{arm}}
%else
# Verified arches from snapd upstream
ExclusiveArch:  %{ix86} x86_64 %{arm} aarch64 ppc64le s390x
%endif

# If go_compiler is not set to 1, there is no virtual provide. Use golang instead.
BuildRequires:  %{?go_compiler:compiler(go-compiler)}%{!?go_compiler:golang >= 1.9}
BuildRequires:  systemd
%{?systemd_requires}

Requires:       snap-confine%{?_isa} = %{version}-%{release}
Requires:       squashfs-tools

%if 0%{?rhel} && 0%{?rhel} < 8
# Rich dependencies not available, always pull in squashfuse
# snapd will use squashfs.ko instead of squashfuse if it's on the system
# NOTE: Amazon Linux 2 does not have squashfuse, squashfs.ko is part of the kernel package
%if ! 0%{?amzn2}
Requires:       squashfuse
Requires:       fuse
%endif
%else
# snapd will use squashfuse in the event that squashfs.ko isn't available (cloud instances, containers, etc.)
Requires:       ((squashfuse and fuse) or kmod(squashfs.ko))
%endif

# bash-completion owns /usr/share/bash-completion/completions
Requires:       bash-completion

%if 0%{?with_selinux}
# Force the SELinux module to be installed
Requires:       %{name}-selinux = %{version}-%{release}
%endif

%if 0%{?fedora} && 0%{?fedora} < 30
# snapd-login-service is no more
# Note: Remove when F29 is EOL
Obsoletes:      %{name}-login-service < 1.33
Provides:       %{name}-login-service = 1.33
Provides:       %{name}-login-service%{?_isa} = 1.33
%endif

%if ! 0%{?with_bundled}
BuildRequires: golang(github.com/boltdb/bolt)
BuildRequires: golang(github.com/coreos/go-systemd/activation)
BuildRequires: golang(github.com/godbus/dbus)
BuildRequires: golang(github.com/godbus/dbus/introspect)
BuildRequires: golang(github.com/gorilla/mux)
BuildRequires: golang(github.com/jessevdk/go-flags)
BuildRequires: golang(github.com/juju/ratelimit)
BuildRequires: golang(github.com/kr/pretty)
BuildRequires: golang(github.com/kr/text)
BuildRequires: golang(github.com/mvo5/goconfigparser)
BuildRequires: golang(github.com/seccomp/libseccomp-golang)
BuildRequires: golang(github.com/snapcore/go-gettext)
BuildRequires: golang(golang.org/x/crypto/openpgp/armor)
BuildRequires: golang(golang.org/x/crypto/openpgp/packet)
BuildRequires: golang(golang.org/x/crypto/sha3)
BuildRequires: golang(golang.org/x/crypto/ssh/terminal)
BuildRequires: golang(golang.org/x/xerrors)
BuildRequires: golang(golang.org/x/xerrors/internal)
BuildRequires: golang(gopkg.in/check.v1)
BuildRequires: golang(gopkg.in/macaroon.v1)
BuildRequires: golang(gopkg.in/mgo.v2/bson)
BuildRequires: golang(gopkg.in/retry.v1)
BuildRequires: golang(gopkg.in/tomb.v2)
BuildRequires: golang(gopkg.in/yaml.v2)
%endif

%description
Snappy is a modern, cross-distribution, transactional package manager
designed for working with self-contained, immutable packages.

%package -n snap-confine
Summary:        Confinement system for snap applications
License:        GPLv3
BuildRequires:  autoconf
BuildRequires:  automake
BuildRequires:  libtool
BuildRequires:  gcc
BuildRequires:  gettext
BuildRequires:  gnupg
BuildRequires:  pkgconfig(glib-2.0)
BuildRequires:  pkgconfig(libcap)
BuildRequires:  pkgconfig(libseccomp)
%if 0%{?with_selinux}
BuildRequires:  pkgconfig(libselinux)
%endif
BuildRequires:  pkgconfig(libudev)
BuildRequires:  pkgconfig(systemd)
BuildRequires:  pkgconfig(udev)
BuildRequires:  xfsprogs-devel
BuildRequires:  glibc-static
%if ! 0%{?rhel}
BuildRequires:  libseccomp-static
%endif
BuildRequires:  valgrind
BuildRequires:  %{_bindir}/rst2man
%if 0%{?fedora}
# ShellCheck in EPEL is too old...
BuildRequires:  %{_bindir}/shellcheck
%endif

# Ensures older version from split packaging is replaced
Obsoletes:      snap-confine < 2.19

%description -n snap-confine
This package is used internally by snapd to apply confinement to
the started snap applications.

%if 0%{?with_selinux}
%package selinux
Summary:        SELinux module for snapd
License:        GPLv2+
BuildArch:      noarch
BuildRequires:  selinux-policy, selinux-policy-devel
Requires(post): selinux-policy-base >= %{_selinux_policy_version}
Requires(post): policycoreutils
%if 0%{?rhel} == 7
Requires(post): policycoreutils-python
%else
Requires(post): policycoreutils-python-utils
%endif
Requires(pre):  libselinux-utils
Requires(post): libselinux-utils

%description selinux
This package provides the SELinux policy module to ensure snapd
runs properly under an environment with SELinux enabled.
%endif

%if 0%{?with_devel}
%package devel
Summary:       Development files for %{name}
BuildArch:     noarch

%if 0%{?with_check} && ! 0%{?with_bundled}
%endif

%if ! 0%{?with_bundled}
Requires:      golang(github.com/boltdb/bolt)
Requires:      golang(github.com/coreos/go-systemd/activation)
Requires:      golang(github.com/godbus/dbus)
Requires:      golang(github.com/godbus/dbus/introspect)
Requires:      golang(github.com/gorilla/mux)
Requires:      golang(github.com/jessevdk/go-flags)
Requires:      golang(github.com/juju/ratelimit)
Requires:      golang(github.com/kr/pretty)
Requires:      golang(github.com/kr/text)
Requires:      golang(github.com/mvo5/goconfigparser)
Requires:      golang(github.com/seccomp/libseccomp-golang)
Requires:      golang(github.com/snapcore/go-gettext)
Requires:      golang(golang.org/x/crypto/openpgp/armor)
Requires:      golang(golang.org/x/crypto/openpgp/packet)
Requires:      golang(golang.org/x/crypto/sha3)
Requires:      golang(golang.org/x/crypto/ssh/terminal)
Requires:      golang(golang.org/x/xerrors)
Requires:      golang(golang.org/x/xerrors/internal)
Requires:      golang(gopkg.in/check.v1)
Requires:      golang(gopkg.in/macaroon.v1)
Requires:      golang(gopkg.in/mgo.v2/bson)
Requires:      golang(gopkg.in/retry.v1)
Requires:      golang(gopkg.in/tomb.v2)
Requires:      golang(gopkg.in/yaml.v2)
%else
# These Provides are unversioned because the sources in
# the bundled tarball are unversioned (they go by git commit)
# *sigh*... I hate golang...
Provides:      bundled(golang(github.com/snapcore/bolt))
Provides:      bundled(golang(github.com/coreos/go-systemd/activation))
Provides:      bundled(golang(github.com/godbus/dbus))
Provides:      bundled(golang(github.com/godbus/dbus/introspect))
Provides:      bundled(golang(github.com/gorilla/mux))
Provides:      bundled(golang(github.com/jessevdk/go-flags))
Provides:      bundled(golang(github.com/juju/ratelimit))
Provides:      bundled(golang(github.com/kr/pretty))
Provides:      bundled(golang(github.com/kr/text))
Provides:      bundled(golang(github.com/mvo5/goconfigparser))
Provides:      bundled(golang(github.com/mvo5/libseccomp-golang))
Provides:      bundled(golang(github.com/snapcore/go-gettext))
Provides:      bundled(golang(golang.org/x/crypto/openpgp/armor))
Provides:      bundled(golang(golang.org/x/crypto/openpgp/packet))
Provides:      bundled(golang(golang.org/x/crypto/sha3))
Provides:      bundled(golang(golang.org/x/crypto/ssh/terminal))
Provides:      bundled(golang(golang.org/x/xerrors))
Provides:      bundled(golang(golang.org/x/xerrors/internal))
Provides:      bundled(golang(gopkg.in/check.v1))
Provides:      bundled(golang(gopkg.in/macaroon.v1))
Provides:      bundled(golang(gopkg.in/mgo.v2/bson))
Provides:      bundled(golang(gopkg.in/retry.v1))
Provides:      bundled(golang(gopkg.in/tomb.v2))
Provides:      bundled(golang(gopkg.in/yaml.v2))
%endif

# Generated by gofed
Provides:      golang(%{import_path}/advisor) = %{version}-%{release}
Provides:      golang(%{import_path}/arch) = %{version}-%{release}
Provides:      golang(%{import_path}/asserts) = %{version}-%{release}
Provides:      golang(%{import_path}/asserts/assertstest) = %{version}-%{release}
Provides:      golang(%{import_path}/asserts/signtool) = %{version}-%{release}
Provides:      golang(%{import_path}/asserts/snapasserts) = %{version}-%{release}
Provides:      golang(%{import_path}/asserts/sysdb) = %{version}-%{release}
Provides:      golang(%{import_path}/asserts/systestkeys) = %{version}-%{release}
Provides:      golang(%{import_path}/boot) = %{version}-%{release}
Provides:      golang(%{import_path}/boot/boottest) = %{version}-%{release}
Provides:      golang(%{import_path}/bootloader) = %{version}-%{release}
Provides:      golang(%{import_path}/bootloader/androidbootenv) = %{version}-%{release}
Provides:      golang(%{import_path}/bootloader/grubenv) = %{version}-%{release}
Provides:      golang(%{import_path}/bootloader/ubootenv) = %{version}-%{release}
Provides:      golang(%{import_path}/client) = %{version}-%{release}
Provides:      golang(%{import_path}/cmd) = %{version}-%{release}
Provides:      golang(%{import_path}/cmd/cmdutil) = %{version}-%{release}
Provides:      golang(%{import_path}/cmd/snap-seccomp/syscalls) = %{version}-%{release}
Provides:      golang(%{import_path}/cmd/snaplock) = %{version}-%{release}
Provides:      golang(%{import_path}/daemon) = %{version}-%{release}
Provides:      golang(%{import_path}/dirs) = %{version}-%{release}
Provides:      golang(%{import_path}/errtracker) = %{version}-%{release}
Provides:      golang(%{import_path}/features) = %{version}-%{release}
Provides:      golang(%{import_path}/gadget) = %{version}-%{release}
Provides:      golang(%{import_path}/httputil) = %{version}-%{release}
Provides:      golang(%{import_path}/i18n) = %{version}-%{release}
Provides:      golang(%{import_path}/image) = %{version}-%{release}
Provides:      golang(%{import_path}/interfaces) = %{version}-%{release}
Provides:      golang(%{import_path}/interfaces/apparmor) = %{version}-%{release}
Provides:      golang(%{import_path}/interfaces/backends) = %{version}-%{release}
Provides:      golang(%{import_path}/interfaces/builtin) = %{version}-%{release}
Provides:      golang(%{import_path}/interfaces/dbus) = %{version}-%{release}
Provides:      golang(%{import_path}/interfaces/hotplug) = %{version}-%{release}
Provides:      golang(%{import_path}/interfaces/ifacetest) = %{version}-%{release}
Provides:      golang(%{import_path}/interfaces/kmod) = %{version}-%{release}
Provides:      golang(%{import_path}/interfaces/mount) = %{version}-%{release}
Provides:      golang(%{import_path}/interfaces/policy) = %{version}-%{release}
Provides:      golang(%{import_path}/interfaces/seccomp) = %{version}-%{release}
Provides:      golang(%{import_path}/interfaces/systemd) = %{version}-%{release}
Provides:      golang(%{import_path}/interfaces/udev) = %{version}-%{release}
Provides:      golang(%{import_path}/interfaces/utils) = %{version}-%{release}
Provides:      golang(%{import_path}/jsonutil) = %{version}-%{release}
Provides:      golang(%{import_path}/jsonutil/safejson) = %{version}-%{release}
Provides:      golang(%{import_path}/logger) = %{version}-%{release}
Provides:      golang(%{import_path}/metautil) = %{version}-%{release}
Provides:      golang(%{import_path}/netutil) = %{version}-%{release}
Provides:      golang(%{import_path}/osutil) = %{version}-%{release}
Provides:      golang(%{import_path}/osutil/squashfs) = %{version}-%{release}
Provides:      golang(%{import_path}/osutil/strace) = %{version}-%{release}
Provides:      golang(%{import_path}/osutil/sys) = %{version}-%{release}
Provides:      golang(%{import_path}/osutil/udev/crawler) = %{version}-%{release}
Provides:      golang(%{import_path}/osutil/udev/netlink) = %{version}-%{release}
Provides:      golang(%{import_path}/overlord) = %{version}-%{release}
Provides:      golang(%{import_path}/overlord/assertstate) = %{version}-%{release}
Provides:      golang(%{import_path}/overlord/auth) = %{version}-%{release}
Provides:      golang(%{import_path}/overlord/cmdstate) = %{version}-%{release}
Provides:      golang(%{import_path}/overlord/configstate) = %{version}-%{release}
Provides:      golang(%{import_path}/overlord/configstate/config) = %{version}-%{release}
Provides:      golang(%{import_path}/overlord/configstate/configcore) = %{version}-%{release}
Provides:      golang(%{import_path}/overlord/configstate/proxyconf) = %{version}-%{release}
Provides:      golang(%{import_path}/overlord/configstate/settings) = %{version}-%{release}
Provides:      golang(%{import_path}/overlord/devicestate) = %{version}-%{release}
Provides:      golang(%{import_path}/overlord/devicestate/devicestatetest) = %{version}-%{release}
Provides:      golang(%{import_path}/overlord/hookstate) = %{version}-%{release}
Provides:      golang(%{import_path}/overlord/hookstate/ctlcmd) = %{version}-%{release}
Provides:      golang(%{import_path}/overlord/hookstate/hooktest) = %{version}-%{release}
Provides:      golang(%{import_path}/overlord/ifacestate) = %{version}-%{release}
Provides:      golang(%{import_path}/overlord/ifacestate/ifacerepo) = %{version}-%{release}
Provides:      golang(%{import_path}/overlord/ifacestate/udevmonitor) = %{version}-%{release}
Provides:      golang(%{import_path}/overlord/patch) = %{version}-%{release}
Provides:      golang(%{import_path}/overlord/servicestate) = %{version}-%{release}
Provides:      golang(%{import_path}/overlord/snapshotstate) = %{version}-%{release}
Provides:      golang(%{import_path}/overlord/snapshotstate/backend) = %{version}-%{release}
Provides:      golang(%{import_path}/overlord/snapstate) = %{version}-%{release}
Provides:      golang(%{import_path}/overlord/snapstate/backend) = %{version}-%{release}
Provides:      golang(%{import_path}/overlord/standby) = %{version}-%{release}
Provides:      golang(%{import_path}/overlord/state) = %{version}-%{release}
Provides:      golang(%{import_path}/overlord/storecontext) = %{version}-%{release}
Provides:      golang(%{import_path}/polkit) = %{version}-%{release}
Provides:      golang(%{import_path}/progress) = %{version}-%{release}
Provides:      golang(%{import_path}/progress/progresstest) = %{version}-%{release}
Provides:      golang(%{import_path}/release) = %{version}-%{release}
Provides:      golang(%{import_path}/sandbox/seccomp) = %{version}-%{release}
Provides:      golang(%{import_path}/sanity) = %{version}-%{release}
Provides:      golang(%{import_path}/selinux) = %{version}-%{release}
Provides:      golang(%{import_path}/snap) = %{version}-%{release}
Provides:      golang(%{import_path}/snap/naming) = %{version}-%{release}
Provides:      golang(%{import_path}/snap/pack) = %{version}-%{release}
Provides:      golang(%{import_path}/snap/snapdir) = %{version}-%{release}
Provides:      golang(%{import_path}/snap/snapenv) = %{version}-%{release}
Provides:      golang(%{import_path}/snap/snaptest) = %{version}-%{release}
Provides:      golang(%{import_path}/snap/squashfs) = %{version}-%{release}
Provides:      golang(%{import_path}/spdx) = %{version}-%{release}
Provides:      golang(%{import_path}/store) = %{version}-%{release}
Provides:      golang(%{import_path}/store/storetest) = %{version}-%{release}
Provides:      golang(%{import_path}/strutil) = %{version}-%{release}
Provides:      golang(%{import_path}/strutil/quantity) = %{version}-%{release}
Provides:      golang(%{import_path}/strutil/shlex) = %{version}-%{release}
Provides:      golang(%{import_path}/systemd) = %{version}-%{release}
Provides:      golang(%{import_path}/tests/lib/fakestore/refresh) = %{version}-%{release}
Provides:      golang(%{import_path}/tests/lib/fakestore/store) = %{version}-%{release}
Provides:      golang(%{import_path}/testutil) = %{version}-%{release}
Provides:      golang(%{import_path}/timeout) = %{version}-%{release}
Provides:      golang(%{import_path}/timeutil) = %{version}-%{release}
Provides:      golang(%{import_path}/timings) = %{version}-%{release}
Provides:      golang(%{import_path}/userd) = %{version}-%{release}
Provides:      golang(%{import_path}/userd/ui) = %{version}-%{release}
Provides:      golang(%{import_path}/wrappers) = %{version}-%{release}
Provides:      golang(%{import_path}/x11) = %{version}-%{release}
Provides:      golang(%{import_path}/xdgopenproxy) = %{version}-%{release}

%description devel
This package contains library source intended for
building other packages which use import path with
%{import_path} prefix.
%endif

%if 0%{?with_unit_test} && 0%{?with_devel}
%package unit-test-devel
Summary:         Unit tests for %{name} package

%if 0%{?with_check}
#Here comes all BuildRequires: PACKAGE the unit tests
#in %%check section need for running
%endif

# test subpackage tests code from devel subpackage
Requires:        %{name}-devel = %{version}-%{release}

%description unit-test-devel
This package contains unit tests for project
providing packages with %{import_path} prefix.
%endif

%prep
%if ! 0%{?with_bundled}
%setup -q
# Ensure there's no bundled stuff accidentally leaking in...
rm -rf vendor/*
%else
# Extract each tarball properly
%setup -q -D -b 1
%endif

%build
# Generate version files
./mkversion.sh "%{version}-%{release}"

# We don't want/need squashfuse in the rpm, as it's available in Fedora and EPEL
sed -e 's:_ "github.com/snapcore/squashfuse"::g' -i systemd/systemd.go

# Build snapd
mkdir -p src/github.com/snapcore
ln -s ../../../ src/github.com/snapcore/snapd

%if ! 0%{?with_bundled}
export GOPATH=$(pwd):%{gopath}
%else
export GOPATH=$(pwd):$(pwd)/Godeps/_workspace:%{gopath}
%endif

GOFLAGS=
%if 0%{?with_test_keys}
GOFLAGS="$GOFLAGS -tags withtestkeys"
%endif

%if ! 0%{?with_bundled}
# We don't need mvo5 fork for seccomp, as we have seccomp 2.3.x
sed -e "s:github.com/mvo5/libseccomp-golang:github.com/seccomp/libseccomp-golang:g" -i cmd/snap-seccomp/*.go
# We don't need the snapcore fork for bolt - it is just a fix on ppc
sed -e "s:github.com/snapcore/bolt:github.com/boltdb/bolt:g" -i advisor/*.go errtracker/*.go
%endif

# We have to build snapd first to prevent the build from
# building various things from the tree without additional
# set tags.
%gobuild -o bin/snapd $GOFLAGS %{import_path}/cmd/snapd
%gobuild -o bin/snap $GOFLAGS %{import_path}/cmd/snap
%gobuild -o bin/snap-failure $GOFLAGS %{import_path}/cmd/snap-failure

# To ensure things work correctly with base snaps,
# snap-exec, snap-update-ns, and snapctl need to be built statically
%gobuild_static -o bin/snap-exec $GOFLAGS %{import_path}/cmd/snap-exec
%gobuild_static -o bin/snap-update-ns $GOFLAGS %{import_path}/cmd/snap-update-ns
%gobuild_static -o bin/snapctl $GOFLAGS %{import_path}/cmd/snapctl

%if 0%{?rhel}
# There's no static link library for libseccomp in RHEL/CentOS...
sed -e "s/-Bstatic -lseccomp/-Bstatic/g" -i cmd/snap-seccomp/*.go
%endif
%gobuild -o bin/snap-seccomp $GOFLAGS %{import_path}/cmd/snap-seccomp

%if 0%{?with_selinux}
# Build SELinux module
pushd ./data/selinux
make SHARE="%{_datadir}" TARGETS="snappy"
popd
%endif

# Build snap-confine
pushd ./cmd
# FIXME This is a hack to get rid of a patch we have to ship for the
# Fedora package at the moment as /usr/lib/rpm/redhat/redhat-hardened-ld
# accidentially adds -pie for static executables. See
# https://bugzilla.redhat.com/show_bug.cgi?id=1343892 for a few more
# details. To prevent this from happening we drop the linker
# script and define our LDFLAGS manually for now.
export LDFLAGS="-Wl,-z,relro -z now"
autoreconf --force --install --verbose
# FIXME: add --enable-caps-over-setuid as soon as possible (setuid discouraged!)
%configure \
    --disable-apparmor \
%if 0%{?with_selinux}
    --enable-selinux \
%endif
    --libexecdir=%{_libexecdir}/snapd/ \
    --enable-nvidia-biarch \
    %{?with_multilib:--with-32bit-libdir=%{_prefix}/lib} \
    --with-snap-mount-dir=%{_sharedstatedir}/snapd/snap \
    --enable-merged-usr

%make_build
popd

# Build systemd units, dbus services, and env files
pushd ./data
make BINDIR="%{_bindir}" LIBEXECDIR="%{_libexecdir}" \
     SYSTEMDSYSTEMUNITDIR="%{_unitdir}" \
     SNAP_MOUNT_DIR="%{_sharedstatedir}/snapd/snap" \
     SNAPD_ENVIRONMENT_FILE="%{_sysconfdir}/sysconfig/snapd"
popd

%install
install -d -p %{buildroot}%{_bindir}
install -d -p %{buildroot}%{_libexecdir}/snapd
install -d -p %{buildroot}%{_mandir}/man8
install -d -p %{buildroot}%{_environmentdir}
install -d -p %{buildroot}%{_systemdgeneratordir}
install -d -p %{buildroot}%{_systemd_system_env_generator_dir}
install -d -p %{buildroot}%{_unitdir}
install -d -p %{buildroot}%{_sysconfdir}/profile.d
install -d -p %{buildroot}%{_sysconfdir}/sysconfig
install -d -p %{buildroot}%{_sharedstatedir}/snapd/assertions
install -d -p %{buildroot}%{_sharedstatedir}/snapd/cookie
install -d -p %{buildroot}%{_sharedstatedir}/snapd/desktop/applications
install -d -p %{buildroot}%{_sharedstatedir}/snapd/device
install -d -p %{buildroot}%{_sharedstatedir}/snapd/hostfs
install -d -p %{buildroot}%{_sharedstatedir}/snapd/lib/gl
install -d -p %{buildroot}%{_sharedstatedir}/snapd/lib/gl32
install -d -p %{buildroot}%{_sharedstatedir}/snapd/lib/glvnd
install -d -p %{buildroot}%{_sharedstatedir}/snapd/lib/vulkan
install -d -p %{buildroot}%{_sharedstatedir}/snapd/mount
install -d -p %{buildroot}%{_sharedstatedir}/snapd/seccomp/bpf
install -d -p %{buildroot}%{_sharedstatedir}/snapd/snaps
install -d -p %{buildroot}%{_sharedstatedir}/snapd/snap/bin
install -d -p %{buildroot}%{_localstatedir}/snap
install -d -p %{buildroot}%{_localstatedir}/cache/snapd
install -d -p %{buildroot}%{_datadir}/polkit-1/actions
%if 0%{?with_selinux}
install -d -p %{buildroot}%{_datadir}/selinux/devel/include/contrib
install -d -p %{buildroot}%{_datadir}/selinux/packages
%endif

# Install snap and snapd
install -p -m 0755 bin/snap %{buildroot}%{_bindir}
install -p -m 0755 bin/snap-exec %{buildroot}%{_libexecdir}/snapd
install -p -m 0755 bin/snap-failure %{buildroot}%{_libexecdir}/snapd
install -p -m 0755 bin/snapd %{buildroot}%{_libexecdir}/snapd
install -p -m 0755 bin/snap-update-ns %{buildroot}%{_libexecdir}/snapd
install -p -m 0755 bin/snap-seccomp %{buildroot}%{_libexecdir}/snapd
# Ensure /usr/bin/snapctl is a symlink to /usr/libexec/snapd/snapctl
install -p -m 0755 bin/snapctl %{buildroot}%{_libexecdir}/snapd/snapctl
ln -sf %{_libexecdir}/snapd/snapctl %{buildroot}%{_bindir}/snapctl

%if 0%{?with_selinux}
# Install SELinux module
install -p -m 0644 data/selinux/snappy.if %{buildroot}%{_datadir}/selinux/devel/include/contrib
install -p -m 0644 data/selinux/snappy.pp.bz2 %{buildroot}%{_datadir}/selinux/packages
%endif

# Install snap(8) man page
bin/snap help --man > %{buildroot}%{_mandir}/man8/snap.8

# Install the "info" data file with snapd version
install -m 644 -D data/info %{buildroot}%{_libexecdir}/snapd/info

# Install bash completion for "snap"
install -m 644 -D data/completion/snap %{buildroot}%{_datadir}/bash-completion/completions/snap
install -m 644 -D data/completion/complete.sh %{buildroot}%{_libexecdir}/snapd
install -m 644 -D data/completion/etelpmoc.sh %{buildroot}%{_libexecdir}/snapd

# Install snap-confine
pushd ./cmd
%make_install
# Undo the 0111 permissions, they are restored in the files section
chmod 0755 %{buildroot}%{_sharedstatedir}/snapd/void
# We don't use AppArmor
rm -rfv %{buildroot}%{_sysconfdir}/apparmor.d
# ubuntu-core-launcher is dead
rm -fv %{buildroot}%{_bindir}/ubuntu-core-launcher
popd

# Install all systemd and dbus units, and env files
pushd ./data
%make_install BINDIR="%{_bindir}" LIBEXECDIR="%{_libexecdir}" \
              SYSTEMDSYSTEMUNITDIR="%{_unitdir}" \
              SNAP_MOUNT_DIR="%{_sharedstatedir}/snapd/snap" \
              SNAPD_ENVIRONMENT_FILE="%{_sysconfdir}/sysconfig/snapd"
popd


%if 0%{?rhel} == 7
# Install kernel tweaks
# See: https://access.redhat.com/articles/3128691
install -m 644 -D data/sysctl/rhel7-snap.conf %{buildroot}%{_sysctldir}/99-snap.conf
%endif

# Remove snappy core specific units
rm -fv %{buildroot}%{_unitdir}/snapd.system-shutdown.service
rm -fv %{buildroot}%{_unitdir}/snapd.snap-repair.*
rm -fv %{buildroot}%{_unitdir}/snapd.core-fixup.*

# Remove snappy core specific scripts
rm %{buildroot}%{_libexecdir}/snapd/snapd.core-fixup.sh

# Remove snapd apparmor service
rm -f %{buildroot}%{_unitdir}/snapd.apparmor.service
rm -f %{buildroot}%{_libexecdir}/snapd/snapd-apparmor

# Install Polkit configuration
install -m 644 -D data/polkit/io.snapcraft.snapd.policy %{buildroot}%{_datadir}/polkit-1/actions

# Disable re-exec by default
echo 'SNAP_REEXEC=0' > %{buildroot}%{_sysconfdir}/sysconfig/snapd

# Create state.json and the README file to be ghosted
touch %{buildroot}%{_sharedstatedir}/snapd/state.json
touch %{buildroot}%{_sharedstatedir}/snapd/snap/README

# When enabled, create a symlink for /snap to point to /var/lib/snapd/snap
%if %{with snap_symlink}
ln -sr %{buildroot}%{_sharedstatedir}/snapd/snap %{buildroot}/snap
%endif

# source codes for building projects
%if 0%{?with_devel}
install -d -p %{buildroot}/%{gopath}/src/%{import_path}/
echo "%%dir %%{gopath}/src/%%{import_path}/." >> devel.file-list
# find all *.go but no *_test.go files and generate devel.file-list
for file in $(find . -iname "*.go" -o -iname "*.s" \! -iname "*_test.go") ; do
    echo "%%dir %%{gopath}/src/%%{import_path}/$(dirname $file)" >> devel.file-list
    install -d -p %{buildroot}/%{gopath}/src/%{import_path}/$(dirname $file)
    cp -pav $file %{buildroot}/%{gopath}/src/%{import_path}/$file
    echo "%%{gopath}/src/%%{import_path}/$file" >> devel.file-list
done
%endif

# testing files for this project
%if 0%{?with_unit_test} && 0%{?with_devel}
install -d -p %{buildroot}/%{gopath}/src/%{import_path}/
# find all *_test.go files and generate unit-test.file-list
for file in $(find . -iname "*_test.go"); do
    echo "%%dir %%{gopath}/src/%%{import_path}/$(dirname $file)" >> devel.file-list
    install -d -p %{buildroot}/%{gopath}/src/%{import_path}/$(dirname $file)
    cp -pav $file %{buildroot}/%{gopath}/src/%{import_path}/$file
    echo "%%{gopath}/src/%%{import_path}/$file" >> unit-test-devel.file-list
done

# Install additional testdata
install -d %{buildroot}/%{gopath}/src/%{import_path}/cmd/snap/test-data/
cp -pav cmd/snap/test-data/* %{buildroot}/%{gopath}/src/%{import_path}/cmd/snap/test-data/
echo "%%{gopath}/src/%%{import_path}/cmd/snap/test-data" >> unit-test-devel.file-list
%endif

%if 0%{?with_devel}
sort -u -o devel.file-list devel.file-list
%endif

%check
for binary in snap-exec snap-update-ns snapctl; do
    ldd bin/$binary | grep 'not a dynamic executable'
done

# snapd tests
%if 0%{?with_check} && 0%{?with_unit_test} && 0%{?with_devel}
%if ! 0%{?with_bundled}
export GOPATH=%{buildroot}/%{gopath}:%{gopath}
%else
export GOPATH=%{buildroot}/%{gopath}:$(pwd)/Godeps/_workspace:%{gopath}
%endif
%gotest %{import_path}/...
%endif

# snap-confine tests (these always run!)
pushd ./cmd
make check
popd

%files
#define license tag if not already defined
%{!?_licensedir:%global license %doc}
%license COPYING
%doc README.md docs/*
%{_bindir}/snap
%{_bindir}/snapctl
%{_environmentdir}/990-snapd.conf
%if 0%{?rhel} == 7
%{_sysctldir}/99-snap.conf
%endif
%dir %{_libexecdir}/snapd
%{_libexecdir}/snapd/snapctl
%{_libexecdir}/snapd/snapd
%{_libexecdir}/snapd/snap-exec
%{_libexecdir}/snapd/snap-failure
%{_libexecdir}/snapd/info
%{_libexecdir}/snapd/snap-mgmt
%if 0%{?with_selinux}
%{_libexecdir}/snapd/snap-mgmt-selinux
%endif
%{_mandir}/man8/snap.8*
%{_datadir}/applications/snap-handle-link.desktop
%{_datadir}/bash-completion/completions/snap
%{_libexecdir}/snapd/complete.sh
%{_libexecdir}/snapd/etelpmoc.sh
%{_libexecdir}/snapd/snapd.run-from-snap
%{_sysconfdir}/profile.d/snapd.sh
%{_mandir}/man8/snapd-env-generator.8*
%{_systemd_system_env_generator_dir}/snapd-env-generator
%{_unitdir}/snapd.socket
%{_unitdir}/snapd.service
%{_unitdir}/snapd.autoimport.service
%{_unitdir}/snapd.failure.service
%{_unitdir}/snapd.seeded.service
%{_userunitdir}/snapd.session-agent.service
%{_userunitdir}/snapd.session-agent.socket
%{_datadir}/dbus-1/services/io.snapcraft.Launcher.service
%{_datadir}/dbus-1/services/io.snapcraft.Settings.service
%{_datadir}/polkit-1/actions/io.snapcraft.snapd.policy
%{_sysconfdir}/xdg/autostart/snap-userd-autostart.desktop
%config(noreplace) %{_sysconfdir}/sysconfig/snapd
%dir %{_sharedstatedir}/snapd
%dir %{_sharedstatedir}/snapd/assertions
%dir %{_sharedstatedir}/snapd/cookie
%dir %{_sharedstatedir}/snapd/desktop
%dir %{_sharedstatedir}/snapd/desktop/applications
%dir %{_sharedstatedir}/snapd/device
%dir %{_sharedstatedir}/snapd/hostfs
%dir %{_sharedstatedir}/snapd/lib
%dir %{_sharedstatedir}/snapd/lib/gl
%dir %{_sharedstatedir}/snapd/lib/gl32
%dir %{_sharedstatedir}/snapd/lib/glvnd
%dir %{_sharedstatedir}/snapd/lib/vulkan
%dir %{_sharedstatedir}/snapd/mount
%dir %{_sharedstatedir}/snapd/seccomp
%dir %{_sharedstatedir}/snapd/seccomp/bpf
%dir %{_sharedstatedir}/snapd/snaps
%dir %{_sharedstatedir}/snapd/snap
%ghost %dir %{_sharedstatedir}/snapd/snap/bin
%dir %{_localstatedir}/cache/snapd
%dir %{_localstatedir}/snap
%ghost %{_sharedstatedir}/snapd/state.json
%ghost %{_sharedstatedir}/snapd/snap/README
%if %{with snap_symlink}
/snap
%endif

%files -n snap-confine
%doc cmd/snap-confine/PORTING
%license COPYING
%dir %{_libexecdir}/snapd
# For now, we can't use caps
# FIXME: Switch to "%%attr(0755,root,root) %%caps(cap_sys_admin=pe)" asap!
%attr(6755,root,root) %{_libexecdir}/snapd/snap-confine
%{_libexecdir}/snapd/snap-device-helper
%{_libexecdir}/snapd/snap-discard-ns
%{_libexecdir}/snapd/snap-gdb-shim
%{_libexecdir}/snapd/snap-seccomp
%{_libexecdir}/snapd/snap-update-ns
%{_libexecdir}/snapd/system-shutdown
%{_mandir}/man8/snap-confine.8*
%{_mandir}/man8/snap-discard-ns.8*
%{_systemdgeneratordir}/snapd-generator
%attr(0111,root,root) %{_sharedstatedir}/snapd/void

%if 0%{?with_selinux}
%files selinux
%license data/selinux/COPYING
%doc data/selinux/README.md
%{_datadir}/selinux/packages/snappy.pp.bz2
%{_datadir}/selinux/devel/include/contrib/snappy.if
%endif

%if 0%{?with_devel}
%files devel -f devel.file-list
%license COPYING
%doc README.md
%dir %{gopath}/src/%{provider}.%{provider_tld}/%{project}
%endif

%if 0%{?with_unit_test} && 0%{?with_devel}
%files unit-test-devel -f unit-test-devel.file-list
%license COPYING
%doc README.md
%endif

%post
%if 0%{?rhel} == 7
%sysctl_apply 99-snap.conf
%endif
%systemd_post %{snappy_svcs}
%systemd_user_post %{snappy_user_svcs}
# If install, test if snapd socket and timer are enabled.
# If enabled, then attempt to start them. This will silently fail
# in chroots or other environments where services aren't expected
# to be started.
if [ $1 -eq 1 ] ; then
   if systemctl -q is-enabled snapd.socket > /dev/null 2>&1 ; then
      systemctl start snapd.socket > /dev/null 2>&1 || :
   fi
fi

%preun
%systemd_preun %{snappy_svcs}
%systemd_user_preun %{snappy_user_svcs}

# Remove all Snappy content if snapd is being fully uninstalled
if [ $1 -eq 0 ]; then
   %{_libexecdir}/snapd/snap-mgmt --purge || :
fi

%postun
%systemd_postun_with_restart %{snappy_svcs}
%systemd_user_postun %{snappy_user_svcs}

%if 0%{?with_selinux}
%triggerun -- snapd < 2.39
# TODO: the trigger relies on a very specific snapd version that introduced SELinux
# mount context, figure out how to update the trigger condition to run when needed

# Trigger on uninstall, with one version of the package being pre 2.38 see
# https://rpm-packaging-guide.github.io/#triggers-and-scriptlets for details
# when triggers are run
if [ "$1" -eq 2 -a "$2" -eq 1 ]; then
   # Upgrade from pre 2.38 version
   %{_libexecdir}/snapd/snap-mgmt-selinux --patch-selinux-mount-context=system_u:object_r:snappy_snap_t:s0 || :

   # snapd might have created fontconfig cache directory earlier, but with
   # incorrect context due to bugs in the policy, make sure it gets the right one
   # on upgrade when the new policy was introduced
   if [ -d "%{_localstatedir}/cache/fontconfig" ]; then
      restorecon -R %{_localstatedir}/cache/fontconfig || :
   fi
elif [ "$1" -eq 1 -a "$2" -eq 2 ]; then
   # Downgrade to a pre 2.38 version
   %{_libexecdir}/snapd/snap-mgmt-selinux --remove-selinux-mount-context=system_u:object_r:snappy_snap_t:s0 || :
fi

%pre selinux
%selinux_relabel_pre

%post selinux
%selinux_modules_install %{_datadir}/selinux/packages/snappy.pp.bz2
%selinux_relabel_post

%posttrans selinux
%selinux_relabel_post

%postun selinux
%selinux_modules_uninstall snappy
if [ $1 -eq 0 ]; then
    %selinux_relabel_post
fi
%endif


%changelog
* Tue Oct 01 2019 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.42
 - tests: disable {contacts,calendar}-service tests on debian-sid
 - tests/main/snap-run: disable strace test cases on Arch
 - cmd/system-shutdown: include correct prototype for die
 - snap/naming: add test for hook name connect-plug-i2c
 - cmd/snap-confine: allow digits in hook names
 - gadget: do not fail the update when old gadget snap is missing
   bare content
 - tests: disable {contacts,calendar}-service tests on Arch Linux
 - tests: move "centos-7" to unstable systems
 - interfaces/docker-support,kubernetes-support: misc updates for
   strict k8s
 - packaging: remove obsolete usr.lib.snapd.snap-confine in
   postinst
 - tests: add test that ensures our snapfuse binary actually works
 - packaging: use snapfuse_ll to speed up snapfuse performance
 - usersession/userd: make sure to export DBus interfaces before
   requesting a name
 - data/selinux: allow snapd to issue sigkill to journalctl
 - store: download propagates options to delta download
 - wrappers: allow snaps to install icon theme icons
 - debug: state-inspect debugging utility
 - sandbox/cgroup: introduce cgroup wrappers package
 - snap-confine: fix return value checks for udev functions
 - cmd/model: output tweaks, add'l tests
 - wrappers/services: add ServicesEnableState + unit tests
 - tests: fix newline and wrong test name pointed out in previous PRs
 - tests: extend mount-ns test to handle mimics
 - run-checks, tests/main/go: allow gofmt checks to be skipped on
   19.10
 - tests/main/interfaces-{calendar,contacts}-service: disable on
   19.10
 - tests: part3 making tests work on ubuntu-core-18
 - tests: fix interfaces-timeserver-control on 19.10
 - overlord/snapstate: config revision code cleanup and extra tests
 - devicestate: allow remodel to different kernels
 - overlord,daemon: adjust startup timeout via EXTEND_TIMEOUT_USEC
   using an estimate
 - tests/main/many: increase kill-timeout to 5m
 - interfaces/kubernetes-support: allow systemd-run to ptrace read
   unconfined
 - snapstate: auto transition on experimental.snapd-snap=true
 - tests: retry checking until the written file on desktop-portal-
   filechooser
 - tests: unit test for a refresh failing on configure hook
 - tests: remove mount_id and parent_id from mount-ns test data
 - tests: move classic-ubuntu-core-transition* to nightly
 - tests/mountinfo-tool: proper formatting of opt_fields
 - overlord/configstate: special-case "null" in transaction Changes()
 - snap-confine: fallback gracefully on a cgroup v2 only system
 - tests: debian sid now ships new seccomp, adjust tests
 - tests: explicitly restore after using LXD
 - snapstate: make progress reporting less granular
 - bootloader: little kernel support
 - fixme: rename ubuntu*architectures to dpkg*architectures
 - tests: run dbus-launch inside a systemd unit
 - channel: introduce Resolve and ResolveLocked
 - tests: run failing tests on ubuntu eoan due to is now set as
   unstable
 - systemd: detach rather than unmount .mount units
 - cmd/snap-confine: add unit tests for sc_invocation, cleanup memory
   leaks in tests
 - boot,dirs,image: introduce boot.MakeBootable, use it in image
   instead of ad hoc code
 - cmd/snap-update-ns: clarify sharing comment
 - tests/overlord/snapstate: refactor for cleaner test failures
 - cmd/snap-update-ns: don't propagate detaching changes
 - interfaces: allow reading mutter Xauthority file
 - cmd/snap-confine: fix /snap duplication in legacy mode
 - tests: fix mountinfo-tool filtering when used with rewriting
 - seed,image,o/devicestate: extract seed loading to seed/seed16.go
 - many: pass the rootdir and options to bootloader.Find
 - tests: part5 making tests work on ubuntu-core-18
 - cmd/snap-confine: keep track of snap instance name and the snap
   name
 - cmd: unify die() across C programs
 - tests: add functions to make an abstraction for the snaps
 - packaging/fedora, tests/lib/prepare-restore: helper tool for
   packing sources for RPM
 - cmd/snap: improve help and error msg for snapshot commands
 - hookstate/ctlcmd: fix snapctl set help message
 - cmd/snap: don't append / to snap name just because a dir exists
 - tests: support fastly-global.cdn.snapcraft.io url on proxy-no-core
   test
 - tests: add --quiet switch to retry-tool
 - tests: add unstable stage for travis execution
 - tests: disable interfaces-timeserver-control on 19.10
 - tests: don't guess in is_classic_confinement_supported
 - boot, etc: simplify BootParticipant (etc) usage
 - tests: verify retry-tool not retrying missing commands
 - tests: rewrite "retry" command as retry-tool
 - tests: move debug section after restore
 - cmd/libsnap-confine-private, cmd/s-c: use constants for
   snap/instance name lengths
 - tests: measure behavior of the device cgroup
 - boot, bootloader, o/devicestate: boot env manip goes in boot
 - tests: enabling ubuntu 19.10-64 on spread.yaml
 - tests: fix ephemeral mount table in left over by prepare
 - tests: add version-tool for comparing versions
 - cmd/libsnap: make feature flag enum 1<<N style
 - many: refactor boot/boottest and move to bootloader/bootloadertest
 - tests/cross/go-build: use go list rather than shell trickery
 - HACKING.md: clarify where "make fmt" is needed
 - osutil: make flock test more robust
 - features, overlord: make parallel-installs exported, export flags
   on startup
 - overlord/devicestate:  support the device service returning a
   stream of assertions
 - many: add snap model command, add /v2/model, /v2/model/serial REST
   APIs
 - debian: set GOCACHE dir during build to fix FTBFS on eoan
 - boot, etc.: refactor boot to have a lookup with different imps
 - many: add the start of Core 20 extensions support to the model
   assertion
 - overlord/snapstate: revert track-risk behavior change and
   validation on install
 - cmd/snap,image,seed:  move image.ValidateSeed to
   seed.ValidateFromYaml
 - image,o/devicestate,seed: oops, make sure to clear seedtest
   helpers
 - tests/main/snap-info: update check.py for test-snapd-tools 2.0
 - tests: moving tests to nightly suite
 - overlord/devicestate,seed:  small step, introduce
   seed.LoadAssertions and use it from firstboot
 - snapstate: add comment to checkVersion vs strutil.VersionCompare
 - tests: add unit tests for cmd_whoami
 - tests: add debug section to interfaces-contacts-service
 - many: introduce package seed and seedtest
 - interfaces/bluez: enable communication between bluetoothd and
   meshd via dbus
 - cmd/snap: fix snap switch message
 - overlord/snapstate: check channel names on install
 - tests: check snap_daemon user and group on system-usernames-
   illegal test are not created
 - cmd/snap-confine: fix group and permission of .info files
 - gadget: do not error on gadget refreshes with multiple volumes
 - snap: use deterministic paths to find the built deb
 - tests: just build snapd commands on go-build test
 - tests: re-enable mount-ns test on classic
 - tests: rename fuse_support to fuse-support
 - tests: move restore-project-each code to existing function
 - tests: simplify interfaces-account-control test
 - i18n, vendor, packaging: drop github.com/ojii/gettext.go, use
   github.com/snapcore/go-gettext
 - tests: always say 'restore: |'
 - tests: new test to check the output after refreshing/reverting
   core
 - snapstate: validate all system-usernames before creating them
 - tests: fix system version check on listing test for external
   backend
 - tests: add check for snap_daemon user/group
 - tests: don't look for lxcfs in mountinfo
 - tests: adding support for arm devices on ubuntu-core-device-reg
   test
 - snap: explicitly forbid trying to parallel install from seed
 - tests: remove trailing spaces from shell scripts
 - tests: remove locally installed revisions of core
 - tests: fix removal of snaps on ubuntu-core
 - interfaces: support Tegra display drivers
 - tests: move interfaces-contacts-service to /tmp
 - interfaces/network-manager: allow using
   org.freedesktop.DBus.ObjectManager
 - tests: restore dpkg selections after upgrade-from-2.15 test
 - tests: pass --remove to userdel on core
 - snap/naming: simplify SnapSet somewhat
 - devicestate/firstboot: check for missing bases early
 - httputil: rework protocol error detection
 - tests: unmount fuse connections only if not initially mounted
 - snap: prevent duplicated snap name and snap files when parsing
   seed.yaml
 - tests: re-implement user tool in python
 - image: improve/tweak some warning/error messages
 - cmd/libsnap-confine-private: add checks for parallel instances
   feature flag
 - tests: wait_for_service shows status after actual first minute
 - sanity: report proper errror when fuse is needed but not available
 - snap/naming: introduce SnapRef, Snap, and SnapSet
 - image: support prepare-image --classic for snapd snap only
   imagesConsequently:
 - tests/main/mount-ns: account for clone_children in cpuset cgroup
   on 18.04
 - many:  merging asserts.Batch Precheck with CommitTo and other
   clarifications
 - devicestate: add missing test for remodeling possibly removing
   required flag
 - tests: use user-tool to remove test user in the non-home test
 - overlord/configstate: sort patch keys to have deterministic order
   with snap set
 - many: generalize assertstate.Batch to asserts.Batch, have
   assertstate.AddBatch
 - gadget, overlord/devicestate: rename Position/Layout
 - store, image, cmd: make 'snap download' leave partials
 - httputil: improve http2 PROTOCOL_ERROR detection
 - tests: add new "user-tool" helper and use in system-user tests
 - tests: clean up after NFS tests
 - ifacestate: optimize auto-connect by setting profiles once after
   all connects
 - hookstate/ctlcmd: snapctl unset command
 - tests: allow test user XDG_RUNTIME_DIR to phase out
 - tests: cleanup "snap_daemon" user in system-usernames-install-
   twice
 - cmd/snap-mgmt: set +x on startup
 - interfaces/wayland,x11: allow reading an Xwayland Xauth file
 - many: move channel parsing to snap/channel
 - check-pr-title.py: allow {} in pr prefix
 - tests: spam test logs less while waiting for systemd unit to stop
 - tests: remove redundant activation check for snapd.socket
   snapd.service
 - tests: trivial snapctl test cleanup
 - tests: ubuntu 18.10 removed from the google-sru backend on the
   spread.yaml
 - tests: add new cases into arch_test
 - tests: clean user and group for test system-usernames-install-
   twice
 - interfaces: k8s worker node updates
 - asserts: move Model to its own model.go
 - tests: unmount binfmt_misc on cleanup
 - tests: restore nsdelegate clobbered by LXD
 - cmd/snap: fix snap unset help string
 - tests: unmount fusectl after testing
 - cmd/snap: fix remote snap info for parallel installed snaps

* Fri Aug 30 2019 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.41
 - overlord/snapstate: revert track-risk behavior
 - tests: fix snap info test
 - httputil: rework protocol error detection
 - gadget: do not error on gadget refreshes with multiple volumes
 - i18n, vendor, packaging: drop github.com/ojii/gettext.go, use
   github.com/snapcore/go-gettext
 - snapstate: validate all system-usernames before creating them
 - mkversion.sh: fix version from git checkouts
 - interfaces/network-{control,manager}: allow 'k' on
   /run/resolvconf/**
 - interfaces/wayland,x11: allow reading an Xwayland Xauth file
 - interfaces: k8s worker node updates
 - debian: re-enable systemd environment generator
 - many: create system-usernames user/group if both don't exist
 - packaging: fix symlink for snapd.session-agent.socket
 - tests: change cgroups so that LXD doesn't have to
 - interfaces/network-setup-control: allow dbus netplan apply
   messages
 - tests: add /var/cache/snapd to the snapd state to prevent error on
   the store
 - tests: add test for services disabled during refresh hook
 - many: simpler access to snap-seccomp version-info
 - snap: cleanup some tests, clarify some errorsThis is a follow up
   from work on system usernames:
 - osutil: add osutil.Find{Uid,Gid}
 - tests: use a different archive based on the spread backend on go-
   build test
 - cmd/snap-update-ns: fix pair of bugs affecting refresh of snap
   with layouts
 - overlord/devicestate: detect clashing concurrent (ongoing, just
   finished) remodels or changes
 - interfaces/docker-support: declare controls-device-cgroup
 - packaging: fix removal of old apparmor profile
 - store: use track/risk for "channel" name when parsing store
   details
 - many: allow 'system-usernames' with libseccomp > 2.4 and golang-
   seccomp > 0.9.0
 - overlord/devicestate, tests: use gadget.Update() proper, spread
   test
 - overlord/configstate/configcore: allow setting start_x=1 to enable
   CSI camera on RPi
 - interfaces: remove BeforePrepareSlot from commonInterface
 - many: support system-usernames for 'snap_daemon' user
 - overlord/devicestate,o/snapstate: queue service commands before
   mark-seeded and other final tasks
 - interfaces/mount: discard mount ns on backend Remove
 - packaging/fedora: build on RHEL8
 - overlord/devicestate: support seeding a classic system with the
   snapd snap and no core
 - interfaces: fix test failure in gpio_control_test
 - interfaces, policy: remove sanitize helpers and use minimal policy
   check
 - packaging: use %systemd_user_* macros to enable session agent
   socket according to presets
 - snapstate, store: handle 429s on catalog refresh a little bit
   better
 - tests: part4 making tests work on ubuntu-core-18
 - many: drop snap.ReadGadgetInfo wrapper
 - xdgopenproxy: update test API to match upstream
 - tests: show why sbuild failed
 - data/selinux: allow mandb_t to search /var/lib/snapd
 - tests: be less verbose when checking service status
 - tests: set sbuild test as manual
 - overlord: DeviceCtx must find the remodel context for a remodel
   change
 - tests: use snap info --verbose to check for base
 - sanity: unmount squashfs with --lazy
 - overlord/snapstate: keep current track if only risk is specified
 - interfaces/firewall-control: support nft routing expressions and
   device groups
 - gadget: support for writing symlinks
 - tests: mountinfo-tool fail if there are no matches
 - tests: sync journal log before start the test
 - cmd/snap, data/completion: improve completion for 'snap debug'
 - httputil: retry for http2 PROTOCOL_ERROR
 - Errata commit: pulseaudio still auto-connects on classic
 - interfaces/misc: updates for k8s 1.15 (and greengrass test)
 - tests: set GOTRACEBACK=1 when running tests
 - cmd/libsnap: don't leak memory in sc_die_on_error
 - tests: improve how the system is restored when the upgrade-
   from-2.15 test fails
 - interfaces/bluetooth-control: add udev rules for BT_chrdev devices
 - interfaces: add audio-playback/audio-record and make pulseaudio
   manually connect
 - tests: split the sbuild test in 2 depending on the type of build
 - interfaces: add an interface granting access to AppStream metadata
 - gadget: ensure filesystem labels are unique
 - usersession/agent: use background context when stopping the agent
 - HACKING.md: update spread section, other updates
 - data/selinux: allow snap-confine to read entries on nsfs
 - tests: respect SPREAD_DEBUG_EACH on the main suite
 - packaging/debian-sid: set GOCACHE to a known writable location
 - interfaces: add gpio-control interface
 - cmd/snap: use showDone helper with 'snap switch'
 - gadget: effective structure role fallback, extra tests
 - many: fix unit tests getting stuck
 - tests: remove installed snap on restore
 - daemon: do not modify test data in user suite
 - data/selinux: allow read on sysfs
 - packaging/debian: don't md5sum absent files
 - tests: remove test-snapd-curl
 - tests: remove test-snapd-snapctl-core18 in restore
 - tests: remove installed snap in the restore section
 - tests: remove installed test snap
 - tests: correctly escape mount unit path
 - cmd/Makefile.am: support building with the go snap
 - tests: work around classic snap affecting the host
 - tests: fix typo "current"
 - overlord/assertstate: add Batch.Precheck to check for the full
   validity of the batch before Commit
 - tests: restore cpuset clone_children clobbered by lxd
 - usersession: move userd package to usersession/userd
 - tests: reformat and fix markdown in snapd-state.md
 - gadget: select the right updater for given structure
 - tests: show stderr only if it exists
 - sessionagent: add a REST interface with socket activation
 - tests: remove locally installed core in more tests
 - tests: remove local revision of core
 - packaging/debian-sid: use correct apparmor Depends for Debian
 - packaging/debian-sid: merge debian upload changes back into master
 - cmd/snap-repair: make sure the goroutine doesn't stick around on
   timeout
 - packaging/fedora: github.com/cheggaaa/pb is no longer used
 - configstate/config: fix crash in purgeNulls
 - boot, o/snapst, o/devicest: limit knowledge of boot vars to boot
 - client,cmd/snap: stop depending on status/status-code in the JSON
   responses in client
 - tests: unmount leftover /run/netns
 - tests: switch mount-ns test to manual
 - overlord,daemon,cmd/snapd:  move expensive startup to dedicated
   StartUp methods
 - osutil: add EnsureTreeState helper
 - tests: measure properties of various  mount namespaces
 - tests: part2 making tests work on ubuntu-core-18
 - interfaces/policy: minimal policy check for replacing
   sanitizeReservedFor helpers (1/2)
 - interfaces: add an interface that grants access to the PackageKit
   service
 - overlord/devicestate: update gadget update handlers and mocks
 - tests: add mountinfo-tool --ref-x1000
 - tests: remove lxd / lxcfs if pre-installed
 - tests: removing support for ubuntu cosmic on spread test suite
 - tests: don't leak /run/netns mount
 - image: clean up the validateSuite
 - bootloader: remove "Dir()" from Bootloader interface
 - many: retry to reboot if snapd gets restarted before expected
   reboot
 - overlord: implement re-registration remodeling
 - cmd: revert PR#6933 (tweak of GOMAXPROCS)
 - cmd/snap: add snap unset command
 - many: add Client-User-Agent to "SnapAction" install API call
 - tests: first part making tests run on ubuntu-core-18
 - hookstate/ctlcmd: support hidden commands in snapctl
 - many: replace snapd snap name checks with type checks (3/4)
 - overlord: mostly stop needing Kernel/CoreInfo, make GadgetInfo
   consider a DeviceContext
 - snapctl: handle unsetting of config options with "!"
 - tests: move core migration snaps to tests/lib/snaps dir
 - cmd/snap: handle unsetting of config options with "!"
 - cmd/snap, etc: add health to 'snap list' and 'snap info'
 - gadget: use struct field names when intializing data in mounted
   updater unit tests
 - cmd/snap-confine: bring /lib/firmware from the host
 - snap: set snapd snap type (1/4)
 - snap: add checks in validate-seed for missing base/default-
   provider
 - daemon: replace shutdownServer with net/http's native shutdown
   support
 - interfaces/builtin: add exec "/bin/runc" to docker-support
 - gadget: mounted filesystem updater
 - overlord/patch: simplify conditions for re-applying sublevel
   patches for level 6
 - seccomp/compiler: adjust test case names and comment for later
   changes
 - tests: fix error doing snap pack running failover test
 - tests: don't preserve size= when rewriting mount tables
 - tests: allow reordering of rewrite operations
 - gadget: main update routine
 - overlord/config: normalize nulls to support config unsetting
   semantics
 - snap-userd-autostart: don't list as a startup application on the
   GUI
 - tests: renumber snap revisions as seen via writable
 - tests: change allocation for mount options
 - tests: re-enable ns-re-associate test
 - tests: mountinfo-tool allow many --refs
 - overlord/devicestate: implement reregRemodelContext with the
   essential re-registration logic
 - tests: replace various numeric mount options
 - gadget: filesystem image writer
 - tests: add more unit tests for mountinfo-tool
 - tests: introduce mountinfo-tool --ref feature
 - tests: refactor mountinfo-tool rewrite state
 - tests: allow renumbering mount namespace identifiers
 - snap: refactor and explain layout blacklisting
 - tests: renumber snap revisions as seen via hostfs
 - daemon, interfaces, travis: workaround build ID with Go 1.9, use
   1.9 for travis tests
 - cmd/libsnap: add sc_error_init_{simple,api_misuse}
 - gadget: make raw updater handle shifted structures
 - tests/lib/nested: create WORK_DIR before accessing it
 - cmd/libsnap: rename SC_LIBSNAP_ERROR to SC_LIBSNAP_DOMAIN
 - cmd,tests: forcibly discard mount namespace when bases change
 - many: introduce healthstate, run check-health
   post-(install/refresh/try/revert)
 - interfaces/optical-drive: add scsi-generic type 4 and 5 support
 - cmd/snap-confine: exit from helper when parent dies

* Fri Jul 12 2019 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.40
 - overlord/patch: simplify conditions for re-applying sublevel
   patches for level 6
 - cmd,tests: forcibly discard mount namespace when bases change
 - cmd/snap-confine: handle device cgroup before pivot
 - cmd/snap-apparmor-service: quit if there are no profiles
 - cmd/snap, image: add --target-directory and --basename to 'snap
   download'
 - interfaces: add jack1 implicit classic interface
 - interfaces: miscellaneous policy updates
 - daemon: classic confinement is not supported on core
 - interfaces: bluetooth-control: add mtk BT device node
 - cmd/snap-seccomp: initial support for negative arguments with
   uid/gid caching
 - snap-confine: move seccomp load after permanent privilege drop
 - tests: new profiler snap used to track cpu and memory for snapd
   and snap commands
 - debian: make maintainer scripts do nothing on powerpc
 - gadget: mounted filesystem writer
 - cmd/snap: use padded checkers for snapshot output
 - bootloader: switch to bootloader_test style testing
 - gadget: add a wrapper for generating partitioned images with
   sfdisk
 - tests/main/snap-seccomp-syscalls: add description
 - tests: continue executing on errors either updating the repo db or
   installing dependencies
 - cmd/snap-seccomp/syscalls: add io_uring syscalls
 - systemd: add InstanceMode enumeration to control which systemd
   instance to control
 - netutil: extract socket activation helpers from daemon package.
 - interfaces: spi: update regex rules to accept spi nodes like
   spidev12345.0
 - gadget: fallback device lookup
 - many: add strutil.ElliptLeft, use it for shortening cohorts
 - wrappers: allow sockets under $XDG_RUNTIME_DIR
 - gadget: add wrapper for creating and populating filesystems
 - gadget: add writer for offset-write
 - gadget: support relative symlinks in device lookup
 - snap, snapstate: additional validation of base field
 - many: fix some races and missing locking, make sure UDevMonitor is
   stopped
 - boot: move ExtractKernelAssets
 - daemon, snap: screenshots _only_ shows the deprecation notice,
   from 2.39
 - osutil: add a workaround for overlayfs apparmor as it is used on
   Manjaro
 - snap: introduce GetType() function for snap.Info
 - tests: update systems to be used for during sru validation
 - daemon: increase `shutdownTimeout` to 25s to deal with slow
   HW
 - interfaces/network-manager: move deny ptrace to the connected slot
 - interfaces: allow locking of pppd files
 - cmd/snap-exec: fix snap completion for classic snaps with non
   /usr/lib/snapd libexecdir
 - daemon: expose pprof endpoints
 - travis: disable snap pack on OSX
 - client, cmd/snap: expose the new cohort options for snap ops
 - overlord/snapstate: tweak switch summaries
 - tests: reuse the image created initially for nested tests
   execution
 - tests/lib/nested: tweak assert disk prepare step
 - daemon, overlord/snapstate: support leave-cohort
 - tests/main/appstream-id: collect debug info
 - store,daemon: add client-user-agent support to store.SnapInfo
 - tests: add check for invalid PR titles in the static checks
 - tests: add snap-tool for easier access to internal tools
 - daemon: unexport file{Response,Stream}
 - devicestate: make TestUpdateGadgetOnClassicErrorsOut less racy
 - tests: fix test desktop-portal-filechooser
 - tests: sort commands from DumpCommands in the dumpDbHook
 - cmd/snap: add unit test for "advise-snap --dump-db".
 - bootloader: remove extra mock bootloader implementation
 - daemon: tweak for "add api endpoint for download" PR
 - packaging: fix reproducible build error
 - tests: synchronize journal logs before check logs
 - tests: fix snap service watchdog test
 - tests: use more readable test directory names
 - tests/regression/lp-1805485: update test description
 - overlord: make changes conflict with remodel
 - tests: make sure the snapshot unit test uses a snapshot time
   relative to Now()
 - tests: revert "tests: stop catalog-update/apt-hooks test for now"
 - tests: mountinfo-tool --one prints matches on failure
 - data/selinux: fix policy for snaps with bases and classic snaps
 - debian: fix building on eoan by tweaking golang build-deps
 - packaging/debian-sid: update required golang version to 1.10
 - httputil: handle "no such host" error explicitly and do not retry
   it
 - overlord/snapstate, & fallout: give Install a *RevisionOptions
 - cmd/snap: don't run install on 'snap --help install'
 - gadget: raw/bare structure writer and updater
 - daemon, client, cmd/snap: show cohort key in snap info --verbose
 - overlord/snapstate: add update-gadget task when needed, block
   other changes
 - image: turn a missing default content provider into an error
 - overlord/devicestate: update-gadget-assets task handler with
   stubbed gadget callbacks
 - interface: builtin: avahi-observe/control: update label for
   implicit slot
 - tests/lib/nested: fix multi argument copy_remote
 - tests/lib/nested: have mkfs.ext4 use a rootdir instead of mounting
   an image
 - packaging: fix permissions powerpc docs dir
 - overlord: mock store to avoid net requests
 - debian: rework how we run autopkgtests
 - interface: builtin: avahi-observe/control: allow slots
   implementation also by app snap on classic system
 - interfaces: builtin: utils: add helper function to identify system
   slots
 - interfaces: add missing adjtimex to time-control
 - overlord/snapstate, snap: support base = "none"
 - daemon, overlord/snapstate: give RevisionOptions a CohortKey
 - data/selinux: permit init_t to remount snappy_snap_t
 - cmd/snap: test for a friendly error on 'okay' without 'warnings'
 - cmd/snap: support snap debug timings --startup=.. and measure
   loadState time
 - advise-snap: add --dump-db which dumps the command database
 - interfaces/docker-support: support overlayfs on ubuntu core
 - cmd/okay: Remove err message when warning file not exist
 - devicestate: disallow removal of snaps used in booting early
 - packaging: fix build-depends on powerpc
 - tests: run spread tests on opensuse leap 15.1
 - strutil/shlex: fix ineffassign
 - cmd/snapd: ensure GOMAXPROCS is at least 2
 - cmd/snap-update-ns: detach unused mount points
 - gadget: record gadget root directory used during positioning
 - tests: force removal to prevent restore fails when directory
   doesn't exist on lp-1801955 test
 - overlord: implement store switch remodeling
 - tests: stop using ! for naive negation in shell scripts
 - snap,store,daemon,client: send new "Snap-Client-User-Agent" header
   in Search()
 - osutil: now that we require golang-1.10, use user.LookupGroup()
 - spread.yaml,tests: change MATCH and REBOOT to cmds
 - packaging/fedora: force external linker to ensure static linking
   and -extldflags use
 - timings: tweak the conditional for ensure timings
 - timings: always store ensure timings as long as they have an
   associated change
 - cmd/snap: tweak the output of snap debug timings --ensure=...
 - overlord/devicestate: introduce remodel kinds and
   contextsregistrationContext:
 - snaptest: add helper for mocking snap with contents
 - snapstate: allow removal of non-model kernels
 - tests: change strace parameters on snap-run test to avoid the test
   gets stuck
 - gadget: keep track of the index where structure content was
   defined
 - cmd/snap-update-ns: rename leftover ctx to upCtx
 - tests: add "not" command
 - spread.yaml: use "snap connections" in debug
 - tests: fix how strings are matched on auto-refresh-retry test
 - spread-shellcheck: add support for variants and environment
 - gadget: helper for shifting structure start position
 - cmd/snap-update-ns: add several TODO comments
 - cmd/snap-update-ns: rename ctx to upCtx
 - spread.yaml: make HOST: usage shellcheck-clean
 - overlord/snapstate, daemon: snapstate.Switch now takes a
   RevisionOption
 - tests: add mountinfo-tool
 - many: make snapstate.Update take *RevisionOptions instead of chan,
   rev
 - tests/unit/spread-shellcheck: temporary workaround for SC2251
 - daemon: refactor user ops to api_users
 - cmd/snap, tests: refactor info to unify handling of 'direct' snaps
 - cmd/snap-confine: combine sc_make_slave_mount_ns into caller
 - cmd/snap-update-ns: use "none" for propagation changes
 - cmd/snap-confine: don't pass MS_SLAVE along with MS_BIND
 - cmd/snap, api, snapstate: implement "snap remove --purge"
 - tests: new hotplug test executed on ubuntu core
 - tests: running tests on fedora 30
 - gadget: offset-write: fix validation, calculate absolute position
 - data/selinux: allow snap-confine to do search on snappy_var_t
   directories
 - daemon, o/snapstate, store: support for installing from cohorts
 - cmd/snap-confine: do not mount over non files/directories
 - tests: validates snapd from ppa
 - overlord/configstate: don't panic on invalid configuration
 - gadget: improve device lookup, add helper for mount point lookup
 - cmd/snap-update-ns: add tests for executeMountProfileUpdate
 - overlord/hookstate: don't run handler unless hooksup.Always
 - cmd/snap-update-ns: allow changing mount propagation
 - systemd: workaround systemctl show quirks on older systemd
   versions
 - cmd/snap: allow option descriptions to start with the command
 - many: introduce a gadget helper for locating device matching given
   structure
 - cmd/snap-update-ns: fix golint complaints about variable names
 - cmd/snap: unit tests for debug timings
 - testutil: support sharing-related mount flags
 - packaging/fedora: Merge changes from Fedora Dist-Git and drop EOL
   Fedora releases
 - cmd/snap: support for --ensure argument for snap debug timings
 - cmd,sandbox: tweak seccomp version info handling
 - gadget: record sector size in positioned volume
 - tests: make create-user test support managed devices
 - packaging: build empty package on powerpc
 - overlord/snapstate: perform hard refresh check
 - gadget: add volume level update checks
 - cmd/snap: mangle descriptions that have indent > terminal width
 - cmd/snap-update-ns: rename applyFstab to executeMountProfileUpdate
 - cmd/snap-confine: unshare per-user mount ns once
 - tests: retry govendor sync
 - tests: avoid removing snaps which are cached to speed up the
   prepare on boards
 - tests: fix how the base snap are deleted when there are multiple
   to deleted on reset
 - cmd/snap-update-ns: merge apply functions
 - many: introduce assertstest.SigningAccounts and AddMany test
   helpers
 - interfaces: special-case "snapd" in sanitizeSlotReservedForOS*
   helpers
 - cmd/snap-update-ns: make apply{User,System}Fstab identical
 - gadget: introduce checkers for sanitizing structure updates
 - cmd/snap-update-ns: move apply{Profile,{User,System}Fstab} to same
   file
 - overlord/devicestate: introduce registrationContext
 - cmd/snap-update-ns: add no-op load/save current user profile logic
 - devicestate: set "new-model" on the remodel change
 - devicestate: use deviceCtx in checkGadgetOrKernel
 - many: use a fake assertion model in the device contexts for tests
 - gadget: fix handling of positioning constrains for structures of
   MBR role
 - snap-confine: improve error when running on a not /home homedir
 - devicestate: make Remodel() return a state.Change
 - many: make which store to use contextualThis reworks
   snapstate.Store instead of relying solely on DeviceContext,
   because:
 - tests: enable tests on centos 7 again
 - interfaces: add login-session-control interface
 - tests: extra debug for snapshot-basic test
 - overlord,overlord/devicestate: do without GadgetInfo/KernelInfo in
   devicestate
 - gadget: more validation checks for legacy MBR structure type &
   role
 - osutil: fix TestReadBuildGo test in sbuild
 - data: update XDG_DATA_DIRS via the systemd environment.d mechanism
   too
 - many: do without device state/assertions accessors based on state
   only outside of devicestate/tests
 - interfaces/dbus: fix unit tests when default snap mount dir is not
   /snap
 - tests: add security-seccomp to verify seccomp with arg filtering
 - snapshotstate: disable automatic snapshots on core for now
 - snapstate: auto-install snapd when needed
 - overlord/ifacestate: update static attributes of "content"
   interface
 - interfaces: add support for the snapd snap in the dbus backend*
 - overlord/snapstate: tweak autorefresh logic if network is not
   available
 - snapcraft: also include ld.so.conf from libc in the snapcraft.yml
 - snapcraft.yaml: fix links ld-linux-x86-64.so.2/ld64.so.2
 - overlord: pass a DeviceContext to the checkSnap implementations
 - daemon: add RootOnly flag to commands
 - many:  make access to the device model assertion etc contextual
   via a DeviceCtx hook/DeviceContext interface
 - snapcraft.yaml: include libc6 in snapd
 - tests: reduce snapcraft leftovers from PROJECT_PATH,  temp disable
   centos
 - overlord: make the store context composably backed by separate
   backends for device asserts/info etc.
 - snapstate: revert "overlord/snapstate: remove PlugsOnly"
 - osutil,cmdutil: move CommandFromCore and make it use the snapd
   snap (if available)
 - travis: bump Go version to 1.10.x
 - cmd/snap-update-ns: remove instanceName argument from applyProfile
 - gadget: embed volume in positioned volume, rename fields
 - osutil: use go build-id when no gnu build-id is available
 - snap-seccomp: add 4th field to version-info for golang-seccomp
   features
 - cmd/snap-update-ns: merge computeAndSaveSystemChanges into
   applySystemFstab
 - cmd/snap, client, daemon, store: create-cohort
 - tests: give more time until nc returns on appstream test
 - tests: run spread tests on ubuntu 19.04
 - gadget: layout, smaller fixes
 - overlord: update static attrs when reloading connections
 - daemon: verify snap instructions for multi-snap requests
 - overlord/corecfg: make expiration of automatic snapshots
   configurable (4/4)
 - cmd/snap-update-ns: pass MountProfileUpdate to
   apply{System,User}Fstab
 - snap: fix interface bindings on implicit hooks
 - tests: improve how snaps are cached
 - cmd/snap-update-ns: formatting tweaks
 - data/selinux: policy tweaks
 - cmd/snap-update-ns: move locking to the common layer
 - overlord: use private YAML inside several tests
 - cmd/snap, store, image: support for cohorts in "snap download"
 - overlord/snapstate: add timings to critical task handlers and the
   backend
 - cmd: add `snap debug validate-seed <path>` cmd
 - state: add possible error return to TaskSet.Edge()
 - snap-seccomp: use username regex as defined in osutil/user.go
 - osutil: make IsValidUsername public and fix regex
 - store: serialize the acquisition of device sessions
 - interfaces/builtin/desktop: fonconfig v6/v7 cache handling on
   Fedora
 - many: move Device/SetDevice to devicestate, start of making them
   pluggable in storecontext
 - overlord/snapstate: remove PlugsOnly
 - interfaces/apparmor: allow running /usr/bin/od
 - spread: add qemu:fedora-29-64
 - tests: make test parallel-install-interfaces work for boards with
   pre-installed snaps
 - interfaces/builtin/intel_mei: fix /dev/mei* AppArmor pattern
 - spread.yaml: add qemu:centos-7-64
 - overlord/devicestate: extra measurements related to
   populateStateFromSeed
 - cmd/snap-update-ns: move Assumption to {System,User}ProfileUpdate
 - cmd/libsnap: remove fringe error function
 - gadget: add validation of cross structure overlap and offset
   writes
 - cmd/snap-update-ns: refactor of profile application (3/N)
 - data/selinux: tweak the policy for runuser and s-c, interpret
   audit entries
 - tests: fix spaces issue in the base snaps names to remove during
   reset phase
 - tests: wait for man db cache is updated before after install snapd
   on Fedora
 - tests: extend timeout of sbuild test

* Fri Jun 21 2019 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.39.3
  - daemon: increase `shutdownTimeout` to 25s to deal with slow HW
  - spread: run tests against openSUSE 15.1
  - data/selinux: fix policy for snaps with bases and classic snaps

* Wed Jun 05 2019 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.39.2
 - debian: rework how we run autopkgtests
 - interfaces/docker-support: add overlayfs accesses for ubuntu core
 - data/selinux: permit init_t to remount snappy_snap_t
 - strutil/shlex: fix ineffassign
 - packaging: fix build-depends on powerpc

* Wed May 29 2019 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.39.1
 - spread: enable Fedora 30
 - cmd/snap-confine, data/selinux: cherry pick Fedora 30 fixes
 - tests/unit/spread-shellcheck: temporary workaround for SC2251
 - packaging: build empty package on powerpc
 - interfaces: special-case "snapd" in sanitizeSlotReservedForOS*
   helper
 - cmd/snap: mangle descriptions that have indent > terminal width
 - cmd/snap-confine: unshare per-user mount ns once
 - tests: avoid adding spaces to the base snaps names
 - systemd: workaround systemctl show quirks on older systemd
   versions

* Mon May 06 2019 Neal Gompa <ngompa13@gmail.com> - 2.39-1
- Release 2.39 to Fedora (RH#1699087)
- Enable basic SELinux integration
- Fix changelog entry to fix build for EPEL 7
- Exclude bash and POSIX sh shebangs from mangling (LP:1824158)
- Drop some old pre Fedora 28 logic

* Fri May 03 2019 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.39
 - overlord/ifacestate: update static attributes of "content"
   interface
 - data/selinux: tweak the policy for runuser and s-c, interpret
   audit entries
 - snapshotstate: disable automatic snapshots on core for now
 - overlord/corecfg: make expiration of automatic snapshots
   configurable
 - snapstate: auto-install snapd when needed
 - interfaces: add support for the snapd snap in the dbus backend
 - overlord/snapstate: tweak autorefresh logic if network is not
   available
 - interfaces/apparmor: allow running /usr/bin/od
 - osutil,cmdutil: move CommandFromCore and make it use the snapd
   snap (if available)
 - daemon: also verify snap instructions for multi-snap requests
 - data/selinux: allow snap-confine to mount on top of bin
 - data/selinux: auto transition /var/snap to snappy_var_t
 - cmd: add `snap debug validate-seed <path>` cmd
 - interfaces/builtin/desktop: fonconfig v6/v7 cache handling on
   Fedora
 - interfaces/builtin/intel_mei: fix /dev/mei* AppArmor pattern
 - tests: make snap-connections test work on boards with snaps pre-
   installed
 - tests: check for /snap/core16/current in core16-provided-by-core
 - tests: run livepatch test on 18.04 as well
 - devicestate: deal correctly with the "required" flag on Remodel
 - snapstate,state: add TaskSet.AddAllWithEdges() and use in doUpdate
 - snapstate: add new NoReRefresh flag and use in Remodel()
 - many: allow core as a fallback for core16
 - snapcraft: build static fontconfig in the snapd snap
 - cmd/snap-confine: remove unused sc_open_snap_{update,discard}_ns
 - data/selinux: allow snapd to execute runuser under snappy_t
 - spread, tests: do not leave mislabeled files in restorecon test,
   attempt to catch similar files
 - interfaces: cleanup internal tool lookup in system-key
 - many: move auth.AuthContext to store.DeviceAndAuthContext, the
   implemention to a separate storecontext packageThis:
 - overlord/devicestate: measurements around ensure and related tasks
 - cmd: tweak internal tool lookup to accept more possible locations
 - overlord/snapstate,snapshotstate: create snapshot on snap removal
 - tests: run smoke tests on (almost) pristine systems
 - tests: system disable ssh for config defaults in gadget
 - cmd/debug: integrate new task timings with "snap debug timings"
 - tests/upgrade/basic, packaging/fedoar: restore SELinux context of
   /var/cache/fontconfig, patch pre-2.39 mount units
 - image: simplify prefer local logic  and fixes
 - tests/main/selinux-lxd: make sure LXD from snaps works cleanly
   with enforcing SELinux
 - tests: deny ioctl - TIOCSTI with garbage in high bits
 - overlord: factor out mocking of device service and gadget w.
   prepare-device for registration tests
 - data/selinux, tests/main/selinux-clean: fine tune the policy, make
   sure that no denials are raised
 - cmd/libsnap,osutil: fix parsing of mountinfo
 - ubuntu: disable -buildmode=pie on armhf to fix memory issue
 - overlord/snapstate: inhibit refresh for up to a week
 - cmd/snap-confine: prevent cwd restore permission bypass
 - overlord/ifacestate: introduce HotplugKey type use short key in
   change summaries
 - many: make Remodel() download everything first before installing
 - tests: fixes discovered debugging refresh-app-awareness
 - overlord/snapstate: track time of postponed refreshes
 - snap-confine: set rootfs_dir in sc_invocation struct
 - tests: run create-user on core devices
 - boot: add flag file "meta/force-kernel-extraction"
 - tests: add regression test for systemctl race fix
 - overlord/snapshotstate: helpers for snapshot expirations
 - overlord,tests: perform soft refresh check in doInstall
 - tests: enable tests that write /etc/{hostname,timezone} on core18
 - overlord/ifacestate: implement String() method of
   HotplugDeviceInfo for better logs/messages
 - cmd/snap-confine: move ubuntu-core fallback checks
 - testutil: fix MockCmd for shellcheck 0.5
 - snap, gadget: move gadget read/validation into separate package,
   tweak naming
 - tests: split travis spread execution in 2 jobs for ubuntu and non
   ubuntu systems
 - testutil: make mocked command work with shellcheck from snaps
 - packaging/fedora, tests/upgrade/basic: patch existing mount units
   with SELinux context on upgrade
 - metautil, snap: extract yaml value normalization to a helper
   package
 - tests: use apt via eatmydata
 - dirs,overlord/snapstate: add Soft and Hard refresh checks
 - cmd/snap-confine: allow using tools from snapd snap
 - cmd,interfaces: replace local helpers with cmd.InternalToolPath
 - tweak: fix "make hack" on Fedora
 - snap: add validation of gadget.yaml
 - cmd/snap-update-ns: refactor of profile application
 - cmd/snap,client,daemon,store: layout and sanity tweaks for
   find/search options
 - tests: add workaround for missing cache reset on older snapd
 - interfaces: deal with the snapd snap correctly for apparmor 2.13
 - release-tools: add debian-package-builder
 - tests: enable opensuse 15 and add force-resolution installing
   packages
 - timings: AddTag helper
 - testutil: run mocked commands through shellcheck
 - overlord/snapshotstate: support auto flag
 - client, daemon, store: search by common-id
 - tests: all the systems for google backend with 6 workers
 - interfaces: hotplug nested vm test, updated serial-port interface
   for hotplug.
 - sanity: use proper SELinux context when mounting squashfs
 - cmd/libsnap: neuter variables in cleanup functions
 - interfaces/adb-support: account for hubs on sysfs path
 - interfaces/seccomp: regenerate changed profiles only
 - snap: reject layouts to /lib/{firmware,modules}
 - cmd/snap-confine, packaging: support SELinux
 - selinux, systemd: support mount contexts for snap images
 - interfaces/builtin/opengl: allow access to Tegra X1
 - cmd/snap: make 'snap warnings' output yamlish
 - tests: add check to detect a broken snap on reset
 - interfaces: add one-plus devices to adb-support
 - cmd: prevent umask from breaking snap-run chain
 - tests/lib/pkgdb: allow downgrade when installing packages in
   openSUSE
 - cmd/snap-confine: use fixed private tmp directory
 - snap: tweak parsing errors of gadget updates
 - overlord/ifacemgr: basic measurements
 - spread: refresh metadata on openSUSE
 - cmd/snap-confine: pass sc_invocation instead of numerous args
   around
 - snap/gadget: introduce volume update info
 - partition,bootloader: rename 'partition' package to 'bootloader'
 - interfaces/builtin: add dev/pts/ptmx access to docker_support
 - tests: restore sbuild test
 - strutil: make SplitUnit public, allow negative numbers
 - overlord/snapstate,: retry less for auto-stuff
 - interfaces/builtin: add add exec "/" to docker-support
 - cmd/snap: fix regression of snap saved command
 - cmd/libsnap: rename C enum for feature flag
 - cmd: typedef mountinfo structures
 - tests/main/remodel: clean up before reverting the state
 - cmd/snap-confine: umount scratch dir using UMOUNT_NOFOLLOW
 - timings: add new helpers, Measurer interface and DurationThreshold
 - cmd/snap-seccomp: version-info subcommand
 - errortracker: fix panic in Report if db cannot be opened
 - sandbox/seccomp: a helper package wrapping calls to snap-seccomp
 - many: add /v2/model API, `snap remodel` CLI and spread test
 - tests: enable opensuse tumbleweed back
 - overlord/snapstate, store: set a header when auto-refreshing
 - data/selinux, tests: refactor SELinux policy, add minimal tests
 - spread: restore SELinux context when we mess with system files
 - daemon/api: filter connections with hotplug-gone=true
 - daemon: support returning assertion information as JSON with the
   "json" query parameter
 - cmd/snap: hide 'interfaces' command, show deprecation notice
 - timings: base API for recording timings in state
 - cmd/snap-confine: drop unused dependency on libseccomp
 - interfaces/apparmor: factor out test boilerplate
 - daemon: extract assertions api endpoint implementation into
   api_asserts.go
 - spread.yaml: bump delta reference
 - cmd/snap-confine: track per-app and per-hook processes
 - cmd/snap-confine: make sc_args helpers const-correct
 - daemon: move a function that was between an other struct and its
   methods
 - overlord/snapstate: fix restoring of "old-current" revision config
   in undoLinkSnap
 - cmd/snap, client, daemon, ifacestate: show a leading attribute of
   a connection
 - cmd/snap-confine: call sc_should_use_normal_mode once
 - cmd/snap-confine: populate enter_non_classic_execution_environment
 - daemon: allow downloading snaps blobs via .../file
 - cmd/snap-confine: introduce sc_invocation
 - devicestate: add initial Remodel support
 - snap: remove obsolete license-* fields in the yaml
 - cmd/libsnap: add cgroup-pids-support module
 - overlord/snapstate/backend: make LinkSnap clean up more
 - snapstate: only keep 2 snaps on classic
 - ctlcmd/tests: tests tweaks (followup to #6322)

* Tue Apr 23 2019 Robert-André Mauchin <zebob.m@gmail.com> - 2.38-3
- Rebuilt for fix in golang-github-seccomp-libseccomp-golang

* Fri Apr 05 2019 Neal Gompa <ngompa13@gmail.com> - 2.38-2
- Readd snapd-login-service Provides for gnome-software for F29 and older

* Thu Mar 21 2019 Neal Gompa <ngompa13@gmail.com> - 2.38-1
- Release 2.38 to Fedora (RH#1691296)
- Switch to officially released main source tarball
- Drop obsolete snapd-login-service Provides

* Thu Mar 21 2019 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.38
 - overlord/snapstate,: retry less for auto-stuff
 - cmd/snap: fix regression of snap saved command
 - interfaces/builtin: add dev/pts/ptmx access to docker_support
 - overlord/snapstate, store: set a header when auto-refreshing
 - interfaces/builtin: add add exec "/" to docker-support
 - cmd/snap, client, daemon, ifacestate: show a leading attribute of
   a connection
 - interface: avahi-observe: Fixing socket permissions on 4.15
   kernels
 - tests: check that apt works before using it
 - apparmor: support AppArmor 2.13
 - snapstate: restart into the snapd snap on classic
 - overlord/snapstate: during refresh, re-refresh on epoch bump
 - cmd, daemon: split out the common bits of mapLocal and mapRemote
 - cmd/snap-confine: chown private /tmp to root.root
 - cmd/snap-confine: drop uid from random /tmp name
 - overlord/hookstate: apply pending transaction changes onto
   temporary configuration for snapctl get
 - cmd/snap: `snap connections` command
 - interfaces/greengrass_support: update accesses for GGC 1.8
 - cmd/snap, daemon: make the connectivity check use GET
 - interfaces/builtin,/udev: add spec support to disable udev +
   device cgroup and use it for greengrass
 - interfaces/intel-mei: small follow up tweaks
 - ifacestate/tests: fix/improve udev mon test
 - interfaces: add multipass-support interface
 - tests/main/high-user-handling: fix the test for Go 1.12
 - interfaces: add new intel-mei interface
 - systemd: decrease the checker counter before unlocking otherwise
   we can get spurious panics
 - daemon/tests: fix race in the disconnect conflict test
 - cmd/snap-confine: allow moving tasks to pids cgroup
 - tests: enable opensuse tumbleweed on spread
 - cmd/snap: fix `snap services` completion
 - ifacestate/hotplug: integration with udev monitor
 - packaging: build snapctl as a static binary
 - packaging/opensuse: move most logic to snapd.mk
 - overlord: fix ensure before slowness on Retry
 - overlord/ifacestate: fix migration of connections on upgrade from
   ubuntu-core
 - daemon, client, cmd/snap: debug GETs ask aspects, not actions
 - tests/main/desktop-portal-*: fix handling of python dependencies
 - interfaces/wayland: allow wayland server snaps function on classic
   too
 - daemon, client, cmd/snap: snap debug base-declaration
 - tests: run tests on opensuse leap 15.0 instead of 42.3
 - cmd/snap: fix error messages for snapshots commands if ID is not
   uint
 - interfaces/seccomp: increase filter precision
 - interfaces/network-manager: no peer label check for hostname1
 - tests: add a tests for xdg-desktop-portal integration
 - tests: not checking 'tracking channel' after refresh core on
   nested execution
 - tests: remove snapweb from tests
 - snap, wrappers: support StartTimeout
 - wrappers: Add an X-SnapInstanceName field to desktop files
 - cmd/snap: produce better output for help on subcommands
 - tests/main/nfs-support: use archive mode for creating fstab backup
 - many: collect time each task runs and display it with `snap debug
   timings <id>`
 - tests: add attribution to helper script
 - daemon: make ucrednetGet not loop
 - squashfs: unset SOURCE_DATE_EPOCH in the TestBuildDate test
 - features,cmd/libsnap: add new feature "refresh-app-awareness"
 - overlord: fix random typos
 - interfaces/seccomp: generate global seccomp profile
 - daemon/api: fix error case for disconnect conflict
 - overlord/snapstate: add some randomness to the catalog refresh
 - tests: disable trusty-proposed for now
 - tests: fix upgrade-from-2.15 with kernel 4.15
 - interfaces/apparmor: allow sending and receiving signals from
   ourselves
 - tests: split the test interfaces-many in 2 and remove snaps on
   restore
 - tests: use snap which takes 15 seconds to install on retryable-
   error test
 - packaging: avoid race in snapd.postinst
 - overlord/snapstate: discard mount namespace when undoing 1st link
   snap
 - cmd/snap-confine: allow writes to /var/lib/**
 - tests: stop catalog-update test for now
 - tests/main/auto-refresh-private: make sure to actually download
   with the expired macaroon
 - many: save media info when installing, show it when listing
 - userd: handle help urls which requires prepending XDG_DATA_DIRS
 - tests: fix NFS home mocking
 - tests: improve snaps-system-env test
 - tests: pre-cache core on core18 systems
 - interfaces/hotplug: renamed RequestedSlotSpec to ProposedSlot,
   removed Specification
 - debian: ensure leftover usr.lib.snapd.snap-confine is gone
 - image,cmd/snap,tests: introduce support for modern prepare-image
   --snap <snap>[=<channel>]
 - overlord/ifacestate: tweak logic for generating unique slot names
 - packaging: import debian salsa packaging work, add sbuild test and
   use in spead
 - overlord/ifacestate: hotplug-add-slot handler
 - image,cmd/snap:  simplify --classic-arch to --arch, expose
   prepare-image
 - tests: run test snap as user in the smoke test
 - cmd/snap: tweak man output to have no doubled up .TP lines
 - cmd/snap, overlord/snapstate: silently ignore classic flag when a
   snap is strictly confined
 - snap-confine: remove special handling of /var/lib/jenkins
 - cmd/snap-confine: handle death of helper process
 - packaging: disable systemd environment generator on 18.04
 - snap-confine: fix classic snaps for users with /var/lib/* homedirs
 - tests/prepare: prevent console-conf from running
 - image: bootstrapToRootDir => setupSeed
 - image,cmd/snap,tests:  introduce prepare-image --classic
 - tests: update smoke/sandbox test for armhf
 - client, daemon: introduce helper for querying snapd API for the
   list of slot/plug connections
 - cmd/snap-confine: refactor and cleanup of seccomp loading
 - snapstate, snap: allow update/switch requests with risk only
   channel to DTRT
 - interfaces: add network-manager-observe interface
 - snap-confine: increase locking timeout to 30s
 - snap-confine: fix incorrect "sanity timeout 3s" message
 - snap-confine: provide proper error message on sc_sanity_timeout
 - snapd,state: improve error message on state reading failure
 - interfaces/apparmor: deny inet/inet6 in snap-update-ns profile
 - snap: fix reexec from the snapd snap for classic snaps
 - snap: fix hook autodiscovery for parallel installed snaps
 - overlord/snapstate: format the refresh time for the log
 - cmd/snap-confine: add special case for Jenkins
 - snapcraft.yaml: fix XBuildDeb PATH for go-1.10
 - overlord/snapstate: validate instance names early
 - overlord/ifacestate: handler for hotplug-update-slot tasks
 - polkit: cast pid to uint32 to keep polkit happy for now
 - snap/naming: move various name validation helpers to separate
   package
 - tests: iterate getting journal logs to support delay on boards on
   daemon-notify test
 - cmd/snap: fix typo in cmd_wait.go
 - snap/channel: improve channel parsing
 - daemon, polkit: pid_t is signed
 - daemon: introduce /v2/connections snapd API endpoint
 - cmd/snap: small refactor of cmd_info's channel handling
 - overlord/snapstate: use an ad-hoc error when no results
 - cmd/snap: wrap "summary" better
 - tests: workaround missing go dependencies in debian-9
 - daemon: try to tidy up the icon stuff a little
 - interfaces: add display-control interface
 - snapcraft.yaml: fix snap building in launchpad
 - tests: update fedora 29 workers to speed up the whole testing time
 - interfaces: add u2f-devices interface and allow reading udev
   +power_supply:* in hardware-observe
 - cmd/snap-update-ns: save errno from strtoul
 - tests: interfaces tests normalization
 - many: cleanup golang.org/x/net/context
 - tests: add spread test for system dbus interface
 - tests: remove -o pipefail
 - interfaces: add block-devices interface
 - spread: enable upgrade suite on fedora
 - tests/main/searching: video section got renamed to photo-and-video
 - interfaces/home: use dac_read_search instead of dac_override with
   'read: all'
 - snap: really run the RunSuite
 - interfaces/camera: allow reading vendor/etc info from
   /run/udev/data/+usb:*
 - interfaces/dbus: be less strict about alternations for well-known
   names
 - interfaces/home: allow dac_override with 'read:
   all'
 - interfaces/pulseaudio: allow reading subdirectories of
   /etc/pulse
 - interfaces/system-observe: allow read on
   /proc/locks
 - run-checks: ensure we use go-1.10 if available
 - tests: get test-snapd-dbus-{provider,consumer} from the beta
   channel
 - interfaces/apparmor: mock presence of overlayfs root
 - spread: increase default kill-timeout to 30min
 - tests: simplify interfaces-contacts-service test
 - packaging/ubuntu: build with golang 1.10
 - ifacestate/tests: extra test for hotplug-connect handler
 - packaging: make sure that /var/lib/snapd/lib/glvnd is accounted
   for
 - overlord/snapstate/backend: call fontconfig helpers from the new
   'current'
 - kvm: load required kernel modules if necessary
 - cmd/snap: use a fake user for 'run' tests
 - tests: update systems for google sru backend
 - tests: fix install-snaps test by changing the snap info regex
 - interfaces: helpers for sorting plug/slot/connection refs
 - tests: moving core-snap-refresh-on-core test from main to nested
   suite
 - tests: fix daemon-notify test checking denials considering all the
   log lines
 - tests: skip lp-1802591 on "official" images
 - tests: fix listing tests to match "snap list --unicode=never"
 - debian: fix silly typo in the spread test invocation
 - interface: raw-usb: Adding ttyACM ttyACA permissions
 - tests: fix enable-disable-unit-gpio test on external boards
 - overlord/ifacestate: helper API to obtain the state of connections
 - tests: define new "tests/smoke" suite and use that for
   autopkgtests
 - cmd/snap-update-ns: explicitly check for return value from
   parse_arg_u
 - interfaces/builtin/opengl: allow access to NVIDIA VDPAU library
 - tests: auto-clean the test directory
 - cmd/snap: further tweak messaging; add a test
 - overlord/ifacestate: handler for hotplug-connect task
 - cmd/snap-confine: join freezer only after setting up user mount
 - cmd/snap-confine: don't preemptively create .mnt files
 - cmd/snap-update-ns: manually implement isspace
 - cmd/snap-update-ns: let the go parser know we are parsing -u
 - cmd/snap-discard-ns: fix name of user fstab files
 - snapshotstate: don't task.Log without the lock
 - tests: exclude some more slow tests from runs in autopkgtest
 - many: remove .user-fstab files from /run/snapd/ns
 - cmd/libsnap: pass --from-snap-confine when calling snap-update-ns
   as user
 - cmd/snap-update-ns: make freezer mockable
 - cmd/snap-update-ns: move XDG code to dedicated file
 - osutil: add helper for loading fstab from string
 - cmd/snap-update-ns: move existing code around, renaming some
   functions
 - overlord/configstate/configcore: support - and _ in cloud init
   field names
 - * cmd/snap-confine: use makedev instead of MKDEV
 - tests: review/fix the autopkgtest failures in disco
 - overlord: drop old v1 store api support from managers test
 - tests: new test for snapshots with more than 1 user

* Thu Feb 28 2019 Neal Gompa <ngompa13@gmail.com> - 2.37.4-2
- Fix accidentally corrupted changelog merge

* Thu Feb 28 2019 Zygmunt Bazyli Krynicki <me@zygoon.pl> - 2.37.4-1
- Release 2.37.4 to Fedora (RH#1683795)
- Fix RPM macro in changelog (rpmlint)
- Fix non-break space in changelog (rpmlint)

* Wed Feb 27 2019 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.37.4
 - squashfs: unset SOURCE_DATE_EPOCH in the TestBuildDate test
 - overlord/ifacestate: fix migration of connections on upgrade from
   ubuntu-core
 - tests: fix upgrade-from-2.15 with kernel 4.15
 - interfaces/seccomp: increase filter precision
 - tests: remove snapweb from tests

* Tue Feb 19 2019 Zygmunt Bazyli Krynicki <me@zygoon.pl> - 2.37.3-1
- Release 2.37.3 to Fedora (RH#1678603)

* Mon Feb 18 2019 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.37.3
 - interfaces/seccomp: generate global seccomp profile
 - overlord/snapstate: add some randomness to the catalog refresh
 - tests: add upgrade test from 2.15.2ubuntu1 -> current snapd
 - snap-confine: fix fallback to ubuntu-core
 - packaging: avoid race in snapd.postinst
 - overlord/snapstate: discard mount namespace when undoing 1st link
   snap
 - cmd/snap-confine: allow writes to /var/lib/** again
 - tests: stop catalog-update/apt-hooks test until the catlog refresh
   is randomized
 - debian: ensure leftover usr.lib.snapd.snap-confine is gone

* Wed Feb 06 2019 Neal Gompa <ngompa13@gmail.com> - 2.37.2-1
- Release 2.37.2 to Fedora (RH#1667460)

* Wed Feb 06 2019 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.37.2
 - cmd/snap, overlord/snapstate: silently ignore classic flag when a
   snap is strictly confined
 - snap-confine: remove special handling of /var/lib/jenkins
 - cmd/snap-confine: handle death of helper process gracefully
 - snap-confine: fix classic snaps for users with /var/lib/* homedirs
   like jenkins/postgres
 - packaging: disable systemd environment generator on 18.04
 - tests: update smoke/sandbox test for armhf
 - cmd/snap-confine: refactor and cleanup of seccomp loading
 - snap-confine: increase locking timeout to 30s
 - snap-confine: fix incorrect "sanity timeout 3s" message
 - snap: fix hook autodiscovery for parallel installed snaps
 - tests: iterate getting journal logs to support delay on boards on
   daemon-notify test
 - interfaces/apparmor: deny inet/inet6 in snap-update-ns profile
 - interfaces: add u2f-devices interface

* Sun Feb 03 2019 Fedora Release Engineering <releng@fedoraproject.org> - 2.36.3-2
- Rebuilt for https://fedoraproject.org/wiki/Fedora_30_Mass_Rebuild

* Tue Jan 29 2019 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.37.1
 - cmd/snap-confine: add special case for Jenkins
 - tests: workaround missing go dependencies in debian-9
 - daemon, polkit: pid_t is signed
 - interfaces: add display-control interface
 - interfaces: add block-devices interface
 - tests/main/searching: video section got renamed to photo-and-video
 - interfaces/camera: allow reading vendor/etc info from
   /run/udev/data/+usb
 - interfaces/dbus: be less strict about alternations for well-known
   names
 - interfaces/home: allow dac_read_search with 'read: all'
 - interfaces/pulseaudio: allow reading subdirectories of
   /etc/pulse
 - interfaces/system-observe: allow read on
   /proc/locks
 - tests: get test-snapd-dbus-{provider,consumer} from the beta
   channel
 - interfaces/apparmor: mock presence of overlayfs root
 - packaging/{fedora,opensuse,ubuntu}: add /var/lib/snapd/lib/glvnd

* Wed Jan 16 2019 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.37
 - snapd: fix race in TestSanityFailGoesIntoDegradedMode test
 - cmd: fix snap-device-helper to deal correctly with hooks
 - tests: various fixes for external backend
 - interface: raw-usb: Adding ttyACM[0-9]* as many serial devices
   have device node /dev/ttyACM[0-9]
 - tests: fix enable-disable-unit-gpio test on external boards
 - tests: define new "tests/smoke" suite and use that for
   autopkgtests
 - interfaces/builtin/opengl: allow access to NVIDIA VDPAU
   library
 - snapshotstate: don't task.Log without the lock
 - overlord/configstate/configcore: support - and _ in cloud init
   field names
 - cmd/snap-confine: use makedev instead of MKDEV
 - tests: review/fix the autopkgtest failures in disco
 - systemd: allow only a single daemon-reload at the same time
 - cmd/snap: only auto-enable unicode to a tty
 - cmd/snap: right-align revision and size in info's channel map
 - dirs, interfaces/builtin/desktop: system fontconfig cache path is
   different on Fedora
 - tests: fix "No space left on device" issue on amazon-linux
 - store: undo workaround for timezone-less released-at
 - store, snap, cmd/snap: channels have released-at
 - snap-confine: fix incorrect use "src" var in mount-support.c
 - release: support probing SELinux state
 - release-tools: display self-help
 - interface: add new `{personal,system}-files` interface
 - snap: give Epoch an Equal method
 - many: remove unused interface code
 - interfaces/many: use 'unsafe' with docker-support change_profile
   rules
 - run-checks: stop running HEAD of staticcheck
 - release: use sync.Once around lazy intialized state
 - overlord/ifacestate: include interface name in the hotplug-
   disconnect task summary
 - spread: show free space in debug output
 - cmd/snap: attempt to restore SELinux context of snap user
   directories
 - image: do not write empty etc/cloud
 - tests: skip snapd snap on reset for core systems
 - cmd/snap-discard-ns: fix umount(2) typo
 - overlord/ifacestate: hotplug-remove-slot task handler
 - overlord/ifacestate: handler for hotplug-disconnect task
 - ifacestate/hotplug: updateDevice helper
 - tests: reset snapd state on tests restore
 - interfaces: return security setup errors
 - overlord: make InstallMany work like UpdateMany, issuing a single
   request to get candidates
 - systemd/systemd.go: add missing tests for systemd.IsActive
 - overlord/ifacestate: addHotplugSeqWaitTask helper
 - cmd/snap-confine: refactor call to snap-update-ns --user-mounts
 - tests: new backend used to run upgrade test suite
 - travis: short circuit failures in static and unit tests travis job
 - cmd: automatically fix localized <option>s to <option>
 - overlord/configstate,features: expose features to snapd tools
 - selinux: package to query SELinux status and verify/restore file
   contexts
 - wrappers: use new systemd.IsActive in core18 early boot
 - cmd: add tests for lintArg and lintDesc
 - httputil: retry on temporary net errors
 - cmd/snap-confine: remove unused sc_discard_preserved_mount_ns
 - wrappers: only restart service in core18 when they are active
 - overlord/ifacestate: helpers for serializing hotplug changes
 - packaging/{fedora,opensuse}: own /var/lib/snapd/cookie
 - systemd: start snapd.autoimport.service in --no-block mode
 - data/selinux: fix syntax error in definition of snappy_admin
   interface
 - snap/info: bind global plugs/slots to implicit hooks
 - cmd/snap-confine: remove SC_NS_MNT_FILE
 - spread: record each tests/upgrade job
 - osutil: do not import dirs
 - cmd/snap-confine: fix typo "a pipe"
 - tests: make security-device-cgroups-{devmode,jailmode} work on arm
   devices
 - tests: force test-snapd-daemon-notify exit 0 when the interface is
   not connected
 - overlord/snapstate: run 'remove' hook before 'auto-disconnect'
 - centos: enable SELinux support on CentOS 7
 - apparmor: allow hard link to snap-specific semaphore files
 - tests/lib/pkgdb: disable weak deps on Fedora
 - release: detect too old apparmor_parser
 - tests: improve how the log is checked to see if the system is
   waiting for a reboot
 - cmd, dirs, interfaces/apparmor: update distro identification to
   support ID="archlinux"
 - spread, tests: add Fedora 29
 - cmd/snap-confine: refactor calling snapd tools into helper module
 - apparmor: allow snap-update-ns access to common devices
 - cmd/snap-confine: capture initialized per-user mount ns
 - tests: reduce verbosity around package installation
 - data: set KillMode=process for snapd
 - cmd/snap: handle DNS error gracefully
 - spread, tests: use checkpoints when dumping audit log
 - tests/lib/prepare: make sure that SELinux context of repacked core
   snap is controlled
 - testutils: split checkers, tweak tests
 - tests: fix for tests test-*-cgroup
 - spread: show AVC audits when debugging, start auditd on Fedora
 - spread: drop Fedora 27, add Fedora 29
 - tests/lib/reset: restore context of removed snapd directories
 - testutil: add File{Present,Absent} checkers
 - snap: add new `snap run --trace-exec`
 - tests: fix for failover test on how logs are checked
 - snapctl: add "services"
 - overlord/snapstate: use file timestamp to initialize timer
 - cmd/libsnap: introduce and use sc_strdup
 - interfaces: let NM access ifindex/ifupdown files
 - overlord/snapstate: on refresh, check new rev can read current
 - client, store: don't use store from client (use client from store)
 - tests/main/parallel-install-store: verify installation of more
   than one instance at a time
 - overlord: don't write system key if security setup fails
 - packaging/fedora/snapd.spec: fix bogus date in changelog
 - snapstate: update fontconfig caches on install
 - interfaces/apparmor/backend.go:411:38: regular expression does not
   contain any meta characters (SA6004)
 - asserts/header_checks.go:199:35: regular expression does not
   contain any meta characters (SA6004)
 - run staticcheck every time :-)
 - tests/lib/systemd-escape/main.go:46:14: printf-style function with
   dynamic first argument and no further arguments should use print-
   style function instead (SA1006)
 - tests/lib/fakestore/cmd/fakestore/cmd_run.go:66:15: the channel
   used with signal.Notify should be buffered (SA1017)
 - tests/lib/fakedevicesvc/main.go:55:15: the channel used with
   signal.Notify should be buffered (SA1017)
 - spdx/parser.go:30:1: only the first constant has an explicit type
   (SA9004)
 - overlord/snapstate/snapmgr.go:553:21: printf-style function with
   dynamic first argument and no further arguments should use print-
   style function instead (SA1006)
 - overlord/patch/patch3.go:44:70: printf-style function with dynamic
   first argument and no further arguments should use print-style
   function instead (SA1006)
 - cmd/snap/cmd_advise.go:200:2: empty branch (SA9003)
 - osutil/udev/netlink/conn.go:120:5: ineffective break statement.
   Did you mean to break out of the outer loop? (SA4011)
 - daemon/api.go:992:22: printf-style function with dynamic first
   argument and no further arguments should use print-style function
   instead (SA1006)
 - cmd/snapd/main.go:94:5: ineffective break statement. Did you mean
   to break out of the outer loop? (SA4011)
 - cmd/snap/cmd_userd.go:73:15: the channel used with signal.Notify
   should be buffered (SA1017)
 - cmd/snap/cmd_help.go:102:7: io.Writer.Write must not modify the
   provided buffer, not even temporarily (SA1023)
 - release: probe apparmor features lazily
 - overlord,daemon: mock security backends for testing
 - cmd/libsnap: move apparmor-support to libsnap
 - cmd: drop cruft from snap-discard-ns build rules
 - cmd/snap-confine: use snap-discard-ns ns to discard stale
   namespaces
 - cmd/snap-confine: handle mounted shared /run/snapd/ns
 - many: fix composite literals with unkeyed fields
 - dirs, wrappers, overlord/snapstate: make completion + bases work
 - tests: revert "tests: restore in restore, not prepare"
 - many: validate title
 - snap: make description maximum in runes, not bytes
 - tests: discard mount namespaces in reset.sh
 - tests/lib: sync cla check back from snapcraft
 - Revert "cmd/snap, tests/main/snap-info: highlight the current
   channel"
 - daemon: remove enableInternalInterfaceActions
 - mkversion: use "test -n" rather than "! test -z"
 - run-checks: assorted fixes
 - tests: restore in restore, not in prepare
 - cmd/snap: fix missing newline in "snap keys" error message
 - snap: epoch lists must contain no duplicate entries
 - interfaces/avahi_observe: Fix typo in comment
 - tests: add SPREAD_JOB to the description of
   systemd_create_and_start_unit
 - daemon, vendor: bump github.com/coreos/go-systemd/activation,
   handle API changes
 - Revert "cmd/snap-confine: don't allow mapping lib{uuid,blkid}"
 - packaging/fedora: use %%_sysctldir macro
 - cmd/snap-confine: remove unneeded unshare
 - sanity: extend the kernel version check to cover CentOS/RHEL
   kernels
 - wrappers: remove all desktop files from a snap on removal
 - snap: add an explicit check for `epoch: null` loading
 - snap: check max description length in validate
 - spread, tests: add CentOS support
 - cmd/snap-confine: allow mapping more libc shards
 - cmd/snap-discard-ns: add support for --from-snap-confine
 - tests: make tinyproxy support systemd notify
 - tests: fix shellcheck
 - snap, store: rename `snap.Epoch`'s `Unset` to `IsZero`
 - store: add a test for a non-zero epoch refresh (with epoch bump)
 - store: v1 search doesn't send epoch, stop pretending it does
 - snap: make any "0" epoch be Unset, and marshalled to {[0],[0]}
 - overlord/snapstate: amend test should send local revision
 - tests: use mock-gpio.py in enable-disable-units-gpio test
 - snap: enforce minimal snap name len of 2
 - cmd/libsnap: add sc_verify_snap_lock
 - cmd/snap-update-ns: extra debugging of trespassing events
 - userd: force zenity width if the text displayed is long
 - overlord/snapstate, store: always send epochs
 - cmd/snap-confine,snap-update-ns: discard quirks
 - cmd/snap: add nanosleep to blacklisted syscalls when running with
   --strace
 - cmd/snap-update-ns, tests: clean trespassing paths
 - nvidia, interfaces/builtin: OpenCL fixes
 - ifacestate/hotplug: removeDevice helper
 - cmd: install snap-discard-ns in "make hack"
 - overlord/ifacestate: setup security backends phased by backends
   first
 - ifacestate/helpers: added SystemSnapName mapper helper method
 - overlord/ifacestate: set hotplug-key of the connection when
   connecting hotplug slots
 - snapd: allow snap-update-ns to read /proc/version
 - cmd: handle tumbleweed and leap in autogen.sh
 - interfaces/tests: MockHotplugSlot test helper
 - store,daemon: make UserInfo,LoginUser part of the store interface
 - overlord/ifacestate: use remapper when checking if system snap is
   installed
 - tests: fix how pinentry is prepared for new gpg v 2.1 and 2.2
 - packaging/arch: fix bash completions path
 - interfaces/builtin: add device-buttons interface for accessing
   events
 - tests, fakestore: extend refresh tests with parallel installed
   snaps
 - snap, store, overlord/snapshotstate: drop epoch pointers
 - snap: make Epoch default to {[0],[0]} on load from yaml
 - data/completion: pass documented arguments to completion functions
 - tests: skip opensuse from interfaces-openvswitch-support test
 - tests: simple reproducer for snap try and hooks bug
 - snapstate: do not allow classic mode for strict snaps
 - snap: make Epoch's MarshalJSON not simplify
 - store: remove unused currentSnap and currentSnapJSON
 - many: some small doc comment fixes in recent hotplug code
 - ifacestate/udevmonitor: added callback to signal end of
   enumeration
 - cmd/libsnap: add simplified feature flag checker
 - interfaces/opengl: add additional accesses for cuda
 - tests: add core18 only hooks test and fix running core18 only on
   classic
 - sanity, release, cmd/snap: refuse to try to do things on WSL.
 - cmd: make coreSupportsReExec faster
 - overlord/ifacestate: don't remove the dash when generating unique
   slot name
 - cmd/snap-seccomp: add full complement of ptrace constants
 - cmd: update autogen.sh for opensuse
 - interfaces/apparmor: allow access to /run/snap.$SNAP_INSTANCE_NAME
 - spread.yaml: add more systems to the autopkgtest and qemu backends
 - daemon: spool sideloaded snap into blob dir
   overlord/snapstate: address review feedback
 - packaging/opensuse: stop using golang-packaging
 - overlord/snapshots: survive an unknown user
 - wrappers: fix generating of service units with multiple `before`
   dependencies
 - data: run snapd.autoimport.service only after seeding
 - cmd/snap: unhide --name parameter to snap install, tweak help
   message
 - packaging/fedora: Merge changes from Fedora Dist-Git
 - tests/main/snap-service-after-before-install: verify after/before
   in snap install
 - overlord/ifacestate: mark connections disconnected by hotplug with
   hotplug-gone
 - ifacestate/ifacemgr: don't reload hotplug-gone connections on
   startup
 - tests: install dependencies during prepare
 - tests,store,daemon: ensure proxy settings are honored in
   auth/userinfo too
 - tests: core 18 does not support classic confinement
 - tests: add debug output for degraded test
 - strutil: make VersionCompare faster
 - overlord/snapshotstate/backend: survive missing directories
 - overlord/ifacestate: use map[string]*connState when passing conns
   around
 - tests: move fedora 28 to manual
 - overlord/snapshotstate/backend: be more verbose when
   SNAPPY_TESTING=1
 - tests: removing fedora 26 system from spread.yaml
 - tests: linode execution is not needed anymore
 - tests/lib: adjust to changed systemctl behaviour on debian-9
 - tests: fixes and new backend for tests on nested suite
 - strutil: let MatchCounter work with a nil regexp
 - ifacestate/helpers: findConnsForHotplugKey helper
 - many: move regexp.(Must)Compile out of non-init functions into
   variables
 - store: also make snaps downloaded via deltas 0600
 - snap: use Lstat to determine snap size, remove
   ReadSnapInfoExceptSize
 - interfaces/builtin: add adb-support interface
 - tests: fail if install_snap_local fails
 - strutil: add extra test to CommaSeparatedList as suggested by
   mborzecki
 - cmd/snap, daemon, strutil: use CommaSeparatedList to split a CSL
 - ifacestate: optimize disconnect hooks
 - cmd/snap-update-ns: parse the -u <uid> command line option
 - cmd/snap, tests: snapshots for all
 - client, cmd/daemon: allow disabling keepalive, improve degraded
   mode unit tests
 - snap: only show "next" refresh time if its after the hold time
 - overlord/snapstate: run tests for classic snaps even on systems
   that don't support classic
 - overlord/standby: fix a race between standby goroutine and stop
 - cmd/snap-exec: don't fail on some try mode snaps
 - cmd/snap, userd, testutil: tweak DBus tests to use private session
   bus connection
 - cmd: remove remnants of sc_should_populate_mount_ns
 - client, daemon, cmd/snap: indicate that services are socket/timer
   activated
 - cmd/snap-seccomp: only look for PTRACE_GETFPX?REGS where available
 - cmd/snap-confine: remove SC_NS_FAIL_GRACEFULLY
 - snap/pack, cmd/snap: allow specifying the filename of 'snap pack'
 - cmd/snap-discard-ns: add support for per-user mount namespaces
 - cmd/snap-confine: remove stale mount profile along stale namespace
 - data/apt: close stderr when calling snap in the apt install hook.
 - tests/main: fixes for the new shellcheck
 - testutil, cmd/snap: introduce and use testutil.EqualsWrapped and
   fly
 - tests: initial setup for testing current branch on nested vm and
   hotplug management
 - cmd: refactor IPC and lifecycle of the helper process
 - tests/main/parallel-install-store: the store has caught up, do not
   expect failures
 - overlord/snapstate, snap, wrappers: start services in the right
   order during install
 - interfaces/browser-support, cmd/snap-seccomp: Allow read-only
   ptrace, for the Breakpad crash reporter
 - snap,client: use a different exit code for retryable errors
 - overlord/ifacestate: don't conflict on own discard-snap tasks when
   refreshing & doing garbage collection
 - cmd/snap: tweak `snap services` output when there is no services
 - interfaces/many: updates to support k8s worker nodes
 - cmd/snap: gnome-software install via snap:// handler
 - overlord/many: cleanup use of snapName vs. instanceName
 - snapstate: add command-chain to supported featureset
 - daemon, snap: mark screenshots as deprecated
 - interfaces: fix decoding of json numbers for static/dynamic
   attributes* ifstate: fix decoding of json numbers
 - cmd/snap: try not to panic on error from "snap try"
 - tests: new cosmic image for spread tests on gce
 - interfaces/system-key: add parser mtime and only discover features
   on write
 - overlord/snapshotstate/backend: detect path to tar in unit tests
 - tests/unit/gccgo: drop gccgo unit tests
 - cmd: use relative file names in locking APIs
 - interfaces: fix NormalizeInterfaceAttributes, add tests
 - overlord/snapshotstate/backend: fall back on sudo when no runuser
 - cmd/snap-confine: reduce verbosity of debug and error messages
 - systemd: extend Status() to work for socket and timer units
 - interfaces: typo 'allows' for consistency with other ifaces
 - systemd,wrappers: don't start disabled services
 - ifacestate: simplify task chaining in ifacestate.Connect
 - tests: ensure that goa-daemon is off
 - snap/pack, snap/squashfs: remove extra copy before mksquashfs
 - cmd/snap: block 'snap help <cmd> --all'
 - asserts, image: ensure kernel, gadget, base and required-snaps use
   valid snap names
 - apparmor: add unit test for probeAppArmorParser and simplify code
 - interfaces/apparmor: conditionally add explicit deny rules for
   ptrace
 - po: sync translations from launchpad
 - osutil: tweak handling of error adduser errors
 - cmd: rename ns_group to mount_ns
 - tests/main/interfaces-accounts-service: more debugging
 - snap/pack, snap/squashfs: use type to determine mksquashfs args
 - data/systemd, wrappers: tweak system-shutdown helper for core18
 - tests: show list of processes when ifaces-accounts-service fails
 - tests: do not run degraded test in autopkgtest env
 - snap: overhaul validation error messages
 - ifacestate/hooks: only create interface hook tasks if hooks exist
 - osutil: workaround overlayfs on ubuntu 18.10
 - interfaces/home: don't allow snaps to write to $HOME/bin
 - interfaces: improve Attr error further
 - snapstate: tweak GetFeatureFlagBool() to have a default argument
 - many: cleanup remaining parallel installs TODOs
 - image: improve validation of extra snaps

* Tue Dec 18 2018 Neal Gompa <ngompa13@gmail.com> - 2.36.3-1
- Release 2.36.3 to Fedora
- Remove merged patch

* Fri Dec 14 2018 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.36.3
 - wrappers: use new systemd.IsActive in core18 early boot
 - httputil: retry on temporary net errors
 - wrappers: only restart service in core18 when they are active
 - systemd: start snapd.autoimport.service in --no-block mode
 - data/selinux: fix syntax error in definition of snappy_admin
   interfacewhen installing selinux-policy-devel package.
 - centos: enable SELinux support on CentOS 7
 - cmd, dirs, interfaces/apparmor: update distro identification to
   support ID="archlinux"
 - apparmor: allow hard link to snap-specific semaphore files
 - overlord,apparmor: new syskey behaviour + non-ignored snap-confine
   profile errors
 - snap: add new `snap run --trace-exec` call
 - interfaces/backends: detect too old apparmor_parser

* Thu Nov 29 2018 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.36.2
 - daemon, vendor: bump github.com/coreos/go-systemd/activation,
   handle API changes
 - snapstate: update fontconfig caches on install
 - overlord,daemon: mock security backends for testing
 - sanity, spread, tests: add CentOS
 - Revert "cmd/snap, tests/main/snap-info: highlight the current
   channel"
 - cmd/snap: add nanosleep to blacklisted syscalls when running with
   --strace
 - tests: add regression test for LP: #1803535
 - snap-update-ns: fix trailing slash bug on trespassing error
 - interfaces/builtin/opengl: allow reading /etc/OpenCL/vendors
 - cmd/snap-confine: nvidia: pick up libnvidia-opencl.so
 - interfaces/opengl: add additional accesses for cuda

* Wed Nov 21 2018 Neal Gompa <ngompa13@gmail.com> - 2.36-4
- Fix backport patch

* Wed Nov 21 2018 Neal Gompa <ngompa13@gmail.com> - 2.36-3
- Backport fixes for EL7 support

* Wed Nov 14 2018 Neal Gompa <ngompa13@gmail.com> - 2.36-2
- Fix runtime dependency for selinux subpackage for EL7

* Fri Nov 09 2018 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.36.1
 - tests,snap-confine: add core18 only hooks test and fix running
   core18 only hooks on classic
 - interfaces/apparmor: allow access to
   /run/snap.$SNAP_INSTANCE_NAME
 - spread.yaml: add more systems to the autopkgtest and qemu backends
 - daemon: spool sideloaded snap into blob dir
 - wrappers: fix generating of service units with multiple `before`
   dependencies
 - data: run snapd.autoimport.service only after seeding
 - tests,store,daemon: ensure proxy settings are honored in
   auth/userinfo too
 - packaging/fedora: Merge changes from Fedora Dist-Git
 - tests/lib: adjust to changed systemctl behaviour on debian-9
 - tests/main/interfces-accounts-service: switch to busctl, more
   debugging
 - store: also make snaps downloaded via deltas 0600
 - cmd/snap-exec: don't fail on some try mode snaps
 - cmd/snap, userd, testutil: tweak DBus tests to use private session
   bus connection
 - tests/main: fixes for the new shellcheck
 - cmd/snap-confine: remove stale mount profile along stale namespace
 - data/apt: close stderr when calling snap in the apt install hook

* Sun Nov 04 2018 Neal Gompa <ngompa13@gmail.com> - 2.36-1
- Release 2.36 to Fedora

* Wed Oct 24 2018 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.36
 - overlord/snapstate, snap, wrappers: start services in the right
   order during install
 - tests: the store has caught up, drop gccgo test, update cosmic
   image
 - cmd/snap: try not to panic on error from "snap try"`--devmode`
 - overlord/ifacestate: don't conflict on own discard-snap tasks when
   refreshing & doing garbage collection
 - snapstate: add command-chain to supported featureset
 - daemon, snap: mark screenshots as deprecated
 - interfaces: fix decoding of json numbers for static/dynamic
   attributes
 - data/systemd, wrappers: tweak system-shutdown helper for core18
 - interfaces/system-key: add parser mtime and only discover features
   on write
 - interfaces: fix NormalizeInterfaceAttributes, add tests
 - systemd,wrappers: don't start disabled services
 - ifacestate/hooks: only create interface hook tasks if hooks exist
 - tests: do not run degraded test in autopkgtest env
 - osutil: workaround overlayfs on ubuntu 18.10
 - interfaces: include invalid type in Attr error
 - many: enable layouts by default
 - interfaces/default: don't scrub with change_profile with classic
 - cmd/snap: speed up unit tests
 - vendor, cmd/snap: refactor to accommodate the new less buggy go-
   flags
 - daemon: expose snapshots to the API
 - interfaces: updates for default, screen-inhibit-control, tpm,
   {hardware,system,network}-observe
 - interfaces/hotplug: rename HotplugDeviceKey method to HotplugKey,
   update test interface
 - interfaces/tests: use TestInterface instead of a custom local
   helper
 - overlord/snapstate: export getFeatureFlagBool.
 - osutil,asserts,daemon: support force password change in system-
   user assertion
 - snap, wrappers: support restart-delay, generate RestartSec=<value>
   in service units
 - tests/ifacestate: moved asserts-related mocking into helper
 - image: fetch device store assertion if available
 - many: enable AppArmor on Arch
 - interfaces/repo: two helper methods for hotplug
 - overlord/ifacestate: add hotplug slots with implicit slots
 - interfaces/hotplug: helpers and struct updates
 - tests: run the snapd tests on Ubuntu 18.10
 - snapstate: only report errors if there is an actual error
 - store: speedup unit tests
 - spread-shellcheck: fix interleaved error messages, tweaks
 - apparmor: create SnapAppArmorDir in setupSnapConfineReexec
 - ifacestate: implementation of defaultDeviceKey function for
   hotplug
 - cmd/snap-update-ns: remove empty placeholders used for mounting
 - snapshotstate: restore to current revision
 - tests/lib: rework the CLA checker
 - many: support and consider store friendly-stores when checking
   device scope constraints
 - overlord/snapstate: block parallel installs of snapd, core, base,
   kernel, gadget snaps
 - overlord/patch: patch for static plug/slot attributes
 - interfaces: honor static attributes when reloading conns
 - osutils: unit tests speedup; introduce «run-checks --short-
   unit».
 - systemd, wrappers: speed up wrappers unit tests
 - client: speedup unit tests
 - spread-shellcheck: use threads to parallelise
 - snap: validate plug and slot names
 - osutil, interfaces/apparmor: add and use of osutil.UnlinkMany
 - wrappers: do not depend on network.taget in socket units, tweak
   generated units
 - interfaces/apparmor: (un)load profiles in one apparmor_parser call
 - store: gracefully handle unexpected errors in 'action'
   response
 - cmd: put our manpages in section 8
 - overlord: don't make become-operational interfere with user
   requests
 - store: tweak unmatched refresh result error log
 - snap, client, daemon, store: use and expose "media" more
 - tests,cmd/snap-update-ns: add test showing mount update bug
   cmd/snap-update-ns: better detection of snapd-made tmpfs
 - tests: spread tests for aliases with parallel installed snaps
 - interfaces/seccomp: allow using statx by default
 - store: gracefully handle unexpected errors in 'action' response
 - overlord/snapshotstate: chown the tempdir
 - cmd/snap: attempt to start the document portal if running with a
   session bus
 - snap: detect layouts vs layout in snap.yaml
 - interfaces/apparmor: handle overlayfs snippet for snap-update-ns
 - snapcraft.yaml: set grade to stable
 - tests: shellchecks, final round
 - interfaces/apparmor: handle overlayfs snippet for snap-update-ns
 - snap: detect layouts vs layout in snap.yaml
 - overlord/snapshotstate: store epoch in snapshot, check on restore
 - cmd/snap: tweak UX of snap refresh --list
 - overlord/snapstate: improve consistency, use validateInfoAndFlags
   also in InstallPath
 - snap: give Epoch a CanRead helper
 - overlord/snapshotstate: small refactor of internal helpers
 - interfaces/builtin: adding missing permission to create
   /run/wpa_supplicant directory
 - interfaces/builtin: avahi interface update
 - client, daemon: support passing of 'unaliased' option when
   installing from local files
 - selftest: rename selftest.Run() to sanity.Check()
 - interfaces/apparmor: report apparmor support level and policy
 - ifacestate: helpers for generating slot names for hotplug
 - overlord/ifacestate: make sure to pass in the Model assertion when
   enforcing policies
 - overlord/snapshotstate: store the SnapID in snapshot, block
   restore if changed
 - interfaces: generalize writable mimic profile
 - asserts,interfaces/policy: add support for on-store/on-brand/on-
   model plug/slot rule constraints
 - many: fetch the device store assertion together and in the context
   of interpreting snap-declarations
 - tests: disable gccgo tests on 18.04 for now, until dh-golang vs
   gccgo is fixed
 - tests/main/parallel-install-services: add spread test for snaps
   with services
 - tests/main/snap-env: extend to cover parallel installations of
   snaps
 - tests/main/parallel-install-local: rename from *-sideload, extend
   to run snaps
 - cmd/snapd,daemon,overlord: without snaps, stop and wait for socket
 - cmd/snap: tame the help zoo
 - tests/main/parallel-install-store: run installed snap
 - cmd/snap: add a bunch of TRANSLATORS notes (and a little more
   i18n)
 - cmd: fix C formatting
 - tests: remove unneeded cleanup from layout tests
 - image: warn on missing default-providers
 - selftest: add test to ensure selftest.checks is up-to-date
 - interfaces/apparmor, interfaces/builtin: tweaks for parallel snap
   installs
 - userd: extend the list of supported XDG Desktop properties when
   autostarting user applications
 - cmd/snap-update-ns: enforce trespassing checks
 - selftest: actually run the kernel version selftest
 - snapd: go into degraded mode when the selftest fails
 - tests: add test that runs snapctl with a core18 snap
 - tests: add snap install hook with base: core18
 - overlord/{snapstate,assertstate}: parallel instances and
   refresh validation
 - interfaces/docker-support: add rules to read apparmor macros
 - tests: make nfs test available for more systems
 - tests: cleanup copy/paste dup in interfaces-network-setup-control
 - tests: using single sh snap in interface tests
 - overlord/snapstate: improve cleaup in mount-snap handler
 - tests: don't fail interfaces-bluez test if bluez is already
   installed
 - tests: find snaps just for edge and beta channels
 - daemon, snapstate: consistent snap list [--all] output with broken
   snaps
 - tests: fix listing to allow extra things in the notes column
 - cmd/snap: improve UX when removing specific snap revision
 - cmd/snap, tests/main/snap-info: highlight the current channel
 - interfaces/testiface: added TestHotplugInterface
 - snap: tweak commands
 - interfaces/hotplug: hotplug spec takes one slot definition
 - overlord/snapstate, snap: handle shared snap directories when
   installing/remove snaps with instance key
 - interfaces/opengl: misc accesses for VA-API
 - client, cmd/snap: expose warnings to the world
 - cmd/snap-update-ns: introduce trespassing state tracking
 - cmd/snap: commands no longer build their own client
 - tests: try to build cmd/snap for darwin
 - daemon: make error responders not printf when called with 1
   argument
 - many: return real snap name in API response
 - overlord/state: return latest LastAdded time in WarningsSummary
 - many: mount namespace mapping for parallel installs of snaps
 - ifacestate/autoconnect: do not self-conflict on setup-profiles if
   core-phase-2
 - client, cmd/snap: on !linux, exit when the client tries to Do
   something
 - tests: refactor for nested suite and tests fixed
 - tests: use lxd's waitready instead of polling lxd socket
 - ifacestate: don't initialize udev monitor until we have a system
   snap
 - interfaces: extra argument for static attrs in
   NewConnectedPlug/NewConnectedSlot
 - packaging/arch: sync packaging with AUR
 - snapstate/tests: serialize all appends in fake backend
 - snap-confine: make /lib/modules optional
 - cmd/snap: handle "snap interfaces core" better
 - store: move download tests into downloadSuite
 - tests,interfaces: run interfaces-account-control on UC18
 - tests: fix install snaps test by adding link to /snap
 - tests: fix for nested test suite
 - daemon: fix snap list --all with parallel snap instances
 - snapstate: refactor tests to use SetModel*
 - wrappers: fix snap services order in tests
 - many: provide salt for generating instance-key in store requests
 - ifacestate: fix hang when retrying content providers
 - snapd-env-generator: fix when PATH is empty or unset
 - overlord/assertstate: propagate TaskSnapSetup error
 - client: catch and expose logs errors
 - overlord: integrate device enumeration with udev monitor
 - daemon, overlord/state: warnings pipeline
 - tests: add publisher regex to fix the snap-info test pass on sru
 - cmd: use systemdsystemgeneratorsdir, cleanup automake complaints,
   tweaks
 - cmd/snap-update-ns: remove the unused Secure type
 - osutil, o/snapshotstate, o/sss/backend: quick fixes
 - tests: update the listing expression to support core from
   different channels
 - store: use stable instance key in store refresh requests
 - cmd/snap-update-ns: detach Mk{Prefix,{File,Dir,Symlink{,All}}}
 - overlord/patch: support for sublevel patches
 - tests: update prepare/restore for nightly suite
 - cmd/snap-update-ns: detach BindMount from the Secure type
 - cmd/snap-update-ns: re-factor pair of helpers to call fstatfs once
 - ifacestate: retry on "discard-snap" in autoconnect conflict check
 - cmd/snap-update-ns: separate OpenPath from the Secure struct
 - wrappers: remove Wants=network-online.target
 - tests: add new core16-base test
 - store: refactor tests so that they work as store_test package
 - many: add refresh.rate-limit core option
 - tests: run account-control test with different bases
 - tests: port proxy test to use python tinyproxy
 - overlord: introduce snapshotstate.
 - testutil: allow Fstatfs results to vary over time
 - snap-update-ns: add comments about the "deadcode" in bootstrap.go
 - overlord: add chg.Err() in testUpdateWithAutoconnectRetry
 - many: remove deadcode
 - tests: also run unit/gccgo in 18.04
 - tests: introduce a helper for installing local snaps with --name
 - tests: avoid removing core snap on reset
 - snap: use snap.SideInfo in test to fix build with gccgo
 - partition: remove unused runCommand
 - image: fix incorrect error when using local bases
 - overlord/snapstate: fix format
 - cmd: fix format
 - tests: setting "storage: preserve-size" just for amazon-linux
   system
 - tests: test for the hostname interface
 - interfaces/modem-manager: allow access to more USB strings
 - overlord: instantiate UDevMonitor
 - interfaces/apparmor: tweak naming, rename to AddLayout()
 - interfaces: take instance name in ifacetest.InstallSnap
 - snapcraft: do not use --dirty in mkversion
 - cmd: add systemd environment generator
 - devicestate: support getting (http) proxy from core config
 - many: rename ClientOpts to ClientOptions
 - prepare-image-grub-core18: remove image root in restore
 - overlord/ifacestate: remove "old-conn" from connect/undo connect
   handlers
 - packaging/fedora: Merge changes from Fedora Dist-Git
 - image: handle errors when downloadedSnapsInfoForBootConfig has no
   data
 - tests: use official core18 model assertion in tests
 - snap-confine: map /var/lib/extrausers into snaps mount-namespace
 - overlord,store: support proxy settings internally too
 - cmd/snap: bring back 'snap version'
 - interfaces/mount: tweak naming of things
 - strutil: fix MatchCounter to also work with buffer reuse
 - cmd,interfaces,tests: add /mnt to removable-media interface
 - systemd: do not run "snapd.snap-repair.service.in on firstboot
   bootstrap
 - snap/snapenv: drop some instance specific variables, use instance-
   specific ones for user locations
 - firstboot: sort by type when installing the firstboot snaps
 - cmd, cmd/snap: better support for non-linux
 - strutil: add new ParseByteSize
 - image: detect and error if bases are missing
 - interfaces/apparmor: do not downgrade confinement on arch with
   linux-hardened 4.17.4+
 - daemon: add pokeStateLock helper to the daemon tests
 - snap/squashfs: improve error message from Build on mksquashfs
   failure
 - tests: remove /etc/alternatives from dirs-not-shared-with-host
 - cmd: support re-exec into the "snapd" snap
 - spdx: remove "Other Open Source" from the support licenses
 - snap: add new type "TypeSnapd" and attach to the snapd snap
 - interfaces: retain order of inserted security backends
 - tests: spread test for parallel-installs desktop file handling
 - overlord/devicestate: use OpenSSL's PEM format when generating
   keys
 - cmd: remove --skip-command-chain from snap run and snap-exec
 - selftest: detect if apparmor is unusable and error
 - snap,snap-exec: support command-chain for hooks
 - tests: significantly reduce execution time for managers test
 - snapstate: use new "snap.ByType" sorting
 - overlord/snapstate: fix UpdateMany() to work with parallel
   instances
 - testutil: have File* checker produce more useful error output
 - overlord/ifacestate: introduce connectOpts
 - interfaces: parallel instances support, extend unit tests
 - tests: normalize tests
 - snapstate: make InstallPath() return *snap.Info too
 - snap: add ByType sorting
 - interfaces: add cifs-mount interface
 - tests: use file based markers in snap-service-stop-mode
 - osutil: reorg and stub out things to get it building on darwin
 - tests/main/layout: cleanup after the test
 - osutil/sys: small tweaks to let it build on darwin
 - daemon, overlord/snapstate: set instance name when installing from
   snap file
 - many: move Uname to osutil, for more DRY and easier porting.
 - cmd/snap: create snap user directory when running parallel
   installed snaps
 - cmd/snap-confine: switch to validation of SNAP_INSTANCE_NAME
 - tests: basic test for parallel installs from the store
 - image: download the gadget from the model.GadgetTrack()
 - snapstate: add support for gadget tracks in model assertion
 - image: add support for "gadget=track"
 - overlord: handle sigterm during shutdown better
 - tests: add the original function to fix the errors on new kernels
 - tests/main/lxd: pull lxd from candidate; reënable i386
 - wayland: add extra sockets that are used by older toolkits (e.g.
   gtk3)
 - asserts: add support for gadget tracks in the model assertion
 - overlord/snapstate: improve feature flag validation
 - tests/main/lxd: run ubuntu-16.04 only on 64 bit variant
 - interfaces: workaround for activated services and newer DBus
 - tests: get the linux-image-extra available for the current kernel
 - interfaces: add new "sysfs-name" to i2c interfaces code
 - interfaces: disconnect hooks
 - cmd/libsnap: unify detection of core/classic with go
 - tests: fix autopkgtest failures in cosmic
 - snap: fix advice json
 - overlord/snapstate: parallel snap install
 - store: backward compatible instance-key handling for non-instance
   snaps
 - interfaces: add screencast-legacy for video and audio recording
 - tests: skip unsupported architectures for fedora-base-smoke test
 - tests: avoid using the journalctl cursor when it has not been
   created yet
 - snapstate: ensure normal snaps wait for the "snapd" snap on
   refresh
 - tests: enable lxd again everywhere
 - tests: new test for udisks2 interface
 - interfaces: add cpu-control for setting CPU tunables
 - overlord/devicestate: fix tests, set seeded in registration
   through proxy tests
 - debian: add missing breaks on cosmic
 - devicestate: only run device-hook when fully seeded
 - seccomp: conditionally add socketcall() based on system and base
 - tests: new test for juju client observe interface
 - overlord/devicestate: DTRT w/a snap proxy to reach a serial vault
 - snapcraft: set version information for the snapd snap
 - cmd/snap, daemon: error out if trying to install a snap using
   empty name
 - hookstate: simplify some hook tests
 - cmd/snap-confine: extend security tag validation to cover instance
   names
 - snap: fix mocking of systemkey in snap-run tests
 - packaging/opensuse: fix static build of snap-update-ns and snap-
   exec
 - interfaces/builtin: addtl network-manager resolved DBus fix
 - udev: skip TestParseUdevEvent on ppc
 - interfaces: miscellaneous policy updates
 - debian: add tzdata to build-dep to ensure snapd builds correctly
 - cmd/libsnap-confine-private: intoduce helpers for validating snap
   instance name and instance key
 - snap,snap-exec: support command-chain for app
 - interfaces/builtin: network-manager resolved DBus changes
 - snap: tweak `snap wait` command
 - cmd/snap-update-ns: introduce validation of snap instance names
 - cmd/snap: fix some corner-case test setup weirdness
 - cmd,dirs: fix various issues discovered by a Fedora base snap
 - tests/lib/prepare: fix extra snaps test

* Mon Oct 15 2018 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.35.5
 - interfaces/home: don't allow snaps to write to $HOME/bin
 - osutil: workaround overlayfs on ubuntu 18.10

* Fri Oct 05 2018 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.35.4
  - wrappers: do not depend on network.taget in socket units, tweak
    generated units

* Fri Oct 05 2018 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.35.3
 - overlord: don't make become-operational interfere with user
   requests
 - docker_support.go: add rules to read apparmor macros
 - interfaces/apparmor: handle overlayfs snippet for snap-update-
   nsFixes:
 - snapcraft.yaml: add workaround to fix snapcraft build
 - interfaces/opengl: misc accesses for VA-API

* Wed Sep 12 2018 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.35.2
 - cmd,overlord/snapstate: go 1.11 format fixes
 - ifacestate: fix hang when retrying content providers
 - snap-env-generator: do nothing when PATH is unset
 - interfaces/modem-manager: allow access to more USB strings

* Mon Sep 03 2018 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.35.1
 - packaging/fedora: Merge changes from Fedora Dist-Git
 - snapcraft: do not use --diry in mkversion.sh
 - cmd: add systemd environment generator
 - snap-confine: map /var/lib/extrausers into snaps mount-namespace
 - tests: cherry-pick test fixes from master for 2.35
 - systemd: do not run "snapd.snap-repair.service.in on firstboot
   bootstrap
 - interfaces: retain order of inserted security backends
 - selftest: detect if apparmor is unusable and error

* Sat Aug 25 2018 Neal Gompa <ngompa13@gmail.com> - 2.35-1
- Release 2.35 to Fedora (RH#1598946)

* Mon Aug 20 2018 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.35
 - snapstate: add support for gadget tracks in model assertion
 - image: add support for "gadget=track"
 - asserts: add support for gadget tracks in the model assertion
 - interfaces: add new "sysfs-name" to i2c interfaces code
 - overlord: handle sigterm during shutdown better
 - wayland: add extra sockets that are used by older toolkits
 - snap: fix advice json
 - tests: fix autopkgtest failures in cosmic
 - store: backward compatible instance-key handling for non-instance
   snaps
 - snapstate: ensure normal snaps wait for the "snapd" snap on
   refresh
 - interfaces: add cpu-control for setting CPU tunables
 - debian: add missing breaks on comisc
 - overlord/devicestate: DTRT w/a snap proxy to reach a serial vault
 - devicestate: only run device-hook when fully seeded
 - seccomp: conditionally add socketcall() based on system and base
 - interfaces/builtin: addtl network-manager resolved DBus fix
 - hookstate: simplify some hook tests
 - udev: skip TestParseUdevEvent on ppc
 - interfaces: miscellaneous policy updates
 - debian: add tzdata to build-dep to ensure snapd builds correctly
 - interfaces/builtin: network-manager resolved DBus changes
 - tests: add spread test for fedora29 base snap
 - cmd/libsnap: treat distributions with VARIANT_ID=snappy as "core"
 - dirs: fix SnapMountDir inside a Fedora base snap
 - tests: fix snapd-failover for core18 with external backend
 - overlord/snapstate: always clean SnapState when doing Get()
 - overlod/ifacestate: always use a new SnapState when fetching the
   snap state
 - overlord/devicestate: have the serial request talk to the proxy if
   set
 - interfaces/hotplug: udevadm output parser
 - tests: New test for daemon-notify interface
 - image: ensure "core" is ordered early if base: and core is used
 - cmd/snap-confine: snap-device-helper parallel installs support
 - tests: enable interfaces-framebuffer everywhere
 - tests: reduce nc wait time from 2 to 1 second
 - snap/snapenv: add snap instance specific variables
 - cmd/snap-confine: add minimal test for snap-device-helper
 - tests: enable snapctl test on core18
 - overlord: added UDevMonitor for future hotplug support
 - wrappers: do not glob when removing desktop files
 - tests: add dbus monitor log to interfaces-accounts-service
 - tests: add core-18 systems to external backend
 - wrappers: account for changed app wrapper in parallel installed
   snaps
 - wrappers: make sure that the tests pass on non-Ubuntu too
 - many: add snapd snap failure handling
 - tests: new test for dvb interface
 - configstate: accept refresh.timer=managed
 - tests: new test for snap logs command
 - wrapper: generate all the snapd unit files when generating
   wrappers
 - store: keep all files with link-count > 1 in the cache
 - store: be less verbose in the common refresh case of "no updates"
 - snap-confine: update snappy-app-dev path
 - debian: ensure dependency on fixed apt on 18.04
 - snapd: add initial software watchdog for snapd
 - daemon, systemd: change journalctl -n=all to --no-tail
 - systemd: fix snapd.apparmor.service.in dependencies
 - snapstate: refuse to remove bases or core if snaps need them
 - snap: introduce package-level helpers for building snap related
   directory/file paths
 - overlord/devicestate: deny parallel install of kernel or gadget
   snaps
 - store: clean up parallel-install TODOs in store tests
 - timeutil: fix first weekday of the month schedule
 - interfaces: match all possible tty but console
 - tests: shellchecks part 5
 - cmd/snap-confine: allow ptrace read for 4.18 kernels
 - advise: make the bolt database do the atomic rename dance
 - tests/main/apt-hooks: debug dump of commands.db
 - tests/lib/prepare-restore: update Arch Linux kernel LOCALVERSION
   handling
 - snap: validate instance name as part of Validate()
 - daemon: if a snap is inactive, don't ask systemd about its
   services.
 - udev: skip TestParseUdevEvent on s390x
 - tests: switch core-amd64-18 to use `kernel: pc-kernel=18`
 - asserts,image: add support for new kernel=track syntax
 - tests: new gce image for fedora 27
 - interfaces/apparmor: use the cache in mtime-resilient way
 - store, overlord/snapstate: introduce instance name in store APIs
 - tests: drive-by cleanup of redudant pkgname matching
 - tests: ensure apt-hook is only run after catalog update ran
 - tests: use pkill instead of kilall
 - tests/main: another bunch of updates for Amazon Linux 2
 - tests/lib/snaps: avoid using relative command paths that go up in
   the  directory tree
 - tests: disable/fix more tests for Amazon Linux 2
 - overlord: introduce InstanceKey to SnapState and SnapSetup,
   renames
 - daemon: make sure most change generating handlers can produce
   errors with kinds
 - tests/main/interfaces-calendar-service: skip the test on AMZN2
 - tests/lib/snaps: avoid using relative command paths that go up in
   the directory tree
 - cmd/snap: add a green check mark to verified publishers
 - cmd/snap: fix two issues in the cmd/snap unit tests
 - packaging/fedora: fix target path of /snap symlink
 - cmd/snap: support `--last=<type>?` to mean "no error on empty"
 - cmd/snap-confine: (nvidia) pick up libnvidia-glvkspirv.so
 - strutil: detect and bail out of Unmarshal on duplicate key
 - packaging/fedora(amzn2): disable SELinux, drop dependency on
   squashfuse for AMZN2
 - spread, tests: add support for Amazon Linux 2
 - packaging/fedora: Add Amazon Linux 2 support
 - many: make Wait/Stop optional on StateManagers
 - snap/squashfs: stop printing unsquashfs info to stderr
 - snap: add support for `snap advise-snap --from-apt`
 - overlord/ifacestate: ignore connect if already connected
 - tests: change the service snap used instead of network-bind-
   consumer
 - interfaces/network-control: update for wpa-supplicant and ifupdown
 - tests: fix raciness in stop mode tests
 - logger: try to not have double dates
 - debian: use deb-systemd-invoke instead of systemctl directly
 - tests: run all main tests on core18
 - many: finish sharing a single TaskRunner with all the the managers
 - interfaces/repo: added AllHotplugInterfaces helper
 - snapstate: ensure kernel-track is honored on switch/refresh
 - overlord/ifacestate: support implicit slots on snapd
 - image: add support for "kernel-track" in `snap prepare-image`
 - tests: add test that ensures we do not boot any system in degraded
   state
 - tests: update tests to work on core18
 - cmd/snap: check for typographic dashes in command
 - tests: fix tests expecting old email address
 - client: add some existing error kinds that were not listed in
   client.go
 - tests: add missing slots in classic and core provider test snaps
 - overlord,daemon,cmd: re-map snap names around the edges of snapd
 - tests: use install_local in snap-run-hooks
 - coreconfig: add support for `snap set system network.disable-
   ipv6`
 - overlord/snapstate: dedupe default content providers
 - osutil/udev: sync with upstream
 - debian: do not ship snapd.apparmor.service on ubuntu
 - overlord: have SnapManager use a passed in TaskRunner created by
   Overlord
 - many: streamline the generic conflict check mechanisms
 - tests: remove unneeded setup code in snap-run-symlink
 - cmd/snap: print unset license as "unset", instead of "unknown"
 - asserts: add (optional) kernel-track to model assertion
 - snap/squashfs, tests: pass -n[o-progress] to {mk,un}squashfs
 - interfaces/pulseaudio: be clear that the interface allows playback
   and record
 - snap: support hook environment
 - interfaces: fix typo "daemonNotify" (add missing "n")
 - interfaces: tweak tests of daemon-notify, use common naming
 - interfaces: allow invoking systemd-notify when daemon-notify is
   connected
 - store: make snap blobs be 0600
 - interfaces,daemon: move JSON types to the daemon
 - tests: prepare needs to handle bin/snapctl being a symlink
 - tests: do not mask errors in interfaces-timezone-control (#5405)
 - packaging: put snapctl into /usr/lib/snapd and symlink in usr/bin
 - tests: add basic integration test for spread hold
 - overlord/snapstate: improve PlugsOnly comment
 - many: assorted shellcheck fixes
 - store, daemon, client, cmd/snap: expose "scope", default to wide
 - snapstate: allow setting "refresh.timer=managed"
 - cmd/snap: display a link to data privacy notice for interactive
   snap login
 - client, cmd/snap: pass snap instance name when installing from
   file
 - cmd/snap: add 'debug paths' command
 - snapstate: make sure all *link-*snap tasks carry a snap type and
   further hints
 - devicestate: fix race when refreshing a snap with snapd-control
 - tests: fix tests on arch
 - tests: start active system units on reset
 - tests: new test for joystick interface
 - tests: moving install of dependencies to pkgdb helper
 - tests: enable new fedora image with test dependencies installed
 - tests: start using the new opensuse image with test dependencies
 - tests: check catalog refresh before and after restart snapd
 - tests: stop restarting journald service on prepare
 - interfaces: make core-support a no-op interface
 - interfaces: prefer "snapd" when resolving implicit connections
 - interfaces/hotplug: add hotplug Specification and
   HotplugDeviceInfo
 - many: lessen the use of core-support
 - tests: fixes for the autopkgtest failures in cosmic
 - tests: remove extra ' which breaks interfaces-bluetooth-control
   test
 - dirs: fix antergos typo
 - tests: use grep to avoid non-matching messages from MATCH
 - dirs: improve distro detection for Antegros
 - vendor: switch to latest bson
 - interfaces/builtin: create can-bus interface
 - tests: "snap connect" is idempotent so just connect
 - many: use extra "releases" information on store "revision-not-
   found" errors to produce better errors
 - interfaces: treat "snapd" snap as type:os
 - interfaces: tweak tests to have less repetition of "core" and
   "ubuntu…
 - tests: simplify econnreset test
 - snap: add helper for renaming slots
 - devicestate: fix panic in firstboot code when no snaps are seeded
 - tests: add artful for sru validation on google backend
 - snap,interfaces: move interface name validation to snap
 - overlord/snapstate: introduce path to fake backend ops
 - cmd/snap-confine: fix snaps running on core18
 - many: expose publisher's validation throughout the API

* Fri Jul 27 2018 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.34.3
 - interfaces/apparmor: use the cache in mtime-resilient way
 - cmd/snap-confine: (nvidia) pick up libnvidia-glvkspirv.so
 - snapstate: allow setting "refresh.timer=managed"
 - spread: switch Fedora and openSUSE images

* Thu Jul 19 2018 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.34.2
 - packaging: fix bogus date in fedora snapd.spec
 - tests: fix tests expecting old email address

* Tue Jul 17 2018 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.34.1
 - tests: cherry-pick test fixes from master for 2.34
 - coreconfig: add support for `snap set system network.disable-
   ipv6`
 - debian: do not ship snapd.apparmor.service on ubuntu
 - overlord/snapstate: dedupe default content providers
 - interfaces/builtin: create can-bus interface

* Sat Jul 14 2018 Fedora Release Engineering <releng@fedoraproject.org> - 2.33.1-2
- Rebuilt for https://fedoraproject.org/wiki/Fedora_29_Mass_Rebuild

* Fri Jul 06 2018 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.34
 - store, daemon, client, cmd/snap: expose "scope", default to wide*
 - tests: fix arch tests
 - snapstate: make sure all *link-*snap tasks carry a snap type and
   further hints
 - snapstate: allow setting "refresh.timer=managed"
 - cmd/snap: display a link to data privacy notice for interactive
   snap login
 - devicestate: fix race when refreshing a snap with snapd-control
 - tests: skip interfaces-framebuffer when no /dev/fb0 is found
 - tests: run interfaces-contacts-service only where test-snapd-eds
   is available
 - many: expose publisher's validation throughout the API
 - many: use extra "releases" information on store "revision-not-
   found" errors to produce better errors
 - dirs: improve distro detection for Antegros
 - Revert "dirs: improve identification of Arch Linux like systems"
 - devicestate: fix panic in firstboot code when no snaps are seeded
 - i18n: use xgettext-go --files-from to avoid running into cmdline
   size limits
 - interfaces: move ValidateName helper to utils
 - snapstate,ifstate: wait for pending restarts before auto-
   connecting
 - snap: account for parallel installs in wrappers, place info and
   tests
 - configcore: fix incorrect handling of keys with numbers (like
   gpu_mem_512)
 - tests: fix tests when no keyboard input detected
 - overlord/configstate: add watchdog options
 - snap-mgmt: fix for non-existent dbus system policy dir,
   shellchecks
 - tests/main/snapd-notify: use systemd's service properties rater
   than the journal
 - snapstate: allow removal of snap.TypeOS when using a model with a
   base
 - interfaces: make findSnapdPath smarter
 - tests: run "arp" tests only if arp is available
 - spread: increase the number of auto retries for package downloads
   in opensuse
 - cmd/snap-confine: fix nvidia support under lxd
 - corecfg: added experimental.hotplug feature flag
 - image: block installation of parallel snap instances
 - interfaces: moved normalize method to interfaces/utils and made it
   public
 - api/snapctl: allow -h and --help for regular users.
 - interfaces/udisks2: also implement implicit classic slot
 - cmd/snap-confine: include CUDA runtime libraries
 - tests: disable auto-refresh test on core18
 - many: switch to account validation: unproven|verified
 - overlord/ifacestate: get/set connection state only via helpers
 - tests: adding extra check to validate journalctl is showing
   current test data
 - data: add systemd environment configuration
 - i18n: handle write errors in xgettext-go
 - snap: helper for validating snap instance names
 - snap{/snaptest}: set instance key based on snap name
 - userd: fix running unit tests on KDE
 - tests/main/econnreset: limit ingress traffic to 512kB/s
 - snap: introduce a struct Channel to represent store channels, and
   helpers to work with it
 - tests: add fedora to distro_clean_package_cache function
 - many: rename snap.Info.StoreName() to snap.Info.SnapName()
 - tests: add spread test to ensure snapd/core18 are not removable
 - tests: tweaks for running the main tests on core18
 - overlord/{config,snap}state: introduce experimental.parallel-
   instances feature flag
 - strutil: support iteration over almost clean paths
 - strutil: add PathIterator.Rewind
 - tests: update interfaces-timeserver-control to core18
 - tests: add halt-timeout to google backend
 - tests: skip security-udev-input-subsystem without /dev/input/by-
   path
 - snap: introduce the instance key field
 - packaging/opensuse: remaining packaging updates for 2.33.1
 - overlord/snapstate: disallow installing snapd on baseless models
 - tests: disable core tests on all core systems (16 and 18)
 - dirs: improve identification of Arch Linux like systems
 - many: expose full publisher info over the snapd API
 - tests: disable core tests on all core systems (16 and 18)
 - tests/main/xdg-open: restore or clean up xdg-open
 - tests/main/interfaces-firewall-control: shellcheck fix
 - snapstate: sort "snapd" first
 - systemd: require snapd.socket in snapd.seeded.service; make sure
   snapd.seeded
 - spread-shellcheck: use the latest shellcheck available from snaps
 - tests: use "ss" instead of "netstat" (netstat is not available in
   core18)
 - data/complete: fix three out of four shellcheck warnings in
   data/complete
 - packaging/opensuse: fix typo, missing assignment
 - tests: initial core18 spread image building
 - overlord: introduce a gadget-connect task and use it at first boot
 - data/completion: fix inconsistency in +x and shebang
 - firstboot: mark essential snaps as "Required" in the state
 - spread-shellcheck: use a whitelist of files that are allowed to
   fail validation
 - packaging/opensuse: build position-independent binaries
 - ifacestate: prevent running interface hooks twice when self-
   connecting on autoconnect
 - data: remove /bin/sh from snapd.sh
 - tests: fix shellcheck 0.5.0 warnings
 - packaging/opensuse: snap-confine should be 06755
 - packaging/opensuse: ship apparmor integration if enabled
 - interfaces/udev,misc: only trigger udev events on input subsystem
   as needed
 - packaging/opensuse: add missing bits for snapd.seeded.service
 - packaging/opensuse: don't use %-macros in comments
 - tests: shellchecks part 4
 - many: rename snap.Info.Name() to snap.Info.InstanceName(), leave
   parallel-install TODOs
 - store: drop unused: channel map types, and details fixture.
 - store: have a basic test about the unmarshalling of /search
   results
 - tests: show executed tests on current system when a test fails
 - tests: fix for the download of the big snap
 - interfaces/apparmor: add chopTree
 - tests: remove double debug: | entry in tests and add more checks
 - cmd/snap-update-ns: introduce mimicRequired helper
 - interfaces: move assertions around for better failure line number
 - store: log a nice clear "download succeeded" message
 - snap: run snap-confine from the re-exec location
 - snapstate: support restarting snapd from the snapd snap on core18
 - tests: show status of the partial test-snapd-huge snap in
   econnreset test
 - tests: fix interfaces-calendar-service test when gvfsd-metadata
   loks the xdg dirctory
 - store: switch store.SnapInfo to use the new v2/info endpoint
 - interfaces: add Repository.AllInterfaces
 - snapstate: stop using evolving SnapSpec internally, use an
   internal-only snapSpec instead
 - cmd/libsnap-confine-private: introduce a helper for splitting snap
   name
 - tests: econnreset/retry tweaks
 - store, et al: kill dead code that uses the bulk endpoint
 - tests/lib/prepare-restore: fix upgrade/reboot handling on arch
 - cmd/snap-update-ns,strutil: move PathIterator to strutil, add
   Depth helper
 - data/systemd/snapd.run-from-snap: ensure snapd tooling is
   available
 - store: switch connectivity check to use v2/info
 - devicestate: support seeding from a base snap instead of core
 - snapstate,ifacestate: remove core-phase-2 handling
 - interfaces/docker-support: update for docker 18.05
 - tests: enable fedora 28 again
 - overlord/ifacestate:  simplify checkConnectConflicts and also
   connect signature
 - snap: parse connect instructions in gadget.yaml
 - tests: fix snapd-repair.timer on ubuntu-core-snapd-run- from-snap
   test
 - interfaces/apparmor: allow killing snap-update-ns
 - tests: skip "try" test on s390x
 - store, image: have 'snap download' use v2/refresh action=download
 - interfaces/policy: test that base policy can be parsed
 - tests: publish test-snapd-appstreamid for any architecture
 - snap: don't include newline in hook environment
 - cmd/snap-update-ns: use RCall with SyscallsEqual
 - cmd/snap-update-ns: add IsSnapdCreatedPrivateTmpfs and tests
 - tests: skip security-dev-input-event-denied on s390x/arm64
 - interfaces: add the dvb interface
 - daemon: paging is not a thing.
 - cmd/snap-mgmt: remove system key on purge
 - testutil: syscall sequence checker
 - cmd/snap-update-ns: fix a leaking file descriptor in MkSymlink
 - packaging: use official bolt in the errtracker on fedora
 - many: add `snap debug connectivity` command* many: add `snap debug
   connectivity` command
 - configstate: deny configuration of base snaps and for the "snapd"
   snap
 - interfaces/raw-usb: also allow usb serial devices
 - snap: reject more layout locations
 - errtracker: do not send duplicated reports
 - httputil: extra debug if an error is not retried
 - cmd/snap-update-ns: improve wording in many errors
 - cmd/snap: use snaptest.MockSnapCurrent in `snap run` tests
 - cmd/snap-update-ns: add helper for checking for read-only
   filesystems
 - interfaces/builtin/docker: use commonInterface over specific
   struct
 - testutil: add test support for Fstatfs
 - cmd/snap-update-ns: discard the concept of segments
 - cmd/libsnap-confine-private: helper for extracting store snap name
   from local-name
 - tests: fix flaky test for hooks undo
 - interfaces: add {contacts,calendar}-service interfaces
 - tests: retry 'restarting into..' match in the snap-confine-from-
   core test
 - systemd: adjust TestWriteMountUnitForDirs() to use
   squashfs.MockUseFuse(false)
 - data: add helper that can generate/start/stop the snapd service
 - sefltest: advise reboot into 4.4 on trusty running 3.13
 - selftest: add new selftest package that tests squashfs mounting
 - store, jsonutil: move store.getStructFields to
   jsonutil.StructFields
 - ifacestate: improved conflict and error handling when creating
   autoconnect tasks
 - cmd/snap-confine: applied make fmt
 - interfaces/udev: call 'udevadm settle --timeout=10' after
   triggering events
 - tests: wait more time until snap start to be downloaded on
   econnreset test
 - snapstate: ensure fakestore returns TypeOS for the core snap
 - tests: fix lxd test which hangs on restore
 - cmd/snap-update-ns: add PathIterator
 - asserts,image: add support for models with bases
 - tests: shellchecks part 3
 - overlord/hookstate: support undo for hooks
 - interfaces/tpm: Allow access to the kernel resource manager
 - tests: skip appstream-id test for core systems 32 bits
 - interfaces/home: remove redundant common interface assignment
 - tests: reprioritise a few tests that are known to be slow
 - cmd/snap: small help tweaks and fixes
 - tests: add test to ensure /dev/input/event* for non-joysticks is
   denied
 - spread-shellcheck: silly fix & pep8
 - spread: switch fedora 28 to manual
 - client,cmd/snap,daemon,tests: expose base of a snap over API, show
   it in snap info --verbose
 - tests: fix lxd test - --auto now sets up networking
 - tests: adding fedora-28 to spread.yaml
 - interfaces: add juju-client-observe interface
 - client, daemon: add a "mounted-from" entry to local snaps' JSON
 - image: set model.DisplayName() in bootenv as "snap_menuentry"
 - packaging/opensuse: Refactor packaging to support all openSUSE
   targets
 - interfaces/joystick: force use of the device cgroup with joystick
   interface
 - interfaces/hardware-observe: allow access to /etc/sensors* for
   libsensors
 - interfaces: remove Plug/Slot types
 - interface hooks: update old AutoConnect methods
 - snapcraft: run with DEB_BUILD_OPTIONS=nocheck
 - overlord/{config,snap}state: the number of inactive revisions is
   config
 - cmd/snap: check with snapd for unknown sections
 - tests: moving test helpers from sh to bash
 - data/systemd: add snapd.apparmor.service
 - many: expose AppStream IDs (AKA common ID)
 - many: hold refresh when on metered connections
 - interfaces/joystick: also support modern evdev joysticks and
   gamepads
 - xdgopenproxy: skip TestOpenUnreadableFile when run as root
 - snapcraft: use dpkg-buildpackage options that work in xenial
 - spread: openSUSE LEAP 42.2 was EOLd in January, remove it
 - get-deps: work with an unset GOPATH too
 - interfaces/apparmor: use strict template on openSUSE tumbleweed
 - packaging: filter out verbose flags from "dh-golang"
 - packaging: fix description
 - snapcraft.yaml: add minimal snapcraft.yaml with custom build

* Fri Jun 22 2018 Neal Gompa <ngompa13@gmail.com> - 2.33.1-1
- Release 2.33.1 to Fedora (RH#1567916)

* Thu Jun 21 2018 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.33.1
 - many: improve udev trigger on refresh experience
 - systemd: require snapd.socket in snapd.seeded.service
 - snap: don't include newline in hook environment
 - interfaces/apparmor: allow killing snap-update-ns
 - tests: skip "try" test on s390x
 - tests: skip security-dev-input-event-denied when /dev/input/by-
   path/ is missing
 - tests: skip security-dev-input-event-denied on s390x/arm64

* Fri Jun 08 2018 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.33
 - packaging: use official bolt in the errtracker on fedora
 - many: add `snap debug connectivity` command
 - interfaces/raw-usb: also allow usb serial devices
 - errtracker: do not send duplicated reports
 - selftest: add new selftest package that tests squashfs mounting
 - tests: backport lxd force stop and econnreset fixes
 - tests: add test to ensure /dev/input/event* for non-joysticks is
   denied
 - interfaces/joystick: support modern evdev joysticks
 - interfaces: add juju-client-observe
 - interfaces/hardware-observe: allow access to /etc/sensors* for
   libsensors
 - many: holding refresh on metered connections
 - many: expose AppStream IDs (AKA common ID)
 - tests: speed up save/restore snapd state for all-snap systems
   during tests execution
 - interfaces/apparmor: use helper to load stray profile
 - tests: ubuntu core abstraction
 - overlord/snapstate: don't panic in a corner case interaction of
   cleanup tasks and pruning
 - interfaces/apparmor: add 'mediate_deleted' profile flag for all
   snaps
 - tests: new parameter for the journalctl rate limit
 - spread-shellcheck: port to python
 - interfaces/home: add 'read' attribute to allow non-owner read to
   @{HOME}
 - testutil: import check.v1 differently to workaround gccgo error
 - interfaces/many: miscellaneous updates for default, desktop,
   desktop-legacy, system-observe, hardware-observe, opengl and gpg-
   keys
 - snapstate/hooks: reorder autoconnect and reconnect hooks
 - daemon: update unit tests to match current master
 - overlord/snapshotstate/backend: introducing the snapshot backend
 - many: support 'system' nickname in interfaces
 - userd: add the "snap" scheme to the whitelist
 - many: make rebooting of core on refresh immediate, refactor logic
   around it
 - tests/main/snap-service-timer: account for service timer being in
   the 'running' state
 - interfaces/builtin: allow access to libGLESv* too for opengl
   interface
 - daemon: fix unit tests on arch
 - interfaces/default,process-control: miscellaneous signal policy
   fixes
 - interfaces/bulitin: add write permission to optical-drive
 - configstate: validate known core.* options
 - snap, wrappers: systemd WatchdogSec support
 - ifacestate: do not auto-connect manually disconnected interfaces
 - systemd: mock useFuse() so testsuite passes in container via lxd
   snap
 - snap/env: fix env duplication logic
 - snap: some doc comments fixes and additions
 - cmd/snap-confine, interfaces/opengl: allow access to glvnd EGL
   vendor files
 - ifacestate: unify reconnect and autoconnect methods
 - tests: fix user mounts test for external systems
 - overlord/snapstate,overlord/auth,store: coalesce no auth user
   refresh requests
 - boot,partition: improve tests/docs around SetNextBoot()
 - many: improve `snap wait` command
 - snap: fix `snap interface --attrs` output when numbers are used
 - cmd/snap-update-ns: poke holes when creating source paths for
   layouts
 - snapstate: support getting new bases/default-providers on refresh
 - ifacemgr: remove stale connections on startup
 - asserts: use Attrer in policy checks
 - testutil: record system call errors / return values
 - tests: increase timeouts to make tests reliable on slow boards
 - repo: pass and return ConnRef via pointers
 - interfaces: add xdg-document-portal support to desktop interface
 - debian: add a zenity|kdialog suggests
 - snapstate: make TestDoPrereqRetryWhenBaseInFlight less brittle
 - tests: go must be installed as a classic snap
 - tests: use journalctl cursors instead rotating logs
 - daemon: add confinement-options to /v2/system-info
   daemon: refactor classic support flag to be more structured
 - tests: build spread in the autopkgtests with a more recent go
 - cmd/snap: fix the message when snap.channel != snap.tracking
 - overlord/snapstate: allow core defaults configuration via 'system'
   key
 - many: add "snap debug sandbox-features" and needed bits
 - interfaces: interface hooks for refresh
 - snapd.core-fixup.sh: add workaround for corrupted uboot.env
 - boot: clear "snap_mode" when needed
 - many: add wait command and `snapd.seeded` service
 - interfaces: move host font update-ns AppArmor rules to desktop
   interface
 - jsonutil/safejson: introducing safejson.String &
   safejson.Paragraph
 - cmd/snap-update-ns: use Secure.BindMount to bind mount files
 - cmd/snap-update-ns,tests: mimic the mode and ownership of
   directories
 - cmd/snap-update-ns: add support for ignoring mounts with missing
   source/target
 - interfaces: interface hooks implementation
 - cmd/libsnap: fix compile error on more restrictive gcc
   cmd/libsnap: fix compilation errors on gcc 8
 - interfaces/apparmor: allow bash and dash to be in /usr/bin/
 - cmd/snap-confine: allow any base snap to provide /etc/alternatives
 - tests: fix interfaces-network test for systems with partial
   confinement
 - spread.yaml: add cosmic (18.10) to autopkgtest/qemu
 - tests: ubuntu 18.04 or higher does not need linux-image-extra-
 - configcore: validate experimental.layouts option
 - interfaces:minor autoconnect cleanup
 - HACKING: fix typos
 - spread: add adt for ubuntu 18.10
 - tests: skip test lp-1721518 for arch, snapd is failing to start
   after reboot
 - interfaces/x11: allow X11 slot implementations
 - tests: checking interfaces declaring the specific interface
 - snap: improve error for snaps not available in the given context
 - cmdstate: add missing test for default timeout handling
 - tests: shellcheck spread tasks
 - cmd/snap: update install/refresh help vs --revision
 - cmd/snap-confine: add support for per-user mounts
 - snap: do not use overly short timeout in `snap
   {start,stop,restart}`
 - tests: adding google-sru backend replacing linode-sur
 - interfaces/apparmor: fix incorrect apparmor profile glob
 - systemd: replace ancient paths with 16.04+ standards
 - overlord,systemd: store snap revision in mount units
 - testutil: add test helper for SysLstat
 - testutil,cmd: rename test helper of Lstat to OsLstat
 - testutil: document all fake syscall/os functions
 - osutil,interfaces,cmd: use less hardcoded strings
 - testutil: rename UNMOUNT_NOFOLLOW to umountNoFollow
 - testutil: don't dot-import check.v1
 - store: getStructFields takes pointers now
 - tests: drop `linux-image-extra-$(uname -r)` install in 18.04
 - many: fix false negatives reported by vet
 - osutil,interfaces: use uint32 for uid, gid
 - many: fix various issues reported by shellcheck
 - tests: add pending shutdown detection
 - image: support refreshing soft-expired user macaroons in tooling
 - interfaces/builtin, daemon: cleanup mocked builtin interfaces in
   daemon tests
 - interfaces/builtin: add support for software-watchdog interface
 - spread: auto accept key changes when calling dnf
 - snap,overlord/snapstate: introduce and use BrokenSnapError
 - tests: detect kernel oops during tests and abort tests in this
   case
 - tests: bring back one missing test in snap-service-stop-mode
 - debian: update LP bug for the 2.32.5 SRU
 - userd: set up journal logging streams for autostarted apps
 - snap,tests : don't fail if we cannot stat MountFile
 - tests: smaller fixes for Arch tests
 - tests: run interfaces-broadcom-asic-control early
 - client: support for snapshot sets, snapshots, and snapshot actions
 - tests: skip interfaces-content test on core devices
 - cmd: generalize locking to global, snap and per-user locks
 - release-tools: handle the snapd-x.y.z version
 - packaging: fix incorrectly auto-generated changelog entry for
   2.32.5
 - tests: add arch to CI
 - systemd: add helper for opening stream file descriptors to the
   journal
 - cmd/snap: handle distros with no version ID
 - many: add "stop-mode: sig{term,hup,usr[12]}{,-all}" instead of
   conflating that with refresh-mode
 - tests: removing linode-sru backend
 - tests: updating bionic version for spread tests on google
 - overlord/snapstate: poll for up to 10s if a snap is unexpectedly
   not mounted in doMountSnap
 - overlord/snapstate: allow to get an error from readInfo instead of
   a broken stub, use it in doMountSnap
 - snap: snap.AppInfo is now a fmt.Stringer
 - tests: move fedora 27 to google backend
 - many: add `core.problem-reports.disabled` option
 - cmd/snap-update-ns: remove the need for stash directory in secure
   bind mount implementation
 - errtracker: check for whoopsie.service instead of reading
   /etc/whoopsie
 - cmd/snap: user session application autostart v3
 - tests: add test to ensure `snap refresh --amend` works with
   different channels
 - tests: add check for OOM error after each test
 - cmd/snap-seccomp: graceful handling of non-multilib host
 - interfaces/shutdown: allow calling SetWallMessage
 - cmd/snap-update-ns: add secure bind mount implementation for use
   with user mounts
 - snap: fix `snap advise-snap --command` output to match spec
 - overlord/snapstate: on multi-snap refresh make sure bases and core
   are finished before dependent snaps
 - overlord/snapstate: introduce envvars to control the channels for
   based and prereqs
 - cmd/snap-confine: ignore missing cgroups in snap-device-helper
 - debian: add gbp.conf script to build snapd via `gbp buildpackage`
 - daemon,overlord/hookstate: stop/wait for running hooks before
   closing the snapctl socket
 - advisor: use json for package database
 - interfaces/hostname-control: allow setting the hostname via
   syscall and systemd
 - tests/main/interfaces-opengl-nvidia: verify access to 32bit
   libraries
 - interfaces: misc updates for default, firewall-control, fuse-
   support and process-control
 - data/selinux: Give snapd access to more aspects of the system
 - many: use the new install/refresh API by switching snapstate to
   use store.SnapAction
 - errtracker: make TestJournalErrorSilentError work on gccgo
 - ifacestate: add to the repo also snaps that are pending being
   activated but have a done setup-profiles
 - snapstate, ifacestate: inject auto-connect tasks try 2
 - cmd/snap-confine: allow creating missing gl32, gl, vulkan dirs
 - errtracker: add more fields to aid debugging
 - interfaces: make system-key more robust against invalid fstab
   entries
 - overlord,interfaces: be more vocal about broken snaps and read
   errors
 - ifacestate: injectTasks helper
 - osutil: fix fstab parser to allow for # in field values
 - cmd/snap-mgmt: remove timers, udev rules, dbus policy files
 - release-tools: add repack-debian-tarball.sh
 - daemon,client: add build-id to /v2/system-info
 - cmd: make fmt (indent 2.2.11)
 - interfaces/content: add rule so slot can access writable files at
   plug's mountpoint
 - interfaces: add /var/lib/snapd/snap to @{INSTALL_DIR}
 - ifacestate: don't surface errors from stale connections
 - cmd/snap-update-ns: convert Secure* family of functions into
   methods
 - tests: adjust canonical-livepatch test on GCE
 - tests: fix quoting issues in econnreset test
 - cmd/snap-confine: make /run/media an alias of /media
 - cmd/snap-update-ns: rename i to segNum
 - interfaces/serial: change pattern not to exclude /dev/ttymxc*
 - spread: disable StartLimitInterval option on opensuse-42.3
 - configstate: give a chance to immediately recompute the next
   refresh time when schedules are set
 - cmd/snap-confine: attempt to detect if multiarch host uses
   arch triplets
 - store: add Store.SnapAction to support the new install/refresh API
   endpoint
 - tests: adding test for removable-media interface
 - tests: update interface tests to remove extra checks and normalize
   tests
 - timeutil: in Human, count days with fingers
 - vendor: update gopkg.in/yaml.v2 to the latest version
 - cmd/snap-confine: fix Archlinux compatibility
 - cmd/snapd: make sure signal handlers are established during early
   daemon startup
 - cmd/snap-confine: apparmor: allow creating prefix path for
   gl/vulkan
 - osutil: use tilde suffix for temporary files used for atomic
   replacement
 - tests: copy or sanity check core users using usernames
 - tests: disentangle etc vs extrausers in core tests
 - tests: fix snap-run tests when snapd is not running
 - overlord/configstate: change how ssh is stopped/started
 - snap: make `snap run` look at the system-key for security profiles
 - strutil, cmd/snap: drop strutil.WordWrap, first pass at
   replacement
 - tests: adding opensuse-42.3 to google
 - cmd/snap: fix one issue with noWait error handling logic, add
   tests plus other cleanups
 - cmd/snap-confine: nvidia: preserve globbed file prefix
 - advisor: add comment why osutil.FileExists(dirs.SnapCommandsDB) is
   needed
 - interfaces,release: probe seccomp features lazily
 - tests: change debug for layout test
 - advisor: deal with missing commands.db file
 - interfaces/apparmor: simplify UpdateNS internals
 - polkit: Pass caller uid to PolicyKit authority
 - tests: moving debian 9 from linode to google backend
 - cmd/snap-confine: nvidia: add tls/libnvidia-tls.so* glob
 - po: specify charset in po/snappy.pot
 - interfaces: harden snap-update-ns profile
 - snap: Call SanitizePlugsSlots from InfoFromSnapYaml
 - tests: update tests to deal with s390x quirks
 - debian: run snap.mount upgrade fixup *before* debhelper
 - tests: move xenial i386 to google backend
 - snapstate: add compat mode for default-provider
 - tests: a bunch of test fixes for s390x from looking at the
   autopkgtest logs
 - packaging: recommend "gnupg" instead of "gnupg1 | gnupg"
 - interfaces/builtin: let MM change qmi device attributes
 - tests: add workaround for s390x failure
 - snap/pack, cmd/snap: add `snap pack --check-skeleton`
 - daemon: support 'system' as nickname of the core snap
 - cmd/snap-update-ns: use x-snapd.{synthetic,needed-by} in practice
 - devicestate: add DeviceManager.Registered returning a channel
   closed when the device is known to be registered
 - store: Sections and WriteCatalogs need to strictly send device
   auth only if the device has a custom store
 - tests: add bionic system to google backend
 - many: fix shellcheck warnings in bionic
 - cmd/snap-update-ns: don't fail on existing symlinks
 - tests: make autopkgtest tests more targeted
 - cmd/snap-update-ns: fix creation of layout symlinks
 - spread,tests: move suite-level prepare/restore to central script
 - many: propagate contexts enough to be able to mark store
   operations done from the Ensure loop
 - snap: don't create empty Change with "Hold" state on disconnect
 - snap: unify snap name validation w/python; enforce length limit.
 - cmd/snap: use shlex when parsing `snap run --strace` arguments
 - osutil,testutil: add symlinkat(2) and readlinkat(2)
 - tests: autopkgtest may have non edge core too
 - tests: adding checks before stopping snapd service to avoid job
   canceled on ubuntu 14.04
 - errtracker: respect the /etc/whoopsie configuration
 - overlord/snapstate:  hold refreshes for 2h after seeding on
   classic
 - cmd/snap: tweak and polish help strings
 - snapstate: put layout feature behind feature flag
 - tests: force profile re-generation via system-key
 - snap/squashfs: when installing from seed, try symlink before cp
 - wrappers: services which are socket or timer activated should not
   be started during boot
 - many: go vet cleanups
 - tests: define MATCH from spread
 - packaging/fedora: Merge changes from Fedora Dist-Git plus trivial
   fix
 - cmd/snap: use timeutil.Human to show times in `snap refresh
   --time`
 - cmd/snap: in changes and tasks, default to human-friendly times
 - many: support holding refreshes by setting refresh.hold
 - Revert "cmd/snap: use timeutil.Human to show times in `snap
   refresh -…-time`"
 - cmd/snap: use timeutil.Human to show times in `snap refresh
   --time`
 - tests/main/snap-service-refresh-mode: refactor the test to rely on
   comparing PIDs
 - tests/main/media-sharing: improve the test to cover /media and
   /run/media
 - store: enable deltas for core devices too
 - cmd/snap: unhide --no-wait; make wait use go via waitMixin
 - strutil/shlex: import github.com/google/shlex into the tree
 - vendor: update github.com/mvo5/libseccomp-golang
 - overlord/snapstate: block install of "system"
 - cmd/snap: "current"→"installed"; "refreshed"→"refresh-date"
 - many: add the snapd-generator
 - cmd/snap-seccomp: Cancel the atomic file on error, not just Close
 - polkit: ensure error is properly set if dialog is dismissed
 - snap-confine, snap-seccomp: utilize new seccomp logging features
 - progress: tweak ansimeter cvvis use to no longer confuse minicom
 - xdgopenproxy: integrate xdg-open implementation into snapctl
 - tests: avoid removing preinstalled snaps on core
 - tests: chroot into core to run xdg-open there
 - userd: add an OpenFile method for launching local files with xdg-
   open
 - tests: moving ubuntu core from linode to google backend
 - run-checks: remove accidental bashism
 - i18n: simplify NG usage by doing the modulo math in-package.
 - snap/squashfs: set timezone when calling unsquashfs to get the
   build date
 - timeutil: timeutil.Human(t) gives a human-friendly string for t
 - snap: add autostart app property
 - tests: add support for external backend executions on listing test
 - tests: make interface-broadcom-asic-control test work on rpi
 - configstate: when disable "ssh" we must disable the "sshd" service
 - interfaces/apparmor,system-key: add upperdir snippets for strict
   snaps on livecd
 - snap/squashfs: add BuildDate
 - store: parse the JSON format used by the coming new store API to
   convey snap information
 - many: remove snapd.refresh.{timer,service}
 - tests: adding ubuntu-14.04-64 to the google backend
 - interfaces: add xdg-desktop-portal support to desktop interface
 - packaging/arch: sync with snapd/snapd-git from AUR
 - wrappers, tests/main/snap-service-timer: restore missing commit,
   add spread test for timer services
 - store: don't ask for snap_yaml_raw except on the details endpoint
 - many: generate and use per-snap snap-update-ns profile
 - tests: add debug for layout test
 - wrappers: detect whether systemd-analyze can be used in unit tests
 - osutil: allow creating strings out of MountInfoEntry
 - servicestate: use systemctl enable+start and disable+stop instead
   of --now flag
 - osutil: handle file being matched by multiple patterns
 - daemon, snap: fix InstallDate, make a method of *snap.Info
 - wrappers: timer services
 - wrappers: generator for systemd OnCalendar schedules
 - asserts: fix flaky storeSuite.TestCheckAuthority
 - tests: fix dependency for ubuntu artful
 - spread: start moving towards google backend
 - tests: add a spread test for layouts
 - ifacestate: be consistent passing Retry.After as named field
 - cmd/snap-update-ns: use recursive bind mounts for writable mimic
 - testutil: allow mocking syscall.Fstat
 - overlord/snapstate: verify that default schedule is randomized and
   is  not a single time
 - many: simplify mocking of home-on-NFS
 - cmd/snap-update-ns: use syscall.Symlink instead of os.Symlink
 - store: move infoFromRemote into details.go close to snapDetails
 - userd/tests: Test kdialog calls and mock kdialog too to make tests
   work in KDE
 - cmd/snap: tweaks to 'snap info' (feat. installed->current rename)
 - cmd/snap: add self-strace to `snap run`
 - interfaces/screen-inhibit-control,network-status: fix dbus path
   and interface typos
 - update-pot: Force xgettext() to return true
 - store: cleanup test naming, dropping remoteRepo  and
   UbuntuStore(Repository)? references
 - store: reorg auth refresh

* Wed May 16 2018 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.32.9
 - tests: run all spread tests inside GCE
 - tests: build spread in the autopkgtests with a more recent go

* Fri May 11 2018 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.32.8
 - snapd.core-fixup.sh: add workaround for corrupted uboot.env

* Fri May 11 2018 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.32.7
 - many: add wait command and seeded target (2
 - snapd.core-fixup.sh: add workaround for corrupted uboot.env
 - boot: clear "snap_mode" when needed
 - cmd/libsnap: fix compile error on more restrictive gcc
 - tests: cherry-pick commits to move spread to google backend
 - spread.yaml: add cosmic (18.10) to autopkgtest/qemu
 - userd: set up journal logging streams for autostarted apps

* Sun Apr 29 2018 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.32.6
 - snap: do not use overly short timeout in `snap
   {start,stop,restart}`
 - interfaces/apparmor: fix incorrect apparmor profile glob
 - tests: detect kernel oops during tests and abort tests in this
   case
 - tests: run interfaces-boradcom-asic-control early
 - tests: skip interfaces-content test on core devices

* Mon Apr 16 2018 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.32.5
 - many: add "stop-mode: sig{term,hup,usr[12]}{,-all}" instead of
   conflating that with refresh-mode
 - overlord/snapstate:  poll for up to 10s if a snap is unexpectedly
   not mounted in doMountSnap
 - daemon: support 'system' as nickname of the core snap

* Thu Apr 12 2018 Neal Gompa <ngompa13@gmail.com> - 2.32.4-1
- Release 2.32.4 to Fedora (RH#1553734)

* Wed Apr 11 2018 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.32.4
 - cmd/snap: user session application autostart
 - overlord/snapstate: introduce envvars to control the channels for
   bases and prereqs
 - overlord/snapstate: on multi-snap refresh make sure bases and core
   are finished before dependent snaps
 - many: use the new install/refresh /v2/snaps/refresh store API

* Wed Apr 11 2018 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.32.3.2
 - errtracker: make TestJournalErrorSilentError work on
   gccgo
 - errtracker: check for whoopsie.service instead of reading
   /etc/whoopsie

* Wed Apr 11 2018 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.32.3.1
 - debian: add gbp.conf script to build snapd via `gbp
   buildpackage`
 - tests: add check for OOM error after each test
 - cmd/snap-seccomp: graceful handling of non-multilib host
 - interfaces/shutdown: allow calling SetWallMessage
 - data/selinux: Give snapd access to more aspects of the system
 - daemon,overlord/hookstate: stop/wait for running hooks before
   closing the snapctl socket
 - cmd/snap-confine: ignore missing cgroups in snap-device-helper
 - interfaces: misc updates for default, firewall-control, fuse-
   support and process-control
 - overlord: test fix, address corner case

* Thu Apr 05 2018 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.32.3
 - ifacestate: add to the repo also snaps that are pending being
   activated but have a done setup-profiles
 - snapstate: inject autoconnect tasks in doLinkSnap for regular
   snaps
 - cmd/snap-confine: allow creating missing gl32, gl, vulkan dirs
 - errtracker: add more fields to aid debugging
 - interfaces: make system-key more robust against invalid fstab
   entries
 - cmd/snap-mgmt: remove timers, udev rules, dbus policy files
 - overlord,interfaces: be more vocal about broken snaps and read
   errors
 - osutil: fix fstab parser to allow for # in field values

* Sat Mar 31 2018 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.32.2
 - interfaces/content: add rule so slot can access writable files at
   plug's mountpoint
 - tests: adjust canonical-livepatch test on GCE
 - interfaces/serial: change pattern not to exclude /dev/ttymxc
 - spread.yaml: switch Fedora 27 tests to manual
 - store: Sections and WriteCatalogs need to strictly send device
   auth only if the device has a custom store
 - configstate: give a chance to immediately recompute the next
   refresh time when schedules are set
 - cmd/snap-confine: attempt to detect if multiarch host uses arch
   triplets
 - vendor: update gopkg.in/yaml.v2 to the latest version (#4945)

* Mon Mar 26 2018 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.32.1
 - cmd/snapd: make sure signal handlers are established during early
   daemon startup
 - osutil: use tilde suffix for temporary files used for atomic
   replacement
 - cmd/snap-confine: apparmor: allow creating prefix path for
   gl/vulkan
 - tests: disentangle etc vs extrausers in core tests
 - packaging: fix changelogs' typo

* Sat Mar 24 2018 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.32
 - snap: make `snap run` look at the system-key for security profiles
 - overlord/configstate: change how ssh is stopped/started
 - cmd/snap-confine: nvidia: preserve globbed file prefix
 - advisor: deal with missing commands.db file
 - interfaces,release: probe seccomp features lazily
 - interfaces: harden snap-update-ns profile
 - polkit: Pass caller uid to PolicyKit authority
 - tests: change debug for layout test
 - cmd/snap-confine: don't use per-snap s-u-n profile
 - many: backported fixes for layouts and symlinks
 - cmd/snap-confine: nvidia: add tls/libnvidia-tls.so* glob
 - cmd/snap-update-ns: use x-snapd.{synthetic,needed-by} in practice
 - snap: Call SanitizePlugsSlots from InfoFromSnapYaml
 - cmd/snap-confine: fix ptrace rule with snap-confine peer
 - tests: update tests to deal with s390x quirks
 - snapstate: add compat mode for default-provider"snapname:ifname"
 - snap-confine: fallback to /lib/udev/snappy-app-dev if the core is
   older
 - tests: a bunch of test fixes for s390x from looking at the
   autopkgtest logs
 - packaging: recommend "gnupg" instead of "gnupg1 | gnupg"
 - interfaces/builtin: let MM change qmi device attributes
 - debian: undo snap.mount system unit removal
 - snap: don't create empty Change with "Hold" state on disconnect
 - tests: add workaround for s390x failure
 - tests: make autopkgtest tests more targeted
 - many: propagate contexts enough to be able to mark store
   operations done from the Ensure loop
 - store: cleanup test naming, dropping remoteRepo and
   UbuntuStore(Repository)? references
 - store: reorg auth refresh
 - tests: autopkgtest may have non edge core too
 - data: translate polkit strings
 - snapstate: put layout feature behind feature flag
 - errtracker: respect the /etc/whoopsie configuration
 - overlord/snapstate: hold refreshes for 2h after seeding on classic
 - many: cherry-pick relevant `go vet` 1.10 fixes to 2.32
 - snap/squashfs: when installing from seed, try symlink before cp
 - wrappers: services which are socket or timer activated should not
   be started during boot
 - many: generate and use per-snap snap-update-ns profile
 - many: support holding refreshes by setting refresh.hold
 - snap-confine, snap-seccomp: utilize new seccomp logging features
 - many: remove snapd.refresh.{timer,service}
 - many: add the snapd-generator
 - polkit: do not shadow dbus errors, avoid panic in case of errors
 - polkit: ensure error is properly set if dialog is dismissed
 - xdgopenproxy: integrate xdg-open implementation into snapctl
 - userd: add an OpenFile method for launching local files with xdg-
   open
 - asserts:  use a timestamp for the assertion after the signing key
   has been created
 - ifacestate: be consistent passing Retry.After as named field
 - interfaces/apparmor,system-key: add upperdir snippets for strict
   snaps on livecd
   interfaces/apparmor,system-key: add upperdir snippets for strict
   snaps
 - configstate: when disable "ssh" we must disable the "sshd"
   service
 - store: don't ask for snap_yaml_raw except on the details endpoint
 - osutil: handle file being matched by multiple patterns
 - cmd/snap-update-ns: use recursive bind mounts for writable mimic
 - cmd/snap-update-ns: use syscall.Symlink instead of os.Symlink
 - interfaces/screen-inhibit-control,network-status: fix dbus path
   and interface typos
 - interfaces/network-status: fix use of '/' in interface in DBus
   rule
 - interfaces/screen-inhibit-control: fix use of '.' in path in DBus
   rule
 - overlord/snapstate: fix task iteration order in
   TestDoPrereqRetryWhenBaseInFlight
 - interfaces: add an interface for gnome-online-accounts D-Bus
   service
 - snap: pass full timer spec in `snap run --timer`
 - cmd/snap: introduce `snap run --timer`
 - snapstate: auto install default-providers for content snaps
 - hooks/strutil: limit the number of data read from the hooks to
   avoid oom
 - osutil: aggregate mockable symbols
 - tests: make sure snapd is running before attempting to remove
   leftover snaps
 - timeutil: account for 24h wrap when flattening clock spans
 - many: send  new Snap-CDN header with none or with cloud instance
   placement info as needed
 - cmd/snap-update-ns,testutil: move syscall testing helpers
 - tests: disable interfaces-location-control on s390x
 - tests: new spread test for gpio-memory-control interface
 - tests: spread test for broadcom-asic-control interface
 - tests: make restore of interfaces-password-manager-service more
   robust
 - tests/lib/prepare-restore: sync journal before rotating and
   vacuuming
 - overlord/snapstate: use spread in the default refresh schedule
 - tests: fixes for autopkgtest in bionic
 - timeutil: introduce helpers for checking it time falls inside the
   schedule
 - cmd/snap-repair,httputil: set snap-repair User-Agent on requests
 - vendor: resync formatting of vendor.json
 - snapstate/ifacestate: auto-connect tasks
 - cmd/snap: also include tracking channel in list output.
 - interfaces/apparmor: use snap revision with surrounding '.' when
   replacing in glob
 - debian,vendor: import github.com/snapcore/squashfs and use
 - many: implement "refresh-mode: {restart,endure,...}" for services
 - daemon: make the ast-inspecting test smarter; drop 'exceptions'
 - tests: new spread test for kvm interface
 - cmd/snap: tweaks to 'snap info' output
 - snap: remove underscore from version validator regexp
 - testutil: add File{Matches,Equals,Contains} checkers.
 - snap: improve the version validator's error messages.
 - osutil: refactor EnsureFileState to separate out the comparator
 - timeutil: fix scheduling on nth weekday of the month
 - cmd/snap-update-ns: small refactor for upcoming per-user mounts
 - many: rename snappy-app-dev to snap-device-helper
 - systemd: add default target for timers
 - interfaces: miscellaneous policy updates for home, opengl, time-
   control, network, et al
 - cmd/snap: linter cleanups
 - interfaces/mount: generate per-user mount profiles
 - cmd/snap: use proper help strings for `snap userd --help`
 - packaging: provide a compat symlink for snappy-app-dev
 - interfaces/time-control,netlink-audit: adjust for util-linux
   compiled with libaudit
 - tests: adding new test to validate the raw-usb interface
 - snap: add support for `snap run --gdb`
 - interfaces/builtin: allow MM to access login1
 - packaging: fix build on sbuild
 - store: revert PR#4532 and do not display displayname
 - interfaces/mount: add support for per-user mount entries
 - cmd/system-shutdown: move sync to be even more pessimistic
 - osutil: reimplement IsMounted with LoadMountInfo
 - tests/main/ubuntu-core-services: enable snapd.refresh.timer for
   the test
 - many: don't allow layout construction to silently fail
 - interfaces/apparmor: ensure snap-confine profile for reexec is
   current
 - interfaces/apparmor: generalize apparmor load and unload helpers
 - tests: removing packages which are not needed anymore to generate
   random data
 - snap: improve `snap run` comments/naming
 - snap: allow options for --strace, e.g. `snap run --strace="-tt"`
 - tests: fix spread test failures on 18.04
 - systemd: update comment on SocketsTarget
 - osutil: add and update docstrings
 - osutil: parse mount entries without options field
 - interfaces: mock away real mountinfo/fstab
 - many: move /lib/udev/snappy-app-dev to /usr/lib/snapd/snappy-app-
   dev
 - overlord/snapstate/backend: perform cleanup if snap setup fails
 - tests/lib/prepare: disable snapd.refresh.timer
 - daemon: remove redundant UserOK markings from api commands
 - snap: introduce  timer service data types and validation
 - cmd/snap: fix UX of snap services
 - daemon: allow `snapctl get` from any uid
 - debian, snap: only static link libseccomp in snap-seccomp on
   ubuntu
 - all: snap versions are now validated
 - many: add nfs-home flag to system-key
 - snap: disallow layouts in various special directories
 - cmd/snap: add help for service commands.
 - devicestate: fix autopkgtest failure in
   TestDoRequestSerialErrorsOnNoHost
 - snap,interfaces: allow using bind-file layouts
 - many: move mount code to osutil
 - snap: understand directories in layout blacklist
 - snap: use custom unsquashfsStderrWriter for unsquashfs error
   detection
 - tests/main/user-data-handling: get rid of ordering bug
 - snap: exclude `gettimeofday` from `snap run --strace`
 - tests: check if snapd.socket is active before stoping it
 - snap: sort layout elements before validating
 - strutil: introducing MatchCounter
 - snap: detect unsquashfs write failures
 - spread: add missing ubuntu-18.04-arm64 to available autopkgtest
   machines
 - cmd/snap-confine: allow mounting anywhere, effectively
 - daemon: improve ucrednet code for the snap.socket
 - release, interfaces: add new release.AppArmorFeatures helper
 - snap: apply some golint suggestions
 - many: add interfaces.SystemKey() helper
 - tests: new snaps to test installs nightly
 - tests: skip alsa interface test when the system does not have any
   audio devices
 - debian/rules: workaround for
   https://github.com/golang/go/issues/23721
 - interfaces/apparmor: early support for snap-update-ns snippets
 - wrappers: cleanup enabled service sockets
 - cmd/snap-update-ns: large refactor / update of unit tests
 - interfaces/apparmor: remove leaked future layout code
 - many: allow constructing layouts (phase 1)
 - data/systemd: for debugging/testing use /etc/environment also for
   snap-repair runs
 - cmd/snap-confine: create lib/{gl,gl32,vulkan} under /var/lib/snapd
   and chown as root:root
 - overlord/configstate/config: make [GS]etSnapConfig use *RawMessage
 - daemon: refactor snapFooMany helpers a little
 - cmd/snap-confine: allow snap-update-ns to chown things
 - interfaces/apparmor: use a helper to set the scope
 - overlord/configstate/config: make SetSnapConfig delete on empty
 - osutil: make MkdirAllChown clean the path passed in
 - many: at seeding try to capture cloud information into core config
   under "cloud"
 - cmd/snap: add completion conversion helper to increase DRY
 - many: remove "content" argument from snaptest.MockSnap()
 - osutil: allow using many globs in EnsureDirState
 - cmd/snap-confine: fix read-only filesystem when mounting nvidia
   files in biarch
 - tests: use root path to /home/test/tmp to avoid lack of space
   issue
 - packaging: create /var/lib/snapd/lib/{gl,gl32,vulkan} as part of
   packaging
 - tests: update kill-timeout focused on making tests pass on boards
 - advisor: ensure commands.db has mode 0644 and add test
 - snap: improve validation of snap layouts
 - tests: ensure disabled services are masked
 - interfaces/desktop-legacy,unity7: support gtk2/gvfs gtk_show_uri()
 - systemd, wrappers: start all snap services in one systemctl call
 - mir: software clients need access to shared memory /dev/shm/#*
 - snap: add support for `snap advise-snap pkgName`
 - snap: fix command-not-found on core devices
 - tests: new spead test for openvswitch-support interface
 - tests: add integration for local snap licenses
 - config: add (Get|Set)SnapConfig to do bulk config e.g. from
   snapshots
 - cmd/snap: display snap license information
 - tests: enable content sharing test for $SNAP
 - osutil: add ContextWriter and RunWithContext helpers.
 - osutil: add DirExists and IsDirNotExist

* Fri Mar 09 2018 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.31.2
 - many: add the snapd-generator
 - polkit: ensure error is properly set if dialog is dismissed
 - xdgopenproxy: integrate xdg-open implementation into snapctl
 - userd: add an OpenFile method for launching local files with xdg-
   open
 - configstate: when disable "ssh" we must disable the "sshd"
   service
 - many: remove snapd.refresh.{timer,service}
 - interfaces/builtin: allow MM to access login1
 - timeutil: account for 24h wrap when flattening clock spans
 - interfaces/screen-inhibit-control,network-status: fix dbus path
   and interface typos
 - systemd, wrappers: start all snap services in one systemctl
   call
 - tests: disable interfaces-location-control on s390x

* Mon Mar 05 2018 Neal Gompa <ngompa13@gmail.com> - 2.31.1-2
- Fix dependencies for devel subpackage

* Sun Mar 04 2018 Neal Gompa <ngompa13@gmail.com> - 2.31.1-1
- Release 2.31.1 to Fedora (RH#1542483)
- Drop all backported patches as they're part of this release

* Tue Feb 20 2018 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.31.1
 - tests: multiple autopkgtest related fixes for 18.04
 - overlord/snapstate: use spread in the default refresh schedule
 - timeutil: fix scheduling on nth weekday of the month
 - interfaces: miscellaneous policy updates for home, opengl, time-
   control, network, et al
 - cmd/snap: use proper help strings for `snap userd --help`
 - interfaces/time-control,netlink-audit: adjust for util-linux
   compiled with libaudit
 - rules: do not static link on powerpc
 - packaging: revert LDFLAGS rewrite again after building snap-
   seccomp
 - store: revert PR#4532 and do not display displayname
 - daemon: allow `snapctl get` from any uid
 - debian, snap: only static link libseccomp in snap-seccomp on
   ubuntu
 - daemon: improve ucrednet code for the snap.socket

* Fri Feb 09 2018 Fedora Release Engineering <releng@fedoraproject.org> - 2.30-2
- Rebuilt for https://fedoraproject.org/wiki/Fedora_28_Mass_Rebuild

* Tue Feb 06 2018 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.31
 - cmd/snap-confine: allow snap-update-ns to chown things
 - cmd/snap-confine: fix read-only filesystem when mounting nvidia
   files in biarch
 - packaging: create /var/lib/snapd/lib/{gl,gl32,vulkan} as part of
   packaging
 - advisor: ensure commands.db has mode 0644 and add test
 - interfaces/desktop-legacy,unity7: support gtk2/gvfs gtk_show_uri()
 - snap: improve validation of snap layoutsRules for validating
   layouts:
 - snap: fix command-not-found on core devices
 - cmd/snap: display snap license information
 - tests: enable content sharing test for $SNAP
 - userd: add support for a simple UI that can be used from userd
 - snap-confine/nvidia: Support legacy biarch trees for GLVND systems
 - tests: generic detection of gadget and kernel snaps
 - cmd/snap-update-ns: refactor and improve Change.Perform to handle
   EROFS
 - cmd/snap: improve output when snaps were found in a section or the
   section is invalid
 - cmd/snap-confine,tests: hide message about stale base snap
 - cmd/snap-mgmt: fix out of source tree build
 - strutil/quantity: new package that exports formatFoo (from
   progress)
 - cmd/snap: snap refresh --time with new and legacy schedules
 - state: unknown tasks handler
 - cmd/snap-confine,data/systemd: fix removal of snaps inside LXD
 - snap: add io.snapcraft.Settings to `snap userd`
 - spread: remove more EOLed releases
 - snap: tidy up top-level help output
 - snap: fix race in `snap run --strace`
 - tests: update "searching" test to match store changes
 - store: use the "publisher" when populating the "publisher" field
 - snap: make `snap find --section` show all sections
 - tests: new test to validate location control interface
 - many: add new `snap refresh --amend <snap>` command
 - tests/main/kernel-snap-refresh-on-core: skip the whole test if
   edge and stable are the same version
 - tests: set test kernel-snap-refresh-on-core to manual
 - tests: new spread test for interface gpg-keys
 - packaging/fedora: Merge changes from Fedora Dist-Git plus trivial
   fix
 - interfaces: miscellaneous policy updates
 - interfaces/builtin: Replace Solus support with GLVND support
 - tests/main/kernel-snap-refresh-on-core: do not fail if edge and
   stable kernels are the same version
 - snap: add `snap run --strace` to be able to strace snap apps
 - tests: new spread test for ssh-keys interface
 - errtracker: include detected virtualisation
 - tests: add new kernel refresh/revert test for spread-cron
 - interfaces/builtin: blacklist zigbee dongle
 - cmd/snap-confine: discard stale mount namespaces
 - cmd: remove unused execArg0/execEnv
 - snap,interfaces/mount: disallow nobody/nogroup
 - cmd/snap: improve `snap aliases` output when no aliases are
   defined
 - tests/lib/snaps/test-snapd-service: refactor service reload
 - tests: new spread test for gpg-public-keys interface
 - tests: new spread test for ssh-public-keys interface
 - spread: setup machine creation on Linode
 - interfaces/builtin: allow introspecting UDisks2
 - interfaces/builtin: add support for content "source" section
 - tests: new spread test for netlink-audit interface
 - daemon: avoid panic'ing building an error response w/no snaps
   given
 - interfaces/mount,snap: early support for snap layouts
 - daemon: unlock state even if RefreshSchedule() fails
 - arch: add "armv8l" to ubuntuArchFromKernelArch table
 - tests: fix for test interface-netlink-connector
 - data/dbus: add AssumedAppArmorLabel=unconfined
 - advisor: use forked bolt to make it work on ppc
 - overlord/snapstate: record the 'kind' of conflicting change
 - dirs: fix snap mount dir on Manjaro
 - overlord/{snapstate,configstate}, daemon: introduce refresh.timer,
   fallback to refresh.schedule
 - config: add support for `snap set core proxy.no_proxy=...`
 - snap-mgmt: extend spread tests, stop, disable and cleanup snap
   services
 - spread.yaml: add fedora 27
 - cmd/snap-confine: allow snap-update-ns to poke writable holes in
   $SNAP
 - packaging/14.04: move linux-generic-lts-xenial to recommends
 - osutil/sys: ppc has 32-bit getuid already
 - snapstate: make no autorefresh message clearer
 - spread: try to enable Fedora once more
 - overlord/snapstate: do a minimal sanity check on containers
 - configcore: ensure config.txt has a final newline
 - cmd/libsnap-confine-private: print failed mount/umount regardless
   of SNAP_CONFINE_DEBUG
 - debian/tests: add missing autopkgtest test dependencies for debian
 - image: port ini handling to goconfigparser
 - tests/main/snap-service-after-before: add test for after/before
   service ordering
 - tests: enabling opensuse for tests
 - tests: update auto-refresh-private to match messages from current
   master
 - dirs: check if distro 'is like' fedora when picking path to
   libexecdir
 - tests: fix "job canceled" issue and improve cleanup for snaps
 - cmd/libsnap-confine-private: add debug build of libsnap-confine-
   private.a, link it into snap-confine-debug
 - vendor: remove x/sys/unix to fix builds on arm64 and powerpc
 - image: let consume snapcraft export-login files from tooling
 - interfaces/mir: allow Wayland socket and non-root sockets
 - interfaces/builtin: use snap.{Plug,Slot}Info over
   interfaces.{Plug,Slot}
 - tests: add simple snap-mgmt test
 - wrappers: autogenerate After/Before in systemd's service files for
   apps
 - snap: add usage hints in `snap download`
 - snap: provide more meaningful errors for installMany and friends
 - cmd/snap: show header/footer when `snap find` is used without
   arguments
 - overlord/snapstate: for Enable's tasks refer to the first task
   with snap-setup, do not duplicate
 - tests: add hard-coded fully expired macaroons to run related tests
 - cmd/snap-update-ns: new test features
 - cmd/snap-update-ns: we don't want to bind mount symlinks
 - interfaces/mount: test OptsToCommonFlags, filter out x-snapd.
   options
 - cmd/snap-update-ns: untangle upcoming cyclic initialization
 - client, daemon: update user's email when logging in with new
   account
 - tests: ensure snap-confine apparmor profile is parsable
 - snap: do not leak internal errors on install/refresh etc
 - snap: fix missing error check when multiple snaps are refreshed
 - spread: trying to re-enable tests on Fedora
 - snap: fix gadget.yaml parsing for multi volume gadgets
 - snap: give the snap.Container interface a Walk method
 - snap: rename `snap advise-command` to `snap advise-snap --command`
 - overlord/snapstate: no refresh just for hints if there was a
   recent regular full refresh
 - progress: switch ansimeter's Spin() to use a spinner
 - snap: support `command-not-found` symlink for `snap advise-
   command`
 - daemon: store email, ID and macaroon when creating a new user
 - snap: app startup after/before validation
 - timeutil: refresh timer take 2
 - store, daemon/api: Rename MyAppsServer, point to
   dashboard.snapcraft.io instead
 - tests: use "quiet" helper instead of "dnf -q" to get errors on
   failures
 - cmd/snap-update-ns: improve mocking for tests
 - many: implement the advisor backend, populate it from the store
 - tests: make less calls to the package manager
 - tests/main/confinement-classic: enable the test on Fedora
 - snap: do not leak internal network errors to the user
 - snap: use stdout instead of stderr for "fetching" message
 - tests: fix test whoami, share successful_login.exp
 - many: refresh with appropriate creds
 - snap: add new `snap advice-command` skeleton
 - tests: add test that ensures we never parse versions as numbers
 - overlord/snapstate: override Snapstate.UserID in refresh if the
   installing user is gone
 - interfaces: allow socket "shutdown" syscall in default profile
 - snap: print friendly message if `snap keys` is empty
 - cmd/snap-update-ns: add execWritableMimic
 - snap: make `snap info invalid-snap` output more user friendly
 - cmd/snap,  tests/main/classic-confinement: fix snap-exec path when
   running under classic confinement
 - overlord/ifacestate: fix disable/enable cycle to setup security
 - snap: fix snap find " " output
 - daemon: add new polkit action to manage interfaces
 - packaging/arch: disable services when removing
 - asserts/signtool: support for building tools on top that fill-
   in/compute some headers
 - cmd: clarify "This leaves %s tracking %s." message
 - daemon: return "bad-query" error kind for store.ErrBadQuery
 - taskrunner/many: KnownTaskKinds helper
 - tests/main/interfaces-fuse_support: fix confinement, allow
   unmount, fix spread tests
 - snap: use the -no-fragments mksquashfs option
 - data/selinux: allow messages from policykit
 - tests: fix catalog-update wait loop
 - tests/lib/prepare-restore: disable rate limiting in journald
 - tests: change interfaces-fuse_support to be debug friendly
 - tests/main/postrm-purge: stop snapd before purge
 - This is an example of test log:https://paste.ubuntu.com/26215170/
 - tests/main/interfaces-fuse_support: dump more debugging
   information
 - interfaces/dbus: adjust slot policy for listen, accept and accept4
   syscalls
 - tests: save the snapd-state without compression
 - tests/main/searching: handle changes in featured snaps list
 - overlord/snapstate: fix auto-refresh summary for 2 snaps
 - overlord/auth,daemon: introduce an explicit auth.ErrInvalidUser
 - interfaces: add /proc/partitions to system-observe (This addresses
   LP#1708527.)
 - tests/lib: introduce helpers for setting up /dev/random using
   /dev/urandom in project prepare
 - tests: new test for interface network status
 - interfaces: interfaces: also add an app/hook-specific udev RUN
   rule for hotplugging
 - tests: fix external backend for tests that need DEBUG output
 - tests: do not disable refresh timer on external backend
 - client: send all snap related bool json fields
 - interfaces/desktop,unity7: allow status/activate/lock of
   screensavers
 - tests/main: source mkpinentry.sh
 - tests: fix security-device-cgroups-serial-port test for rpi and db
 - cmd/snap-mgmt: add more directories for cleanup and refactor
   purge() code
 - snap: YAML and data structures for app before/after ordering
 - tests: set TRUST_TEST_KEYS=false for all the external backends
 - packaging/arch: install snap-mgmt tool
 - tests: add support on tests for cm3 gadget
 - interfaces/removable-media: also allow 'k' (lock)
 - interfaces: use ConnectedPlug/ConnectedSlot types (step 2)
 - interfaces: rename sanitize methods
 - devicestate: fix misbehaving test when using systemd-resolved
 - interfaces: added Ref() helpers, restored more detailed error
   message on spi iface
 - debian: make "gnupg" a recommends
 - interfaces/many: misc updates for default, browser-support,
   opengl, desktop, unity7, x11
 - interfaces: PlugInfo/SlotInfo/ConnectedPlug/ConnectedSlot
   attribute helpers
 - interfaces: update fixme comments
 - tests: make interfaces-snapd-control-with-manage more robust
 - userd: generalize dbusInterface
 - interfaces: use ConnectedPlug/ConnectedSlot types (step 1)
 - hookstate: add compat "configure-snapd" task.
 - config, overlord/snapstate, timeutil: rename ParseSchedule to
   ParseLegacySchedule
 - tests: adding tests for time*-control interfaces
 - tests: new test to check interfaces after reboot the system
 - cmd/snap-mgmt: fixes
 - packaging/opensuse-42.2: package and use snap-mgmt
 - corecfg: also "mask" services when disabling them
 - cmd/snap-mgmt: introduce snap-mgmt tool
 - configstate: simplify ConfigManager
 - interfaces: add gpio-memory-control interface
 - cmd: disable check-syntax-c
 - packaging/arch: add bash-completion as optional dependency
 - corecfg: rename package to overlord/configstate/configcore
 - wrappers: fix unit tests to use dirs.SnapMountDir
 - osutil/sys: reimplement getuid and chown with the right int type
 - interfaces-netlink-connector: fix sourcing snaps.sh

* Thu Jan 25 2018 Neal Gompa <ngompa13@gmail.com> - 2.30-1
- Release 2.30 to Fedora (RH#1527519)
- Backport fix to correctly locate snapd libexecdir on Fedora derivatives (RH#1536895)
- Refresh SELinux policy fix patches with upstream backport version

* Mon Dec 18 2017 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.30
 - tests: set TRUST_TEST_KEYS=false for all the external backends
 - tests: fix external backend for tests that need DEBUG output
 - tests: do not disable refresh timer on external backend
 - client: send all snap related bool json fields
 - interfaces: interfaces: also add an app/hook-specific udev RUN
   rule for hotplugging
 - interfaces/desktop,unity7: allow status/activate/lock of
   screensavers
 - tests/main: source mkpinentry.sh
 - devicestate: use a different nowhere domain
 - interfaces: add ssh-keys, ssh-public-keys, gpg-keys and gpg-public
   keys interfaces
 - interfaces/many: misc updates for default, browser-support, opengl,
   desktop, unity7, x11
 - devicestate: fix misbehaving test when using systemd-resolved
 - interfaces/removable-media: also allow 'k' (lock)
 - interfaces/many: misc updates for default, browser-support,
   opengl, desktop, unity7, x11
 - corecfg: also "mask" services when disabling them
 - tests: add support for autopkgtests on s390x
 - snapstate: support for pre-refresh hook
 - many: allow to configure core before it is installed
 - devicestate: fix unkeyed fields error
 - snap-confine: create mount target for lib32,vulkan on demand
 - snapstate: add support for refresh.schedule=managed
 - cmd/snap-update-ns: teach update logic to handle synthetic changes
 - many: remove configure-snapd task again and handle internally
 - snap: fix TestDirAndFileMethods() test to work with gccgo
 - debian: ensure /var/lib/snapd/lib/vulkan is available
 - cmd/snap-confine: use #include instead of bare include
 - snapstate: store userID in snapstate
 - snapd.dirs: add var/lib/snapd/lib/gl32
 - timeutil, overlod/snapstate: cleanup remaining pieces of timeutil
   weekday support
 - packaging/arch: install missing directories, manpages and version
   info
 - snapstate,store: store if a snap is a paid snap in the sideinfo
 - packaging/arch: pre-create snapd directories when packaging
 - tests/main/manpages: set LC_ALL=C as man may complain if the
   locale is unset or unsupported
 - repo: ConnectedPlug and ConnectedSlot types
 - snapd: fix handling of undo in the taskrunner
 - store: fix download caching and add integration test
 - snapstate: move autorefresh code into autoRefresh helper
 - snapctl: don't error out on start/stop/restart from configure hook
   during install or refresh
 - cmd/snap-update-ns: add planWritableMimic
 - deamon: don't omit responses, even if null
 - tests: add test for frame buffer interface
 - tests/lib: fix shellcheck errors
 - apparmor: generate the snap-confine re-exec profile for
   AppArmor{Partial,Full}
 - tests: remove obsolete workaround
 - snap: use existing files in `snap download` if digest/size matches
 - tests: merge pepare-project.sh into prepare-restore.sh
 - tests: cache snaps to $TESTSLIB/cache
 - tests: set -e, -o pipefail in prepare-restore.sh
 - apparmor: generate the snap-confine re-exec profile for
   AppArmor{Partial,Full}
 - cmd/snap-seccomp: fix uid/gid restrictions tests on Arch
 - tests: document and slightly refactor prepare/restore code
 - snapstate: ensure RefreshSchedule() gives accurate results
 - snapstate: add new refresh-hints helper and use it
 - spread.yaml,tests: move most of project-wide prepare/restore to
   separate file
 - timeutil: introduce helpers for weekdays and TimeOfDay
 - tests: adding new test for uhid interface
 - cmd/libsnap: fix parsing of empty mountinfo fields
 - overlord/devicestate:  best effort to go to early full retries for
   registration on the like of DNS no host
 - spread.yaml: bump delta ref to 2.29
 - tests: adding test to test physical memory observe interface
 - cmd, errtracker: get rid of SNAP_DID_REEXEC environment
 - timeutil: remove support to parse weekday schedules
 - snap-confine: add workaround for snap-confine on 4.13/upstream
 - store: do not log the http body for catalog updates
 - snapstate: move catalogRefresh into its own helper
 - spread.yaml: fix shellcheck issues and trivial refactor
 - spread.yaml: move prepare-each closer to restore-each
 - spread.yaml: increase workers for opensuse to 3
 - tests: force delete when tests are restore to avoid suite failure
 - test: ignore /snap/README
 - interfaces/opengl: also allow read on 'revision' in
   /sys/devices/pci...
 - interfaces/screen-inhibit-control: fix case in screen inhibit
   control
 - asserts/sysdb: panic early if pointed to staging but staging keys
   are not compiled-in
 - interfaces: allow /bin/chown and fchownat to root:root
 - timeutil: include test input in error message in
   TestParseSchedule()
 - interfaces/browser-support: adjust base declaration for auto-
   connection
 - snap-confine: fix snap-confine under lxd
 - store: bit less aggressive retry strategy
 - tests: add new `fakestore new-snap-{declaration,revision}` helpers
 - cmd/snap-update-ns: add secureMkfileAll
 - snap: use field names when initializing composite literals
 - HACKING: fix path in snap install
 - store: add support for flags in ListRefresh()
 - interfaces: remove invalid plugs/slots from SnapInfo on
   sanitization.
 - debian: add missing udev dependency
 - snap/validate: extend socket validation tests
 - interfaces: add "refresh-schedule" attribute to snapd-control
 - interfaces/builtin/account_control: use gid owning /etc/shadow to
   setup seccomp rules
 - cmd/snap-update-ns: tweak changePerform
 - interfaces,tests: skip unknown plug/slot interfaces
 - tests: disable interfaces-network-control-tuntap
 - cmd: use a preinit_array function rather than parsing
   /proc/self/cmdline
 - interfaces/time*_control: explicitly deny noisy read on
   /proc/1/environ
 - cmd/snap-update-ns: misc cleanups
 - snapd: allow hooks to have slots
 - fakestore: add go-flags to prepare for `new-snap-declaration` cmd
 - interfaces/browser-support: add shm path for nwjs
 - many: add magic /snap/README file
 - overlord/snapstate: support completion for command aliases
 - tests: re-enable tun/tap test on Debian
 - snap,wrappers: add support for socket activation
 - repo: use PlugInfo and SlotInfo for permanent plugs/slots
 - tests/interfaces-network-control-tuntap: disable on debian-
   unstable for now
 - cmd/snap-confine: Loosen the NVIDIA Vulkan ICD glob
 - cmd/snap-update-ns: detect and report read-only filesystems
 - cmd/snap-update-ns: re-factor secureMkdirAll into
   secureMk{Prefix,Dir}
 - run-checks, tests/lib/snaps/: shellcheck fixes
 - corecfg: validate refresh.schedule when it is applied
 - tests: adjust test to match stderr
 - snapd: fix snap cookie bugs
 - packaging/arch: do not quote MAKEFLAGS
 - state: add change.LaneTasks helper
 - cmd/snap-update-ns: do not assume 'nogroup' exists
 - tests/lib: handle distro specific grub-editenv naming
 - cmd/snap-confine: Add missing bi-arch NVIDIA filesthe
   `/var/lib/snapd/lib/gl:/var/lib/snapd/lib/gl/vdpau` paths within
 - cmd: Support exposing NVIDIA Vulkan ICD files to the snaps
 - cmd/snap-confine: Implement full 32-bit NVIDIA driver support
 - packaging/arch: packaging update
 - cmd/snap-confine: Support bash as base runtime entry
 - wrappers: do not error on incorrect Exec= lines
 - interfaces: fix udev tagging for hooks
 - tests/set-proxy-store: exclude ubuntu-core-16 via systems: key
 - tests: new tests for network setup control and observe interfaces
 - osutil: add helper for obtaining group ID of given file path
 - daemon,overlord/snapstate: return snap-not-installed error in more
   cases
 - interfaces/builtin/lxd_support: allow discovering of host's os-
   release
 - configstate: add support for configure-snapd for
   snapstate.IgnoreHookError
 - tests:  add a spread test for proxy.store setting together with
   store assertion
 - cmd/snap-seccomp: do not use group 'shadow' in tests
 - asserts/assertstest:  fix use of hardcoded value when the passed
   or default keys should be used
 - interfaces/many: misc policy updates for browser-support, cups-
   control and network-status
 - tests: fix xdg-open-compat
 - daemon: for /v2/logs, 404 when no services are found
 - packaging/fedora: Merge changes from Fedora Dist-Git
 - cmd/snap-update-ns: add new helpers for mount entries
 - cmd/snap-confine: Respect biarch nature of libdirs
 - cmd/snap-confine: Ensure snap-confine is allowed to access os-
   release
 - cmd: fix re-exec bug with classic confinement for host snapd <
   2.28
 - interfaces/kmod: simplify loadModules now that errors are ignored
 - tests: disable xdg-open-compat test
 - tests: add test that checks core reverts on core devices
 - dirs: use alt root when checking classic confinement support
   without …
 - interfaces/kmod: treat failure to load module as non-fatal
 - cmd/snap-update-ns: fix golint and some stale comments
 - corecfg:  support setting proxy.store if there's a matching store
   assertion
 - overlord/snapstate: toggle ignore-validation as needed as we do
   for channel
 - tests: fix security-device-cgroup* tests on devices with
   framebuffer
 - interfaces/raw-usb: match on SUBSYSTEM, not SUBSYSTEMS
 - interfaces: add USB interface number attribute in udev rule for
   serial-port interface
 - overlord/devicestate: switch to the new endpoints for registration
 - snap-update-ns: add missing unit test for desired/current profile
   handling
 - cmd/{snap-confine,libsnap-confine-private,snap-shutdown}: cleanup
   low-level C bits
 - ifacestate: make interfaces.Repository available via state cache
 - overlord/snapstate: cleanups around switch-snap*
 - cmd/snapd,client,daemon: display ignore-validation flag through
   the notes mechanism
 - cmd/snap-update-ns: add logging to snap-update-ns
 - many: have a timestamp on store assertions
 - many: lookup and use the URL from a store assertion if one is set
   for use
 - tests/test-snapd-service: fix shellcheck issues
 - tests: new test for hardware-random-control interface
 - tests: use `snap change --last=install` in snapd-reexec test
 - repo, daemon: use PlugInfo, SlotInfo
 - many: handle core configuration internally instead of using the
   core configure hook
 - tests: refactor and expand content interface test
 - snap-seccomp: skip in-kernel bpf tests for socket() in trusty/i386
 - cmd/snap-update-ns: allow Change.Perform to return changes
 - snap-confine: Support biarch Linux distribution confinement
 - partition/ubootenv: don't panic when uboot.env is missing the eof
   marker
 - cmd/snap-update-ns: allow fault injection to provide dynamic
   result
 - interfaces/mount: exspose mount.{Escape,Unescape}
 - snapctl: added long help to stop/start/restart command
 - cmd/snap-update-ns: create missing mount points automatically.
 - cmd: downgrade log message in InternalToolPath to Debugf()
 - tests: wait for service status change & file update in the test to
   avoid races
 - daemon, store: forward SSO invalid credentials errors as 401
   Unauthorized responses
 - spdx: fix for WITH syntax, require a license name before the
   operator
 - many: reorg things in preparation to make handling of the base url
   in store dynamic
 - hooks/configure: queue service restarts
 - cmd/snap: warn when a snap is not from the tracking channel
 - interfaces/mount: add support for parsing x-snapd.{mode,uid,gid}=
 - cmd/snap-confine: add detection of stale mount namespace
 - interfaces: add plugRef/slotRef helpers for PlugInfo/SlotInfo
 - tests: check for invalid udev files during all tests
 - daemon: use newChange() in changeAliases for consistency
 - servicestate: use taskset
 - many: add support for /home on NFS
 - packaging,spread: fix and re-enable opensuse builds

* Sun Dec 17 2017 Neal Gompa <ngompa13@gmail.com> - 2.29.4-3
- Add patch to SELinux policy to allow snapd to receive replies from polkit

* Sun Nov 19 2017 Neal Gompa <ngompa13@gmail.com> - 2.29.4-2
- Add missing bash completion files and cache directory

* Sun Nov 19 2017 Neal Gompa <ngompa13@gmail.com> - 2.29.4-1
- Release 2.29.4 to Fedora (RH#1508433)
- Install Polkit configuration (RH#1509586)
- Drop changes to revert cheggaaa/pb import path used

* Fri Nov 17 2017 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.29.4
 - snap-confine: fix snap-confine under lxd
 - tests: disable classic-ubuntu-core-transition on i386 temporarily
 - many: reject bad plugs/slots
 - interfaces,tests: skip unknown plug/slot interfaces
 - store: enable "base" field from the store
 - packaging/fedora: Merge changes from Fedora Dist-Git

* Thu Nov 09 2017 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.29.3
 - daemon: cherry-picked /v2/logs fixes
 - cmd/snap-confine: Respect biarch nature of libdirs
 - cmd/snap-confine: Ensure snap-confine is allowed to access os-
   release
 - interfaces: fix udev tagging for hooks
 - cmd: fix re-exec bug with classic confinement for host snapd
 - tests: disable xdg-open-compat test
 - cmd/snap-confine: add slave PTYs and let devpts newinstance
   perform mediation
 - interfaces/many: misc policy updates for browser-support, cups-
   control and network-status
 - interfaces/raw-usb: match on SUBSYSTEM, not SUBSYSTEMS
 - tests: fix security-device-cgroup* tests on devices with
   framebuffer

* Fri Nov 03 2017 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.29.2
  - snapctl: disable stop/start/restart (2.29)
  - cmd/snap-update-ns: fix collection of changes made

* Fri Nov 03 2017 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.29.1
 - interfaces: fix incorrect signature of ofono DBusPermanentSlot
 - interfaces/serial-port: udev tag plugged slots that have just
   'path' via KERNEL
 - interfaces/hidraw: udev tag plugged slots that have just 'path'
   via KERNEL
 - interfaces/uhid: unconditionally add existing uhid device to the
   device cgroup
 - cmd/snap-update-ns: fix mount rules for font sharing
 - tests: disable refresh-undo test on trusty for now
 - tests: use `snap change --last=install` in snapd-reexec test
 - Revert " wrappers: fail install if exec-line cannot be re-written
 - interfaces: don't udev tag devmode or classic snaps
 - many: make ignore-validation sticky and send the flag with refresh
   requests

* Mon Oct 30 2017 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.29
 - interfaces/many: miscellaneous updates based on feedback from the
   field
 - snap-confine: allow reading uevents from any where in /sys
 - spread: add bionic beaver
 - debian: make packaging/ubuntu-14.04/copyright a real file again
 - tests: cherry pick the fix for services test into 2.29
 - cmd/snap-update-ns: initialize logger
 - hooks/configure: queue service restarts
 - snap-{confine,seccomp}: make @unrestricted fully unrestricted
 - interfaces: clean system apparmor cache on core device
 - debian: do not build static snap-exec on powerpc
 - snap-confine: increase sanity_timeout to 6s
 - snapctl: cherry pick service commands changes
 - cmd/snap: tell translators about arg names and descs req's
 - systemd: run all mount units before snapd.service to avoid race
 - store: add a test to show auth failures are forwarded by doRequest
 - daemon: convert ErrInvalidCredentials to a 401 Unauthorized error.
 - store: forward on INVALID_CREDENTIALS error as
   ErrInvalidCredentials
 - daemon: generate a forbidden response message if polkit dialog is
   dismissed
 - daemon: Allow Polkit authorization to cancel changes.
 - travis: switch to container based test runs
 - interfaces: reduce duplicated code in interface tests mocks
 - tests: improve revert related testing
 - interfaces: sanitize plugs and slots early in ReadInfo
 - store: add download caching
 - preserve TMPDIR and HOSTALIASES across snap-confine invocation
 - snap-confine: init all arrays with `= {0,}`
 - tests: adding test for network-manager interface
 - interfaces/mount: don't generate legacy per-hook/per-app mount
   profiles
 - snap: introduce structured epochs
 - tests: fix interfaces-cups-control test for cups-2.2.5
 - snap-confine: cleanup incorrectly created nvidia udev tags
 - cmd/snap-confine: update valid security tag regexp
 - cmd/libsnap: enable two stranded tests
 - cmd,packaging: enable apparmor on openSUSE
 - overlord/ifacestate: refresh all security backends on startup
 - interfaces/dbus: drop unneeded check for
   release.ReleaseInfo.ForceDevMode
 - dbus: ensure io.snapcraft.Launcher.service is created on re-
   exec
 - overlord/auth: continue for now supporting UBUNTU_STORE_ID if the
   model is generic-classic
 - snap-confine: add support for handling /dev/nvidia-modeset
 - interfaces/network-control: remove incorrect rules for tun
 - spread: allow setting SPREAD_DEBUG_EACH=0 to disable debug-each
   section
 - packaging: remove .mnt files on removal
 - tests: fix econnreset scenario when the iptables rule was not
   created
 - tests: add test for lxd interface
 - run-checks: use nakedret static checker to check for naked
   returns on long functions
 - progress: be more flexible in testing ansimeter
 - interfaces: fix udev rules for tun
 - many: implement our own ANSI-escape-using progress indicator
 - snap-exec: update tests to follow main_test pattern
 - snap: support "command: foo $ENV_STRING"
 - packaging: update nvidia configure options
 - snap: add new `snap pack` and use in tests
 - cmd: correctly name the "Ubuntu" and "Arch" NVIDIA methods
 - cmd: add autogen case for solus
 - tests: do not use http://canihazip.com/ which appears to be down
 - hooks: commands for controlling own services from snapctl
 - snap: refactor cmdGet.Execute()
 - interfaces/mount: make Change.Perform testable and test it
 - interfaces/mount,cmd/snap-update-ns: move change code
 - snap-confine: is_running_on_classic_distribution() looks into os-
   release
 - interfaces: misc updates for default, browser-support, home and
   system-observe
 - interfaces: deny lttng by default
 - interfaces/lxd: lxd slot implementation can also be an app snap
 - release,cmd,dirs: Redo the distro checks to take into account
   distribution families
 - cmd/snap: completion for alias and unalias
 - snap-confine: add new SC_CLEANUP and use it
 - snap: refrain from running filepath.Base on random strings
 - cmd/snap-confine: put processes into freezer hierarchy
 - wrappers: fail install if exec-line cannot be re-written
 - cmd/snap-seccomp,osutil: make user/group lookup functions public
 - snapstate: deal with snap user data in the /root/ directory
 - interfaces: Enhance full-confinement support for biarch
   distributions
 - snap-confine: Only attempt to copy/mount NVIDIA libs when NVIDIA
   is used
 - packaging/fedora: Add Fedora 26, 27, and Rawhide symlinks
 - overlord/snapstate: prefer a smaller corner case for doing the
   wrong thing
 - cmd/snap-repair:  set user agent for snap-repair http requests
 - packaging: bring down the delta between 14.04 and 16.04
 - snap-confine: Ensure lib64 biarch directory is respected
 - snap-confine: update apparmor rules for fedora based base snaps
 - tests: Increase SNAPD_CONFIGURE_HOOK_TIMEOUT to 3 minutes to
   install real snaps
 - daemon: use client.Snap instead of map[string]interface{} for
   snaps.
 - hooks: rename refresh hook to post-refresh
 - git: make the .gitingore file a bit more targeted
 - interfaces/opengl: don't udev tag nvidia devices and use snap-
   confine instead
 - cmd/snap-{confine,update-ns}: apply mount profiles using snap-
   update-ns
 - cmd: update "make hack"
 - interfaces/system-observe: allow clients to enumerate DBus
   connection names
 - snap-repair: implement `snap-repair {list,show}`
 - dirs,interfaces: create snap-confine.d on demand when re-executing
 - snap-confine: fix base snaps on core
 - cmd/snap-repair: fix tests when running as root
 - interfaces: add Connection type
 - cmd/snap-repair: skip disabled repairs
 - cmd/snap-repair: prefer leaking unmanaged fds on test failure over
   closing random ones
 - snap-repair: make `repair` binary available for repair scripts
 - snap-repair: fix missing Close() in TestStatusHappy
 - cmd/snap-confine,packaging: import snapd-generated policy
 - cmd/snap: return empty document if snap has no configuration
 - snap-seccomp: run secondary-arch tests via gcc-multilib
 - snap: implement `snap {repair,repairs}` and pass-through to snap-
   repair
 - interfaces/builtin: allow receiving dbus messages
 - snap-repair: implement `snap-repair {done,skip,retry}`
 - data/completion: small tweak to snap completion snippet
 - dirs: fix classic support detection
 - cmd/snap-repair: integrate root public keys for repairs
 - tests: fix ubuntu core services
 - tests: add new test that checks that the compat snapd-xdg-open
   works
 - snap-confine: improve error message if core/u-core cannot be found
 - tests: only run tests/regression/nmcli on amd64
 - interfaces: mount host system fonts in desktop interface
 - interfaces: enable partial apparmor support
 - snapstate: auto-install missing base snaps
 - spread: work around temporary packaging issue in debian sid
 - asserts,cmd/snap-repair: introduce a mandatory summary for repairs
 - asserts,cmd/snap-repair: represent RepairID internally as an int
 - tests: test the real "xdg-open" from the core snap
 - many: implement fetching sections and package names periodically.
 - interfaces/network: allow using netcat as client
 - snap-seccomp, osutil: use osutil.AtomicFile in snap-seccomp
 - snap-seccomp: skip mknod syscall on arm64
 - tests: add trivial canonical-livepatch test
 - tests: add test that ensures that all core services are working
 - many: add logger.MockLogger() and use it in the tests
 - snap-repair: fix test failure in TestRepairHitsTimeout
 - asserts: add empty values check in HeadersFromPrimaryKey
 - daemon: remove unused installSnap var in test
 - daemon: reach for Overlord.Loop less thanks to overlord.Mock
 - snap-seccomp: manually resolve socket() call in tests
 - tests: change regex used to validate installed ubuntu core snap
 - cmd/snapctl: allow snapctl -h without a context (regression fix).
 - many: use snapcore/snapd/i18n instead of i18n/dumb
 - many: introduce asserts.NotFoundError replacing both ErrNotFound
   and store.AssertionNotFoundError
 - packaging: don't include any marcos in comments
 - overlord: use overlord.Mock in more tests, make sure we check the
   outcome of Settle
 - tests: try to fix staging tests
 - store: simplify api base url config
 - systemd: add systemd.MockJournalctl()
 - many: provide systemd.MockSystemctl() helper
 - tests: improve the listing test to not fail for e.g. 2.28~rc2
 - snapstate: give snapmgrTestSuite.settle() more time to settle
 - tests: fix regex to check core version on snap list
 - debian: update trusted account-keys check on 14.04 packaging
 - interfaces: add udev netlink support to hardware-observe
 - overlord: introduce Mock which enables to use Overlord.Settle for
   settle in many more places
 - snap-repair: execute the repair and capture logs/status
 - tests: run the tests/unit/go everywhere
 - daemon, snapstate: move ensureCore from daemon/api.go into
   snapstate.go
 - cmd/snap: get keys or root document
 - spread.yaml: turn suse to manual given that it's breaking master
 - many: configure store from state, reconfigure store at runtime
 - osutil: AtomicWriter (an io.Writer), and io.Reader versions of
   AtomicWrite*
 - tests: check for negative syscalls in runBpf() and skip those
   tests
 - docs: use abolute path in PULL_REQUEST_TEMPLATE.md
 - store: move device auth endpoint uris to config (#3831)

* Sat Oct 14 2017 Neal Gompa <ngompa13@gmail.com> - 2.28.5-2
- Properly fix the build for Fedora 25
- Incorporate misc build fixes

* Sat Oct 14 2017 Neal Gompa <ngompa13@gmail.com> - 2.28.5-1
- Release 2.28.5 to Fedora (RH#1502186)
- Build snap-exec and snap-update-ns statically to support base snaps

* Fri Oct 13 2017 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.28.5
  - snap-confine: cleanup broken nvidia udev tags
  - cmd/snap-confine: update valid security tag regexp
  - overlord/ifacestate: refresh udev backend on startup
  - dbus: ensure io.snapcraft.Launcher.service is created on re-
    exec
  - snap-confine: add support for handling /dev/nvidia-modeset
  - interfaces/network-control: remove incorrect rules for tun

* Thu Oct 12 2017 Neal Gompa <ngompa13@gmail.com> - 2.28.4-1
- Release 2.28.4 to Fedora (RH#1501141)
- Drop distro check backport patches (released with 2.28.2)

* Wed Oct 11 2017 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.28.4
  - interfaces/opengl: don't udev tag nvidia devices and use snap-
    confine instead
  - debian: fix replaces/breaks for snap-xdg-open (thanks to apw!)

* Wed Oct 11 2017 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.28.3
  - interfaces/lxd: lxd slot implementation can also be an app
    snap

* Tue Oct 10 2017 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.28.2
  - interfaces: fix udev rules for tun
  - release,cmd,dirs: Redo the distro checks to take into account
    distribution families

* Sun Oct 08 2017 Neal Gompa <ngompa13@gmail.com> - 2.28.1-1
- Release 2.28.1 to Fedora (RH#1495852)
- Drop userd backport patches, they are part of 2.28 release
- Backport changes to rework distro checks to fix derivative distro usage of snapd
- Revert import path change for cheggaaa/pb as it breaks build on Fedora
- Add a posttrans relabel to snapd-selinux to ensure everything is labeled correctly

* Wed Sep 27 2017 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.28.1
  - snap-confine: update apparmor rules for fedora based basesnaps
  - snapstate: rename refresh hook to post-refresh for consistency

* Mon Sep 25 2017 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.28
 - hooks: rename refresh to after-refresh
 - snap-confine: bind mount /usr/lib/snapd relative to snap-confine
 - cmd,dirs: treat "liri" the same way as "arch"
 - snap-confine: fix base snaps on core
 - hooks: substitute env vars when executing hooks
 - interfaces: updates for default, browser-support, desktop, opengl,
   upower and stub-resolv.conf
 - cmd,dirs: treat manjaro the same as arch
 - systemd: do not run auto-import and repair services on classic
 - packaging/fedora: Ensure vendor/ is empty for builds and fix spec
   to build current master
 - many: fix TestSetConfNumber missing an Unlock and other fragility
   improvements
 - osutil: adjust StreamCommand tests for golang 1.9
 - daemon: allow polkit authorisation to install/remove snaps
 - tests: make TestCmdWatch more robust
 - debian: improve package description
 - interfaces: add netlink kobject uevent to hardware observe
 - debian: update trusted account-keys check on 14.04 packaging
 - interfaces/network-{control,observe}: allow receiving
   kobject_uevent() messages
 - tests: fix lxd test for external backend
 - snap-confine,snap-update-ns: add -no-pie to fix FTBFS on
   go1.7,ppc64
 - corecfg: mock "systemctl" in all corecfg tests
 - tests: fix unit tests on Ubuntu 14.04
 - debian: add missing flags when building static snap-exec
 - many: end-to-end support for the bare base snap
 - overlord/snapstate: SetRootDir from SetUpTest, not in just some
   tests
 - store: have an ad-hoc method on cfg to get its list of uris for
   tests
 - daemon: let client decide whether to allow interactive auth via
   polkit
 - client,daemon,snap,store: add license field
 - overlord/snapstate: rename HasCurrent to IsInstalled, remove
   superfluous/misleading check from All
 - cmd/snap: SetRootDir from SetUpTest, not in just some individual
   tests.
 - systemd: rename snap-repair.{service,timer} to snapd.snap-
   repair.{service,timer}
 - snap-seccomp: remove use of x/net/bpf from tests
 - httputil: more naive per go version way to recreate a default
   transport for tls reconfig
 - cmd/snap-seccomp/main_test.go: add one more syscall for arm64
 - interfaces/opengl: use == to compare, not =
 - cmd/snap-seccomp/main_test.go: add syscalls for armhf and arm64
 - cmd/snap-repair: track and use a lower bound for the time for
   TLS checks
 - interfaces: expose bluez interface on classic OS
 - snap-seccomp: add in-kernel bpf tests
 - overlord: always try to get a serial, lazily on classic
 - tests: add nmcli regression test
 - tests: deal with __PNR_chown on aarch64 to fix FTBFS on arm64
 - tests: add autopilot-introspection interface test
 - vendor: fix artifact from manually editing vendor/vendor.json
 - tests: rename complexion to test-snapd-complexion
 - interfaces: add desktop and desktop-legacy
   interfaces/desktop: add new 'desktop' interface for modern DEs*
   interfaces/builtin/desktop_test.go: use modern testing techniques*
   interfaces/wayland: allow read on /etc/drirc for Plasma desktop*
   interfaces/desktop-legacy: add new 'legacy' interface (currently
   for a11y and input)
 - tests: fix race in snap userd test
 - devices/iio: add read/write for missing sysfs entries
 - spread: don't set HTTPS?_PROXY for linode
 - cmd/snap-repair: check signatures of repairs from Next
 - env: set XDG_DATA_DIRS for wayland et.al.
 - interfaces/{default,account-control}: Use username/group instead
   of uid/gid
 - interfaces/builtin: use udev tagging more broadly
 - tests: add basic lxd test
 - wrappers: ensure bash completion snaps install on core
 - vendor: use old golang.org/x/crypto/ssh/terminal to build on
   powerpc again
 - docs: add PULL_REQUEST_TEMPLATE.md
 - interfaces: fix network-manager plug
 - hooks: do not error out when hook is optional and no hook handler
   is registered
 - cmd/snap: add userd command to replace snapd-xdg-open
 - tests: new regex used to validate the core version on extra snaps
   ass...
 - snap: add new `snap switch` command
 - tests: wait more and more debug info about fakestore start issues
 - apparmor,release: add better apparmor detection/mocking code
 - interfaces/i2c: adjust sysfs rule for alternate paths
 - interfaces/apparmor: add missing call to dirs.SetRootDir
 - cmd: "make hack" now also installs snap-update-ns
 - tests: copy files with less verbosity
 - cmd/snap-confine: allow using additional libraries required by
   openSUSE
 - packaging/fedora: Merge changes from Fedora Dist-Git
 - snapstate: improve the error message when classic confinement is
   not supported
 - tests: add test to ensure amd64 can run i386 syscall binaries
 - tests: adding extra info for fakestore when fails to start
 - tests: install most important snaps
 - cmd/snap-repair: more test coverage of filtering
 - squashfs: remove runCommand/runCommandWithOutput as we do not need
   it
 - cmd/snap-repair: ignore superseded revisions, filter on arch and
   models
 - hooks: support for refresh hook
 - Partial revert "overlord/devicestate, store: update device auth
   endpoints URLs"
 - cmd/snap-confine: allow reading /proc/filesystems
 - cmd/snap-confine: genearlize apparmor profile for various lib
   layout
 - corecfg: fix proxy.* writing and add integration test
 - corecfg: deal with system.power-key-action="" correctly
 - vendor: update vendor.json after (presumed) manual edits
 - cmd/snap: in `snap info`, don't print a newline between tracks
 - daemon: add polkit support to /v2/login
 - snapd,snapctl: decode json using Number
 - client: fix go vet 1.7 errors
 - tests: make 17.04 shellcheck clean
 - tests: remove TestInterfacesHelp as it breaks when go-flags
   changes
 - snapstate: undo a daemon restart on classic if needed
 - cmd/snap-repair: recover brand/model from
   /var/lib/snapd/seed/assertions checking signatures and brand
   account
 - spread: opt into unsafe IO during spread tests
 - snap-repair: update snap-repair/runner_test.go for API change in
   makeMockServer
 - cmd/snap-repair: skeleton code around actually running a repair
 - tests: wait until the port is listening after start the fake store
 - corecfg: fix typo in tests
 - cmd/snap-repair: test that redirects works during fetching
 - osutil: honor SNAPD_UNSAFE_IO for testing
 - vendor: explode and make more precise our golang.go/x/crypto deps,
   use same version as Debian unstable
 - many: sanitize NewStoreStack signature, have shared default store
   test private keys
 - systemd: disable `Nice=-5` to fix error when running inside lxd
 - spread.yaml: update delta ref to 2.27
 - cmd/snap-repair: use E-Tags when refetching a repair to retry
 - interfaces/many: updates based on chromium and mrrescue denials
 - cmd/snap-repair: implement most logic to get the next repair to
   run/retry in a brand sequence
 - asserts/assertstest: copy headers in SigningDB.Sign
 - interfaces: convert uhid to common interface and test cases
   improvement for time_control and opengl
 - many tests: move all panicing fake store methods to a common place
 - asserts: add store assertion type
 - interfaces: don't crash if content slot has no attributes
 - debian: do not build with -buildmode=pie on i386
 - wrappers: symlink completion snippets when symlinking binaries
 - tests: adding more debug information for the interfaces-cups-
   control …
 - apparmor: pass --quiet to parser on load unless SNAPD_DEBUG is set
 - many: allow and support serials signed by the 'generic' authority
   instead of the brand
 - corecfg: add proxy configuration via `snap set core
   proxy.{http,https,ftp}=...`
 - interfaces: a bunch of interfaces test improvement
 - tests: enable regression and completion suites for opensuse
 - tests: installing snapd for nested test suite
 - interfaces: convert lxd_support to common iface
 - interfaces: add missing test for camera interface.
 - snap: add support for parsing snap layout section
 - cmd/snap-repair: like for downloads we cannot have a timeout (at
   least for now), less aggressive retry strategies
 - overlord: rely on more conservative ensure interval
 - overlord,store: no piles of return args for methods gathering
   device session request params
 - overlord,store: send model assertion when setting up device
   sessions
 - interfaces/misc: updates for unity7/x11, browser-
   support, network-control and mount-observe
   interfaces/unity7,x11: update for NETLINK_KOBJECT_UEVENT
   interfaces/browser-support: update sysfs reads for
   newer browser versions, interfaces/network-control: rw for
   ieee80211 advanced wireless interfaces/mount-observe: allow read
   on sysfs entries for block devices
 - tests: use dnf --refresh install to avert stale cache
 - osutil: ensure TestLockUnlockWorks uses supported flock
 - interfaces: convert lxd to common iface
 - tests: restart snapd to ensure re-exec settings are applied
 - tests: fix interfaces-cups-control test
 - interfaces: improve and tweak bunch of interfaces test cases.
 - tests: adding extra worker for fedora
 - asserts,overlord/devicestate: support predefined assertions that
   don't establish foundational trust
 - interfaces: convert two hardware_random interfaces to common iface
 - interfaces: convert io_ports_control to common iface
 - tests: fix for  upgrade test on fedora
 - daemon, client, cmd/snap: implement snap start/stop/restart
 - cmd/snap-confine: set _FILE_OFFSET_BITS to 64
 - interfaces: covert framebuffer to commonInterface
 - interfaces: convert joystick to common iface
 - interfaces/builtin: add the spi interface
 - wrappers, overlord/snapstate/backend: make link-snap clean up on
   failure.
 - interfaces/wayland: add wayland interface
 - interfaces: convert kvm to common iface
 - tests: extend upower-observe test to cover snaps providing slots
 - tests: enable main suite for opensuse
 - interfaces: convert physical_memory_observe to common iface
 - interfaces: add missing test for optical_drive interface.
 - interfaces: convert physical_memory_control to common iface
 - interfaces: convert ppp to common iface
 - interfaces: convert time-control to common iface
 - tests: fix failover test
 - interfaces/builtin: rework for avahi interface
 - interfaces: convert broadcom-asic-control to common iface
 - snap/snapenv: document the use of CoreSnapMountDir for SNAP
 - packaging/arch: drop patches merged into master
 - cmd: fix mustUnsetenv docstring (thanks to Chipaca)
 - release: remove default from VERSION_ID
 - tests: enable regression, upgrade and completion test suites for
   fedora
 - tests: restore interfaces-account-control properly
 - overlord/devicestate, store: update device auth endpoints URLs
 - tests: fix install-hook test failure
 - tests: download core and ubuntu-core at most once
 - interfaces: add common support for udev
 - overlord/devicestate: fix, don't assume that the serial is backed
   by a 1-key chain
 - cmd/snap-confine: don't share /etc/nsswitch from host
 - store: do not resume a download when we already have the whole
   thing
 - many: implement "snap logs"
 - store: don't call useDeltas() twice in quick succession
 - interfaces/builtin: add kvm interface
 - snap/snapenv: always expect /snap for $SNAP
 - cmd: mark arch as non-reexecing distro
 - cmd: fix tests that assume /snap mount
 - gitignore: ignore more build artefacts
 - packaging: add current arch packaging
 - interfaces/unity7: allow receiving media key events in (at least)
   gnome-shell
 - interfaces/many, cmd/snap-confine: miscellaneous policy updates
 - interfaces/builtin: implement broadcom-asic-control interface
 - interfaces/builtin: reduce duplication and remove cruft in
   Sanitize{Plug,Slot}
 - tests: apply underscore convention for SNAPMOUNTDIR variable
 - interfaces/greengrass-support: adjust accesses now that have
   working snap
 - daemon, client, cmd/snap: implement "snap services"
 - tests: fix refresh tests not stopping fake store for fedora
 - many: add the interface command
 - overlord/snapstate/backend: some copydata improvements
 - many: support querying and completing assertion type names
 - interfaces/builtin: discard empty Validate{Plug,Slot}
 - cmd/snap-repair:  start of Runner, implement first pass of Peek
   and Fetch
 - tests: enable main suite on fedora
 - snap: do not always quote the snap info summary
 - vendor: update go-flags to address crash in "snap debug"
 - interfaces: opengl support pci device and vendor
 - many: start implenting "base" snap type on the snapd side
 - arch,release: map armv6 correctly
 - many: expose service status in 'snap info'
 - tests: add browser-support interface test
 - tests: disable snapd-notify for the external backend
 - interfaces: Add /run/uuid/request to openvswitch
 - interfaces: add password-manager-service implicit classic
   interface
 - cmd: rework reexec detection
 - cmd: fix re-exec bug when starting from snapd 2.21
 - tests: dependency packages installed during prepare-project
 - tests: remove unneeded check for re-exec in InternalToolPath()
 - cmd,tests: fix classic confinement confusing re-execution code
 - store: configurable base api
 - tests: fix how package lists are updated for opensuse and fedora

* Sun Sep 10 2017 Neal Gompa <ngompa13@gmail.com> - 2.27.6-1
- Release 2.27.6 to Fedora (RH#1489437)

* Thu Sep 07 2017 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.27.6
  - interfaces: add udev netlink support to hardware-observe
  - interfaces/network-{control,observe}: allow receiving
    kobject_uevent() messages

* Mon Sep 04 2017 Neal Gompa <ngompa13@gmail.com> - 2.27.5-1
- Release 2.27.5 to Fedora (RH#1483177)
- Backport userd from upstream to support xdg-open

* Wed Aug 30 2017 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.27.5
  - interfaces: fix network-manager plug regression
  - hooks: do not error when hook handler is not registered
  - interfaces/alsa,pulseaudio: allow read on udev data for sound
  - interfaces/optical-drive: read access to udev data for /dev/scd*
  - interfaces/browser-support: read on /proc/vmstat and misc udev
    data

* Thu Aug 24 2017 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.27.4
  - snap-seccomp: add secondary arch for unrestricted snaps as well

* Fri Aug 18 2017 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.27.3
  - systemd: disable `Nice=-5` to fix error when running inside lxdSee
    https://bugs.launchpad.net/snapd/+bug/1709536

* Wed Aug 16 2017 Neal Gompa <ngompa13@gmail.com> - 2.27.2-2
- Bump to rebuild for F27 and Rawhide

* Wed Aug 16 2017 Neal Gompa <ngompa13@gmail.com> - 2.27.2-1
- Release 2.27.2 to Fedora (RH#1482173)

* Wed Aug 16 2017 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.27.2
 - tests: remove TestInterfacesHelp as it breaks when go-flags
   changes
 - interfaces: don't crash if content slot has no attributes
 - debian: do not build with -buildmode=pie on i386
 - interfaces: backport broadcom-asic-control interface
 - interfaces: allow /usr/bin/xdg-open in unity7
 - store: do not resume a download when we already have the whole
   thing

* Mon Aug 14 2017 Neal Gompa <ngompa13@gmail.com> - 2.27.1-1
- Release 2.27.1 to Fedora (RH#1481247)

* Mon Aug 14 2017 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.27.1
 - tests: use dnf --refresh install to avert stale cache
 - tests: fix test failure on 14.04 due to old version of
   flock
 - updates for unity7/x11, browser-support, network-control,
   mount-observe
 - interfaces/unity7,x11: update for NETLINK_KOBJECT_UEVENT
 - interfaces/browser-support: update sysfs reads for
   newer browser versions
 - interfaces/network-control: rw for ieee80211 advanced wireless
 - interfaces/mount-observe: allow read on sysfs entries for block
   devices

* Thu Aug 10 2017 Neal Gompa <ngompa13@gmail.com> - 2.27-1
- Release 2.27 to Fedora (RH#1458086)

* Thu Aug 10 2017 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.27
 - fix build failure on 32bit fedora
 - interfaces: add password-manager-service implicit classic interface
 - interfaces/greengrass-support: adjust accesses now that have working
   snap
 - interfaces/many, cmd/snap-confine: miscellaneous policy updates
 - interfaces/unity7: allow receiving media key events in (at least)
   gnome-shell
 - cmd: fix re-exec bug when starting from snapd 2.21
 - tests: restore interfaces-account-control properly
 - cmd: fix tests that assume /snap mount
 - cmd: mark arch as non-reexecing distro
 - snap-confine: don't share /etc/nsswitch from host
 - store: talk to api.snapcraft.io for purchases
 - hooks: support for install and remove hooks
 - packaging: fix Fedora support
 - tests: add bluetooth-control interface test
 - store: talk to api.snapcraft.io for assertions
 - tests: remove snapd before building from branch
 - tests: add avahi-observe interface test
 - store: orders API now checks if customer is ready
 - cmd/snap: snap find only searches stable
 - interfaces: updates default, mir, optical-observe, system-observe,
   screen-inhibit-control and unity7
 - tests: speedup prepare statement part 1
 - store: do not send empty refresh requests
 - asserts: fix error handling in snap-developer consistency check
 - systemd: add explicit sync to snapd.core-fixup.sh
 - snapd: generate snap cookies on startup
 - cmd,client,daemon: expose "force devmode" in sysinfo
 - many: introduce and use strutil.ListContains and also
   strutil.SortedListContains
 - assserts,overlord/assertstate: test we don't accept chains of
   assertions founded on a self-signed key coming externally
 - interfaces: enable access to bridge settings
 - interfaces: fix copy-pasted iio vs io in io-ports-control
 - cmd/snap-confine: various small fixes and tweaks to seccomp
   support code
 - interfaces: bring back seccomp argument filtering
 - systemd, osutil: rework systemd logs in preparation for services
   commands
 - tests: store /etc/systemd/system/snap-*core*.mount in snapd-
   state.tar.gz
 - tests: shellcheck improvements for tests/main tasks - first set of
   tests
 - cmd/snap: `--last` for abort and watch, and aliases
   (search→find, change→tasks)
 - tests: shellcheck improvements for tests/lib scripts
 - tests: create ramdisk if it's not present
 - tests: shellcheck improvements for nightly upgrade and regressions
   tests
 - snapd: fix for snapctl get panic on null config values.
 - tests: fix for rng-tools service not restarting
 - systemd: add snapd.core-fixup.service unit
 - cmd: avoid using current symlink in InternalToolPath
 - tests: fix timeout issue for test refresh core with hanging …
 - intefaces: control bridged vlan/ppoe-tagged traffic
 - cmd/snap: include snap type in notes
 - overlord/state: Abort() only visits each task once
 - tests: extend find-private test to cover more cases
 - snap-seccomp: skip socket() tests on systems that use socketcall()
   instead of socket()
 - many: support snap title as localized/title-cased name
 - snap-seccomp: deal with mknod on aarch64 in the seccomp tests
 - interfaces: put base policy fragments inside each interface
 - asserts: introduce NewDecoderWithTypeMaxBodySize
 - tests: fix snapd-notify when it takes more time to restart
 - snap-seccomp: fix snap-seccomp tests in artful
 - tests: fix for create-key task to avoid rng-tools service ramains
   alive
 - snap-seccomp: make sure snap-seccomp writes the bpf file
   atomically
 - tests: do not disable ipv6 on core systems
 - arch: the kernel architecture name is armv7l instead of armv7
 - snap-confine: ensure snap-confine waits some seconds for seccomp
   security profiles
 - tests: shellcheck improvements for tests/nested tasks
 - wrappers: add SyslogIdentifier to the service unit files.
 - tests: shellcheck improvements for unit tasks
 - asserts: implement FindManyTrusted as well
 - asserts: open up and optimize Encoder to help avoiding unnecessary
   copying
 - interfaces: simplify snap-confine by just loading pre-generated
   bpf code
 - tests: restart rng-tools services after few seconds
 - interfaces, tests: add mising dbus abstraction to system-observe
   and extend spread test
 - store: change main store host to api.snapcraft.io
 - overlord/cmdstate: new package for running commands as tasks.
 - spread: help libapt resolve installing libudev-dev
 - tests: show the IP from .travis.yaml
 - tests/main: use pkgdb function in more test cases
 - cmd,daemon: add debug command for displaying the base policy
 - tests: prevent quoting error on opensuse
 - tests: fix nightly suite
 - tests: add linode-sru backend
 - snap-confine: validate SNAP_NAME against security tag
 - tests: fix ipv6 disable for ubuntu-core
 - tests: extend core-revert test to cover bluez issues
 - interfaces/greengrass-support: add support for Amazon Greengrass
   as a snap
 - asserts: support timestamp and optional disabled header on repair
 - tests: reboot after upgrading to snapd on the -proposed pocket
 - many: fix test cases to work with different DistroLibExecDir
 - tests: reenable help test on ubuntu and debian systems
 - packaging/{opensuse,fedora}: allow package build with testkeys
   included
 - tests/lib: generalize RPM build support
 - interfaces/builtin: sync connected slot and permanent slot snippet
 - tests: fix snap create-key by restarting automatically rng-tools
 - many: switch to use http numeric statuses as agreed
 - debian: add missing  Type=notify in 14.04 packaging
 - tests: mark interfaces-openvswitch as manual due to prepare errors
 - debian: unify built_using between the 14.04 and 16.04 packaging
   branch
 - tests: pull from urandom when real entropy is not enough
 - tests/main/manpages: install missing man package
 - tests: add refresh --time output check
 - debian: add missing "make -C data/systemd clean"
 - tests: fix for upgrade test when it is repeated
 - tests/main: use dir abstraction in a few more test cases
 - tests/main: check for confinement in a few more interface tests
 - spread: add fedora snap bin dir to global PATH
 - tests: check that locale-control is not present on core
 - many: snapctl outside hooks
 - tests: add whoami check
 - interfaces: compose the base declaration from interfaces
 - tests: fix spread flaky tests linode
 - tests,packaging: add package build support for openSUSE
 - many: slight improvement of some snap error messaging
 - errtracker: Include /etc/apparmor.d/usr.lib.snap-confine md5sum in
   err reports
 - tests: fix for the test postrm-purge
 - tests: restoring the /etc/environment and service units config for
   each test
 - daemon: make snapd a "Type=notify" daemon and notify when startup
   is done
 - cmd/snap-confine: add support for --base snap
 - many: derive implicit slots from interface meta-data
 - tests: add core revert test
 - tests,packaging: add package build support for Fedora for our
   spread setup
 - interfaces: move base declaration to the policy sub-package
 - tests: fix for snapd-reexec test cheking for restart info on debug
   log
 - tests: show available entropy on error
 - tests: clean journalctl logs on trusty
 - tests: fix econnreset on staging
 - tests: modify core before calling set
 - tests: add snap-confine privilege test
 - tests: add staging snap-id
 - interfaces/builtin: silence ptrace denial for network-manager
 - tests: add alsa interface spread test
 - tests: prefer ipv4 over ipv6
 - tests: fix for econnreset test checking that the download already
   started
 - httputil,store: extract retry code to httputil, reorg usages
 - errtracker: report if snapd did re-execute itself
 - errtracker: include bits of snap-confine apparmor profile
 - tests: take into account staging snap-ids for snap-info
 - cmd: add stub new snap-repair command and add timer
 - many: stop "snap refresh $x --channel invalid" from working
 - interfaces: revert "interfaces: re-add reverted ioctl and quotactl
 - snapstate: consider connect/disconnect tasks in
   CheckChangeConflict.
 - interfaces: disable "mknod |N" in the default seccomp template
   again
 - interfaces,overlord/ifacestate: make sure installing slots after
   plugs works similarly to plugs after slots
 - interfaces/seccomp: add bind() syscall for forced-devmode systems
 - packaging/fedora: Sync packaging from Fedora Dist-Git
 - tests: move static and unit tests to spread task
 - many: error types should be called FooError, not ErrFoo.
 - partition: add directory sync to the save uboot.env file code
 - cmd: test everything (100% coverage \o/)
 - many: make shell scripts shellcheck-clean
 - tests: remove additional setup for docker on core
 - interfaces: add summary to each interface
 - many: remove interface meta-data from list of connections
 - logger (& many more, to accommodate): drop explicit syslog.
 - packaging: import packaging bits for opensuse
 - snapstate,many: implement snap install --unaliased
 - tests/lib: abstract build dependency installation a bit more
 - interfaces, osutil: move flock code from interfaces/mount to
   osutil
 - cmd: auto import assertions only from ext4,vfat file systems
 - many: refactor in preparation for 'snap start'
 - overlord/snapstate: have an explicit code path last-refresh
   unset/zero => immediately refresh try
 - tests: fixes for executions using the staging store
 - tests: use pollinate to seed the rng
 - cmd/snap,tests: show the sha3-384 of the snap for snap info
   --verbose SNAP-FILE
 - asserts: simplify and adjust repair assertion definition
 - cmd/snap,tests: show the snap id if available in snap info
 - daemon,overlord/auth: store from model assertion wins
 - cmd/snap,tests/main: add confinement switch instead of spread
   system blacklisting
 - many: cleanup MockCommands and don't leave a process around after
   hookstate tests
 - tests: update listing test to the core version number schema
 - interfaces: allow snaps to use the timedatectl utility
 - packaging: Add Fedora packaging files
 - tests/libs: add distro_auto_remove_packages function
 - cmd/snap: correct devmode note for anomalous state
 - tests/main/snap-info: use proper pkgdb functions to install distro
   packages
 - tests/lib: use mktemp instead of tempfile to work cross-distro
 - tests: abstract common dirs which differ on distributions
 - many: model and expose interface meta-data.
 - overlord: make config defaults from gadget work also at first boot
 - interfaces/log-observe: allow using journalctl from hostfs for
   classic distro
 - partition,snap: add support for android boot
 - errtracker: small simplification around readMachineID
 - snap-confine: move rm_rf_tmp to test-utils.
 - tests/lib: introduce pkgdb helper library
 - errtracker: try multiple paths to read machine-id
 - overlord/hooks: make sure only one hook for given snap is executed
   at a time.
 - cmd/snap-confine: use SNAP_MOUNT_DIR to setup /snap inside the
   confinement env
 - tests: bump kill-timeout and remove quiet call on build
 - tests/lib/snaps: add a test store snap with a passthrough
   configure hook
 - daemon: teach the daemon to wait on active connections when
   shutting down
 - tests: remove unit tests task
 - tests/main/completion: source from /usr/share/bash-completion
 - assertions: add "repair" assertion
 - interfaces/seccomp: document Backend.NewSpecification
 - wrappers: make StartSnapServices cleanup any services that were
   added if a later one fails
 - overlord/snapstate: avoid creating command aliases for daemons
 - vendor: remove unused packages
 - vendor,partition: fix panics from uenv
 - cmd,interfaces/mount: run snap-update-ns and snap-discard-ns from
   core if possible
 - daemon: do not allow to install ubuntu-core anymore
 - wrappers: service start/stop were inconsistent
 - tests: fix failing tests (snap core version, syslog changes)
 - cmd/snap-update-ns: add actual implementation
 - tests: improve entropy also for ubuntu
 - cmd/snap-confine: use /etc/ssl from the core snap
 - wrappers: don't convert between []byte and string needlessly.
 - hooks: default timeout
 - overlord/snapstate: Enable() was ignoring the flags from the
   snap's state, resulting in losing "devmode" on disable/enable.
 - difs,interfaces/mount: add support for locking namespaces
 - interfaces/mount: keep track of kept mount entries
 - tests/main: move a bunch of greps over to MATCH
 - interfaces/builtin: make all interfaces private
 - interfaces/mount: spell unmount correctly
 - tests: allow 16-X.Y.Z version of core snap
 - the timezone_control interface only allows changing /etc/timezone
   and /etc/writable/timezone. systemd-timedated also updated the
   link of /etc/localtime and /etc/writable/localtime ... allow
   access to this file too
 - cmd/snap-confine: aggregate operations holding global lock
 - api, ifacestate: resolve disconnect early
 - interfaces/builtin: ensure we don't register interfaces twice

* Thu Aug 03 2017 Fedora Release Engineering <releng@fedoraproject.org> - 2.26.3-5
- Rebuilt for https://fedoraproject.org/wiki/Fedora_27_Binutils_Mass_Rebuild

* Thu Jul 27 2017 Fedora Release Engineering <releng@fedoraproject.org> - 2.26.3-4
- Rebuilt for https://fedoraproject.org/wiki/Fedora_27_Mass_Rebuild

* Thu May 25 2017 Neal Gompa <ngompa13@gmail.com> - 2.26.3-3
- Cover even more stuff for proper erasure on final uninstall (RH#1444422)

* Sun May 21 2017 Neal Gompa <ngompa13@gmail.com> - 2.26.3-2
- Fix error in script for removing Snappy content (RH#1444422)
- Adjust changelog bug references to be specific on origin

* Wed May 17 2017 Neal Gompa <ngompa13@gmail.com> - 2.26.3-1
- Update to snapd 2.26.3
- Drop merged and unused patches
- Cover more Snappy content for proper erasure on final uninstall (RH#1444422)
- Add temporary fix to ensure generated seccomp profiles don't break snapctl

* Mon May 01 2017 Neal Gompa <ngompa13@gmail.com> - 2.25-1
- Update to snapd 2.25
- Ensure all Snappy content is gone on final uninstall (RH#1444422)

* Tue Apr 11 2017 Neal Gompa <ngompa13@gmail.com> - 2.24-1
- Update to snapd 2.24
- Drop merged patches
- Install snap bash completion and snapd info file

* Wed Apr 05 2017 Neal Gompa <ngompa13@gmail.com> - 2.23.6-4
- Test if snapd socket and timer enabled and start them if enabled on install

* Sat Apr 01 2017 Neal Gompa <ngompa13@gmail.com> - 2.23.6-3
- Fix profile.d generation so that vars aren't expanded in package build

* Fri Mar 31 2017 Neal Gompa <ngompa13@gmail.com> - 2.23.6-2
- Fix the overlapping file conflicts between snapd and snap-confine
- Rework package descriptions slightly

* Thu Mar 30 2017 Neal Gompa <ngompa13@gmail.com> - 2.23.6-1
- Rebase to snapd 2.23.6
- Rediff patches
- Re-enable seccomp
- Fix building snap-confine on 32-bit arches
- Set ExclusiveArch based on upstream supported arch list

* Wed Mar 29 2017 Neal Gompa <ngompa13@gmail.com> - 2.23.5-1
- Rebase to snapd 2.23.5
- Disable seccomp temporarily avoid snap-confine bugs (LP#1674193)
- Use vendorized build for non-Fedora

* Mon Mar 13 2017 Neal Gompa <ngompa13@gmail.com> - 2.23.1-1
- Rebase to snapd 2.23.1
- Add support for vendored tarball for non-Fedora targets
- Use merged in SELinux policy module

* Sat Feb 11 2017 Fedora Release Engineering <releng@fedoraproject.org> - 2.16-2
- Rebuilt for https://fedoraproject.org/wiki/Fedora_26_Mass_Rebuild

* Wed Oct 19 2016 Zygmunt Krynicki <me@zygoon.pl> - 2.16-1
- New upstream release

* Tue Oct 18 2016 Neal Gompa <ngompa13@gmail.com> - 2.14-2
- Add SELinux policy module subpackage

* Tue Aug 30 2016 Zygmunt Krynicki <me@zygoon.pl> - 2.14-1
- New upstream release

* Tue Aug 23 2016 Zygmunt Krynicki <me@zygoon.pl> - 2.13-1
- New upstream release

* Thu Aug 18 2016 Zygmunt Krynicki <me@zygoon.pl> - 2.12-2
- Correct license identifier

* Thu Aug 18 2016 Zygmunt Krynicki <me@zygoon.pl> - 2.12-1
- New upstream release

* Thu Aug 18 2016 Zygmunt Krynicki <me@zygoon.pl> - 2.11-8
- Add %%dir entries for various snapd directories
- Tweak Source0 URL

* Tue Aug 16 2016 Zygmunt Krynicki <me@zygoon.pl> - 2.11-7
- Disable snapd re-exec feature by default

* Tue Aug 16 2016 Zygmunt Krynicki <me@zygoon.pl> - 2.11-6
- Don't auto-start snapd.socket and snapd.refresh.timer

* Tue Aug 16 2016 Zygmunt Krynicki <me@zygoon.pl> - 2.11-5
- Don't touch snapd state on removal

* Tue Aug 16 2016 Zygmunt Krynicki <me@zygoon.pl> - 2.11-4
- Use ExecStartPre to load squashfs.ko before snapd starts
- Use dedicated systemd units for Fedora

* Tue Aug 16 2016 Zygmunt Krynicki <me@zygoon.pl> - 2.11-3
- Remove systemd preset (will be requested separately according to distribution
  standards).

* Tue Aug 16 2016 Zygmunt Krynicki <me@zygoon.pl> - 2.11-2
- Use Requires: kmod(squashfs.ko) instead of Requires: kernel-modules

* Tue Aug 16 2016 Zygmunt Krynicki <me@zygoon.pl> - 2.11-1
- New upstream release
- Move private executables to /usr/libexec/snapd/

* Fri Jun 24 2016 Zygmunt Krynicki <me@zygoon.pl> - 2.0.9-2
- Depend on kernel-modules to ensure that squashfs can be loaded. Load it afer
  installing the package. This hopefully fixes
  https://github.com/zyga/snapcore-fedora/issues/2

* Fri Jun 17 2016 Zygmunt Krynicki <me@zygoon.pl> - 2.0.9
- New upstream release
  https://github.com/snapcore/snapd/releases/tag/2.0.9

* Tue Jun 14 2016 Zygmunt Krynicki <me@zygoon.pl> - 2.0.8.1
- New upstream release

* Fri Jun 10 2016 Zygmunt Krynicki <me@zygoon.pl> - 2.0.8
- First package for Fedora
