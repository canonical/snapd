# With Fedora, nothing is bundled. For everything else, bundling is used.
# To use bundled stuff, use "--with vendorized" on rpmbuild
%if 0%{?fedora}
%bcond_with vendorized
%else
%bcond_without vendorized
%endif

# A switch to allow building the package with support for testkeys which
# are used for the spread test suite of snapd.
%bcond_with testkeys

%global with_devel 1
%global with_debug 1
%global with_check 0
%global with_unit_test 0
%global with_test_keys 0

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

%global snappy_svcs     snapd.service snapd.socket snapd.autoimport.service

# Until we have a way to add more extldflags to gobuild macro...
%if 0%{?fedora} >= 26
%define gobuild_static(o:) go build -buildmode pie -compiler gc -tags=rpm_crashtraceback -ldflags "${LDFLAGS:-} -B 0x$(head -c20 /dev/urandom|od -An -tx1|tr -d ' \\n') -extldflags '%__global_ldflags -static'" -a -v -x %{?**};
%endif
%if 0%{?fedora} == 25
%define gobuild_static(o:) go build -compiler gc -ldflags "${LDFLAGS:-} -B 0x$(head -c20 /dev/urandom|od -An -tx1|tr -d ' \\n') -extldflags '-static'" -a -v -x %{?**};
%endif
%if 0%{?rhel} == 7
%define gobuild_static(o:) go build -compiler gc -tags=rpm_crashtraceback -ldflags "${LDFLAGS:-} -B 0x$(head -c20 /dev/urandom|od -An -tx1|tr -d ' \\n') -extldflags '%__global_ldflags -static'" -a -v -x %{?**};
%endif

# These macros are not defined in RHEL 7
%if 0%{?rhel} == 7
%define gobuild(o:) go build -compiler gc -tags=rpm_crashtraceback -ldflags "${LDFLAGS:-} -B 0x$(head -c20 /dev/urandom|od -An -tx1|tr -d ' \\n') -extldflags '%__global_ldflags'" -a -v -x %{?**};
%define gotest() go test -compiler gc -ldflags "${LDFLAGS:-}" %{?**};
%endif

Name:           snapd
Version:        2.31.1
Release:        0%{?dist}
Summary:        A transactional software package manager
Group:          System Environment/Base
License:        GPLv3
URL:            https://%{provider_prefix}
Source0:        https://%{provider_prefix}/archive/%{version}/%{name}-%{version}.tar.gz
%if 0%{?with_bundled}
Source1:        https://%{provider_prefix}/releases/download/%{version}/%{name}_%{version}.only-vendor.tar.xz
%endif

%if 0%{?with_goarches}
# e.g. el6 has ppc64 arch without gcc-go, so EA tag is required
ExclusiveArch:  %{?go_arches:%{go_arches}}%{!?go_arches:%{ix86} x86_64 %{arm}}
%else
# Verified arches from snapd upstream
ExclusiveArch:  %{ix86} x86_64 %{arm} aarch64 ppc64le s390x
%endif

# If go_compiler is not set to 1, there is no virtual provide. Use golang instead.
BuildRequires:  %{?go_compiler:compiler(go-compiler)}%{!?go_compiler:golang}
BuildRequires:  systemd
%{?systemd_requires}

Requires:       snap-confine%{?_isa} = %{version}-%{release}
Requires:       squashfs-tools

%if 0%{?fedora} >= 26 || 0%{?rhel} >= 8
# snapd will use squashfuse in the event that squashfs.ko isn't available (cloud instances, containers, etc.)
Requires:       ((squashfuse and fuse) or kmod(squashfs.ko))
%else
# Rich dependencies not available, always pull in squashfuse
# snapd will use squashfs.ko instead of squashfuse if it's on the system
Requires:       squashfuse
Requires:       fuse
%endif

# bash-completion owns /usr/share/bash-completion/completions
Requires:       bash-completion

# Force the SELinux module to be installed
Requires:       %{name}-selinux = %{version}-%{release}

%if ! 0%{?with_bundled}
BuildRequires: golang(github.com/boltdb/bolt)
BuildRequires: golang(github.com/cheggaaa/pb)
BuildRequires: golang(github.com/coreos/go-systemd/activation)
BuildRequires: golang(github.com/godbus/dbus)
BuildRequires: golang(github.com/godbus/dbus/introspect)
BuildRequires: golang(github.com/gorilla/mux)
BuildRequires: golang(github.com/jessevdk/go-flags)
BuildRequires: golang(github.com/mvo5/goconfigparser)
BuildRequires: golang(github.com/ojii/gettext.go)
BuildRequires: golang(github.com/seccomp/libseccomp-golang)
BuildRequires: golang(golang.org/x/crypto/openpgp/armor)
BuildRequires: golang(golang.org/x/crypto/openpgp/packet)
BuildRequires: golang(golang.org/x/crypto/sha3)
BuildRequires: golang(golang.org/x/crypto/ssh/terminal)
BuildRequires: golang(golang.org/x/net/context)
BuildRequires: golang(golang.org/x/net/context/ctxhttp)
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
Group:          System Environment/Base
BuildRequires:  autoconf
BuildRequires:  automake
BuildRequires:  libtool
BuildRequires:  gcc
BuildRequires:  gettext
BuildRequires:  gnupg
BuildRequires:  indent
BuildRequires:  pkgconfig(glib-2.0)
BuildRequires:  pkgconfig(libcap)
BuildRequires:  pkgconfig(libseccomp)
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
%if 0%{?fedora} >= 25
# ShellCheck in F24 and older doesn't work
BuildRequires:  %{_bindir}/shellcheck
%endif

# Ensures older version from split packaging is replaced
Obsoletes:      snap-confine < 2.19

%description -n snap-confine
This package is used internally by snapd to apply confinement to
the started snap applications.

%package selinux
Summary:        SELinux module for snapd
Group:          System Environment/Base
License:        GPLv2+
BuildArch:      noarch
BuildRequires:  selinux-policy, selinux-policy-devel
Requires(post): selinux-policy-base >= %{_selinux_policy_version}
Requires(post): policycoreutils
Requires(post): policycoreutils-python-utils
Requires(pre):  libselinux-utils
Requires(post): libselinux-utils

%description selinux
This package provides the SELinux policy module to ensure snapd
runs properly under an environment with SELinux enabled.


%if 0%{?with_devel}
%package devel
Summary:       %{summary}
BuildArch:     noarch

%if 0%{?with_check} && ! 0%{?with_bundled}
%endif

%if ! 0%{?with_bundled}
Requires:      golang(github.com/boltdb/bolt)
Requires:      golang(github.com/cheggaaa/pb)
Requires:      golang(github.com/coreos/go-systemd/activation)
Requires:      golang(github.com/godbus/dbus)
Requires:      golang(github.com/godbus/dbus/introspect)
Requires:      golang(github.com/gorilla/mux)
Requires:      golang(github.com/jessevdk/go-flags)
Requires:      golang(github.com/mvo5/goconfigparser)
Requires:      golang(github.com/ojii/gettext.go)
Requires:      golang(github.com/seccomp/libseccomp-golang)
Requires:      golang(golang.org/x/crypto/openpgp/armor)
Requires:      golang(golang.org/x/crypto/openpgp/packet)
Requires:      golang(golang.org/x/crypto/sha3)
Requires:      golang(golang.org/x/crypto/ssh/terminal)
Requires:      golang(golang.org/x/net/context)
Requires:      golang(golang.org/x/net/context/ctxhttp)
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
Provides:      bundled(golang(github.com/boltdb/bolt))
Provides:      bundled(golang(github.com/cheggaaa/pb))
Provides:      bundled(golang(github.com/coreos/go-systemd/activation))
Provides:      bundled(golang(github.com/godbus/dbus))
Provides:      bundled(golang(github.com/godbus/dbus/introspect))
Provides:      bundled(golang(github.com/gorilla/mux))
Provides:      bundled(golang(github.com/jessevdk/go-flags))
Provides:      bundled(golang(github.com/mvo5/goconfigparser))
Provides:      bundled(golang(github.com/mvo5/libseccomp-golang))
Provides:      bundled(golang(github.com/ojii/gettext.go))
Provides:      bundled(golang(golang.org/x/crypto/openpgp/armor))
Provides:      bundled(golang(golang.org/x/crypto/openpgp/packet))
Provides:      bundled(golang(golang.org/x/crypto/sha3))
Provides:      bundled(golang(golang.org/x/crypto/ssh/terminal))
Provides:      bundled(golang(golang.org/x/net/context))
Provides:      bundled(golang(golang.org/x/net/context/ctxhttp))
Provides:      bundled(golang(gopkg.in/check.v1))
Provides:      bundled(golang(gopkg.in/macaroon.v1))
Provides:      bundled(golang(gopkg.in/mgo.v2/bson))
Provides:      bundled(golang(gopkg.in/retry.v1))
Provides:      bundled(golang(gopkg.in/tomb.v2))
Provides:      bundled(golang(gopkg.in/yaml.v2))
%endif

# Generated by gofed
Provides:      golang(%{import_path}/arch) = %{version}-%{release}
Provides:      golang(%{import_path}/asserts) = %{version}-%{release}
Provides:      golang(%{import_path}/asserts/assertstest) = %{version}-%{release}
Provides:      golang(%{import_path}/asserts/signtool) = %{version}-%{release}
Provides:      golang(%{import_path}/asserts/snapasserts) = %{version}-%{release}
Provides:      golang(%{import_path}/asserts/sysdb) = %{version}-%{release}
Provides:      golang(%{import_path}/asserts/systestkeys) = %{version}-%{release}
Provides:      golang(%{import_path}/boot) = %{version}-%{release}
Provides:      golang(%{import_path}/boot/boottest) = %{version}-%{release}
Provides:      golang(%{import_path}/client) = %{version}-%{release}
Provides:      golang(%{import_path}/cmd) = %{version}-%{release}
Provides:      golang(%{import_path}/daemon) = %{version}-%{release}
Provides:      golang(%{import_path}/dirs) = %{version}-%{release}
Provides:      golang(%{import_path}/errtracker) = %{version}-%{release}
Provides:      golang(%{import_path}/httputil) = %{version}-%{release}
Provides:      golang(%{import_path}/i18n) = %{version}-%{release}
Provides:      golang(%{import_path}/image) = %{version}-%{release}
Provides:      golang(%{import_path}/interfaces) = %{version}-%{release}
Provides:      golang(%{import_path}/interfaces/apparmor) = %{version}-%{release}
Provides:      golang(%{import_path}/interfaces/backends) = %{version}-%{release}
Provides:      golang(%{import_path}/interfaces/builtin) = %{version}-%{release}
Provides:      golang(%{import_path}/interfaces/dbus) = %{version}-%{release}
Provides:      golang(%{import_path}/interfaces/ifacetest) = %{version}-%{release}
Provides:      golang(%{import_path}/interfaces/kmod) = %{version}-%{release}
Provides:      golang(%{import_path}/interfaces/mount) = %{version}-%{release}
Provides:      golang(%{import_path}/interfaces/policy) = %{version}-%{release}
Provides:      golang(%{import_path}/interfaces/seccomp) = %{version}-%{release}
Provides:      golang(%{import_path}/interfaces/systemd) = %{version}-%{release}
Provides:      golang(%{import_path}/interfaces/udev) = %{version}-%{release}
Provides:      golang(%{import_path}/jsonutil) = %{version}-%{release}
Provides:      golang(%{import_path}/logger) = %{version}-%{release}
Provides:      golang(%{import_path}/osutil) = %{version}-%{release}
Provides:      golang(%{import_path}/overlord) = %{version}-%{release}
Provides:      golang(%{import_path}/overlord/assertstate) = %{version}-%{release}
Provides:      golang(%{import_path}/overlord/auth) = %{version}-%{release}
Provides:      golang(%{import_path}/overlord/cmdstate) = %{version}-%{release}
Provides:      golang(%{import_path}/overlord/configstate) = %{version}-%{release}
Provides:      golang(%{import_path}/overlord/configstate/config) = %{version}-%{release}
Provides:      golang(%{import_path}/overlord/configstate/configcore) = %{version}-%{release}
Provides:      golang(%{import_path}/overlord/devicestate) = %{version}-%{release}
Provides:      golang(%{import_path}/overlord/hookstate) = %{version}-%{release}
Provides:      golang(%{import_path}/overlord/hookstate/ctlcmd) = %{version}-%{release}
Provides:      golang(%{import_path}/overlord/hookstate/hooktest) = %{version}-%{release}
Provides:      golang(%{import_path}/overlord/ifacestate) = %{version}-%{release}
Provides:      golang(%{import_path}/overlord/patch) = %{version}-%{release}
Provides:      golang(%{import_path}/overlord/servicestate) = %{version}-%{release}
Provides:      golang(%{import_path}/overlord/snapstate) = %{version}-%{release}
Provides:      golang(%{import_path}/overlord/snapstate/backend) = %{version}-%{release}
Provides:      golang(%{import_path}/overlord/state) = %{version}-%{release}
Provides:      golang(%{import_path}/overlord/storestate) = %{version}-%{release}
Provides:      golang(%{import_path}/partition) = %{version}-%{release}
Provides:      golang(%{import_path}/partition/androidbootenv) = %{version}-%{release}
Provides:      golang(%{import_path}/partition/grubenv) = %{version}-%{release}
Provides:      golang(%{import_path}/partition/ubootenv) = %{version}-%{release}
Provides:      golang(%{import_path}/polkit) = %{version}-%{release}
Provides:      golang(%{import_path}/progress) = %{version}-%{release}
Provides:      golang(%{import_path}/progress/progresstest) = %{version}-%{release}
Provides:      golang(%{import_path}/release) = %{version}-%{release}
Provides:      golang(%{import_path}/snap) = %{version}-%{release}
Provides:      golang(%{import_path}/snap/pack) = %{version}-%{release}
Provides:      golang(%{import_path}/snap/snapdir) = %{version}-%{release}
Provides:      golang(%{import_path}/snap/snapenv) = %{version}-%{release}
Provides:      golang(%{import_path}/snap/snaptest) = %{version}-%{release}
Provides:      golang(%{import_path}/snap/squashfs) = %{version}-%{release}
Provides:      golang(%{import_path}/spdx) = %{version}-%{release}
Provides:      golang(%{import_path}/store) = %{version}-%{release}
Provides:      golang(%{import_path}/store/storetest) = %{version}-%{release}
Provides:      golang(%{import_path}/strutil) = %{version}-%{release}
Provides:      golang(%{import_path}/systemd) = %{version}-%{release}
Provides:      golang(%{import_path}/tests/lib/fakestore/refresh) = %{version}-%{release}
Provides:      golang(%{import_path}/tests/lib/fakestore/store) = %{version}-%{release}
Provides:      golang(%{import_path}/testutil) = %{version}-%{release}
Provides:      golang(%{import_path}/timeout) = %{version}-%{release}
Provides:      golang(%{import_path}/timeutil) = %{version}-%{release}
Provides:      golang(%{import_path}/userd) = %{version}-%{release}
Provides:      golang(%{import_path}/wrappers) = %{version}-%{release}
Provides:      golang(%{import_path}/x11) = %{version}-%{release}

%description devel
%{summary}

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
%{summary}

This package contains unit tests for project
providing packages with %{import_path} prefix.
%endif

%prep
%setup -q

%if ! 0%{?with_bundled}
# Ensure there's no bundled stuff accidentally leaking in...
rm -rf vendor/*
%else
# Unpack the vendor tarball too...
%setup -q -T -D -b 1
%endif

%build
# Generate version files
./mkversion.sh "%{version}-%{release}"

# We don't want/need squashfuse in the rpm
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
sed -e "s:github.com/snapcore/bolt:github.com/boltdb/bolt:g" -i advisor/*.go
%endif

# We have to build snapd first to prevent the build from
# building various things from the tree without additional
# set tags.
%gobuild -o bin/snapd $GOFLAGS %{import_path}/cmd/snapd
%gobuild -o bin/snap $GOFLAGS %{import_path}/cmd/snap
%gobuild -o bin/snapctl $GOFLAGS %{import_path}/cmd/snapctl

# To ensure things work correctly with base snaps,
# snap-exec and snap-update-ns need to be built statically
%gobuild_static -o bin/snap-exec $GOFLAGS %{import_path}/cmd/snap-exec
%gobuild_static -o bin/snap-update-ns $GOFLAGS %{import_path}/cmd/snap-update-ns

%if 0%{?rhel}
# There's no static link library for libseccomp in RHEL/CentOS...
sed -e "s/-Bstatic -lseccomp/-Bstatic/g" -i cmd/snap-seccomp/*.go
%endif
%gobuild -o bin/snap-seccomp $GOFLAGS %{import_path}/cmd/snap-seccomp

# Build SELinux module
pushd ./data/selinux
make SHARE="%{_datadir}" TARGETS="snappy"
popd

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
# selinux support is not yet available, for now just disable apparmor
# FIXME: add --enable-caps-over-setuid as soon as possible (setuid discouraged!)
%configure \
    --disable-apparmor \
    --libexecdir=%{_libexecdir}/snapd/ \
    --enable-nvidia-biarch \
    %{?with_multilib:--with-32bit-libdir=%{_prefix}/lib} \
    --with-snap-mount-dir=%{_sharedstatedir}/snapd/snap \
    --with-merged-usr

%make_build
popd

# Build systemd and dbus units, and env files
pushd ./data
make BINDIR="%{_bindir}" LIBEXECDIR="%{_libexecdir}" \
     SYSTEMDSYSTEMUNITDIR="%{_unitdir}" \
     SNAP_MOUNT_DIR="%{_sharedstatedir}/snapd/snap" \
     SNAPD_ENVIRONMENT_FILE="%{_sysconfdir}/sysconfig/snapd"
popd

%install
install -d -p %{buildroot}%{_bindir}
install -d -p %{buildroot}%{_libexecdir}/snapd
install -d -p %{buildroot}%{_mandir}/man1
install -d -p %{buildroot}%{_unitdir}
install -d -p %{buildroot}%{_sysconfdir}/profile.d
install -d -p %{buildroot}%{_sysconfdir}/sysconfig
install -d -p %{buildroot}%{_sharedstatedir}/snapd/assertions
install -d -p %{buildroot}%{_sharedstatedir}/snapd/desktop/applications
install -d -p %{buildroot}%{_sharedstatedir}/snapd/device
install -d -p %{buildroot}%{_sharedstatedir}/snapd/hostfs
install -d -p %{buildroot}%{_sharedstatedir}/snapd/mount
install -d -p %{buildroot}%{_sharedstatedir}/snapd/seccomp/bpf
install -d -p %{buildroot}%{_sharedstatedir}/snapd/snaps
install -d -p %{buildroot}%{_sharedstatedir}/snapd/lib/gl
install -d -p %{buildroot}%{_sharedstatedir}/snapd/lib/gl32
install -d -p %{buildroot}%{_sharedstatedir}/snapd/lib/vulkan
install -d -p %{buildroot}%{_sharedstatedir}/snapd/snap/bin
install -d -p %{buildroot}%{_localstatedir}/snap
install -d -p %{buildroot}%{_localstatedir}/cache/snapd
install -d -p %{buildroot}%{_datadir}/polkit-1/actions
install -d -p %{buildroot}%{_datadir}/selinux/devel/include/contrib
install -d -p %{buildroot}%{_datadir}/selinux/packages

# Install snap and snapd
install -p -m 0755 bin/snap %{buildroot}%{_bindir}
install -p -m 0755 bin/snap-exec %{buildroot}%{_libexecdir}/snapd
install -p -m 0755 bin/snapctl %{buildroot}%{_bindir}/snapctl
install -p -m 0755 bin/snapd %{buildroot}%{_libexecdir}/snapd
install -p -m 0755 bin/snap-update-ns %{buildroot}%{_libexecdir}/snapd
install -p -m 0755 bin/snap-seccomp %{buildroot}%{_libexecdir}/snapd

# Install SELinux module
install -p -m 0644 data/selinux/snappy.if %{buildroot}%{_datadir}/selinux/devel/include/contrib
install -p -m 0644 data/selinux/snappy.pp.bz2 %{buildroot}%{_datadir}/selinux/packages

# Install snap(1) man page
bin/snap help --man > %{buildroot}%{_mandir}/man1/snap.1

# Install the "info" data file with snapd version
install -m 644 -D data/info %{buildroot}%{_libexecdir}/snapd/info

# Install bash completion for "snap"
install -m 644 -D data/completion/snap %{buildroot}%{_datadir}/bash-completion/completions/snap
install -m 644 -D data/completion/complete.sh %{buildroot}%{_libexecdir}/snapd
install -m 644 -D data/completion/etelpmoc.sh %{buildroot}%{_libexecdir}/snapd

# Install snap-confine
pushd ./cmd
%make_install
# Undo the 0000 permissions, they are restored in the files section
chmod 0755 %{buildroot}%{_sharedstatedir}/snapd/void
# We don't use AppArmor
rm -rfv %{buildroot}%{_sysconfdir}/apparmor.d
# ubuntu-core-launcher is dead
rm -fv %{buildroot}%{_bindir}/ubuntu-core-launcher
popd

# Install all systemd and dbus units, and env files
pushd ./data
%make_install SYSTEMDSYSTEMUNITDIR="%{_unitdir}" BINDIR="%{_bindir}" LIBEXECDIR="%{_libexecdir}"
# Remove snappy core specific units
rm -fv %{buildroot}%{_unitdir}/snapd.system-shutdown.service
rm -fv %{buildroot}%{_unitdir}/snapd.snap-repair.*
rm -fv %{buildroot}%{_unitdir}/snapd.core-fixup.*
popd

# Remove snappy core specific scripts
rm %{buildroot}%{_libexecdir}/snapd/snapd.core-fixup.sh

# Install Polkit configuration
install -m 644 -D data/polkit/io.snapcraft.snapd.policy %{buildroot}%{_datadir}/polkit-1/actions

# Disable re-exec by default
echo 'SNAP_REEXEC=0' > %{buildroot}%{_sysconfdir}/sysconfig/snapd

# Create state.json and the README file to be ghosted
touch %{buildroot}%{_sharedstatedir}/snapd/state.json
touch %{buildroot}%{_sharedstatedir}/snapd/snap/README

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
%dir %{_libexecdir}/snapd
%{_libexecdir}/snapd/snapd
%{_libexecdir}/snapd/snap-exec
%{_libexecdir}/snapd/info
%{_libexecdir}/snapd/snap-mgmt
%{_mandir}/man1/snap.1*
%{_datadir}/bash-completion/completions/snap
%{_libexecdir}/snapd/complete.sh
%{_libexecdir}/snapd/etelpmoc.sh
%{_sysconfdir}/profile.d/snapd.sh
%{_unitdir}/snapd.socket
%{_unitdir}/snapd.service
%{_unitdir}/snapd.autoimport.service
%{_unitdir}/var-lib-snapd-snap.mount
%{_datadir}/dbus-1/services/io.snapcraft.Launcher.service
%{_datadir}/polkit-1/actions/io.snapcraft.snapd.policy
%config(noreplace) %{_sysconfdir}/sysconfig/snapd
%dir %{_sharedstatedir}/snapd
%dir %{_sharedstatedir}/snapd/assertions
%dir %{_sharedstatedir}/snapd/desktop
%dir %{_sharedstatedir}/snapd/desktop/applications
%dir %{_sharedstatedir}/snapd/device
%dir %{_sharedstatedir}/snapd/hostfs
%dir %{_sharedstatedir}/snapd/mount
%dir %{_sharedstatedir}/snapd/seccomp
%dir %{_sharedstatedir}/snapd/seccomp/bpf
%dir %{_sharedstatedir}/snapd/lib
%dir %{_sharedstatedir}/snapd/lib/gl
%dir %{_sharedstatedir}/snapd/lib/gl32
%dir %{_sharedstatedir}/snapd/lib/vulkan
%dir %{_sharedstatedir}/snapd/snaps
%dir %{_sharedstatedir}/snapd/snap
%ghost %dir %{_sharedstatedir}/snapd/snap/bin
%dir %{_localstatedir}/cache/snapd
%dir %{_localstatedir}/snap
%ghost %{_sharedstatedir}/snapd/state.json
%{_datadir}/dbus-1/services/io.snapcraft.Launcher.service
%{_datadir}/dbus-1/services/io.snapcraft.Settings.service
%ghost %{_sharedstatedir}/snapd/snap/README

%files -n snap-confine
%doc cmd/snap-confine/PORTING
%license COPYING
%dir %{_libexecdir}/snapd
# For now, we can't use caps
# FIXME: Switch to "%%attr(0755,root,root) %%caps(cap_sys_admin=pe)" asap!
%attr(6755,root,root) %{_libexecdir}/snapd/snap-confine
%{_libexecdir}/snapd/snap-discard-ns
%{_libexecdir}/snapd/snap-seccomp
%{_libexecdir}/snapd/snap-update-ns
%{_libexecdir}/snapd/snap-device-helper
%{_libexecdir}/snapd/system-shutdown
%{_libexecdir}/snapd/snap-gdb-shim
%{_mandir}/man1/snap-confine.1*
%{_mandir}/man5/snap-discard-ns.5*
%attr(0000,root,root) %{_sharedstatedir}/snapd/void


%files selinux
%license data/selinux/COPYING
%doc data/selinux/README.md
%{_datadir}/selinux/packages/snappy.pp.bz2
%{_datadir}/selinux/devel/include/contrib/snappy.if

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
%systemd_post %{snappy_svcs}
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

# Remove all Snappy content if snapd is being fully uninstalled
if [ $1 -eq 0 ]; then
   %{_libexecdir}/snapd/snap-mgmt --purge || :
fi


%postun
%systemd_postun_with_restart %{snappy_svcs}

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


%changelog
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
