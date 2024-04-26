# With Fedora, nothing is bundled. For everything else, bundling is used.
# Amazon-linux 2023 is based on fedora but it is bundled
# To use bundled stuff, use "--with vendorized" on rpmbuild
%if 0%{?fedora} && ! 0%{?amzn2023}
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

# Set if valgrind is to be run
%ifnarch ppc64le
%global with_valgrind 1
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

%global snappy_svcs      snapd.service snapd.socket snapd.autoimport.service snapd.seeded.service snapd.mounts.target snapd.mounts-pre.target
%global snappy_user_svcs snapd.session-agent.service snapd.session-agent.socket

# Until we have a way to add more extldflags to gobuild macro...
# Always use external linking when building static binaries.
%if 0%{?fedora} || 0%{?rhel} >= 8 || 0%{?amzn2023}
%define gobuild_static(o:) go build -buildmode pie -compiler gc -tags="rpm_crashtraceback ${BUILDTAGS:-}" -ldflags "-B 0x$(head -c20 /dev/urandom|od -An -tx1|tr -d ' \\n') -linkmode external -extldflags '%__global_ldflags -static'" -a -v -x %{?**};
%endif
%if 0%{?rhel} == 7
# no pass PIE flags due to https://bugzilla.redhat.com/show_bug.cgi?id=1634486
%define gobuild_static(o:) go build -compiler gc -tags="rpm_crashtraceback ${BUILDTAGS:-}" -ldflags "-B 0x$(head -c20 /dev/urandom|od -An -tx1|tr -d ' \\n') -linkmode external -extldflags '%__global_ldflags -static'" -a -v -x %{?**};
%endif

# These macros are missing BUILDTAGS in RHEL 8/9, see RHBZ#1825138
%if 0%{?rhel} >= 8 || 0%{?amzn2023}
%define gobuild(o:) go build -buildmode pie -compiler gc -tags="rpm_crashtraceback ${BUILDTAGS:-}" -ldflags "-B 0x$(head -c20 /dev/urandom|od -An -tx1|tr -d ' \\n') -linkmode external -extldflags '%__global_ldflags'" -a -v -x %{?**};
%endif

# These macros are not defined in RHEL 7
%if 0%{?rhel} == 7
%define gobuild(o:) go build -compiler gc -tags="rpm_crashtraceback ${BUILDTAGS:-}" -ldflags "-B 0x$(head -c20 /dev/urandom|od -An -tx1|tr -d ' \\n') -linkmode external -extldflags '%__global_ldflags'" -a -v -x %{?**};
%define gotest() go test -compiler gc %{?**};
%endif

# Compat path macros
%{!?_environmentdir: %global _environmentdir %{_prefix}/lib/environment.d}
%{!?_systemdgeneratordir: %global _systemdgeneratordir %{_prefix}/lib/systemd/system-generators}
%{!?_systemd_system_env_generator_dir: %global _systemd_system_env_generator_dir %{_prefix}/lib/systemd/system-environment-generators}
%{!?_tmpfilesdir: %global _tmpfilesdir %{_prefix}/lib/tmpfiles.d}

# Fedora selinux-policy includes 'map' permission on a 'file' class. However,
# Amazon Linux 2 does not have the updated policy containing the fix for
# https://bugzilla.redhat.com/show_bug.cgi?id=1574383.
# For now disable SELinux on Amazon Linux 2 until it's fixed.
%if 0%{?amzn2} == 1
%global with_selinux 0
%endif

Name:           snapd
Version:        2.62
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
BuildRequires: make
BuildRequires:  %{?go_compiler:compiler(go-compiler)}%{!?go_compiler:golang >= 1.9}
BuildRequires:  systemd
BuildRequires:  fakeroot
BuildRequires:  squashfs-tools
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

# Require xdelta for delta updates of snap packages.
%if 0%{?fedora} || ( 0%{?rhel} && 0%{?rhel} > 8 )
%if ! 0%{?amzn2023}
Requires:       xdelta
%endif
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
BuildRequires: golang(go.etcd.io/bbolt)
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
BuildRequires: golang(gopkg.in/yaml.v3)
%endif

%description
Snappy is a modern, cross-distribution, transactional package manager
designed for working with self-contained, immutable packages.

%package -n snap-confine
Summary:        Confinement system for snap applications
License:        GPLv3
BuildRequires:  autoconf
BuildRequires:  autoconf-archive
BuildRequires:  automake
BuildRequires:  make
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
%if 0%{?with_valgrind}
BuildRequires:  valgrind
%endif
BuildRequires:  %{_bindir}/rst2man
%if 0%{?fedora} && ! 0%{?amzn2023}
# AL2023 does not have shellcheck
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
BuildRequires:  selinux-policy
BuildRequires:  selinux-policy-devel
BuildRequires:  make
%{?selinux_requires}

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
Requires:      golang(go.etcd.io/bbolt)
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
Requires:      golang(gopkg.in/yaml.v3)
%else
# These Provides are unversioned because the sources in
# the bundled tarball are unversioned (they go by git commit)
# *sigh*... I hate golang...
Provides:      bundled(golang(go.etcd.io/bbolt))
Provides:      bundled(golang(github.com/coreos/go-systemd/activation))
Provides:      bundled(golang(github.com/godbus/dbus))
Provides:      bundled(golang(github.com/godbus/dbus/introspect))
Provides:      bundled(golang(github.com/gorilla/mux))
Provides:      bundled(golang(github.com/jessevdk/go-flags))
Provides:      bundled(golang(github.com/juju/ratelimit))
Provides:      bundled(golang(github.com/kr/pretty))
Provides:      bundled(golang(github.com/kr/text))
Provides:      bundled(golang(github.com/mvo5/goconfigparser))
Provides:      bundled(golang(github.com/seccomp/libseccomp-golang))
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
Provides:      bundled(golang(gopkg.in/yaml.v3))
%endif

# Generated by gofed
Provides:      golang(%{import_path}/advisor) = %{version}-%{release}
Provides:      golang(%{import_path}/arch) = %{version}-%{release}
Provides:      golang(%{import_path}/asserts) = %{version}-%{release}
Provides:      golang(%{import_path}/asserts/assertstest) = %{version}-%{release}
Provides:      golang(%{import_path}/asserts/internal) = %{version}-%{release}
Provides:      golang(%{import_path}/asserts/signtool) = %{version}-%{release}
Provides:      golang(%{import_path}/asserts/snapasserts) = %{version}-%{release}
Provides:      golang(%{import_path}/asserts/sysdb) = %{version}-%{release}
Provides:      golang(%{import_path}/asserts/systestkeys) = %{version}-%{release}
Provides:      golang(%{import_path}/boot) = %{version}-%{release}
Provides:      golang(%{import_path}/boot/boottest) = %{version}-%{release}
Provides:      golang(%{import_path}/bootloader) = %{version}-%{release}
Provides:      golang(%{import_path}/bootloader/androidbootenv) = %{version}-%{release}
Provides:      golang(%{import_path}/bootloader/assets) = %{version}-%{release}
Provides:      golang(%{import_path}/bootloader/assets/genasset) = %{version}-%{release}
Provides:      golang(%{import_path}/bootloader/bootloadertest) = %{version}-%{release}
Provides:      golang(%{import_path}/bootloader/efi) = %{version}-%{release}
Provides:      golang(%{import_path}/bootloader/grubenv) = %{version}-%{release}
Provides:      golang(%{import_path}/bootloader/lkenv) = %{version}-%{release}
Provides:      golang(%{import_path}/bootloader/ubootenv) = %{version}-%{release}
Provides:      golang(%{import_path}/client) = %{version}-%{release}
Provides:      golang(%{import_path}/client/clientutil) = %{version}-%{release}
Provides:      golang(%{import_path}/cmd/snap) = %{version}-%{release}
Provides:      golang(%{import_path}/cmd/snap-bootstrap) = %{version}-%{release}
Provides:      golang(%{import_path}/cmd/snap-bootstrap/triggerwatch) = %{version}-%{release}
Provides:      golang(%{import_path}/cmd/snap-exec) = %{version}-%{release}
Provides:      golang(%{import_path}/cmd/snap-failure) = %{version}-%{release}
Provides:      golang(%{import_path}/cmd/snap-preseed) = %{version}-%{release}
Provides:      golang(%{import_path}/cmd/snap-recovery-chooser) = %{version}-%{release}
Provides:      golang(%{import_path}/cmd/snap-repair) = %{version}-%{release}
Provides:      golang(%{import_path}/cmd/snap-seccomp) = %{version}-%{release}
Provides:      golang(%{import_path}/cmd/snap-seccomp/syscalls) = %{version}-%{release}
Provides:      golang(%{import_path}/cmd/snap-update-ns) = %{version}-%{release}
Provides:      golang(%{import_path}/cmd/snapctl) = %{version}-%{release}
Provides:      golang(%{import_path}/cmd/snapd) = %{version}-%{release}
Provides:      golang(%{import_path}/cmd/snaplock) = %{version}-%{release}
Provides:      golang(%{import_path}/cmd/snaplock/runinhibit) = %{version}-%{release}
Provides:      golang(%{import_path}/daemon) = %{version}-%{release}
Provides:      golang(%{import_path}/dbusutil) = %{version}-%{release}
Provides:      golang(%{import_path}/dbusutil/dbustest) = %{version}-%{release}
Provides:      golang(%{import_path}/desktop/notification) = %{version}-%{release}
Provides:      golang(%{import_path}/desktop/notification/notificationtest) = %{version}-%{release}
Provides:      golang(%{import_path}/dirs) = %{version}-%{release}
Provides:      golang(%{import_path}/docs) = %{version}-%{release}
Provides:      golang(%{import_path}/features) = %{version}-%{release}
Provides:      golang(%{import_path}/gadget) = %{version}-%{release}
Provides:      golang(%{import_path}/gadget/edition) = %{version}-%{release}
Provides:      golang(%{import_path}/gadget/install) = %{version}-%{release}
Provides:      golang(%{import_path}/gadget/internal) = %{version}-%{release}
Provides:      golang(%{import_path}/gadget/quantity) = %{version}-%{release}
Provides:      golang(%{import_path}/httputil) = %{version}-%{release}
Provides:      golang(%{import_path}/i18n) = %{version}-%{release}
Provides:      golang(%{import_path}/i18n/xgettext-go) = %{version}-%{release}
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
Provides:      golang(%{import_path}/kernel) = %{version}-%{release}
Provides:      golang(%{import_path}/logger) = %{version}-%{release}
Provides:      golang(%{import_path}/metautil) = %{version}-%{release}
Provides:      golang(%{import_path}/netutil) = %{version}-%{release}
Provides:      golang(%{import_path}/osutil) = %{version}-%{release}
Provides:      golang(%{import_path}/osutil/disks) = %{version}-%{release}
Provides:      golang(%{import_path}/osutil/mount) = %{version}-%{release}
Provides:      golang(%{import_path}/osutil/squashfs) = %{version}-%{release}
Provides:      golang(%{import_path}/osutil/strace) = %{version}-%{release}
Provides:      golang(%{import_path}/osutil/sys) = %{version}-%{release}
Provides:      golang(%{import_path}/osutil/udev/crawler) = %{version}-%{release}
Provides:      golang(%{import_path}/osutil/udev/netlink) = %{version}-%{release}
Provides:      golang(%{import_path}/overlord) = %{version}-%{release}
Provides:      golang(%{import_path}/overlord/assertstate) = %{version}-%{release}
Provides:      golang(%{import_path}/overlord/assertstate/assertstatetest) = %{version}-%{release}
Provides:      golang(%{import_path}/overlord/auth) = %{version}-%{release}
Provides:      golang(%{import_path}/overlord/cmdstate) = %{version}-%{release}
Provides:      golang(%{import_path}/overlord/configstate) = %{version}-%{release}
Provides:      golang(%{import_path}/overlord/configstate/config) = %{version}-%{release}
Provides:      golang(%{import_path}/overlord/configstate/configcore) = %{version}-%{release}
Provides:      golang(%{import_path}/overlord/configstate/proxyconf) = %{version}-%{release}
Provides:      golang(%{import_path}/overlord/configstate/settings) = %{version}-%{release}
Provides:      golang(%{import_path}/overlord/devicestate) = %{version}-%{release}
Provides:      golang(%{import_path}/overlord/devicestate/devicestatetest) = %{version}-%{release}
Provides:      golang(%{import_path}/overlord/devicestate/fde) = %{version}-%{release}
Provides:      golang(%{import_path}/overlord/devicestate/internal) = %{version}-%{release}
Provides:      golang(%{import_path}/overlord/healthstate) = %{version}-%{release}
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
Provides:      golang(%{import_path}/overlord/snapstate/policy) = %{version}-%{release}
Provides:      golang(%{import_path}/overlord/snapstate/snapstatetest) = %{version}-%{release}
Provides:      golang(%{import_path}/overlord/standby) = %{version}-%{release}
Provides:      golang(%{import_path}/overlord/state) = %{version}-%{release}
Provides:      golang(%{import_path}/overlord/storecontext) = %{version}-%{release}
Provides:      golang(%{import_path}/polkit) = %{version}-%{release}
Provides:      golang(%{import_path}/progress) = %{version}-%{release}
Provides:      golang(%{import_path}/progress/progresstest) = %{version}-%{release}
Provides:      golang(%{import_path}/randutil) = %{version}-%{release}
Provides:      golang(%{import_path}/release) = %{version}-%{release}
Provides:      golang(%{import_path}/sandbox) = %{version}-%{release}
Provides:      golang(%{import_path}/sandbox/apparmor) = %{version}-%{release}
Provides:      golang(%{import_path}/sandbox/cgroup) = %{version}-%{release}
Provides:      golang(%{import_path}/sandbox/seccomp) = %{version}-%{release}
Provides:      golang(%{import_path}/sandbox/selinux) = %{version}-%{release}
Provides:      golang(%{import_path}/sanity) = %{version}-%{release}
Provides:      golang(%{import_path}/secboot) = %{version}-%{release}
Provides:      golang(%{import_path}/seed) = %{version}-%{release}
Provides:      golang(%{import_path}/seed/internal) = %{version}-%{release}
Provides:      golang(%{import_path}/seed/seedtest) = %{version}-%{release}
Provides:      golang(%{import_path}/seed/seedwriter) = %{version}-%{release}
Provides:      golang(%{import_path}/snap) = %{version}-%{release}
Provides:      golang(%{import_path}/snap/channel) = %{version}-%{release}
Provides:      golang(%{import_path}/snap/internal) = %{version}-%{release}
Provides:      golang(%{import_path}/snap/naming) = %{version}-%{release}
Provides:      golang(%{import_path}/snap/pack) = %{version}-%{release}
Provides:      golang(%{import_path}/snap/snapdir) = %{version}-%{release}
Provides:      golang(%{import_path}/snap/snapenv) = %{version}-%{release}
Provides:      golang(%{import_path}/snap/snapfile) = %{version}-%{release}
Provides:      golang(%{import_path}/snap/snaptest) = %{version}-%{release}
Provides:      golang(%{import_path}/snap/squashfs) = %{version}-%{release}
Provides:      golang(%{import_path}/snapdenv) = %{version}-%{release}
Provides:      golang(%{import_path}/snapdtool) = %{version}-%{release}
Provides:      golang(%{import_path}/spdx) = %{version}-%{release}
Provides:      golang(%{import_path}/store) = %{version}-%{release}
Provides:      golang(%{import_path}/store/storetest) = %{version}-%{release}
Provides:      golang(%{import_path}/strutil) = %{version}-%{release}
Provides:      golang(%{import_path}/strutil/chrorder) = %{version}-%{release}
Provides:      golang(%{import_path}/strutil/quantity) = %{version}-%{release}
Provides:      golang(%{import_path}/strutil/shlex) = %{version}-%{release}
Provides:      golang(%{import_path}/sysconfig) = %{version}-%{release}
Provides:      golang(%{import_path}/systemd) = %{version}-%{release}
Provides:      golang(%{import_path}/testutil) = %{version}-%{release}
Provides:      golang(%{import_path}/timeout) = %{version}-%{release}
Provides:      golang(%{import_path}/timeutil) = %{version}-%{release}
Provides:      golang(%{import_path}/timings) = %{version}-%{release}
Provides:      golang(%{import_path}/usersession/agent) = %{version}-%{release}
Provides:      golang(%{import_path}/usersession/autostart) = %{version}-%{release}
Provides:      golang(%{import_path}/usersession/client) = %{version}-%{release}
Provides:      golang(%{import_path}/usersession/userd) = %{version}-%{release}
Provides:      golang(%{import_path}/usersession/userd/ui) = %{version}-%{release}
Provides:      golang(%{import_path}/usersession/xdgopenproxy) = %{version}-%{release}
Provides:      golang(%{import_path}/wrappers) = %{version}-%{release}
Provides:      golang(%{import_path}/x11) = %{version}-%{release}

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
# Apply patches
%autopatch -p1


%build

# Build snapd
mkdir -p src/github.com/snapcore
ln -s ../../../ src/github.com/snapcore/snapd

%if ! 0%{?with_bundled}
export GOPATH=$(pwd):%{gopath}
# FIXME: move spec file really to a go.mod world instead of this hack
rm -f go.mod
export GO111MODULE=off
#%else
#export GOPATH=$(pwd):$(pwd)/Godeps/_workspace:%{gopath}
%endif

# Generate version files
./mkversion.sh "%{version}-%{release}"

# see https://github.com/gofed/go-macros/blob/master/rpm/macros.d/macros.go-compilers-golang
BUILDTAGS=
%if 0%{?with_test_keys}
BUILDTAGS="withtestkeys nosecboot"
%else
BUILDTAGS="nosecboot"
%endif

%if ! 0%{?with_bundled}
# We don't need the snapcore fork for bolt - it is just a fix on ppc
sed -e "s:github.com/snapcore/bolt:github.com/boltdb/bolt:g" -i advisor/*.go
%endif

# We have to build snapd first to prevent the build from
# building various things from the tree without additional
# set tags.
%gobuild -o bin/snapd $GOFLAGS %{import_path}/cmd/snapd
BUILDTAGS="${BUILDTAGS} nomanagers"
%gobuild -o bin/snap $GOFLAGS %{import_path}/cmd/snap
%gobuild -o bin/snap-failure $GOFLAGS %{import_path}/cmd/snap-failure

# To ensure things work correctly with base snaps,
# snap-exec, snap-update-ns, and snapctl need to be built statically
(
%if 0%{?rhel} >= 7
    # since RH Developer tools 2018.4 (and later releases),
    # the go-toolset module is built with FIPS compliance that
    # defaults to using libcrypto.so which gets loaded at runtime via dlopen(),
    # disable that functionality for statically built binaries
    BUILDTAGS="${BUILDTAGS} no_openssl"
%endif
    %gobuild_static -o bin/snap-exec $GOFLAGS %{import_path}/cmd/snap-exec
    %gobuild_static -o bin/snap-update-ns $GOFLAGS %{import_path}/cmd/snap-update-ns
    %gobuild_static -o bin/snapctl $GOFLAGS %{import_path}/cmd/snapctl
)

%if 0%{?rhel}
# There's no static link library for libseccomp in RHEL/CentOS...
sed -e "s/-Bstatic -lseccomp/-Bstatic/g" -i cmd/snap-seccomp/*.go
%endif
%gobuild -o bin/snap-seccomp $GOFLAGS %{import_path}/cmd/snap-seccomp

%if 0%{?with_selinux}
(
%if 0%{?rhel} == 7
    M4PARAM='-D distro_rhel7'
%endif
%if 0%{?rhel} == 7 || 0%{?rhel} == 8
    # RHEL7, RHEL8 are missing the BPF interfaces from their reference policy
    M4PARAM="$M4PARAM -D no_bpf"
%endif
    # Build SELinux module
    cd ./data/selinux
    # pass M4PARAM in env instead of as an override, so that make can still
    # manipulate it freely, for more details see:
    # https://www.gnu.org/software/make/manual/html_node/Override-Directive.html
    M4PARAM="$M4PARAM" make SHARE="%{_datadir}" TARGETS="snappy"
)
%endif

# Build snap-confine
pushd ./cmd
autoreconf --force --install --verbose
# FIXME: add --enable-caps-over-setuid as soon as possible (setuid discouraged!)
%configure \
    --disable-apparmor \
%if 0%{?with_selinux}
    --enable-selinux \
%endif
%if 0%{?rhel} == 7
    --disable-bpf \
%endif
    --libexecdir=%{_libexecdir}/snapd/ \
    --enable-nvidia-biarch \
    %{?with_multilib:--with-32bit-libdir=%{_prefix}/lib} \
    --with-snap-mount-dir=%{_sharedstatedir}/snapd/snap \
    --enable-merged-usr

%make_build %{!?with_valgrind:HAVE_VALGRIND=}
popd

# Build systemd units, dbus services, and env files
pushd ./data
make BINDIR="%{_bindir}" LIBEXECDIR="%{_libexecdir}" DATADIR="%{_datadir}" \
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
install -d -p %{buildroot}%{_tmpfilesdir}
install -d -p %{buildroot}%{_unitdir}
install -d -p %{buildroot}%{_userunitdir}
install -d -p %{buildroot}%{_sysconfdir}/profile.d
install -d -p %{buildroot}%{_sysconfdir}/sysconfig
install -d -p %{buildroot}%{_sharedstatedir}/snapd/assertions
install -d -p %{buildroot}%{_sharedstatedir}/snapd/cookie
install -d -p %{buildroot}%{_sharedstatedir}/snapd/cgroup
install -d -p %{buildroot}%{_sharedstatedir}/snapd/dbus-1/services
install -d -p %{buildroot}%{_sharedstatedir}/snapd/dbus-1/system-services
install -d -p %{buildroot}%{_sharedstatedir}/snapd/desktop/applications
install -d -p %{buildroot}%{_sharedstatedir}/snapd/device
install -d -p %{buildroot}%{_sharedstatedir}/snapd/hostfs
install -d -p %{buildroot}%{_sharedstatedir}/snapd/inhibit
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
install -m 644 -D data/completion/bash/snap %{buildroot}%{_datadir}/bash-completion/completions/snap
install -m 644 -D data/completion/bash/complete.sh %{buildroot}%{_libexecdir}/snapd
install -m 644 -D data/completion/bash/etelpmoc.sh %{buildroot}%{_libexecdir}/snapd
# Install zsh completion for "snap"
install -d -p %{buildroot}%{_datadir}/zsh/site-functions
install -m 644 -D data/completion/zsh/_snap %{buildroot}%{_datadir}/zsh/site-functions/_snap

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
%make_install BINDIR="%{_bindir}" LIBEXECDIR="%{_libexecdir}" DATADIR="%{_datadir}" \
              SYSTEMDSYSTEMUNITDIR="%{_unitdir}" SYSTEMDUSERUNITDIR="%{_userunitdir}" \
              TMPFILESDIR="%{_tmpfilesdir}" \
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
rm -fv %{buildroot}%{_unitdir}/snapd.recovery-chooser-trigger.service

# Remove snappy core specific scripts and binaries
rm %{buildroot}%{_libexecdir}/snapd/snapd.core-fixup.sh
rm %{buildroot}%{_libexecdir}/snapd/system-shutdown

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
    ldd bin/$binary 2>&1 | grep 'not a dynamic executable'
done

# snapd tests
%if 0%{?with_check} && 0%{?with_unit_test} && 0%{?with_devel}
%if ! 0%{?with_bundled}
export GOPATH=%{buildroot}/%{gopath}:%{gopath}
%else
export GOPATH=%{buildroot}/%{gopath}:$(pwd)/Godeps/_workspace:%{gopath}
%endif
# FIXME: we are in the go.mod world now but without this things fall apart
export GO111MODULE=off
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
%{_datadir}/zsh/site-functions/_snap
%{_libexecdir}/snapd/snapd.run-from-snap
%{_sysconfdir}/profile.d/snapd.sh
%{_mandir}/man8/snapd-env-generator.8*
%{_systemd_system_env_generator_dir}/snapd-env-generator
%{_unitdir}/snapd.socket
%{_unitdir}/snapd.service
%{_unitdir}/snapd.autoimport.service
%{_unitdir}/snapd.failure.service
%{_unitdir}/snapd.seeded.service
%{_unitdir}/snapd.mounts.target
%{_unitdir}/snapd.mounts-pre.target
%{_userunitdir}/snapd.session-agent.service
%{_userunitdir}/snapd.session-agent.socket
%{_tmpfilesdir}/snapd.conf
%{_datadir}/dbus-1/services/io.snapcraft.Launcher.service
%{_datadir}/dbus-1/services/io.snapcraft.SessionAgent.service
%{_datadir}/dbus-1/services/io.snapcraft.Settings.service
%{_datadir}/dbus-1/session.d/snapd.session-services.conf
%{_datadir}/dbus-1/system.d/snapd.system-services.conf
%{_datadir}/polkit-1/actions/io.snapcraft.snapd.policy
%{_datadir}/applications/io.snapcraft.SessionAgent.desktop
%{_datadir}/fish/vendor_conf.d/snapd.fish
%{_datadir}/snapd/snapcraft-logo-bird.svg
%{_sysconfdir}/xdg/autostart/snap-userd-autostart.desktop
%config(noreplace) %{_sysconfdir}/sysconfig/snapd
%dir %{_sharedstatedir}/snapd
%dir %{_sharedstatedir}/snapd/assertions
%dir %{_sharedstatedir}/snapd/cookie
%dir %{_sharedstatedir}/snapd/cgroup
%dir %{_sharedstatedir}/snapd/dbus-1
%dir %{_sharedstatedir}/snapd/dbus-1/services
%dir %{_sharedstatedir}/snapd/dbus-1/system-services
%dir %{_sharedstatedir}/snapd/desktop
%dir %{_sharedstatedir}/snapd/desktop/applications
%dir %{_sharedstatedir}/snapd/device
%dir %{_sharedstatedir}/snapd/hostfs
%dir %{_sharedstatedir}/snapd/inhibit
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
# this is typically owned by zsh, but we do not want to explicitly require zsh
%dir %{_datadir}/zsh
%dir %{_datadir}/zsh/site-functions
# similar case for fish
%dir %{_datadir}/fish/vendor_conf.d

%files -n snap-confine
%doc cmd/snap-confine/PORTING
%license COPYING
%dir %{_libexecdir}/snapd
# For now, we can't use caps
# FIXME: Switch to "%%attr(0755,root,root) %%caps(cap_sys_admin=pe)" asap!
%attr(4755,root,root) %{_libexecdir}/snapd/snap-confine
%{_libexecdir}/snapd/snap-device-helper
%{_libexecdir}/snapd/snap-discard-ns
%{_libexecdir}/snapd/snap-gdb-shim
%{_libexecdir}/snapd/snap-gdbserver-shim
%{_libexecdir}/snapd/snap-seccomp
%{_libexecdir}/snapd/snap-update-ns
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
%systemd_user_postun_with_restart %{snappy_user_svcs}

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
* Thu Mar 21 2024 Ernest Lotter <ernest.lotter@canonical.com>
- New upstream release 2.62
 - Aspects based configuration schema support (experimental)
 - Refresh app awareness support for UI (experimental)
 - Support for user daemons by introducing new control switches
   --user/--system/--users for service start/stop/restart
   (experimental)
 - Add AppArmor prompting experimental flag (feature currently
   unsupported)
 - Installation of local snap components of type test
 - Packaging of components with snap pack
 - Expose experimental features supported/enabled in snapd REST API
   endpoint /v2/system-info
 - Support creating and removing recovery systems for use by factory
   reset
 - Enable API route for creating and removing recovery systems using
   /v2/systems with action create and /v2/systems/{label} with action
   remove
 - Lift requirements for fde-setup hook for single boot install
 - Enable single reboot gadget update for UC20+
 - Allow core to be removed on classic systems
 - Support for remodeling on hybrid systems
 - Install desktop files on Ubuntu Core and update after snapd
   upgrade
 - Upgrade sandbox features to account for cgroup v2 device filtering
 - Support snaps to manage their own cgroups
 - Add support for AppArmor 4.0 unconfined profile mode
 - Add AppArmor based read access to /etc/default/keyboard
 - Upgrade to squashfuse 0.5.0
 - Support useradd utility to enable removing Perl dependency for
   UC24+
 - Support for recovery-chooser to use console-conf snap
 - Add support for --uid/--gid using strace-static
 - Add support for notices (from pebble) and expose via the snapd
   REST API endpoints /v2/notices and /v2/notice
 - Add polkit authentication for snapd REST API endpoints
   /v2/snaps/{snap}/conf and /v2/apps
 - Add refresh-inhibit field to snapd REST API endpoint /v2/snaps
 - Add refresh-inhibited select query to REST API endpoint /v2/snaps
 - Take into account validation sets during remodeling
 - Improve offline remodeling to use installed revisions of snaps to
   fulfill the remodel revision requirement
 - Add rpi configuration option sdtv_mode
 - When snapd snap is not installed, pin policy ABI to 4.0 or 3.0 if
   present on host
 - Fix gadget zero-sized disk mapping caused by not ignoring zero
   sized storage traits
 - Fix gadget install case where size of existing partition was not
   correctly taken into account
 - Fix trying to unmount early kernel mount if it does not exist
 - Fix restarting mount units on snapd start
 - Fix call to udev in preseed mode
 - Fix to ensure always setting up the device cgroup for base bare
   and core24+
 - Fix not copying data from newly set homedirs on revision change
 - Fix leaving behind empty snap home directories after snap is
   removed (resulting in broken symlink)
 - Fix to avoid using libzstd from host by adding to snapd snap
 - Fix autorefresh to correctly handle forever refresh hold
 - Fix username regex allowed for system-user assertion to not allow
   '+'
 - Fix incorrect application icon for notification after autorefresh
   completion
 - Fix to restart mount units when changed
 - Fix to support AppArmor running under incus
 - Fix case of snap-update-ns dropping synthetic mounts due to
   failure to match  desired mount dependencies
 - Fix parsing of base snap version to enable pre-seeding of Ubuntu
   Core Desktop
 - Fix packaging and tests for various distributions
 - Add remoteproc interface to allow developers to interact with
   Remote Processor Framework which enables snaps to load firmware to
   ARM Cortex microcontrollers
 - Add kernel-control interface to enable controlling the kernel
   firmware search path
 - Add nfs-mount interface to allow mounting of NFS shares
 - Add ros-opt-data interface to allow snaps to access the host
   /opt/ros/ paths
 - Add snap-refresh-observe interface that provides refresh-app-
   awareness clients access to relevant snapd API endpoints
 - steam-support interface: generalize Pressure Vessel root paths and
   allow access to driver information, features and container
   versions
 - steam-support interface: make implicit on Ubuntu Core Desktop
 - desktop interface: improved support for Ubuntu Core Desktop and
   limit autoconnection to implicit slots
 - cups-control interface: make autoconnect depend on presence of
   cupsd on host to ensure it works on classic systems
 - opengl interface: allow read access to /usr/share/nvidia
 - personal-files interface: extend to support automatic creation of
   missing parent directories in write paths
 - network-control interface: allow creating /run/resolveconf
 - network-setup-control and network-setup-observe interfaces: allow
   busctl bind as required for systemd 254+
 - libvirt interface: allow r/w access to /run/libvirt/libvirt-sock-
   ro and read access to /var/lib/libvirt/dnsmasq/**
 - fwupd interface: allow access to IMPI devices (including locking
   of device nodes), sysfs attributes needed by amdgpu and the COD
   capsule update directory
 - uio interface: allow configuring UIO drivers from userspace
   libraries
 - serial-port interface: add support for NXP Layerscape SoC
 - lxd-support interface: add attribute enable-unconfined-mode to
   require LXD to opt-in to run unconfined
 - block-devices interface: add support for ZFS volumes
 - system-packages-doc interface: add support for reading jquery and
   sphinx documentation
 - system-packages-doc interface: workaround to prevent autoconnect
   failure for snaps using base bare
 - microceph-support interface: allow more types of block devices to
   be added as an OSD
 - mount-observe interface: allow read access to
   /proc/{pid}/task/{tid}/mounts and proc/{pid}/task/{tid}/mountinfo
 - polkit interface: changed to not be implicit on core because
   installing policy files is not possible
 - upower-observe interface: allow stats refresh
 - gpg-public-keys interface: allow creating lock file for certain
   gpg operations
 - shutdown interface: allow access to SetRebootParameter method
 - media-control interface: allow device file locking
 - u2f-devices interface: support for Trustkey G310H, JaCarta U2F,
   Kensington VeriMark Guard, RSA DS100, Google Titan v2

* Wed Mar 06 2024 Ernest Lotter <ernest.lotter@canonical.com>
- New upstream release 2.61.3
 - Install systemd files in correct location for 24.04

* Fri Feb 16 2024 Ernest Lotter <ernest.lotter@canonical.com>
- New upstream release 2.61.2
 - Fix to enable plug/slot sanitization for prepare-image
 - Fix panic when device-service.access=offline
 - Support offline remodeling
 - Allow offline update only remodels without serial
 - Fail early when remodeling to old model revision
 - Fix to enable plug/slot sanitization for validate-seed
 - Allow removal of core snap on classic systems
 - Fix network-control interface denial for file lock on /run/netns
 - Add well-known core24 snap-id
 - Fix remodel snap installation order
 - Prevent remodeling from UC18+ to UC16
 - Fix cups auto-connect on classic with cups snap installed
 - u2f-devices interface support for GoTrust Idem Key with USB-C
 - Fix to restore services after unlink failure
 - Add libcudnn.so to Nvidia libraries
 - Fix skipping base snap download due to false snapd downgrade
   conflict

* Fri Nov 24 2023 Ernest Lotter <ernest.lotter@canonical.com>
- New upstream release 2.61.1
 - Stop requiring default provider snaps on image building and first
   boot if alternative providers are included and available
 - Fix auth.json access for login as non-root group ID
 - Fix incorrect remodelling conflict when changing track to older
   snapd version
 - Improved check-rerefresh message
 - Fix UC16/18 kernel/gadget update failure due volume mismatch with
   installed disk
 - Stop auto-import of assertions during install modes
 - Desktop interface exposes GetIdletime
 - Polkit interface support for new polkit versions
 - Fix not applying snapd snap changes in tracked channel when remodelling

* Fri Oct 13 2023 Philip Meulengracht <philip.meulengracht@canonical.com>
- New upstream release 2.61
 - Fix control of activated services in 'snap start' and 'snap stop'
 - Correctly reflect activated services in 'snap services'
 - Disabled services are no longer enabled again when snap is
   refreshed
 - interfaces/builtin: added support for Token2 U2F keys
 - interfaces/u2f-devices: add Swissbit iShield Key
 - interfaces/builtin: update gpio apparmor to match pattern that
   contains multiple subdirectories under /sys/devices/platform
 - interfaces: add a polkit-agent interface
 - interfaces: add pcscd interface
 - Kernel command-line can now be edited in the gadget.yaml
 - Only track validation-sets in run-mode, fixes validation-set
   issues on first boot.
 - Added support for using store.access to disable access to snap
   store
 - Support for fat16 partition in gadget
 - Pre-seed authority delegation is now possible
 - Support new system-user name  daemon
 - Several bug fixes and improvements around remodelling
 - Offline remodelling support

* Fri Sep 15 2023 Michael Vogt <michael.vogt@ubuntu.com>
- New upstream release 2.60.4
 - i/b/qualcomm_ipc_router.go: switch to plug/slot and add socket
   permission
 - interfaces/builtin: fix custom-device udev KERNEL values
 - overlord: allow the firmware-updater snap to install user daemons
 - interfaces: allow loopback as a block-device

* Fri Aug 25 2023 Michael Vogt <michael.vogt@ubuntu.com>
- New upstream release 2.60.3
 - i/b/shared-memory: handle "private" plug attribute in shared-
   memory interface correctly
 - i/apparmor: support for home.d tunables from /etc/

* Fri Aug 04 2023 Michael Vogt <michael.vogt@ubuntu.com>
- New upstream release 2.60.2
 - i/builtin: allow directories in private /dev/shm
 - i/builtin: add read access to /proc/task/schedstat in system-
   observe
 - snap-bootstrap: print version information at startup
 - go.mod: update gopkg.in/yaml.v3 to v3.0.1 to fix CVE-2022-28948
 - snap, store: filter out invalid snap edited links from store info
   and persisted state
 - o/configcore: write netplan defaults to 00-snapd-config on seeding
 - snapcraft.yaml: pull in apparmor_parser optimization patches from
   https://gitlab.com/apparmor/apparmor/-/merge_requests/711
 - snap-confine: fix missing \0 after readlink
 - cmd/snap: hide append-integrity-data
 - interfaces/opengl: add support for ARM Mali

* Tue Jul 04 2023 Michael Vogt <michael.vogt@ubuntu.com>
- New upstream release 2.60.1
 - install: fallback to lazy unmount() in writeFilesystemContent
 - data: include "modprobe.d" and "modules-load.d" in preseeded blob
 - gadget: fix install test on armhf
 - interfaces: fix typo in network_manager_observe
 - sandbox/apparmor: don't let vendored apparmor conflict with system
 - gadget/update: set parts in laid out data from the ones matched
 - many: move SnapConfineAppArmorDir from dirs to sandbox/apparmor
 - many: stop using `-O no-expr-simplify` in apparmor_parser
 - go.mod: update secboot to latest uc22 branch

* Thu Jun 15 2023 Michael Vogt <michael.vogt@ubuntu.com>
- New upstream release 2.60
 - Support for dynamic snapshot data exclusions
 - Apparmor userspace is vendored inside the snapd snap
 - Added a default-configure hook that exposes gadget default
   configuration options to snaps during first install before
   services are started
 - Allow install from initrd to speed up the initial installation
   for systems that do not have a install-device hook
 - New `snap sign --chain` flag that appends the account and
   account-key assertions
 - Support validation-sets in the model assertion
 - Support new "min-size" field in gadget.yaml
 - New interface: "userns"

* Sat May 27 2023 Michael Vogt <michael.vogt@ubuntu.com>
- New upstream release 2.59.5
 - Explicitly disallow the use of ioctl + TIOCLINUX
   This fixes CVE-2023-1523.

* Fri May 12 2023 Michael Vogt <michael.vogt@ubuntu.com>
- New upstream release 2.59.4
 - Retry when looking for disk label on non-UEFI systems
   (LP: #2018977)
 - Fix remodel from UC20 to UC22

* Wed May 03 2023 Michael Vogt <michael.vogt@ubuntu.com>
- New upstream release 2.59.3
 - Fix quiet boot
 - i/b/physical_memory_observe: allow reading virt-phys page mappings
 - gadget: warn instead of returning error if overlapping with GPT
   header
 - overlord,wrappers: restart always enabled units
 - go.mod: update github.com/snapcore/secboot to latest uc22
 - boot: make sure we update assets for the system-seed-null role
 - many: ignore case for vfat partitions when validating

* Tue Apr 18 2023 Michael Vogt <michael.vogt@ubuntu.com>
- New upstream release 2.59.2
 - Notify users when a user triggered auto refresh finished

* Tue Mar 28 2023 Michael Vogt <michael.vogt@ubuntu.com>
- New upstream release 2.59.1
 - Add udev rules from steam-devices to steam-support interface
 - Bugfixes for layout path checking, dm_crypt permissions,
   mount-control interface parameter checking, kernel commandline
   parsing, docker-support, refresh-app-awareness

* Fri Mar 10 2023 Michael Vogt <michael.vogt@ubuntu.com>
- New upstream release 2.59
 - Support setting extra kernel command line parameters via snap
   configuration and under a gadget allow-list
 - Support for Full-Disk-Encryption using ICE
 - Support for arbitrary home dir locations via snap configuration
 - New nvidia-drivers-support interface
 - Support for udisks2 snap
 - Pre-download of snaps ready for refresh and automatic refresh of
   the snap when all apps are closed
 - New microovn interface
 - Support uboot with `CONFIG_SYS_REDUNDAND_ENV=n`
 - Make "snap-preseed --reset" re-exec when needed
 - Update the fwupd interface to support fully confined fwupd
 - The memory,cpu,thread quota options are no longer experimental
 - Support debugging snap client requests via the
   `SNAPD_CLIENT_DEBUG_HTTP` environment variable
 - Support ssh listen-address via snap configuration
 - Support for quotas on single services
 - prepare-image now takes into account snapd versions going into
   the image, including in the kernel initrd, to fetch supported
   assertion formats

* Tue Feb 21 2023 Michael Vogt <michael.vogt@ubuntu.com>
- New upstream release 2.58.3
 - interfaces/screen-inhibit-control: Add support for xfce-power-
   manager
 - interfaces/network-manager: do not show ptrace read
   denials
 - interfaces: relax rules for mount-control `what` for functionfs
 - cmd/snap-bootstrap: add support for snapd_system_disk
 - interfaces/modem-manager: add net_admin capability
 - interfaces/network-manager: add permission for OpenVPN
 - httputil: fix checking x509 certification error on go 1.20
 - i/b/fwupd: allow reading host os-release
 - boot: on classic+modes `MarkBootSuccessfull` does not need a base
 - boot: do not include `base=` in modeenv for classic+modes installs
 - tests: add spread test that validates revert on boot for core does
   not happen on classic+modes
 - snapstate: only take boot participants into account in
   UpdateBootRevisions
 - snapstate: refactor UpdateBootRevisions() to make it easier to
   check for boot.SnapTypeParticipatesInBoot()

* Wed Jan 25 2023 Michael Vogt <michael.vogt@ubuntu.com>
- New upstream release 2.58.2
 - bootloader: fix dirty build by hardcoding copyright year

* Mon Jan 23 2023 Michael Vogt <michael.vogt@ubuntu.com>
- New upstream release 2.58.1
 - secboot: detect lockout mode in CheckTPMKeySealingSupported
 - cmd/snap-update-ns: prevent keeping unneeded mountpoints
 - o/snapstate: do not infinitely retry when an update fails during
   seeding
 - interfaces/modem-manager: add permissions for NETLINK_ROUTE
 - systemd/emulation.go: use `systemctl --root` to enable/disable
 - snap: provide more error context in `NotSnapError`
 - interfaces: add read access to /run for cryptsetup
 - boot: avoid reboot loop if there is a bad try kernel
 - devicestate: retry serial acquire on time based certificate
   errors
 - o/devicestate: run systemctl daemon-reload after install-device
   hook
 - cmd/snap,daemon: add 'held' to notes in 'snap list'
 - o/snapshotstate: check snapshots are self-contained on import
 - cmd/snap: show user+gating hold info in 'snap info'
 - daemon: expose user and gating holds at /v2/snaps/{name}

* Thu Dec 01 2022 Michael Vogt <michael.vogt@ubuntu.com>
- New upstream release 2.58
 - many: Use /tmp/snap-private-tmp for per-snap private tmps
 - data: Add systemd-tmpfiles configuration to create private tmp dir
 - cmd/snap: test allowed and forbidden refresh hold values
 - cmd/snap: be more consistent in --hold help and err messages
 - cmd/snap: error on refresh holds that are negative or too short
 - o/homedirs: make sure we do not write to /var on build time
 - image: make sure file customizations happen also when we have
   defaultscause
 - tests/fde-on-classic: set ubuntu-seed label in seed partitions
 - gadget: system-seed-null should also have fs label ubuntu-seed
 - many: gadget.HasRole, ubuntu-seed can come also from system-seed-
   null
 - o/devicestate: fix paths for retrieving recovery key on classic
 - cmd/snap-confine: do not discard const qualifier
 - interfaces: allow python3.10+ in the default template
 - o/restart: fix PendingForSystemRestart
 - interfaces: allow wayland slot snaps to access shm files created
   by Firefox
 - o/assertstate: add Sequence() to val set tracking
 - o/assertstate: set val set 'Current' to pinned sequence
 - tests: tweak the libvirt interface test to work on 22.10
 - tests: use system-seed-null role on classic with modes tests
 - boot: add directory for data on install
 - o/devicestate: change some names from esp to seed/seed-null
 - gadget: add system-seed-null role
 - o/devicestate: really add error to new error message
 - restart,snapstate: implement reboot-required notifications on
   classic
 - many: avoid automatic system restarts on classic through new
   overlord/restart logic
 - release: Fix WSL detection in LXD
 - o/state: introduce WaitStatus
 - interfaces: Fix desktop interface rules for document portal
 - client: remove classic check for `snap recovery --show-
   keys`
 - many: create snapd.mounts targets to schedule mount units
 - image: enable sysfs overlay for UC preseeding
 - i/b/network-control: add permissions for using AF_XDP
 - i/apparmor: move mocking of home and overlay conditions to osutil
 - tests/main/degraded: ignore man-db update failures in CentOS
 - cmd/snap: fix panic when running snap w/ flag but w/o subcommand
 - tests: save snaps generated during image preaparation
 - tests: skip building snapd based on new env var
 - client: remove misleading comments in ValidateApplyOptions
 - boot/seal: add debug traces for bootchains
 - bootloader/assets: fix grub.cfg when there are no labels
 - cmd/snap: improve refresh hold's output
 - packaging: enable BPF in RHEL9
 - packaging: do not traverse filesystems in postrm script
 - tests: get microk8s from another branch
 - bootloader: do not specify Core version in grub entry
 - many: refresh --hold follow-up
 - many: support refresh hold/unhold to API and CLI
 - many: expand fully handling links mapping in all components, in
   the API and in snap info
 - snap/system_usernames,tests: Azure IoT Edge system usernames
 - interface: Allow access to
   org.freedesktop.DBus.ListActivatableNames via system-observe
   interface
 - o/devicestate,daemon: use the expiration date from the assertion
   in user-state and REST api (user-removal 4/n)
 - gadget: add unit tests for new install functions for FDE on
   classic
 - cmd/snap-seccomp: fix typo in AF_XDP value
 - tests/connected-after-reboot-revert: run also on UC16
 - kvm: allow read of AMD-SEV parameters
 - data: tweak apt integration config var
 - o/c/configcore: add faillock configuration
 - tests: use dbus-daemon instead of dbus-launch
 - packaging: remove unclean debian-sid patch
 - asserts: add keyword 'user-presence' keyword in system-user
   assertion (auto-removal 3/n)
 - interfaces: steam-support allow pivot /run/media and /etc/nvidia
   mount
 - aspects: initial code
 - overlord: process auto-import assertion at first boot
 - release, snapd-apparmor, syscheck: distinguish WSL1 and WSL2
 - tests: fix lxd-mount-units in ubuntu kinetic
 - tests: new variable used to configure the kernel command line in
   nested tests
 - go.mod: update to newer secboot/uc22 branch
 - autopkgtests: fix running autopkgtest on kinetic
 - tests: remove squashfs leftovers in fakeinstaller
 - tests: create partition table in fakeinstaller
 - o/ifacestate: introduce DebugAutoConnectCheck hook
 - tests: use test-snapd-swtpm instead of swtpm-mvo snap in nested
   helper
 - interfaces/polkit: do not require polkit directory if no file is
   needed
 - o/snapstate: be consistent not creating per-snap save dirs for
   classic models
 - inhibit: use hintFile()
 - tests: use `snap prepare-image` in fde-on-classic mk-image.sh
 - interfaces: add microceph interface
 - seccomp: allow opening XDP sockets
 - interfaces: allow access to icon subdirectories
 - tests: add minimal-smoke test for UC22 and increase minimal RAM
 - overlord: introduce hold levels in the snapstate.Hold* API
 - o/devicestate: support mounting ubuntu-save also on classic with
   modes
 - interfaces: steam-support allow additional mounts
 - fakeinstaller: format SystemDetails result with %+v
 - cmd/libsnap-confine-private: do not panic on chmod failure
 - tests: ensure that fakeinstaller put the seed into the right place
 - many: add stub services for prompting
 - tests: add libfwupd and libfwupdplugin5 to openSUSE dependencies
 - o/snapstate: fix snaps-hold pruning/reset in the presence of
   system holding
 - many: add support for setting up encryption from installer
 - many: support classic snaps in the context of classic and extended
   models
 - cmd/snap,daemon: allow zero values from client to daemon for
   journal rate limit
 - boot,o/devicestate: extend HasFDESetupHook to consider unrelated
   kernels
 - cmd/snap: validation set refresh-enforce CLI support + spread test
 - many: fix filenames written in modeenv for base/gadget plus drive-
   by TODO
 - seed: fix seed test to use a pseudo-random byte sequence
 - cmd/snap-confine: remove setuid calls from cgroup init code
 - boot,o/devicestate: introduce and use MakeRunnableStandaloneSystem
 - devicestate,boot,tests: make `fakeinstaller` test work
 - store: send Snap-Device-Location header with cloud information
 - overlord: fix unit tests after merging master in
 - o/auth: move HasUserExpired into UserState and name it HasExpired,
   and add unit tests for this
 - o/auth: rename NewUserData to NewUserParams
 - many: implementation of finish install step handlers
 - overlord: auto-resolve validation set enforcement constraints
 - i/backends,o/ifacestate: cleanup backends.All
 - cmd/snap-confine: move bind-mount setup into separate function
 - tests/main/mount-ns: update namespace for 18.04
 - o/state: Hold pseudo-error for explicit holding, concept of
   pending changes in prune logic
 - many: support extended classic models that omit kernel/gadget
 - data/selinux: allow snapd to detect WSL
 - overlord: add code to remove users that has an expiration date set
 - wrappers,snap/quota: clear LogsDirectory= in the service unit for
   journal namespaces
 - daemon: move user add, remove operations to overlord device state
 - gadget: implement write content from gadget information
 - {device,snap}state: fix ineffectual assignments
 - daemon: support validation set refresh+enforce in API
 - many: rename AddAffected* to RegisterAffected*, add
   Change|State.Has, fix a comment
 - many: reset store session when setting proxy.store
 - overlord/ifacestate: fix conflict detection of auto-connection
 - interfaces: added read/write access to /proc/self/coredump_filter
   for process-control
 - interfaces: add read access to /proc/cgroups and
   /proc/sys/vm/swappiness to system-observe
 - fde: run fde-reveal-key with `DefaultDependencies=no`
 - many: don't concatenate non-constant format strings
 - o/devicestate: fix non-compiling test
 - release, snapd-apparmor: fixed outdated WSL detection
 - many: add todos discussed in the review in
   tests/nested/manual/fde-on-classic, snapstate cleanups
 - overlord: run install-device hook during factory reset
 - i/b/mount-control: add optional `/` to umount rules
 - gadget/install: split Run in several functions
 - o/devicestate: refactor some methods as preparation for install
   steps implementation
 - tests: fix how snaps are cached in uc22
 - tests/main/cgroup-tracking-failure: fix rare failure in Xenial and
   Bionic
 - many: make {Install,Initramfs}{{,Host},Writable}Dir a  function
 - tests/nested/manual/core20: fix manual test after changes to
   'tests.nested exec'
 - tests: move the unit tests system to 22.04 in github actions
   workflow
 - tests: fix nested errors uc20
 - boot: rewrite switch in SnapTypeParticipatesInBoot()
 - gadget: refactor to allow usage from the installer
 - overlord/devicestate: support for mounting ubuntu-save before the
   install-device hook
 - many: allow to install/update kernels/gadgets on classic with
   modes
 - tests: fix issues related to dbus session and localtime in uc18
 - many: support home dirs located deeper under /home
 - many: refactor tests to use explicit strings instead of
   boot.Install{Initramfs,Host}{Writable,FDEData}Dir
 - boot: add factory-reset cases for boot-flags
 - tests: disable quota tests on arm devices using ubuntu core
 - tests: fix unbound SPREAD_PATH variable on nested debug session
 - overlord: start turning restart into a full state manager
 - boot: apply boot logic also for classic with modes boot snaps
 - tests: fix snap-env test on debug section when no var files were
   created
 - overlord,daemon: allow returning errors when requesting a restart
 - interfaces: login-session-control: add further D-Bus interfaces
 - snapdenv: added wsl to userAgent
 - o/snapstate: support running multiple ops transactionally
 - store: use typed valset keys in store package
 - daemon: add `ensureStateSoon()` when calling systems POST api
 - gadget: add rules for validating classic with modes gadget.yaml
   files
 - wrappers: journal namespaces did not honor journal.persistent
 - many: stub devicestate.Install{Finish,SetupStorageEncryption}()
 - sandbox/cgroup: don't check V1 cgroup if V2 is active
 - seed: add support to load auto import assertion
 - tests: fix preseed tests for arm systems
 - include/lk: update LK recovery environment definition to include
   device lock state used by bootloader
 - daemon: return `storage-encryption` in /systems/<label> reply
 - tests: start using remote tools from snapd-testing-tools project
   in nested tests
 - tests: fix non mountable filesystem error in interfaces-udisks2
 - client: clarify what InstallStep{SetupStorageEncryption,Finish} do
 - client: prepare InstallSystemOptions for real use
 - usersession: Remove duplicated struct
 - o/snapstate: support specific revisions in UpdateMany/InstallMany
 - i/b/system_packages_doc: restore access to Libreoffice
   documentation
 - snap/quota,wrappers: allow using 0 values for the journal rate
   limit
 - tests: add kinetic images to the gce bucket for preseed test
 - multiple: clear up naming convention for thread quota
 - daemon: implement stub `"action": "install"`
 - tests/main/snap-quota-{install/journal}: fix unstable spread tests
 - tests: remove code for old systems not supported anymore
 - tests: third part of the nested helper cleanup
 - image: clean snapd mount after preseeding
 - tests: use the new ubuntu kinetic image
 - i/b/system_observe: honour root dir when checking for
   /boot/config-*
 - tests: restore microk8s test on 16.04
 - tests: run spread tests on arm64 instances in google cloud
 - tests: skip interfaces-udisks2 in fedora
 - asserts,boot,secboot: switch to a secboot version measuring
   classic
 - client: add API for GET /systems/<label>
 - overlord: frontend for --quota-group support (2/2)
 - daemon: add GET support for `/systems/<seed-label>`
 - i/b/system-observe: allow reading processes security label
 - many: support '--purge' when removing multiple snaps
 - snap-confine: remove obsolete code
 - interfaces: rework logic of unclashMountEntries
 - data/systemd/Makefile: add comment warning about "snapd." prefix
 - interfaces: grant access to speech-dispatcher socket (bug 1787245)
 - overlord/servicestate: disallow removal of quota group with any
   limits set
 - data: include snapd/mounts in preseeded blob
 - many: Set SNAPD_APPARMOR_REEXEC=1
 - store/tooling,tests: support UBUNTU_STORE_URL override env var
 - multiple: clear up naming convention for cpu-set quota
 - tests: improve and standardize debug section on tests
 - device: add new DeviceManager.encryptionSupportInfo()
 - tests: check snap download with snapcraft v7+ export-login auth
   data
 - cmd/snap-bootstrap: changes to be able to boot classic rootfs
 - tests: fix debug section for test uc20-create-partitions
 - overlord: --quota-group support (1/2)
 - asserts,cmd/snap-repair: drop not pursued
   AuthorityDelegation/signatory-id
 - snap-bootstrap: add CVM mode* snap-bootstrap: add classic runmode
 - interfaces: make polkit implicit on core if /usr/libexec/polkitd
   exists
 - multiple: move arguments for auth.NewUser into a struct (auto-
   removal 1/n)
 - overlord: track security profiles for non-active snaps
 - tests: remove NESTED_IMAGE_ID from nested manual tests
 - tests: add extra space to ubuntu bionic
 - store/tooling: support using snapcraft v7+ base64-encoded auth
   data
 - overlord: allow seeding in the case of classic with modes system
 - packaging/*/tests/integrationtests: reload ssh.service, not
   sshd.service
 - tests: rework snap-logs-journal test and add missing cleanup
 - tests: add spread test for journal quotas
 - tests: run spread tests in ubuntu kinetic
 - o/snapstate: extend support for holding refreshes
 - devicestate: return an error in checkEncryption() if KernelInfo
   fails
 - tests: fix sbuild test on debian sid
 - o/devicestate: do not run tests in this folder twice
 - sandbox/apparmor: remove duplicate hook into testing package
 - many: refactor store code to be able to use simpler form of auth
   creds
 - snap,store: drop support/consideration for anonymous download urls
 - data/selinux: allow snaps to read certificates
 - many: add Is{Core,Classic}Boot() to DeviceContext
 - o/assertstate: don't refresh enforced validation sets during check
 - go.mod: replace maze.io/x/crypto with local repo
 - many: fix unnecessary use of fmt.Sprintf
 - bootloader,systemd: fix `don't use Yoda conditions (ST1017)`
 - HACKING.md: extend guidelines with common review comments
 - many: progress bars should use the overridable stdouts
 - tests: remove ubuntu 21.10 from sru validation
 - tests: import remote tools
 - daemon,usersession: switch from HeaderMap to Header in tests
 - asserts: add some missing `c.Check()` in the asserts test
 - strutil: fix VersionCompare() to allow multiple `-` in the version
 - testutil: remove unneeded `fmt.Sprintf`
 - boot: remove some unneeded `fmt.Sprintf()` calls
 - tests: implement prepare_gadget and prepare_base and unify all the
   version
 - o/snapstate: refactor managed refresh schedule logic
 - o/assertstate, snapasserts: implementation of
   assertstate.TryEnforceValidationSets function
 - interfaces: add kconfig paths to system-observe
 - dbusutil: move debian patch into dbustest
 - many: change name and input of CheckProvenance to clarify usage
 - tests: Fix a missing parameter in command to wait for device
 - tests: Work-around non-functional --wait on systemctl
 - tests: unify the way the snapd/core and kernel are repacked in
   nested helper
 - tests: skip interfaces-ufisks2 on centos-9
 - i/b/mount-control: allow custom filesystem types
 - interfaces,metautil: make error handling in getPaths() more
   targeted
 - cmd/snap-update-ns: handle mountpoint removal failures with EBUSY
 - tests: fix pc-kernel repacking
 - systemd: add `WantedBy=default.target` to snap mount units
 - tests: disable microk8s test on 16.04

* Tue Nov 15 2022 Michael Vogt <michael.vogt@ubuntu.com>
- New upstream release 2.57.6
  - SECURITY UPDATE: Local privilege escalation
    - snap-confine: Fix race condition in snap-confine when preparing a
      private tmp mount namespace for a snap
    - CVE-2022-3328

* Mon Oct 17 2022 Michael Vogt <michael.vogt@ubuntu.com>
- New upstream release 2.57.5
 - image: clean snapd mount after preseeding
 - wrappers,snap/quota: clear LogsDirectory= in the service unit
   for journal namespaces
 - cmd/snap,daemon: allow zero values from client to daemon for
   journal rate-limit
 - interfaces: steam-support allow pivot /run/media and /etc/nvidia
   mount
 - o/ifacestate: introduce DebugAutoConnectCheck hook
 - release, snapd-apparmor, syscheck: distinguish WSL1 and WSL2
 - autopkgtests: fix running autopkgtest on kinetic
 - interfaces: add microceph interface
 - interfaces: steam-support allow additional mounts
 - many: add stub services
 - interfaces: add kconfig paths to system-observe
 - i/b/system_observe: honour root dir when checking for
   /boot/config-*
 - interfaces: grant access to speech-dispatcher socket
 - interfaces: rework logic of unclashMountEntries

* Thu Sep 29 2022 Michael Vogt <michael.vogt@ubuntu.com>
- New upstream release 2.57.4
 - release, snapd-apparmor: fixed outdated WSL detection
 - overlord/ifacestate: fix conflict detection of auto-connection
 - overlord: run install-device hook during factory reset
 - image/preseed/preseed_linux: add missing new line
 - boot: add factory-reset cases for boot-flags.
 - interfaces: added read/write access to /proc/self/coredump_filter
   for process-control
 - interfaces: add read access to /proc/cgroups and
   /proc/sys/vm/swappiness to system-observe
 - fde: run fde-reveal-key with `DefaultDependencies=no`
 - snapdenv: added wsl to userAgent
 - tests: fix restore section for persistent-journal-namespace
 - i/b/mount-control: add optional `/` to umount rules
 - cmd/snap-bootstrap: changes to be able to boot classic rootfs
 - cmd/snap-bootstrap: add CVM mode

* Thu Sep 15 2022 Michael Vogt <michael.vogt@ubuntu.com>
- New upstream release 2.57.3
 - wrappers: journal namespaces did not honor journal.persistent
 - snap/quota,wrappers: allow using 0 values for the journal rate to
   override the system default values
 - multiple: clear up naming convention for cpu-set quota
 - i/b/mount-control: allow custom filesystem types
 - i/b/system-observe: allow reading processes security label
 - sandbox/cgroup: don't check V1 cgroup if V2 is active
 - asserts,boot,secboot: switch to a secboot version measuring
   classic

* Fri Sep 02 2022 Michael Vogt <michael.vogt@ubuntu.com>
- New upstream release 2.57.2
 - store/tooling,tests: support UBUNTU_STORE_URL override env var
 - packaging/*/tests/integrationtests: reload ssh.service, not
   sshd.service
 - tests: check snap download with snapcraft v7+ export-login auth
   data
 - store/tooling: support using snapcraft v7+ base64-encoded auth
   data
 - many: progress bars should use the overridable stdouts
 - many: refactor store code to be able to use simpler form of auth
   creds
 - snap,store: drop support/consideration for anonymous download urls
 - data: include snapd/mounts in preseeded blob
 - many: Set SNAPD_APPARMOR_REEXEC=1
 - overlord: track security profiles for non-active snaps

* Wed Aug 10 2022 Alberto Mardegan <alberto.mardegan@canonical.com>
- New upstream release 2.57.1
 - cmd/snap-update-ns: handle mountpoint removal failures with EBUSY
 - cmd/snap-update-ns: print current mount entries
 - cmd/snap-update-ns: check the unused mounts with a cleaned path
 - snap-confine: disable -Werror=array-bounds in __overflow tests to
   fix build error on Ubuntu 22.10
 - systemd: add `WantedBy=default.target` to snap mount units
   (LP: #1983528)

* Thu Jul 28 2022 Michael Vogt <michael.vogt@ubuntu.com>
- New upstream release 2.57
 - tests: Fix calls to systemctl is-system-running
 - osutil/disks: handle GPT for 4k disk and too small tables
 - packaging: import change from the 2.54.3-1.1 upload
 - many: revert "features: disable refresh-app-awarness by default
   again"
 - tests: improve robustness of preparation for regression/lp-1803542
 - tests: get the ubuntu-image binary built with test keys
 - tests: remove commented code from lxd test
 - interfaces/builtin: add more permissions for steam-support
 - tests: skip interfaces-network-control on i386
 - tests: tweak the "tests/nested/manual/connections" test
 - interfaces: posix-mq: allow specifying message queue paths as an
   array
 - bootloader/assets: add ttyS0,115200n8 to grub.cfg
 - i/b/desktop,unity7: remove name= specification on D-Bus signals
 - tests: ensure that microk8s does not produce DENIED messages
 - many: support non-default provenance snap-revisions in
   DeriveSideInfo
 - tests: fix `core20-new-snapd-does-not-break-old-initrd` test
 - many: device and provenance revision authority cross checks
 - tests: fix nested save-data test on 22.04
 - sandbox/cgroup: ignore container slices when tracking snaps
 - tests: improve 'ignore-running' spread test
 - tests: add `debug:` section to `tests/nested/manual/connections`
 - tests: remove leaking `pc-kernel.snap` in `repack_kernel_snap`
 - many: preparations for revision authority cross checks including
   device scope
 - daemon,overlord/servicestate: followup changes from PR #11960 to
   snap logs
 - cmd/snap: fix visual representation of 'AxB%' cpu quota modifier.
 - many: expose and support provenance from snap.yaml metadata
 - overlord,snap: add support for per-snap storage on ubuntu-save
 - nested: fix core-early-config nested test
 - tests: revert lxd change to support nested lxd launch
 - tests: add invariant check for leftover cgroup scopes
 - daemon,systemd: introduce support for namespaces in 'snap logs'
 - cmd/snap: do not track apps that wish to stay outside of the life-
   cycle system
 - asserts: allow classic + snaps models and add distribution to
   model
 - cmd/snap: add snap debug connections/connection commands
 - data: start snapd after time-set.target
 - tests: remove ubuntu 21.10 from spread tests due to end of life
 - tests: Update the whitebox word to avoid inclusive naming issues
 - many: mount gadget in run folder
 - interfaces/hardware-observe: clean up reading access to sysfs
 - tests: use overlayfs for interfaces-opengl-nvidia test
 - tests: update fake-netplan-apply test for 22.04
 - tests: add executions for ubuntu 22.04
 - tests: enable centos-9
 - tests: make more robust the files check in preseed-core20 test
 - bootloader/assets: add fallback entry to grub.cfg
 - interfaces/apparmor: add permissions for per-snap directory on
   ubuntu-save partition
 - devicestate: add more path to `fixupWritableDefaultDirs()`
 - boot,secboot: reset DA lockout counter after successful boot
 - many: Revert "overlord,snap: add support for per-snap storage on
   ubuntu-save"
 - overlord,snap: add support for per-snap storage on ubuntu-save
 - tests: exclude centos-7 from kernel-module-load test
 - dirs: remove unused SnapAppArmorAdditionalDir
 - boot,device: extract SealedKey helpers from boot to device
 - boot,gadget: add new `device.TpmLockoutAuthUnder()` and use it
 - interfaces/display-control: allow changing brightness value
 - asserts: add more context to key expiry error
 - many: introduce IsUndo flag in LinkContext
 - i/apparmor: allow calling which.debianutils
 - tests: new profile id for apparmor in test preseed-core20
 - tests: detect 403 in apt-hooks and skip test in this case
 - overlord/servicestate: restart the relevant journald service when
   a journal quota group is modified
 - client,cmd/snap: add journal quota frontend (5/n)
 - gadget/device: introduce package which provides helpers for
   locations of things
 - features: disable refresh-app-awarness by default again
 - many: install bash completion files in writable directory
 - image: fix handling of var/lib/extrausers when preseeding
   uc20
 - tests: force version 2.48.3 on xenial ESM
 - tests: fix snap-network-erros on uc16
 - cmd/snap-confine: be compatible with a snap rootfs built as a
   tmpfs
 - o/snapstate: allow install of unasserted gadget/kernel on
   dangerous models
 - interfaces: dynamic loading of kernel modules
 - many: add optional primary key provenance to snap-revision, allow
   delegating via snap-declaration revision-authority
 - tests: fix boringcripto errors in centos7
 - tests: fix snap-validate-enforce in opensuse-tumbleweed
 - test: print User-Agent on failed checks
 - interfaces: add memory stats to system_observe
 - interfaces/pwm: Remove implicitOnCore/implicitOnClassic
 - spread: add openSUSE Leap 15.4
 - tests: disable core20-to-core22 nested test
 - tests: fix nested/manual/connections test
 - tests: add spread test for migrate-home command
 - overlord/servicestate: refresh security profiles when services are
   affected by quotas
 - interfaces/apparmor: add missing apparmor rules for journal
   namespaces
 - tests: add nested test variant that adds 4k sector size
 - cmd/snap: fix test failing due to timezone differences
 - build-aux/snap: build against the snappy-dev/image PPA
 - daemon: implement api handler for refresh with enforced validation
   sets
 - preseed: suggest to install "qemu-user-static"
 - many: add migrate-home debug command
 - o/snapstate: support passing validation sets to storehelpers via
   RevisionOptions
 - cmd/snapd-apparmor: fix unit tests on distros which do not support
   reexec
 - o/devicestate: post factory reset ensure, spread test update
 - tests/core/basic20: Enable on uc22
 - packaging/arch: install snapd-apparmor
 - o/snapstate: support migrating snap home as change
 - tests: enable snapd.apparmor service in all the opensuse systems
 - snapd-apparmor: add more integration-ish tests
 - asserts: store required revisions for missing snaps in
   CheckInstalledSnaps
 - overlord/ifacestate: fix path for journal redirect
 - o/devicestate: factory reset with encryption
 - cmd/snapd-apparmor: reimplement snapd-apparmor in Go
 - squashfs: improve error reporting when `unsquashfs` fails
 - o/assertstate: support multiple extra validation sets in
   EnforcedValidationSets
 - tests: enable mount-order-regression test for arm devices
 - tests: fix interfaces network control
 - interfaces: update AppArmor template to allow read the memory …
 - cmd/snap-update-ns: add /run/systemd to unrestricted paths
 - wrappers: fix LogNamespace being written to the wrong file
 - boot: release the new PCR handles when sealing for factory reset
 - tests: add support fof uc22 in test uboot-unpacked-assets
 - boot: post factory reset cleanup
 - tests: add support for uc22 in listing test
 - spread.yaml: add ubuntu-22.04-06 to qemu-nested
 - gadget: check also mbr type when testing for implicit data
   partition
 - interfaces/system-packages-doc: allow read-only access to
   /usr/share/cups/doc-root/ and /usr/share/gimp/2.0/help/
 - tests/nested/manual/core20-early-config: revert changes that
   disable netplan checks
 - o/ifacestate: warn if the snapd.apparmor service is disabled
 - tests: add spread execution for fedora 36
 - overlord/hookstate/ctlcmd: fix timestamp coming out of sync in
   unit tests
 - gadget/install: do not assume dm device has same block size as
   disk
 - interfaces: update network-control interface with permissions
   required by resolvectl
 - secboot: stage and transition encryption keys
 - secboot, boot: support and use alternative PCR handles during
   factory reset
 - overlord/ifacestate: add journal bind-mount snap layout when snap
   is in a journal quota group (4/n)
 - secboot/keymgr, cmd/snap-fde-keymgr: two step encryption key
   change
 - cmd/snap: cleanup and make the code a bit easier to read/maintain
   for quota options
 - overlord/hookstate/ctlcmd: add 'snapctl model' command (3/3)
 - cmd/snap-repair: fix snap-repair tests silently failing
 - spread: drop openSUSE Leap 15.2
 - interfaces/builtin: remove the name=org.freedesktop.DBus
   restriction in cups-control AppArmor rules
 - wrappers: write journald config files for quota groups with
   journal quotas (3/n)
 - o/assertstate: auto aliases for apps that exist
 - o/state: use more detailed NoStateError in state
 - tests/main/interfaces-browser-support: verify jupyter notebooks
   access
 - o/snapstate: exclude services from refresh app awareness hard
   running check
 - tests/main/nfs-support: be robust against umount failures
 - tests: update centos images and add new centos 9 image
 - many: print valid/invalid status on snap validate --monitor
 - secboot, boot: TPM provisioning mode enum, introduce
   reprovisioning
 - tests: allow to re-execute aborted tests
 - cmd/snapd-apparmor: add explicit WSL detection to
   is_container_with_internal_policy
 - tests: avoid launching lxd inside lxd on cloud images
 - interfaces: extra htop apparmor rules
 - gadget/install: encrypted system factory reset support
 - secboot: helpers for dealing with PCR handles and TPM resources
 - systemd: improve error handling for systemd-sysctl command
 - boot, secboot: separate the TPM provisioning and key sealing
 - o/snapstate: fix validation sets restoring and snap revert on
   failed refresh
 - interfaces/builtin/system-observe: extend access for htop
 - cmd/snap: support custom apparmor features dir with snap prepare-
   image
 - interfaces/mount-observe: Allow read access to /run/mount/utab
 - cmd/snap: add help strings for set-quota options
 - interfaces/builtin: add README file
 - cmd/snap-confine: mount support cleanups
 - overlord: execute snapshot cleanup in task
 - i/b/accounts_service: fix path of introspectable objects
 - interfaces/opengl: update allowed PCI accesses for RPi
 - configcore: add core.system.ctrl-alt-del-action config option
 - many: structured startup timings
 - spread: switch back to building ubuntu-image from source
 - many: optional recovery keys
 - tests/lib/nested: fix unbound variable
 - run-checks: fail on equality checks w/ ErrNoState
 - snap-bootstrap: Mount as private
 - tests: Test for gadget connections
 - tests: set `br54.dhcp4=false` in the netplan-cfg test
 - tests: core20 preseed/nested spread test
 - systemd: remove the systemctl stop timeout handling
 - interfaces/shared-memory: Update AppArmor permissions for
   mmap+link
 - many: replace ErrNoState equality checks w/ errors.Is()
 - cmd/snap: exit w/ non-zero code on missing snap
 - systemd: fix snapd systemd-unit stop progress notifications
 - .github: Trigger daily riscv64 snapd edge builds
 - interfaces/serial-port: add ttyGS to serial port allow list
 - interfaces/modem-manager: Don't generate DBus plug policy
 - tests: add spread test to test upgrade from release snapd to
   current
 - wrappers: refactor EnsureSnapServices
 - testutil: add ErrorIs test checker
 - tests: import spread shellcheck changes
 - cmd/snap-fde-keymgr: best effort idempotency of add-recovery-key
 - interfaces/udev: refactor handling of udevadm triggers for input
 - secboot: support for changing encryption keys via keymgr

* Wed Jul 13 2022 Michael Vogt <michael.vogt@ubuntu.com>
- New upstream release 2.56.3
 - devicestate: add more path to `fixupWritableDefaultDirs()`
 - many: introduce IsUndo flag in LinkContext
 - i/apparmor: allow calling which.debianutils
 - interfaces: update AppArmor template to allow reading snap's
   memory statistics
 - interfaces: add memory stats to system_observe
 - i/b/{mount,system}-observe: extend access for htop
 - features: disable refresh-app-awarness by default again
 - image: fix handling of var/lib/extrausers when preseeding
   uc20
 - interfaces/modem-manager: Don't generate DBus policy for plugs
 - interfaces/modem-manager: Only generate DBus plug policy on
   Core
 - interfaces/serial_port_test: fix static-checks errors
 - interfaces/serial-port: add USB gadget serial devices (ttyGSX) to
   allowed list
 - interface/serial_port_test: adjust variable IDs

* Wed Jun 15 2022 Michael Vogt <michael.vogt@ubuntu.com>
- New upstream release 2.56.2
 - o/snapstate: exclude services from refresh app awareness hard
   running check
 - cmd/snap: support custom apparmor features dir with snap
   prepare-image

* Wed Jun 15 2022 Michael Vogt <michael.vogt@ubuntu.com>
- New upstream release 2.56.1
 - gadget/install: do not assume dm device has same block size as
   disk
 - gadget: check also mbr type when testing for implicit data
   partition
 - interfaces: update network-control interface with permissions
   required by resolvectl
 - interfaces/builtin: remove the name=org.freedesktop.DBus
   restriction in cups-control AppArmor rules
 - many: print valid/invalid status on snap validate --monitor ...
 - o/snapstate: fix validation sets restoring and snap revert on
   failed refresh
 - interfaces/opengl: update allowed PCI accesses for RPi
 - interfaces/shared-memory: Update AppArmor permissions for
   mmap+linkpaths

* Thu May 19 2022 Michael Vogt <michael.vogt@ubuntu.com>
- New upstream release 2.56
 - portal-info: Add CommonID Field
 - asserts/info,mkversion.sh: capture max assertion formats in
   snapd/info
 - tests: improve the unit testing workflow to run in parallel
 - interfaces: allow map and execute permissions for files on
   removable media
 - tests: add spread test to verify that connections are preserved if
   snap refresh fails
 - tests: Apparmor sandbox profile mocking
 - cmd/snap-fde-keymgr: support for multiple devices and
   authorizations for add/remove recovery key
 - cmd/snap-bootstrap: Listen to keyboard added after start and
   handle switch root
 - interfaces,overlord: add support for adding extra mount layouts
 - cmd/snap: replace existing code for 'snap model' to use shared
   code in clientutil (2/3)
 - interfaces: fix opengl interface on RISC-V
 - interfaces: allow access to the file locking for cryptosetup in
   the dm-crypt interface
 - interfaces: network-manager: add AppArmor rule for configuring
   bridges
 - i/b/hardware-observe.go: add access to the thermal sysfs
 - interfaces: opengl: add rules for NXP i.MX GPU drivers
 - i/b/mount_control: add an optional "/" to the mount target rule
 - snap/quota: add values for journal quotas (journal quota 2/n)
 - tests: spread test for uc20 preseeding covering snap prepare-image
 - o/snapstate: remove deadcode breaking static checks
 - secboot/keymgr: extend unit tests, add helper for identify keyslot
   used error
 - tests: use new snaps.name and snaps.cleanup tools
 - interfaces: tweak getPath() slightly and add some more tests
 - tests: update snapd testing tools
 - client/clientutil: add shared code for printing model assertions
   as yaml or json (1/3)
 - debug-tools: list all snaps
 - cmd/snap: join search terms passed in the command line
 - osutil/disks: partition UUID lookup
 - o/snapshotstate: refactor snapshot read/write logic
 - interfaces: Allow locking in block-devices
 - daemon: /v2/system-recovery-keys remove API
 - snapstate: do not auto-migrate to ~/Snap for core22 just yet
 - tests: run failed tests by default
 - o/snapshotstate: check installed snaps before running 'save' tasks
 - secboot/keymgr: remove recovery key, authorize with existing key
 - deps: bump libseccomp to include build fixes, run unit tests using
   CC=clang
 - cmd/snap-seccomp: only compare the bottom 32-bits of the flags arg
   of copy_file_range
 - osutil/disks: helper for obtaining the UUID of a partition which
   is a mount point source
 - image/preseed: umount the base snap last after writable paths
 - tests: new set of nested tests for uc22
 - tests: run failed tests on nested suite
 - interfaces: posix-mq: add new interface
 - tests/main/user-session-env: remove openSUSE-specific tweaks
 - tests: skip external backend in mem-cgroup-disabled test
 - snap/quota: change the journal quota period to be a time.Duration
 - interfaces/apparmor: allow executing /usr/bin/numfmt in the base
   template
 - tests: add lz4 dependency for jammy to avoid issues repacking
   kernel
 - snap-bootstrap, o/devicestate: use seed parallelism
 - cmd/snap-update-ns: correctly set sticky bit on created
   directories where applicable
 - tests: install snapd while restoring in snap-mgmt
 - .github: skip misspell and ineffassign on go 1.13
 - many: use UC20+/pre-UC20 in user messages as needed
 - o/devicestate: use snap handler for copying and checksuming
   preseeded snaps
 - image, cmd/snap-preseed: allow passing custom apparmor features
   path
 - o/assertstate: fix handling of validation set tracking update in
   enforcing mode
 - packaging: restart our units only after the upgrade
 - interfaces: add a steam-support interface
 - gadget/install, o/devicestate: do not create recovery and
   reinstall keys during installation
 - many: move recovery key responsibility to devicestate/secboot,
   prepare for a future with just optional recovery key
 - tests: do not run mem-cgroup-disabled on external backends
 - snap: implement "star" developers
 - o/devicestate: fix install tests on systems with
   /var/lib/snapd/snap
 - cmd/snap-fde-keymgr, secboot: followup cleanups
 - seed: let SnapHandler provided a different final path for snaps
 - o/devicestate: implement maybeApplyPreseededData function to apply
   preseed artifact
 - tests/lib/tools: add piboot to boot_path()
 - interfaces/builtin: shared-memory drop plugs allow-installation:
   true
 - tests/main/user-session-env: for for opensuse
 - cmd/snap-fde-keymgr, secboot: add a tiny FDE key manager
 - tests: re-execute the failed tests when "Run failed" label is set
   in the PR
 - interfaces/builtin/custom-device: fix unit tests on hosts with
   different libexecdir
 - sandbox: move profile load/unload to sandbox/apparmor
 - cmd/snap: handler call verifications for cmd_quota_tests
 - secboot/keys: introduce a package for secboot key types, use the
   package throughout the code base
 - snap/quota: add journal quotas to resources.go
 - many: let provide a SnapHandler to Seed.Load*Meta*
 - osutil: allow setting desired mtime on the AtomicFile, preserve
   mtime on copy
 - systemd: add systemd.Run() wrapper for systemd-run
 - tests: test fresh install of core22-based snap (#11696)
 - tests: initial set of tests to uc22 nested execution
 - o/snapstate: migration overwrites existing snap dir
 - tests: fix interfaces-location-control tests leaking provider.py
   process
 - tests/nested: fix custom-device test
 - tests: test migration w/ revert, refresh and XDG dir creation
 - asserts,store: complete support for optional primary key headers
   for assertions
 - seed: support parallelism when loading/verifying snap metadata
 - image/preseed, cmd/snap-preseed: create and sign preseed assertion
 - tests: Initial changes to run nested tests on uc22
 - o/snapstate: fix TestSnapdRefreshTasks test after two r-a-a PRs
 - interfaces: add ACRN hypervisor support
 - o/snapstate: exclude TypeSnapd and TypeOS snaps from refresh-app-
   awareness
 - features: enable refresh-app-awareness by default
 - libsnap-confine-private: show proper error when aa_change_onexec()
   fails
 - i/apparmor: remove leftover comment
 - gadget: drop unused code in unit tests
 - image, store: move ToolingStore to store/tooling package
 - HACKING: update info for snapcraft remote build
 - seed: return all essential snaps found if no types are given to
   LoadEssentialMeta
 - i/b/custom_device: fix generation of udev rules
 - tests/nested/manual/core20-early-config: disable netplan checks
 - bootloader/assets, tests: add factory-reset mode, test non-
   encrypted factory-reset
 - interfaces/modem-manager: add support for Cinterion modules
 - gadget: fully support multi-volume gadget asset updates in
   Update() on UC20+
 - i/b/content: use slot.Lookup() as suggested by TODO comment
 - tests: install linux-tools-gcp on jammy to avoid bpftool
   dependency error
 - tests/main: add spread tests for new cpu and thread quotas
 - snap-debug-info: print validation sets and validation set
   assertions
 - many: renaming related to inclusive language part 2
 - c/snap-seccomp: update syscalls to match libseccomp 2657109
 - github: cancel workflows when pushing to pull request branches
 - .github: use reviewdog action from woke tool
 - interfaces/system-packages-doc: allow read-only access to
   /usr/share/gtk-doc
 - interfaces: add max_map_count to system-observe
 - o/snapstate: print pids of running processes on BusySnapError
 - .github: run woke tool on PR's
 - snapshots: follow-up on exclusions PR
 - cmd/snap: add check switch for snap debug state
 - tests: do not run mount-order-regression test on i386
 - interfaces/system-packages-doc: allow read-only access to
   /usr/share/xubuntu-docs
 - interfaces/hardware_observe: add read access for various devices
 - packaging: use latest go to build spread
 - tests: Enable more tests for UC22
 - interfaces/builtin/network-control: also allow for mstp and bchat
   devices too
 - interfaces/builtin: update apparmor profile to allow creating
   mimic over /usr/share*
 - data/selinux: allow snap-update-ns to mount on top of /var/snap
   inside the mount ns
 - interfaces/cpu-control: fix apparmor rules of paths with CPU ID
 - tests: remove the file that configures nm as default
 - tests: fix the change done for netplan-cfg test
 - tests: disable netplan-cfg test
 - cmd/snap-update-ns: apply content mounts before layouts
 - overlord/state: add a helper to detect cyclic dependencies between
   tasks in change
 - packaging/ubuntu-16.04/control: recommend `fuse3 | fuse`
 - many: change "transactional" flag to a "transaction" option
 - b/piboot.go: check EEPROM version for RPi4
 - snap/quota,spread: raise lower memory quota limit to 640kb
 - boot,bootloader: add missing grub.cfg assets mocks in some tests
 - many: support --ignore-running with refresh many
 - tests: skip the test interfaces-many-snap-provided in
   trusty
 - o/snapstate: rename XDG dirs during HOME migration
 - cmd/snap,wrappers: fix wrong implementation of zero count cpu
   quota
 - i/b/kernel_module_load: expand $SNAP_COMMON in module options
 - interfaces/u2f-devices: add Solo V2
 - overlord: add missing grub.cfg assets mocks in manager_tests.go
 - asserts: extend optional primary keys support to the in-memory
   backend
 - tests: update the lxd-no-fuse test
 - many: fix failing golangci checks
 - seed,many: allow to limit LoadMeta to snaps of a precise mode
 - tests: allow ubuntu-image to be built with a compatible snapd tree
 - o/snapstate: account for repeat migration in ~/Snap undo
 - asserts: start supporting optional primary keys in fs backend,
   assemble and signing
 - b/a: do not set console in kernel command line for arm64
 - tests/main/snap-quota-groups: fix spread test
 - sandbox,quota: ensure cgroup is available when creating mem
   quotas
 - tests: add debug output what keeps `/home` busy
 - sanity: rename "sanity.Check" to "syscheck.CheckSystem"
 - interfaces: add pkcs11 interface
 - o/snapstate: undo migration on 'snap revert'
 - overlord: snapshot exclusions
 - interfaces: add private /dev/shm support to shared-memory
   interface
 - gadget/install: implement factory reset for unencrypted system
 - packaging: install Go snap from 1.17 channel in the integration
   tests
 - snap-exec: fix detection if `cups` interface is connected
 - tests: extend gadget-config-defaults test with refresh.retain
 - cmd/snap,strutil: move lineWrap to WordWrapPadded
 - bootloader/piboot: add support for armhf
 - snap,wrappers: add `sigint{,-all}` to supported stop-modes
 - packaging/ubuntu-16.04/control: depend on fuse3 | fuse
 - interfaces/system-packages-doc: allow read-only access to
   /usr/share/libreoffice/help
 - daemon: add a /v2/accessories/changes/{ID} endpoint
 - interfaces/appstream-metadata: Re-create app-info links to
   swcatalog
 - debug-tools: add script to help debugging GCE instances which fail
   to boot
 - gadget/install, kernel: more ICE helpers/support
 - asserts: exclude empty snap id from duplicates lookup with preseed
   assert
 - cmd/snap, signtool: move key-manager related helpers to signtool
   package
 - tests/main/snap-quota-groups: add 219 as possible exit code
 - store: set validation-sets on actions when refreshing
 - github/workflows: update golangci-lint version
 - run-check: use go install instead of go get
 - tests: set as manual the interfaces-cups-control test
 - interfaces/appstream-metadata: Support new swcatalog directory
   names
 - image/preseed: migrate tests from cmd/snap-preseed
 - tests/main/uc20-create-partitions: update the test for new Go
   versions
 - strutil: move wrapGeneric function to strutil as WordWrap
 - many: small inconsequential tweaks
 - quota: detect/error if cpu-set is used with cgroup v1
 - tests: moving ubuntu-image to candidate to fix uc16 tests
 - image: integrate UC20 preseeding with image.Prepare
 - cmd/snap,client: frontend for cpu/thread quotas
 - quota: add test for `Resource.clone()`
 - many: replace use of "sanity" with more inclusive naming (part 2)
 - tests: switch to "test-snapd-swtpm"
 - i/b/network-manager: split rule with more than one peers
 - tests: fix restore of the BUILD_DIR in failover test on uc18
 - cmd/snap/debug: sort changes by their spawn times
 - asserts,interfaces/policy: slot-snap-id allow-installation
   constraints
 - o/devicestate: factory reset mode, no encryption
 - debug-tools/snap-debug-info.sh: print message if no gadget snap
   found
 - overlord/devicestate: install system cleanups
 - cmd/snap-bootstrap: support booting into factory-reset mode
 - o/snapstate, ifacestate: pass preseeding flag to
   AddSnapdSnapServices
 - o/devicestate: restore device key and serial when assertion is
   found
 - data: add static preseed.json file
 - sandbox: improve error message from `ProbeCgroupVersion()`
 - tests: fix the nested remodel tests
 - quota: add some more unit tests around Resource.Change()
 - debug-tools/snap-debug-info.sh: add debug script
 - tests: workaround lxd issue lp:10079 (function not implemented) on
   prep-snapd-in-lxd
 - osutil/disks: blockdev need not be available in the PATH
 - cmd/snap-preseed: address deadcode linter
 - tests/lib/fakestore/store: return snap base in details
 - tests/lib/nested.sh: rm core18 snap after download
 - systemd: do not reload system when enabling/disabling services
 - i/b/kubernetes_support: add access to Java certificates

* Wed May 11 2022 Michael Vogt <michael.vogt@ubuntu.com>
- New upstream release 2.55.5
 - snapstate: do not auto-migrate to ~/Snap for core22 just yet
 - cmd/snap-seccomp: add copy_file_range to
   syscallsWithNegArgsMaskHi32
 - cmd/snap-update-ns: correctly set sticky bit on created
   directories where applicable
 - .github: Skip misspell and ineffassign on go 1.13
 - tests: add lz4 dependency for jammy to avoid issues repacking
   kernel
 - interfaces: posix-mq: add new interface

* Sat Apr 30 2022 Michael Vogt <michael.vogt@ubuntu.com>
- New upstream release 2.55.4
 - tests: do not run mount-order-regression test on i386
 - c/snap-seccomp: update syscalls
 - o/snapstate: overwrite ~/.snap subdir when migrating
 - o/assertstate: fix handling of validation set tracking update in
   enforcing mode
 - packaging: restart our units only after the upgrade
 - interfaces: add a steam-support interface
 - features: enable refresh-app-awareness by default
 - i/b/custom_device: fix generation of udev rules
 - interfaces/system-packages-doc: allow read-only access to
   /usr/share/gtk-doc
 - interfaces/system-packages-doc: allow read-only access to
   /usr/share/xubuntu-docs
 - interfaces/builtin/network-control: also allow for mstp and bchat
   devices too
 - interfaces/builtin: update apparmor profile to allow creating
   mimic over /usr/share
 - data/selinux: allow snap-update-ns to mount on top of /var/snap
   inside the mount ns
 - interfaces/cpu-control: fix apparmor rules of paths with CPU ID

* Fri Apr 08 2022 Michael Vogt <michael.vogt@ubuntu.com>
- New upstream release 2.55.3
 - cmd/snap-update-ns: apply content mounts before layouts
 - many: change "transactional" flag to a "transaction" option
 - b/piboot.go: check EEPROM version for RPi4
 - snap/quota,spread: raise lower memory quota limit to 640kb
 - boot,bootloader: add missing grub.cfg assets mocks in some
   tests
 - many: support --ignore-running with refresh many
 - cmd/snap,wrappers: fix wrong implementation of zero count cpu
   quota
 - quota: add some more unit tests around Resource.Change()
 - quota: detect/error if cpu-set is used with cgroup v1
 - quota: add test for `Resource.clone()
 - cmd/snap,client: frontend for cpu/thread quotas
 - tests: update spread test to check right XDG dirs
 - snap: set XDG env vars to new dirs
 - o/snapstate: initialize XDG dirs in HOME migration
 - i/b/kernel_module_load: expand $SNAP_COMMON in module options
 - overlord: add missing grub.cfg assets mocks in manager_tests.go
 - o/snapstate: account for repeat migration in ~/Snap undo
 - b/a: do not set console in kernel command line for arm64
 - sandbox: improve error message from `ProbeCgroupVersion()`
 - tests/main/snap-quota-groups: fix spread test
 - interfaces: add pkcs11 interface
 - o/snapstate: undo migration on 'snap revert'
 - overlord: snapshot exclusions
 - interfaces: add private /dev/shm support to shared-memory
   interface
 - packaging: install Go snap from 1.17 channel in the integration
   tests
 - snap-exec: fix detection if `cups` interface is connected
 - bootloader/piboot: add support for armhf
 - interfaces/system-packages-doc: allow read-only access to
   /usr/share/libreoffice/help
 - daemon: add a /v2/accessories/changes/{ID} endpoint
 - interfaces/appstream-metadata: Re-create app-info links to
   swcatalog
 - tests/main/snap-quota-groups: add 219 as possible exit code
 - store: set validation-sets on actions when refreshing
 - interfaces/appstream-metadata: Support new swcatalog directory
   names
 - asserts,interfaces/policy: slot-snap-id allow-installation
   constraints
 - i/b/network-manager: change rule for ResolveAddress to check only
   label
 - cmd/snap-bootstrap: support booting into factory-reset mode
 - systemd: do not reload system when enabling/disabling services

* Mon Mar 21 2022 Ian Johnson <ian.johnson@canonical.com>
- New upstream release 2.55.2
 - cmd/snap-update-ns: actually use entirely non-existent dirs

* Mon Mar 21 2022 Ian Johnson <ian.johnson@canonical.com>
- New upstream release 2.55.1
 - cmd/snap-update-ns/change_test.go: use non-exist name foo-runtime
   instead

* Mon Mar 21 2022 Ian Johnson <ian.johnson@canonical.com>
- New upstream release 2.55
 - kernel/fde: add PartitionName to various structs
 - osutil/disks: calculate the last usable LBA instead of reading it
 - snap/quota: additional validation in resources.go
 - o/snapstate: avoid setting up single reboot when update includes
   base, kernel and gadget
 - overlord/state: add helper for aborting unready lanes
 - snap-bootstrap: Partially revert simplifications of mount
   dependencies
 - cmd/snap-update-ns/change.go: sort needed, desired and not reused
   mount entries
 - cmd/snap-preseed, image: move preseeding code to image/preseed
 - interfaces/docker-support: make generic rules not conflict with
   snap-confine
 - i/b/modem-manager: provide access to ObjectManager
 - i/b/network_{control,manager}.go: add more access to resolved
 - overlord/state: drop unused lanes field
 - cmd/snap: make 1.18 vet happy
 - o/snapstate: allow installing the snapd-desktop-integration snap
   even if the user-daemons feature is otherwise disabled
 - snap/quota: fix bug in quota group tree validation code
 - o/snapstate: make sure that snapd is a prerequisite for updating
   base snaps
 - bootloader: add support for piboot
 - i/seccomp/template.go: add close_range to the allowed syscalls
 - snap: add new cpu quotas
 - boot: support factory-reset when sealing and resealing
 - tests: fix test to avoid editing the test-snapd-tools snap.yaml
   file
 - dirs: remove unused SnapMetaDir variable
 - overlord: extend single reboot test to include a non-base, non-
   kernel snap
 - github: replace "sanity check" with "quick check" in workflow
 - fde: add new DeviceUnlock() call
 - many: replace use of "sanity" with more inclusive naming in
   comments
 - asserts: minimal changes to disable authority-delegation before
   full revert
 - tests: updating the test-snapd-cups-control-consumer snap to
   core20 based
 - many: replace use of "sanity" for interface implementation checks
 - cmd/snap-preseed: support for core20 preseeding
 - cmd: set core22 migration related env vars and update spread test
 - interface/opengl: allow read on
   /proc/sys/dev/i915/perf_stream_paranoid
 - tests/lib/tools/report-mongodb: fix typo in help text
 - tests: Include the source github url as part of the mongo db
   issues
 - o/devicestate: split mocks to separate calls for creating a model
   and a gadget
 - snap: Add missing zlib
 - cmd/snap: add support for rebooting to factory-reset
 - interfaces/apparmor: Update base template for systemd-machined
 - i/a/template.go: add ld path for jammy
 - o/devicestate, daemon: introduce factory-reset mode, allow
   switching
 - o/state: fix undo with independent tasks in same change and lane
 - tests: validate tests tools just on google and qemu backends
 - tests/lib/external/snapd-testing-tools: update from upstream
 - tests: skip interfaces-cups-control from debian-sid
 - Increase the times in snapd-sigterm for arm devices
 - interfaces/browser-support: allow RealtimeKit's
   MakeThreadRealtimeWithPID
 - cmd: misc analyzer fixes
 - interfaces/builtin/account-control: allow to execute pam_tally2
 - tests/main/user-session-env: special case bash profile on
   Tumbleweed
 - o/snapstate: implement transactional lanes for prereqs
 - o/snapstate: add core22 migration logic
 - tests/main/mount-ns: unmount /run/qemu
 - release: 2.54.4 changelog to master
 - gadget: add buildVolumeStructureToLocation,
   volumeStructureToLocationMap
 - interfaces/apparmor: add missing unit tests for special devmode
   rules/behavior
 - cmd/snap-confine: coverity fixes
 - interfaces/systemd: use batch systemd operations
 - tests: small adjustments to fix vuln spread tests
 - osutil/disks: trigger udev on the partition device node
 - interfaces/network-control: add D-Bus rules for resolved too
 - interfaces/cpu-control: add extra idleruntime data/reset files to
   cpu-control
 - packaging/ubuntu-16.04/rules: don't run unit tests on riscv64
 - data/selinux: allow the snap command to run systemctl
 - boot: mock amd64 arch for mabootable 20 suite
 - testutil: add Backup helper to save/restore values, usually for
   mocking
 - tests/nested/core/core20-reinstall-partitions: update test summary
 - asserts: return an explicit error when key cannot be found
 - interfaces: custom-device
 - Fix snap-run-gdbserver test by retrying the check
 - overlord, boot: fix unit tests on arches other than amd64
 - Get lxd snap from candidate channel
 - bootloader: allow different names for the grub binary in different
   archs
 - cmd/snap-mgmt, packaging: trigger daemon reload after purging unit
   files
 - tests: add test to ensure consecutive refreshes do garbage
   collection of old revs
 - o/snapstate: deal with potentially invalid type of refresh.retain
   value due to lax validation
 - seed,image: changes necessary for ubuntu-image to support
   preseeding extra snaps in classic images
 - tests: add debugging to snap-confine-tmp-mount
 - o/snapstate: add ~/Snap init related to backend
 - data/env: cosmetic tweak for fish
 - tests: include new testing tools and utils
 - wrappers: do not reload the deamon or restart snapd services when
   preseeding on core
 - Fix smoke/install test for other architectures than pc
 - tests: skip boot loader check during testing preparation on s390x
 - t/m/interfaces-network-manager: use different channel depending on
   system
 - o/devicestate: pick system from seed systems/ for preseeding (1/N)
 - asserts: add preseed assertion type
 - data/env: more workarounds for even older fish shells, provide
   reasonable defaults
 - tests/main/snap-run-devmode-classic: reinstall snapcraft to clean
   up
 - gadget/update.go: add buildNewVolumeToDeviceMapping for existing
   devices
 - tests: allow run spread tests using a private ppaTo validate it
 - interfaces/{cpu,power}-control: add more accesses for commercial
   device tuning
 - gadget: add searchForVolumeWithTraits + tests
 - gadget/install: measure and save disk volume traits during
   install.Run()
 - tests: fix "undo purging" step in snap-run-devmode-classic
 - many: move call to shutdown to the boot package
 - spread.yaml: add core22 version of rsync to skip
 - overlord, o/snapstate: fix mocking on systems without /snap
 - many: move boot.Device to snap.Device
 - tests: smoke test support for core22
 - tests/nested/snapd-removes-vulnerable-snap-confine-revs: use newer
   snaps
 - snapstate: make "remove vulnerable version" message more
   friendly
 - o/devicestate/firstboot_preseed_test.go: remove deadcode
 - o/devicestate: preseeding test cleanup
 - gadget: refactor StructureEncryption to have a concrete type
   instead of map
 - tests: add created_at timestamp to mongo issues
 - tests: fix security-udev-input-subsystem test
 - o/devicestate/handlers_install.go: use --all to get binary data
   too for logs
 - o/snapstate: rename "corecore" -> "core"
 - o/snapstate: implement transactional flag
 - tests: skip ~/.snap migration test on openSUSE
 - asserts,interfaces/policy: move and prepare DeviceScopeConstraint
   for reuse
 - asserts: fetching code should fetch authority-delegation
   assertions with signing keys as needed
 - tests: prepare and restore nested tests
 - asserts: first-class support for formatting/encoding signatory-id
 - asserts: remove unused function, fix for linter
 - gadget: identify/match encryption parts, include in traits info
 - asserts,cmd/snap-repair: support delegation when validating
   signatures
 - many: fix leftover empty snap dirs
 - libsnap-confine-private: string functions simplification
 - tests/nested/manual/core20-cloud-init-maas-signed-seed-data: add
   gadget variant
 - interfaces/u2f-devices: add U2F-TOKEN
 - tests/core/mem-cgroup-disabled: minor fixups
 - data/env: fix fish env for all versions of fish, unexport local
   vars, export XDG_DATA_DIRS
 - tests: reboot test running remodel
 - Add extra disk space to nested images to "avoid No space left on
   device" error
 - tests: add regression tests for disabled memory cgroup operation
 - many: fix issues flagged by golangci and configure it to fail
   build
 - docs: fix incorrect link
 - cmd/snap: rename the verbose logging flag in snap run
 - docs: cosmetic cleanups
 - cmd/snap-confine: build const data structures at compile-
   time
 - o/snapstate: reduce maxInhibition for raa by 1s to avoid confusing
   notification
 - snap-bootstrap: Cleanup dependencies in systemd mounts
 - interfaces/seccomp: Add rseq to base seccomp template
 - cmd/snap-confine: remove mention of "legacy mode" from comment
 - gadget/gadget_test.go: fix variable type
 - gadget/gadget.go: add AllDiskVolumeDeviceTraits
 - spread: non-functional cleanup of go1.6 legacy
 - cmd/snap-confine: update ambiguous comment
 - o/snapstate: revert migration on refresh if flag is disabled
 - packaging/fedora: sync with downstream, packaging improvements
 - tests: updated the documentation to run spread tests using
   external backend
 - osutil/mkfs: Expose more fakeroot flags
 - interfaces/cups: add cups-socket-directory attr, use to specify
   mount rules in backend
 - tests/main/snap-system-key: reset-failed snapd and snapd.socket
 - gadget/install: add unit tests for install.Run()
 - tests/nested/manual/remodel-cross-store,remodel-simple: wait for
   serial
 - vscode: added integrated support for MS VSCODE
 - cmd/snap/auto-import: use osutil.LoadMountInfo impl instead
 - gadget/install: add unit tests for makeFilesystem, allow mocking
   mkfs.Make()
 - systemd: batched operations
 - gadget/install/partition.go: include DiskIndex in synthesized
   OnDiskStructure
 - gadget/install: rm unused support for writing non-filesystem
   structures
 - cmd/snap: close refresh notifications after trying to run a snap
   while inhibited
 - o/servicestate: revert #11003 checking for memory cgroup being
   disabled
 - tests/core/failover: verify failover handling with the kernel snap
 - snap-confine: allow numbers in hook security tag
 - cmd/snap-confine: mount bpffs under /sys/fs/bpf if needed
 - spread: switch to CentOS 8 Stream image
 - overlord/servicestate: disallow mixing snaps and subgroups.
 - cmd/snap: add --debug to snap run
 - gadget: mv modelCharateristics to gadgettest.ModelCharacteristics
 - cmd/snap: remove use of zenity, use notifications for snap run
   inhibition
 - o/devicestate: verify that the new model is self contained before
   remodeling
 - usersession/userd: query xdg-mime to check for fallback handlers
   of a given scheme
 - gadget, gadgettest: reimplement tests to use new gadgettest
   examples.go file
 - asserts: start implementing authority-delegationTODO in later PRs:
 - overlord: skip manager tests on riscv for now
 - o/servicestate: quota group error should be more explanative when
   memory cgroup is disabled
 - i/builtin: allow modem-manager interface to access some files in
   sysfs
 - tests: ensure that interface hook works with hotplug plug
 - tests: fix repair test failure when run in a loop
 - o/snapstate: re-write state after undo migration
 - interfaces/opengl: add support for ARM Mali
 - tests: enable snap-userd-reexec on ubuntu and debian
 - tests: skip bind mount in snapd-snap test when the core snap in
   not repacked
 - many: add transactional flag to snapd API
 - tests: new Jammy image for testing
 - asserts: start generalizing attrMatcherGeneralization is along
 - tests: ensure the ca-certificates package is installed
 - devicestate: ensure permissions of /var/lib/snapd/void are
   correct
 - many: add altlinux support
 - cmd/snap-update-ns: convert some unexpected decimal file mode
   constants to octal.
 - tests: use system ubuntu-21.10-64 in nested tests
 - tests: skip version check on lp-1871652 for sru validation
 - snap/quota: add positive tests for the quota.Resources logic
 - asserts: start splitting out attrMatcher for reuse to
   constraint.go
 - systemd: actually test the function passed as a parameter
 - tests: fix snaps-state test for sru validation
 - many: add Transactional to snapstate.Flags
 - gadget: rename DiskVolume...Opts to DiskVolume...Options
 - tests: Handle PPAs being served from ppa.launchpadcontent.net
 - tests/main/cgroup-tracking-failure: Make it pass when run alone
 - tests: skip migration test on centOS
 - tests: add back systemd-timesyncd to newer debian distros
 - many: add conversion for interface attribute values
 - many: unit test fix when SNAPD_DEBUG=1 is set
 - gadget/install/partition.go: use device rescan trick only when
   gadget says to
 - osutil: refactoring the code exporting mocking APIs to other
   packages
 - mkversion: check that snapd is a git source tree before guessing
   the version
 - overlord: small refactoring of group quota implementation in
   preparation of multiple quota values
 - tests: drop 21.04 tests (it's EOL)
 - osutil/mkfs: Expose option for --lib flag in fakeroot call
 - cmd/snapd-apparmor: fix bad variable initialization
 - packaging, systemd: fix socket (re-)start race
 - tests: fix running tests.invariant on testflinger systems
 - tests: spread test snap dir migration
 - interfaces/shared-memory: support single wild-cards in the
   read/write paths
 - tests: cross store remodel
 - packaging,tests: fix running autopkgtest
 - spread-shellcheck: add a caching layer
 - tests: add jammy to spread executions
 - osutils: deal with ENOENT in UserMaybeSudoUser()
 - packaging/ubuntu-16.04/control: adjust libfuse3 dependency as
   suggested
 - gadget/update.go: add DiskTraitsFromDeviceAndValidate
 - tests/lib/prepare.sh: add debug kernel command line params via
   gadget on UC20
 - check-commit-email: do not fail when current dir is not under git
 - configcore: implement netplan write support via dbus
 - run-checks, check-commit-email.py: check commit email addresses
   for validity
 - tests: setup snapd remodel testing bits
 - cmd/snap: adjust /cmd to migration changes
 - systemd: enable batched calls for systemd calls operation on units
 - o/ifacestate: add convenience Active() method to ConnectionState
   struct
 - o/snapstate: migrate to hidden dir on refresh/install
 - store: fix flaky test
 - i/builtin/xilinx-dma: add interface for Xilinx DMA driver
 - go.mod: tidy up
 - overlord/h/c/umount: remove handling of required parameter
 - systemd: add NeedDaemonReload to the unit state
 - mount-control: step 3
 - tests/nested/manual/minimal-smoke: bump mem to 512 for unencrypted
   case too
 - gadget: fix typo with filesystem message
 - gadget: misc helper fixes for implicit system-data role handling
 - tests: fix uses of fakestore new-snap-declaration
 - spread-shellcheck: use safe_load rather than load with a loder
 - interfaces: allow access to new at-spi socket location in desktop-
   legacy
 - cmd/snap: setup tracking cgroup when invoking a service directly
   as a user
 - tests/main/snap-info: use yaml.safe_load rather than yaml.load
 - cmd/snap: rm unnecessary validation
 - tests: fix `tests/core/create-user` on testflinger pi3
 - tests: fix parallel-install-basic on external UC16 devices
 - tests: ubuntu-image 2.0 compatibility fixes
 - tests/lib/prepare-restore: use go install rather than go get
 - cmd/snap, daemon: add debug command for getting OnDiskVolume
   dump
 - gadget: resolve index ambiguity between OnDiskStructure and
   LaidOutStructuretype: bare structures).
 - tests: workaround missing bluez snap
 - HACKING.md: add dbus-x11 to packages needed to run unit tests
 - spread.yaml: add debian-{10,11}, drop debian-9
 - cmd/snap/quota: fix typo in the help message
 - gadget: allow gadget struct with unspecified filesystem to match
   part with fs
 - tests: re-enable kernel-module-load tests on arm
 - tests/lib/uc20-create-partitions/main.go: setup a logger for
   messages
 - cmd: support installing multiple local snaps
 - usersession: implement method to close notifications via
   usersession REST API
 - data/env: treat XDG_DATA_DIRS like PATH for fish
 - cmd/snap, cmd/snap-confine: extend manpage, update links
 - tests: fix fwupd interface test in debian sid
 - tests: do not run k8s smoke test on 32 bit systems
 - tests: fix testing in trusty qemu
 - packaging: merge 2.54.2 changelog back to master
 - overlord: fix issue with concurrent execution of two snapd
   processes
 - interfaces: add a polkit interface
 - gadget/install/partition.go: wait for udev settle when creating
   partitions too
 - tests: exclude interfaces-kernel-module load on arm
 - tests: ensure that test-snapd-kernel-module-load is
   removed
 - tests: do not test microk8s-smoke on arm
 - packaging, bloader, github: restore cleanliness of snapd info
   file; check in GA workflow
 - tests/lib/tools/tests.invariant: simplify check
 - tests/nested/manual/core20-to-core22: wait for device to be
   initialized before starting a remodel
 - build-aux/snap/snapcraft.yaml: use build-packages, don't fail
   dirty builds
 - tests/lib/tools/tests.invariant: add invariant for detecting
   broken snaps
 - tests/core/failover: replace boot-state with snap debug boot-vars
 - tests: fix remodel-kernel test when running on external devices
 - data/selinux: allow poking /proc/xen
 - gadget: do not crash if gadget.yaml has an empty Volumes section
 - i/b/mount-control: support creating tmpfs mounts
 - packaging: Update openSUSE spec file with apparmor-parser and
   datadir for fish
 - cmd/snap-device-helper: fix variable name typo in the unit tests
 - tests: fixed an issue with retrieval of the squashfuse repo
 - release: 2.54.1
 - tests: tidy up the top-level of ubuntu-seed during tests
 - build-aux: detect/fix dirty git revisions while snapcraft
   building
 - release: 2.54

* Thu Mar 03 2022 Michael Vogt <michael.vogt@ubuntu.com>
- New upstream release 2.54.4
 - t/m/interfaces-network-manager: use different channel depending on
   system
 - many: backport attrer interface changes to 2.54
 - tests: skip version check on lp-1871652 for sru validation
 - i/builtin: allow modem-manager interface to access some files in
   sysfs
 - snapstate: make "remove vulnerable version" message more
   friendly
 - tests: fix "undo purging" step in snap-run-devmode-classic
 - o/snapstate: deal with potentially invalid type of refresh.retain
   value due to lax validation
 - interfaces: custom-device
 - packaging/ubuntu-16.04/control: adjust libfuse3 dependency
 - data/env: fix fish env for all versions of fish
 - packaging/ubuntu-16.04/snapd.postinst: start socket and service
   first
 - interfaces/u2f-devices: add U2F-TOKEN
 - interfaces/seccomp: Add rseq to base seccomp template
 - tests: remove disabled snaps before calling save_snapd_state
 - overlord: skip manager tests on riscv for now
 - interfaces/opengl: add support for ARM Mali
 - devicestate: ensure permissions of /var/lib/snapd/void are
   correct
 - cmd/snap-update-ns: convert some unexpected decimal file mode
   constants to octal.
 - interfaces/shared-memory: support single wild-cards in the
   read/write paths
 - packaging: fix running autopkgtest
 - i/builtin/xilinx-dma-host: add interface for Xilinx DMA driver
 - tests: fix `tests/core/create-user` on testflinger pi3
 - tests: fix parallel-install-basic on external UC16 devices
 - tests: re-enable kernel-module-load tests on arm
 - tests: do not run k8s smoke test on 32 bit systems

* Tue Feb 15 2022 Michael Vogt <michael.vogt@ubuntu.com>
- New upstream release 2.54.3
 - SECURITY UPDATE: Local privilege escalation
  - snap-confine: Add validations of the location of the snap-confine
    binary within snapd.
  - snap-confine: Fix race condition in snap-confine when preparing a
    private mount namespace for a snap.
  - CVE-2021-44730
  - CVE-2021-44731
 - SECURITY UPDATE: Data injection from malicious snaps
  - interfaces: Add validations of snap content interface and layout
    paths in snapd.
  - CVE-2021-4120
  - LP: #1949368

* Thu Jan 06 2022 Ian Johnson <ian.johnson@canonical.com>
- New upstream release 2.54.2
 - tests: exclude interfaces-kernel-module load on arm
 - tests: ensure that test-snapd-kernel-module-load is
   removed
 - tests: do not test microk8s-smoke on arm
 - tests/core/failover: replace boot-state with snap debug boot-vars
 - tests: use snap info|awk to extract tracking channel
 - tests: fix remodel-kernel test when running on external devices
 - .github/workflows/test.yaml: also check internal snapd version for
   cleanliness
 - packaging/ubuntu-16.04/rules: eliminate seccomp modification
 - bootloader/assets/grub_*cfg_asset.go: update Copyright
 - build-aux/snap/snapcraft.yaml: adjust comment about get-version
 - .github/workflows/test.yaml: add check in github actions for dirty
   snapd snaps
 - build-aux/snap/snapcraft.yaml: use build-packages, don't fail
   dirty builds
 - data/selinux: allow poking /proc/xen

* Mon Dec 20 2021 Michael Vogt <michael.vogt@ubuntu.com>
- New upstream release 2.54.1
 - buid-aux: set version before calling ./generate-packaging-dir
   This fixes the "dirty" suffix in the auto-generated version

* Fri Dec 17 2021 Michael Vogt <michael.vogt@ubuntu.com>
- New upstream release 2.54
 - interfaces/builtin/opengl.go: add boot_vga sys/devices file
 - o/configstate/configcore: add tmpfs.size option
 - tests: moving to manual opensuse 15.2
 - cmd/snap-device-helper: bring back the device type identification
   behavior, but for remove action fallback only
 - cmd/snap-failure: use snapd from the snapd snap if core is not
   present
 - tests/core/failover: enable the test on core18
 - o/devicestate: ensure proper order when remodel does a simple
   switch-snap-channel
 - builtin/interfaces: add shared memory interface
 - overlord: extend kernel/base success and failover with bootenv
   checks
 - o/snapstate: check disk space w/o store if possible
 - snap-bootstrap: Mount snaps read only
 - gadget/install: do not re-create partitions using OnDiskVolume
   after deletion
 - many: fix formatting w/ latest go version
 - devicestate,timeutil: improve logging of NTP sync
 - tests/main/security-device-cgroups-helper: more debugs
 - cmd/snap: print a placeholder for version of broken snaps
 - o/snapstate: mock system with classic confinement support
 - cmd: Fixup .clangd to use correct syntax
 - tests: run spread tests in fedora-35
 - data/selinux: allow snapd to access /etc/modprobe.d
 - mount-control: step 2
 - daemon: add multiple snap sideload to API
 - tests/lib/pkgdb: install dbus-user-session during prepare, drop
   dbus-x11
 - systemd: provide more detailed errors for unimplemented method in
   emulation mode
 - tests: avoid checking TRUST_TEST_KEYS on restore on remodel-base
   test
 - tests: retry umounting /var/lib/snapd/seed on uc20 on fsck-on-boot
   test
 - o/snapstate: add hide/expose snap data to backend
 - interfaces: kernel-module-load
 - snap: add support for `snap watch
   --last={revert,enable,disable,switch}`
 - tests/main/security-udev-input-subsystem: drop info from udev
 - tests/core/kernel-and-base-single-reboot-failover,
   tests/lib/fakestore: verify failover scenario
 - tests/main/security-device-cgroups-helper: collect some debug info
   when the test fails
 - tests/nested/manual/core20-remodel: wait for device to have a
   serial before starting a remodel
 - tests/main/generic-unregister: test re-registration if not blocked
 - o/snapstate, assertsate: validation sets/undo on partial failure
 - tests: ensure snapd can be downloaded as a module
 - snapdtool, many: support additional key/value flags in info file
 - data/env: improve fish shell env setup
 - usersession/client: provide a way for client to send messages to a
   subset of users
 - tests: verify that simultaneous refresh of kernel and base
   triggers a single reboot only
 - devicestate: Unregister deletes the device key pair as well
 - daemon,tests: support forgetting device serial via API
 - asserts: change behavior of alternative attribute matcher
 - configcore: relax validation rules for hostname
 - cmd/snap-confine: do not include libglvnd libraries from the host
   system
 - overlord, tests: add managers and a spread test for UC20 to UC22
   remodel
 - HACKING.md: adjust again for building the snapd snap
 - systemd: add support for systemd unit alias names
 - o/snapstate: add InstallPathMany
 - gadget: allow EnsureLayoutCompatibility to ensure disk has all
   laid out structsnow reject/fail:
 - packaging/ubuntu, packaging/debian: depend on dbus-session-bus
   provider (#11111)
 - interfaces/interfaces/scsi_generic: add interface for scsi generic
   de… (#10936)
 - osutil/disks/mockdisk.go: add MockDevicePathToDiskMapping
 - interfaces/microstack-support: set controlsDeviceCgroup to true
 - network-setup-control: add netplan generate D-Bus rules
 - interface/builtin/log_observe: allow to access /dev/kmsg
 - .github/workflows/test.yaml: restore failing of spread tests on
   errors (nested)
 - gadget: tweaks to DiskStructureDeviceTraits + expand test cases
 - tests/lib/nested.sh: allow tests to use their own core18 in extra-
   snaps-path
 - interfaces/browser-support: Update rules for Edge
 - o/devicestate: during remodel first check pending download tasks
   for snaps
 - polkit: add a package to validate polkit policy files
 - HACKING.md: document building the snapd snap and splicing it into
   the core snap
 - interfaces/udev: fix installing snaps inside lxd in 21.10
 - o/snapstate: refactor disk space checks
 - tests: add (strict) microk8s smoke test
 - osutil/strace: try to enable strace on more arches
 - cmd/libsnap-confine-private: fix snap-device-helper device allow
   list modification on cgroup v2
 - tests/main/snapd-reexec-snapd-snap: improve debugging
 - daemon: write formdata file parts to snaps dir
 - systemd: add support for .target units
 - tests: run snap-disconnect on uc16
 - many: add experimental setting to allow using ~/.snap/data instead
   of ~/snap
 - overlord/snapstate: perform a single reboot when updating boot
   base and kernel
 - kernel/fde: add DeviceUnlockKernelHookDeviceMapperBackResolver,
   use w/ disks pkg
 - o/devicestate: introduce DeviceManager.Unregister
 - interfaces: allow receiving PropertiesChanged on the mpris plug
 - tests: new tool used to retrieve data from mongo db
 - daemon: amend ssh keys coming from the store
 - tests: Include the tools from snapd-testing-tools project in
   "$TESTSTOOLS"
 - tests: new workflow step used to report spread error to mongodb
 - interfaces/builtin/dsp: update proc files for ambarella flavor
 - gadget: replace ondisk implementation with disks package, refactor
   part calcs
 - tests: Revert "tests: disable flaky uc18 tests until systemd is
   fixed"
 - Revert: "many: Vendor apparmor-3.0.3 into the snapd snap"
 - asserts: rename "white box" to "clear box" (woke checker)
 - many: Vendor apparmor-3.0.3 into the snapd snap
 - tests: reorganize the debug-each on the spread.yaml
 - packaging: sync with downstream packaging in Fedora and openSUSE
 - tests: disable flaky uc18 tests until systemd is fixed
 - data/env: provide profile setup for fish shell
 - tests: use ubuntu-image 1.11 from stable channel
 - gadget/gadget.go: include disk schema in the disk device volume
   traits too
 - tests/main/security-device-cgroups-strict-enforced: extend the
   comments
 - README.md: point at bugs.launchpad.net/snapd instead of snappy
   project
 - osutil/disks: introduce RegisterDeviceMapperBackResolver + use for
   crypt-luks2
 - packaging: make postrm script robust against `rm` failures
 - tests: print extra debug on auto-refresh-gating test failure
 - o/assertstate, api: move enforcing/monitoring from api to
   assertstate, save history
 - tests: skip the test-snapd-timedate-control-consumer.date to avoid
   NTP sync error
 - gadget/install: use disks functions to implement deviceFromRole,
   also rename
 - tests: the `lxd` test is failing right now on 21.10
 - o/snapstate: account for deleted revs when undoing install
 - interfaces/builtin/block_devices: allow blkid to print block
   device attributes
 - gadget: include size + sector-size in DiskVolumeDeviceTraits
 - cmd/libsnap-confine-private: do not deny all devices when reusing
   the device cgroup
 - interfaces/builtin/time-control: allow pps access
 - o/snapstate/handlers: propagate read errors on "copy-snap-data"
 - osutil/disks: add more fields to Partition, populate them during
   discovery
 - interfaces/u2f-devices: add Trezor and Trezor v2 keys
 - interfaces: timezone-control, add permission for ListTimezones
   DBus call
 - o/snapstate: remove repeated test assertions
 - tests: skip `snap advise-command` test if the store is overloaded
 - cmd: create ~/snap dir with 0700 perms
 - interfaces/apparmor/template.go: allow udevadm from merged usr
   systems
 - github: leave a comment documenting reasons for pipefail
 - github: enable pipefail when running spread
 - osutil/disks: add DiskFromPartitionDeviceNode
 - gadget, many: add model param to Update()
 - cmd/snap-seccomp: add riscv64 support
 - o/snapstate: maintain a RevertStatus map in SnapState
 - tests: enable lxd tests on impish system
 - tests: (partially) revert the memory limits PR#r10241
 - o/assertstate: functions for handling validation sets tracking
   history
 - tests: some improvements for the spread log parser
 - interfaces/network-manager-observe: Update for libnm / dart
   clients
 - tests: add ntp related debug around "auto-refresh" test
 - boot: expand on the fact that reseal taking modeenv is very
   intentional
 - cmd/snap-seccomp/syscalls: update syscalls to match libseccomp
   abad8a8f4
 - data/selinux: update the policy to allow snapd to talk to
   org.freedesktop.timedate1
 - o/snapstate: keep old revision if install doesn't add new one
 - overlord/state: add a unit test for a kernel+base refresh like
   sequence
 - desktop, usersession: observe notifications
 - osutil/disks: add AllPhysicalDisks()
 - timeutil,deviceutil: fix unit tests on systems without dbus or
   without ntp-sync
 - cmd/snap-bootstrap/README: explain all the things (well most of
   them anyways)
 - docs: add run-checks dependency install instruction
 - o/snapstate: do not prune refresh-candidates if gate-auto-refresh-
   hook feature is not enabled
 - o/snapstate: test relink remodel helpers do a proper subset of
   doInstall and rework the verify*Tasks helpers
 - tests/main/mount-ns: make the test run early
 - tests: add `--debug` to netplan apply
 - many: wait for up to 10min for NTP synchronization before
   autorefresh
 - tests: initialize CHANGE_ID in _wait_autorefresh
 - sandbox/cgroup: freeze and thaw cgroups related to services and
   scopes only
 - tests: add more debug around qemu-nbd
 - o/hookstate: print cohort with snapctl refresh --pending (#10985)
 - tests: misc robustness changes
 - o/snapstate: improve install/update tests (#10850)
 - tests: clean up test tools
 - spread.yaml: show `journalctl -e` for all suites on debug
 - tests: give interfaces-udisks2 more time for the loop device to
   appear
 - tests: set memory limit for snapd
 - tests: increase timeout/add debug around nbd0 mounting (up, see
   LP:#1949513)
 - snapstate: add debug message where a snap is mounted
 - tests: give nbd0 more time to show up in preseed-lxd
 - interfaces/dsp: add more ambarella things
 - cmd/snap: improve snap disconnect arg parsing and err msg
 - tests: disable nested lxd snapd testing
 - tests: disable flaky "interfaces-udisks2" on ubuntu-18.04-32
 - o/snapstate: avoid validationSetsSuite repeating snapmgrTestSuite
 - sandbox/cgroup: wait for start transient unit job to finish
 - o/snapstate: fix task order, tweak errors, add unit tests for
   remodel helpers
 - osutil/disks: re-org methods for end of usable region, size
   information
 - build-aux: ensure that debian packaging matches build-base
 - docs: update HACKING.md instructions for snapd 2.52 and later
 - spread: run lxd tests with version from latest/edge
 - interfaces: suppress denial of sys_module capability
 - osutil/disks: add methods to replace gadget/ondisk functions
 - tests: split test tools - part 1
 - tests: fix nested tests on uc20
 - data/selinux: allow snap-confine to read udev's database
 - i/b/common_test: refactor AppArmor features test
 - tests: run spread tests on debian 11
 - o/devicestate: copy timesyncd clock timestamp during install
 - interfaces/builtin: do not probe parser features when apparmor
   isn't available
 - interface/modem-manager: allow connecting to the mbim/qmi proxy
 - tests: fix error message in run-checks
 - tests: spread test for validation sets enforcing
 - cmd/snap-confine: lazy set up of device cgroup, only when devices
   were assigned
 - o/snapstate: deduplicate snap names in remove/install/update
 - tests/main/selinux-data-context: use session when performing
   actions as test user
 - packaging/opensuse: sync with openSUSE packaging, enable AppArmor
   on 15.3+
 - interfaces: skip connection of netlink interface on older
   systems
 - asserts, o/snapstate: honor IgnoreValidation flag when checking
   installed snaps
 - tests/main/apparmor-batch-reload: fix fake apparmor_parser to
   handle --preprocess
 - sandbox/apparmor, interfaces/apparmor: detect bpf capability,
   generate snippet for s-c
 - release-tools/repack-debian-tarball.sh: fix c-vendor dir
 - tests: test for enforcing with prerequisites
 - tests/main/snapd-sigterm: fix race conditions
 - spread: run lxd tests with version from latest/stable
 - run-checks: remove --spread from help message
 - secboot: use latest secboot with tpm legacy platform and v2 fully
   optional
 - tests/lib/pkgdb: install strace on Debian 11 and Sid
 - tests: ensure systemd-timesyncd is installed on debian
 - interfaces/u2f-devices: add Nitrokey 3
 - tests: update the ubuntu-image channel to candidate
 - osutil/disks/labels: simplify decoding algorithm
 - tests: not testing lxd snap anymore on i386 architecture
 - o/snapstate, hookstate: print remaining hold time on snapctl
   --hold
 - cmd/snap: support --ignore-validation with snap install client
   command
 - tests/snapd-sigterm: be more robust against service restart
 - tests: simplify mock script for apparmor_parser
 - o/devicestate, o/servicestate: update gadget assets and cmdline
   when remodeling
 - tests/nested/manual/refresh-revert-fundamentals: re-enable
   encryption
 - osutil/disks: fix bug in BlkIDEncodeLabel, add BlkIDDecodeLabel
 - gadget, osutil/disks: fix some bugs from prior PR'sin the dir.
 - secboot: revert move to new version (revert #10715)
 - cmd/snap-confine: die when snap process is outside of snap
   specific cgroup
 - many: mv MockDeviceNameDisksToPartitionMapping ->
   MockDeviceNameToDiskMapping
 - interfaces/builtin: Add '/com/canonical/dbusmenu' path access to
   'unity7' interface
 - interfaces/builtin/hardware-observer: add /proc/bus/input/devices
   too
 - osutil/disks, many: switch to defining Partitions directly for
   MockDiskMapping
 - tests: remove extra-snaps-assertions test
 - interface/modem-manager: add accept for MBIM/QMI proxy clients
 - tests/nested/core/core20-create-recovery: fix passing of data to
   curl
 - daemon: allow enabling enforce mode
 - daemon: use the syscall connection to get the socket credentials
 - i/builtin/kubernetes_support: add access to Calico lock file
 - osutil: ensure parent dir is opened and sync'd
 - tests: using test-snapd-curl snap instead of http snap
 - overlord: add managers unit test demonstrating cyclic dependency
   between gadget and kernel updates
 - gadget/ondisk.go: include the filesystem UUID in the returned
   OnDiskVolume
 - packaging: fixes for building on openSUSE
 - o/configcore: allow hostnames up to 253 characters, with dot-
   delimited elements
 - gadget/ondisk.go: add listBlockDevices() to get all block devices
   on a system
 - gadget: add mapping trait types + functions to save/load
 - interfaces: add polkit security backend
 - cmd/snap-confine/snap-confine.apparmor.in: update ld rule for
   s390x impish
 - tests: merge coverage results
 - tests: remove "features" from fde-setup.go example
 - fde: add new device-setup support to fde-setup
 - gadget: add `encryptedDevice` and add encryptedDeviceLUKS
 - spread: use `bios: uefi` for uc20
 - client: fail fast on non-retryable errors
 - tests: support running all spread tests with experimental features
 - tests: check that a snap that doesn't have gate-auto-refresh hook
   can call --proceed
 - o/snapstate: support ignore-validation flag when updating to a
   specific snap revision
 - o/snapstate: test prereq update if started by old version
 - tests/main: disable cgroup-devices-v1 and freezer tests on 21.10
 - tests/main/interfaces-many: run both variants on all possible
   Ubuntu systems
 - gadget: mv ensureLayoutCompatibility to gadget proper, add
   gadgettest pkg
 - many: replace state.State restart support with overlord/restart
 - overlord: fix generated snap-revision assertions in remodel unit
   tests

* Thu Dec 02 2021 Ian Johnson <ian.johnson@canonical.com>
- New upstream release 2.53.4
 - devicestate: mock devicestate.MockTimeutilIsNTPSynchronized to
   avoid host env leaking into tests
 - timeutil: return NoTimedate1Error if it can't connect to the
   system bus

* Thu Dec 02 2021 Ian Johnson <ian.johnson@canonical.com>
- New upstream release 2.53.3
 - devicestate: Unregister deletes the device key pair as well
 - daemon,tests: support forgetting device serial via API
 - configcore: relax validation rules for hostname
 - o/devicestate: introduce DeviceManager.Unregister
 - packaging/ubuntu, packaging/debian: depend on dbus-session-bus
   provider
 - many: wait for up to 10min for NTP synchronization before
   autorefresh
 - interfaces/interfaces/scsi_generic: add interface for scsi generic
   devices
 - interfaces/microstack-support: set controlsDeviceCgroup to true
 - interface/builtin/log_observe: allow to access /dev/kmsg
 - daemon: write formdata file parts to snaps dir
 - spread: run lxd tests with version from latest/edge
 - cmd/libsnap-confine-private: fix snap-device-helper device allow
   list modification on cgroup v2
 - interfaces/builtin/dsp: add proc files for monitoring Ambarella
   DSP firmware
 - interfaces/builtin/dsp: update proc file accordingly

* Mon Nov 15 2021 Ian Johnson <ian.johnson@canonical.com>
- New upstream release 2.53.2
 - interfaces/builtin/block_devices: allow blkid to print block
   device attributes/run/udev/data/b{major}:{minor}
 - cmd/libsnap-confine-private: do not deny all devices when reusing
   the device cgroup
 - interfaces/builtin/time-control: allow pps access
 - interfaces/u2f-devices: add Trezor and Trezor v2 keys
 - interfaces: timezone-control, add permission for ListTimezones
   DBus call
 - interfaces/apparmor/template.go: allow udevadm from merged usr
   systems
 - interface/modem-manager: allow connecting to the mbim/qmi proxy
 - interfaces/network-manager-observe: Update for libnm client
   library
 - cmd/snap-seccomp/syscalls: update syscalls to match libseccomp
   abad8a8f4
 - sandbox/cgroup: freeze and thaw cgroups related to services and
   scopes only
 - o/hookstate: print cohort with snapctl refresh --pending
 - cmd/snap-confine: lazy set up of device cgroup, only when devices
   were assigned
 - tests: ensure systemd-timesyncd is installed on debian
 - tests/lib/pkgdb: install strace on Debian 11 and Sid
 - tests/main/snapd-sigterm: flush, use retry
 - tests/main/snapd-sigterm: fix race conditions
 - release-tools/repack-debian-tarball.sh: fix c-vendor dir
 - data/selinux: allow snap-confine to read udev's database
 - interfaces/dsp: add more ambarella things* interfaces/dsp: add
   more ambarella things

* Thu Oct 21 2021 Ian Johnson <ian.johnson@canonical.com>
- New upstream release 2.53.1
 - spread: run lxd tests with version from latest/stable
 - secboot: use latest secboot with tpm legacy platform and v2 fully
   optional (#10946)
 - cmd/snap-confine: die when snap process is outside of snap
   specific cgroup (2.53)
 - interfaces/u2f-devices: add Nitrokey 3
 - Update the ubuntu-image channel to candidate
 - Allow hostnames up to 253 characters, with dot-delimited elements 
   (as suggested by man 7 hostname).
 - Disable i386 until it is possible to build snapd using lxd
 - o/snapstate, hookstate: print remaining hold time on snapctl
   --hold
 - tests/snapd-sigterm: be more robust against service restart
 - tests: add a regression test for snapd hanging on SIGTERM
 - daemon: use the syscall connection to get the socket
   credentials
 - interfaces/builtin/hardware-observer: add /proc/bus/input/devices
   too
 - cmd/snap-confine/snap-confine.apparmor.in: update ld rule for
   s390x impish
 - interface/modem-manager: add accept for MBIM/QMI proxy clients
 - secboot: revert move to new version

* Tue Oct 05 2021 Michael Vogt <michael.vogt@ubuntu.com>
- New upstream release 2.53
 - overlord: fix generated snap-revision assertions in remodel unit
   tests
 - snap-bootstrap: wait in `mountNonDataPartitionMatchingKernelDisk`
 - interfaces/modem-manager: add access to PCIe modems
 - overlord/devicestate: record recovery capable system on a
   successful remodel
 - o/snapstate: use device ctx in prerequisite install/update
 - osutil/disks: support filtering by mount opts in
   MountPointsForPartitionRoot
 - many: support an API flag system-restart-immediate to make snap
   ops proceed immediately with system restarts
 - osutil/disks: add RootMountPointsForPartition
 - overlord/devicestate, tests: enable UC20 remodel, add spread tests
 - cmd/snap: improve snap run help message
 - o/snapstate: support ignore validation flag on install/update
 - osutil/disks: add Disk.FindMatchingPartitionWith{Fs,Part}Label
 - desktop: implement gtk notification backend and provide minimal
   notification api
 - tests: use the latest cpu family for nested tests execution
 - osutil/disks: add Partition struct and Disks.Partitions()
 - o/snapstate: prevent install hang if prereq install fails
 - osutil/disks: add Disk.KernelDevice{Node,Path} methods
 - disks: add `Size(path)` helper
 - tests: reset some mount units failing on ubuntu impish
 - osutil/disks: add DiskFromDevicePath, other misc changes
 - interfaces/apparmor: do not fail during initialization when there
   is no AppArmor profile for snap-confine
 - daemon: implement access checkers for themes API
 - interfaces/seccomp: add clone3 to default template
 - interfaces/u2f-devices: add GoTrust Idem Key
 - o/snapstate: validation sets enforcing on update
 - o/ifacestate: don't fail remove if disconnect hook fails
 - tests: fix error trying to create the extra-snaps dir which
   already exists
 - devicestate: use EncryptionType
 - cmd/libsnap-confine-private: workaround BPF memory accounting,
   update apparmor profile
 - tests: skip system-usernames-microk8s when TRUST_TEST_KEYS is
   false
 - interfaces/dsp: add a usb rule to the ambarella flavor
 - interfaces/apparmor/template.go: allow inspection of dbus
   mediation level
 - tests/main/security-device-cgroups: fix when both variants run on
   the same host
 - cmd/snap-confine: update s-c apparmor profile to allow versioned
   ld.so
 - many: rename systemd.Kind to Backend for a bit more clarity
 - cmd/libsnap-confine-private: fix set but unused variable in the
   unit tests
 - tests: fix netplan test on i386 architecture
 - tests: fix lxd-mount-units test which is based on core20 in ubuntu
   focal system
 - osutil/disks: add new `CreateLinearMapperDevice` helper
 - cmd/snap: wait while inhibition file is present
 - tests: cleanup the job workspace as first step of the actions
   workflow
 - tests: use our own image for ubuntu impish
 - o/snapstate: update default provider if missing required content
 - o/assertstate, api: update validation set assertions only when
   updating all snaps
 - fde: add HasDeviceUnlock() helper
 - secboot: move to new version
 - o/ifacestate: don't lose connections if snaps are broken
 - spread: display information about current device cgroup in debug
   dump
 - sysconfig: set TMPDIR in tests to avoid cluttering the real /tmp
 - tests, interfaces/builtin: introduce 21.10 cgroupv2 variant, tweak
   tests for cgroupv2, update builtin interfaces
 - sysconfig/cloud-init: filter MAAS c-i config from ubuntu-seed on
   grade signed
 - usersession/client: refactor doMany() method
 - interfaces/builtin/opengl.go: add libOpenGL.so* too
 - o/assertstate: check installed snaps when refreshing validation
   set assertions
 - osutil: helper for injecting run time faults in snapd
 - tests: update test nested tool part 2
 - libsnap-confine: use the pid parameter
 - gadget/gadget.go: LaidOutSystemVolumeFromGadget ->
   LaidOutVolumesFromGadget
 - tests: update the time tolerance to fix the snapd-state test
 - .github/workflows/test.yaml: revert #10809
 - tests: rename interfaces-hooks-misbehaving spread test to install-
   hook-misbehaving
 - data/selinux: update the policy to allow s-c to manipulate BPF map
   and programs
 - overlord/devicestate: make settle wait longer in remodel tests
 - kernel/fde: mock systemd-run in unit test
 - o/ifacestate: do not create stray task in batchConnectTasks if
   there are no connections
 - gadget: add VolumeName to Volume and VolumeStructure
 - cmd/libsnap-confine-private: use root when necessary for BPF
   related operations
 - .github/workflows/test.yaml: bump action-build to 1.0.9
 - o/snapstate: enforce validation sets/enforce on InstallMany
 - asserts, snapstate: return full validation set keys from
   CheckPresenceRequired and CheckPresenceInvalid
 - cmd/snap: only log translation warnings in debug/testing
 - tests/main/preseed: update for new base snap of the lxd snap
 - tests/nested/manual: use loop for checking for initialize-system
   task done
 - tests: add a local snap variant to testing prepare-image gating
   support
 - tests/main/security-device-cgroups-strict-enforced: demonstrate
   device cgroup being enforced
 - store: one more tweak for the test action timeout
 - github: do not fail when codecov upload fails
 - o/devicestate: fix flaky test remodel clash
 - o/snapstate: add ChangeID to conflict error
 - tests: fix regex of TestSnapActionTimeout test
 - tests: fix tests for 21.10
 - tests: add test for store.SnapAction() request timeout
 - tests: print user sessions info on debug-each
 - packaging: backports of golang-go 1.13 are good enough
 - sysconfig/cloudinit: add cloudDatasourcesInUseForDir
 - cmd: build gdb shims as static binaries
 - packaging/ubuntu: pass GO111MODULE to dh_auto_test
 - cmd/libsnap-confine-private, tests, sandbox: remove warnings about
   cgroup v2, drop forced devmode
 - tests: increase memory quota in quota-groups-systemd-accounting
 - tests: be more robust against a new day stepping in
 - usersession/xdgopenproxy: move PortalLauncher class to own package
 - interfaces/builtin: fix microstack unit tests on distros using
   /usr/libexec
 - cmd/snap-confine: handle CURRENT_TAGS on systems that support it
 - cmd/libsnap-confine-private: device cgroup v2 support
 - o/servicestate: Update task summary for restart action
 - packaging, tests/lib/prepare-restore: build packages without
   network access, fix building debs with go modules
 - systemd: add AtLeast() method, add mocking in systemdtest
 - systemd: use text.template to generate mount unit
 - o/hookstate/ctlcmd: Implement snapctl refresh --show-lock command
 - o/snapstate: optimize conflicts around snaps stored on
   conditional-auto-refresh task
 - tests/lib/prepare.sh: download core20 for UC20 runs via
   BASE_CHANNEL
 - mount-control: step 1
 - go: update go.mod dependencies
 - o/snapstate: enforce validation sets on snap install
 - tests: revert revert manual lxd removal
 - tests: pre-cache snaps in classic and core systems
 - tests/lib/nested.sh: split out additional helper for adding files
   to VM imgs
 - tests: update nested tool - part1
 - image/image_linux.go: add newline
 - interfaces/block-devices: support to access the state of block
   devices
 - o/hookstate: require snap-refresh-control interface for snapctl
   refresh --proceed
 - build-aux: stage libgcc1 library into snapd snap
 - configcore: add read-only netplan support
 - tests: fix fakedevicesvc service already exists
 - tests: fix interfaces-libvirt test
 - tests: remove travis leftovers
 - spread: bump delta ref to 2.52
 - packaging: ship the `snapd.apparmor.service` unit in debian
 - packaging: remove duplicated `golang-go` build-dependency
 - boot: record recovery capable systems in recovery bootenv
 - tests: skip overlord tests on riscv64 due to timeouts.
 - overlord/ifacestate: fix arguments in unit tests
 - ifacestate: undo repository connection if doConnect fails
 - many: remove unused parameters
 - tests: failure of prereqs on content interface doesn't prevent
   install
 - tests/nested/manual/refresh-revert-fundamentals: fix variable use
 - strutil: add Intersection()
 - o/ifacestate: special-case system-files and force refreshing its
   static attributes
 - interface/builtin: add qualcomm-ipc-router interface for
   AF_QIPCRTR socket protocol
 - tests:  new snapd-state tool
 - codecov: fix files pathnames
 - systemd: add mock systemd helper
 - tests/nested/core/extra-snaps-assertions: fix the match pattern
 - image,c/snap,tests: support enforcing validations in prepare-image
   via --customize JSON validation enforce(|ignore)
 - o/snapstate: enforce validation sets assertions when removing
   snaps
 - many: update deps
 - interfaces/network-control: additional ethernet rule
 - tests: use host-scaled settle timeout for hookstate tests
 - many: move to go modules
 - interfaces: no need for snapRefreshControlInterface struct
 - interfaces: introduce snap-refresh-control interface
 - tests: move interfaces-libvirt test back to 16.04
 - tests: bump the number of retries when waiting for /dev/nbd0p1
 - tests: add more space on ubuntu xenial
 - spread: add 21.10 to qemu, remove 20.10 (EOL)
 - packaging: add libfuse3-dev build dependency
 - interfaces: add microstack-support interface
 - wrappers: fix a bunch of duplicated service definitions in tests
 - tests: use host-scaled timeout to avoid riscv64 test failure
 - many: fix run-checks gofmt check
 - tests: spread test for snapctl refresh --pending/--proceed from
   the snap
 - o/assertstate,daemon: refresh validation sets assertions with snap
   declarations
 - tests: migrate tests that are only executed on xenial to bionic
 - tests: remove opensuse-15.1 and add opensuse-15.3 from spread runs
 - packaging: update master changelog for 2.51.7
 - sysconfig/cloudinit: fix bug around error state of cloud-init
 - interfaces, o/snapstate: introduce AffectsPlugOnRefresh flag
 - interfaces/interfaces/ion-memory-control: add: add interface for
   ion buf
 - interfaces/dsp: add /dev/ambad into dsp interface
 - tests: new spread log parser
 - tests: check files and dirs are cleaned for each test
 - o/hookstate/ctlcmd: unify the error message when context is
   missing
 - o/hookstate: support snapctl refresh --pending from snap
 - many: remove unused/dead code
 - cmd/libsnap-confine-private: add BPF support helpers
 - interfaces/hardware-observe: add some dmi properties
 - snapstate: abort kernel refresh if no gadget update can be found
 - many: shellcheck fixes
 - cmd/snap: add Size column to refresh --list
 - packaging: build without dwarf debugging data
 - snapstate: fix misleading `assumes` error message
 - tests: fix restore in snapfuse spread tests
 - o/assertstate: fix missing 'scheduled' header when auto refreshing
   assertions
 - o/snapstate: fail remove with invalid snap names
 - o/hookstate/ctlcmd: correct err message if missing root
 - .github/workflows/test.yaml: fix logic
 - o/snapstate: don't hold some snaps if not all snaps can be held by
   the given gating snap
 - c-vendor.c: new c-vendor subdir
 - store: make sure expectedZeroFields in tests gets updated
 - overlord: add manager test for "assumes" checking
 - store: deal correctly with "assumes" from the store raw yaml
 - sysconfig/cloudinit.go: add functions for filtering cloud-init
   config
 - cgroup-support: allow to hide cgroupv2 warning via ENV
 - gadget: Export mkfs functions for use in ubuntu-image
 - tests: set to 10 minutes the kill timeout for tests failing on
   slow boards
 - .github/workflows/test.yaml: test github.events key
 - i18n/xgettext-go: preserve already escaped quotes
 - cmd/snap-seccomp/syscalls: update syscalls list to libseccomp
   v2.2.0-428-g5c22d4b
 - github: do not try to upload coverage when working with cached run
 - tests/main/services-install-hook-can-run-svcs: shellcheck issue
   fix
 - interfaces/u2f-devices: add Nitrokey FIDO2
 - testutil: add DeepUnsortedMatches Checker
 - cmd, packaging: import BPF headers from kernel, detect whether
   host headers are usable
 - tests: fix services-refresh-mode test
 - tests: clean snaps.sh helper
 - tests: fix timing issue on security-dev-input-event-denied test
 - tests: update systems for sru validation
 - .github/workflows: add codedov again
 - secboot: remove duplicate import
 - tests: stop the service when is active in test interfaces-
   firewall-control test
 - packaging: remove TEST_GITHUB_AUTOPKGTEST support
 - packaging: merge 2.51.6 changelog back to master
 - secboot: use half the mem for KDF in AddRecoveryKey
 - secboot: switch main key KDF memory cost to 32KB
 - tests: remove the test user just when it was installed on create-
   user-2 test
 - spread: temporarily fix the ownership of /home/ubuntu/.ssh on
   21.10
 - daemon, o/snapstate: handle IgnoreValidation flag on install (2/3)
 - usersession/agent: refactor common JSON validation into own
   function
 - o/hookstate: allow snapctl refresh --proceed from snaps
 - cmd/libsnap-confine-private: fix issues identified by coverity
 - cmd/snap: print logs in local timezone
 - packaging: changelog for 2.51.5 to master
 - build-aux: build with go-1.13 in the snapcraft build too
 - config: rename "virtual" config to "external" config
 - devicestate: add `snap debug timings --ensure=install-system`
 - interfaces/builtin/raw_usb: fix platform typo, fix access to usb
   devices accessible through platform
 - o/snapstate: remove commented out code
 - cmd/snap-device-helper: reimplement snap-device-helper
 - cmd/libsnap-confine-private: fix coverity issues in tests, tweak
   uses of g_assert()
 - o/devicestate/handlers_install.go: add workaround to create dirs
   for install
 - o/assertstate: implement ValidationSetAssertionForEnforce helper
 - clang-format: stop breaking my includes
 - o/snapstate: allow auto-refresh limited to snaps affected by a
   specific gating snap
 - tests: fix core-early-config test to use tests.nested tool
 - sysconfig/cloudinit.go: measure (but don't use) gadget cloud-init
   datasource
 - c/snap,o/hookstate/ctlcmd: add JSON/string strict processing flags
   to snap/snapctl
 - corecfg: add "system.hostname" setting to the system settings
 - wrappers: measure time to enable services in StartServices()
 - configcore: fix early config timezone handling
 - tests/nested/manual: enable serial assertions on testkeys nested
   VM's
 - configcore: fix a bunch of incorrect error returns
 - .github/workflows/test.yaml: use snapcraft 4.x to build the snapd
   snap
 - packaging: merge 2.51.4 changelog back to master
 - {device,snap}state: skip kernel extraction in seeding
 - vendor: move to snapshot-4c814e1 branch and set fixed KDF options
 - tests: use bigger storage on ubuntu 21.10
 - snap: support links map in snap.yaml (and later from the store
   API)
 - o/snapstate: add AffectedByRefreshCandidates helper
 - configcore: register virtual config for timezone reading
 - cmd/libsnap-confine-private: move device cgroup files, add helper
   to deny a device
 - tests: fix cached-results condition in github actions workflow
 - interfaces/tee: add support for Qualcomm qseecom device node
 - packaging: fix build failure on bionic and simplify rules
 - o/snapstate: affectedByRefresh tweaks
 - tests: update nested wait for snapd command
 - interfaces/builtin: allow access to per-user GTK CSS overrides
 - tests/main/snapd-snap: install 4.x snapcraft to build the snapd
   snap
 - snap/squashfs: handle squashfs-tools 4.5+
 - asserts/snapasserts: CheckPresenceInvalid and
   CheckPresenceRequired methods
 - cmd/snap-confine: refactor device cgroup handling to enable easier
   v2 integration
 - tests: skip udp protocol on latest ubuntus
 - cmd/libsnap-confine-private: g_spawn_check_exit_status is
   deprecated since glib 2.69
 - interfaces: s/specifc/specific/
 - github: enable gofmt for Go 1.13 jobs
 - overlord/devicestate: UC20 specific set-model, managers tests
 - o/devicestate, sysconfig: refactor cloud-init config permission
   handling
 - config: add "virtual" config via config.RegisterVirtualConfig
 - packaging: switch ubuntu to use golang-1.13
 - snap: change `snap login --help` to not mention "buy"
 - tests: removing Ubuntu 20.10, adding 21.04 nested in spread
 - tests/many: remove lxd systemd unit to prevent unexpected
   leftovers
 - tests/main/services-install-hook-can-run-svcs: make variants more
   obvious
 - tests: force snapd-session-agent.socket to be re-generated

* Tue Oct 05 2021 Michael Vogt <michael.vogt@ubuntu.com>
- New upstream release 2.52.1
 - snap-bootstrap: wait in `mountNonDataPartitionMatchingKernelDisk`
   for the disk (if not present already)
 - many: support an API flag system-restart-immediate to make snap
   ops proceed immediately with system restarts
 - cmd/libsnap-confine-private: g_spawn_check_exit_status is
   deprecated since glib 2.69
 - interfaces/seccomp: add clone3 to default template
 - interfaces/apparmor/template.go: allow inspection of dbus
   mediation level
 - interfaces/dsp: add a usb rule to the ambarella flavor
 - cmd/snap-confine: update s-c apparmor profile to allow versioned
   ld.so
 - o/ifacestate: don't lose connections if snaps are broken
 - interfaces/builtin/opengl.go: add libOpenGL.so* too
 - interfaces/hardware-observe: add some dmi properties
 - build-aux: stage libgcc1 library into snapd snap
 - interfaces/block-devices: support to access the state of block
   devices
 - packaging: ship the `snapd.apparmor.service` unit in debian

* Fri Sep 03 2021 Ian Johnson <ian.johnson@canonical.com>
- New upstream release 2.52
 - interface/builtin: add qualcomm-ipc-router interface for
   AF_QIPCRTR socket protocol
 - o/ifacestate: special-case system-files and force refreshing its
   static attributes
 - interfaces/network-control: additional ethernet rule
 - packaging: update 2.52 changelog with 2.51.7
 - interfaces/interfaces/ion-memory-control: add: add interface for
   ion buf
 - packaging: merge 2.51.6 changelog back to 2.52
 - secboot: use half the mem for KDF in AddRecoveryKey
 - secboot: switch main key KDF memory cost to 32KB
 - many: merge release/2.51 change to release/2.52
 - .github/workflows/test.yaml: use snapcraft 4.x to build the snapd
   snap
 - o/servicestate: use snap app names for ExplicitServices of
   ServiceAction
 - tests/main/services-install-hook-can-run-svcs: add variant w/o
   --enable
 - o/servicestate: revert only start enabled services
 - tests: adding Ubuntu 21.10 to spread test suite
 - interface/modem-manager: add support for MBIM/QMI proxy clients
 - cmd/snap/model: support storage-safety and snaps headers too
 - o/assertstate: Implement EnforcedValidationSets helper
 - tests: using retry tool for nested tests
 - gadget: check for system-save with multi volumes if encrypting
   correctly
 - interfaces: make the service naming entirely internal to systemd
   BE
 - tests/lib/reset.sh: fix removing disabled snaps
 - store/store_download.go: use system snap provided xdelta3 priority
   + fallback
 - packaging: merge changelog from 2.51.3 back to master
 - overlord: only start enabled services
 - interfaces/builtin: add sd-control interface
 - tests/nested/cloud-init-{never-used,nocloud}-not-vuln: fix tests,
   use 2.45
 - tests/lib/reset.sh: add workaround from refresh-vs-services tests
   for all tests
 - o/assertstate: check for conflicts when refreshing and committing
   validation set asserts
 - devicestate: add support to save timings from install mode
 - tests: new tests.nested commands copy and wait-for
 - install: add a bunch of nested timings
 - tests: drop any-python wrapper
 - store: set ResponseHeaderTimeout on the default transport
 - tests: fix test-snapd-user-service-sockets test removing snap
 - tests: moving nested_exec to nested.tests exec
 - tests: add tests about services vs snapd refreshes
 - client, cmd/snap, daemon: refactor REST API for quotas to match
   CLI org
 - c/snap,asserts: create/delete-key external keypair manager
   interaction
 - tests: revert disable of the delta download tests
 - tests/main/system-usernames-microk8s: disable on centos 7 too
 - boot: support device change
 - o/snapstate: remove unused refreshSchedule argument for
   isRefreshHeld helper
 - daemon/api_quotas.go: handle conflicts, returning conflict
   response
 - tests: test for gate-auto-refresh hook error resulting in hold
 - release: 2.51.2
 - snapstate/check_snap: add snap_microk8s to shared system-
   usernames
 - snapstate: remove temporary snap file for local revisions early
 - interface: allows reading sd cards internal info from block-
   devices interface
 - tests: Renaming tool nested-state to tests.nested
 - testutil: fix typo in json checker unit tests
 - tests: ack assertions by default, add --noack option
 - overlord/devicestate: try to pick alternative recovery labels
   during remodel
 - bootloader/assets: update recovery grub to allow system labels
   generated by snapd
 - tests: print serial log just once for nested tests
 - tests: remove xenial 32 bits
 - sandbox/cgroup: do not be so eager to fail when paths do not exist
 - tests: run spread tests in ubuntu bionic 32bits
 - c/snap,asserts: start supporting ExternalKeypairManager in the
   snap key-related commands
 - tests: refresh control spread test
 - cmd/libsnap-confine-private: do not fail on ENOENT, better getline
   error handling
 - tests: disable delta download tests for now until the store is
   fixed
 - tests/nested/manual/preseed: fix for cloud images that ship
   without core18
 - boot: properly handle tried system model
 - tests/lib/store.sh: revert #10470
 - boot, seed/seedtest: tweak test helpers
 - o/servicestate: TODO and fix preexisting typo
 - o/servicestate: detect conflicts for quota group operations
 - cmd/snap/quotas: adjust help texts for quota commands
 - many/quotas: little adjustments
 - tests: add spread test for classic snaps content slots
 - o/snapstate: fix check-rerefresh task summary when refresh control
   is used
 - many: use changes + tasks for quota group operations
 - tests: fix test snap-quota-groups when checking file
   cgroupProcsFile
 - asserts: introduce ExternalKeypairManager
 - o/ifacestate: do not visit same halt tasks in waitChainSearch to
   avoid cycles
 - tests/lib/store.sh: fix make_snap_installable_with_id()
 - overlord/devicestate, overlord/assertstate: use a temporary DB
   when creating recovery systems
 - corecfg: allow using `# snapd-edit: no` header to disable pi-
   config# snapd-edit: no
 - tests/main/interfaces-ssh-keys: tweak checks for openSUSE
   Tumbleweed
 - cmd/snap: prevent cycles in waitChainSearch with snap debug state
 - o/snapstate: fix populating of affectedSnapInfo.AffectingSnaps for
   marking self as affecting
 - tests: new parameter used by retry tool to set env vars
 - tests: support parameters for match-log on journal-state tool
 - configcore: ignore system.pi-config.* setting on measured kernels
 - sandbox/cgroup: support freezing groups with unified
   hierarchy
 - tests: fix preseed test to used core20 snap on latest systems
 - testutil: introduce a checker which compares the type after having
   passed them through a JSON marshaller
 - store: tweak error message when store.Sections() download fails
 - o/servicestate: stop setting DoneStatus prematurely for quota-
   control
 - cmd/libsnap-confine-private: bump max depth of groups hierarchy to
   32
 - many: turn Contact into an accessor
 - store: make the log with download size a debug one
 - cmd/snap-update-ns: Revert "cmd/snap-update-ns: add SRCDIR to
   include search path"
 - o/devicestate: move SystemMode method before first usage
 - tests: skip tests when the sections cannot be retrieved
 - boot: support resealing with a try model
 - o/hookstate: dedicated handler for gate-auto-refresh hook
 - tests: make sure the /root/snap dir is backed up on test snap-
   user-dir-perms-fixed
 - cmd/snap-confine: make mount ns use check cgroup v2 compatible
 - snap: fix TestInstallNoPATH unit test failure when SUDO_UID is set
 - cmd/libsnap-confine-private/cgroup-support.c: Fix typo
 - cmd/snap-confine, cmd/snapd-generator: fix issues identified by
   sparse
 - o/snapstate: make conditional-auto-refresh conflict with other
   tasks via affected snaps
 - many: pass device/model info to configcore via sysconfig.Device
   interface
 - o/hookstate: return bool flag from Error function of hook handler
   to ignore hook errors
 - cmd/snap-update-ns: add SRCDIR to include search path
 - tests: fix for tests/main/lxd-mount-units test and enable
   ubuntu-21.04
 - overlord, o/devicestate: use a single test helper for resetting to
   a post boot state
 - HACKING.md: update instructions for go1.16+
 - tests: fix restore for security-dev-input-event-denied test
 - o/servicestate: move SetStatus to doQuotaControl
 - tests: fix classic-prepare-image test
 - o/snapstate: prune gating information and refresh-candidates on
   snap removal
 - o/svcstate/svcstatetest, daemon/api_quotas: fix some tests, add
   mock helper
 - cmd: a bunch of tweaks and updates
 - o/servicestate: refactor meter handling, eliminate some common
   parameters
 - o/hookstate/ctlcmd: allow snapctl refresh --pending --proceed
   syntax.
 - o/snapstate: prune refresh candidates in check-rerefresh
 - osutil: pass --extrausers option to groupdel
 - o/snapstate: remove refreshed snap from snaps-hold in
   snapstate.doInstall
 - tests/nested: add spread test for uc20 cloud.conf from gadgets
 - boot: drop model from resealing and boostate
 - o/servicestate, snap/quota: eliminate workaround for buggy
   systemds, add spread test
 - o/servicestate: introduce internal and servicestatetest
 - o/servicestate/quota_control.go: enforce minimum of 4K for quota
   groups
 - overlord/servicestate: avoid unnecessary computation of disabled
   services
 - o/hookstate/ctlcmd: do not call ProceedWithRefresh immediately
   from snapctl
 - o/snapstate: prune hold state during autoRefreshPhase1
 - wrappers/services.go: do not restart disabled or inactive
   services
 - sysconfig/cloudinit.go: allow installing both gadget + ubuntu-seed
   config
 - spread: switch LXD back to latest/candidate channel
 - interfaces/opengl: add support for Imagination PowerVR
 - boot: decouple model from seal/reseal handling via an auxiliary
   type
 - spread, tests/main/lxd: no longer manual, switch to latest/stable
 - github: try out golangci-lint
 - tests: set lxd test to manual until failures are fixed
 - tests: connect 30% of the interfaces on test interfaces-many-core-
   provided
 - packaging/debian-sid: update snap-seccomp patches for latest
   master
 - many: fix imports order (according to gci)
 - o/snapstate: consider held snaps in autoRefreshPhase2
 - o/snapstate: unlock the state before calling backend in
   undoStartSnapServices
 - tests: replace "not MATCH" by NOMATCH in tests
 - README.md: refer to new IRC server
 - cmd/snap-preseed: provide more error info if snap-preseed fails
   early on mount
 - daemon: add a Daemon argument to AccessChecker.CheckAccess
 - c/snap-bootstrap: add bind option with tests
 - interfaces/builtin/netlink_driver_test.go: add test snippet
 - overlord/devicestate: set up recovery system tasks when attempting
   a remodel
 - osutil,strutil,testutil: fix imports order (according to gci)
 - release: merge 2.51.1 changelog
 - cmd: fix imports order (according to gci)
 - tests/lib/snaps/test-snapd-policy-app-consumer: remove dsp-control
   interface
 - o/servicestate: move handlers tests to quota_handlers_test.go file
   instead
 - interfaces: add netlink-driver interface
 - interfaces: remove leftover debug print
 - systemd: refactor property parsers for int values in
   CurrentTasksCount, etc.
 - tests: fix debug section for postrm-purge test
 - tests/many: change all cloud-init passwords for ubuntu to use
   plain_test_passwd
 - asserts,interfaces,snap: fix imports order (according to gci)
 - o/servicestate/quota_control_test.go: test the handlers directly
 - tests: fix issue when checking the udev tag on test security-
   device-cgroups
 - many: introduce Store.SnapExists and use it in
   /v2/accessories/themes
 - o/snapstate: update LastRefreshTime in doLinkSnap handler
 - o/hookstate: handle snapctl refresh --proceed and --hold
 - boot: fix model inconsistency check in modeenv, extend unit tests
 - overlord/servicestate: improve test robustness with locking
 - tests: first part of the cleanup
 - tests: new note in HACKING file to clarify about
   yamlordereddictloader dependency
 - daemon: make CheckAccess return an apiError
 - overlord: fix imports ordering (according to gci)
 - o/servicestate: add quotastate handlers
 - boot: track model's sign key ID, prepare infra for tracking
   candidate model
 - daemon: have apiBaseSuite.errorReq return *apiError directly
 - o/servicestate/service_control.go: add comment about
   ExplicitServices
 - interfaces: builtin: add dm-crypt interface to support external
   storage encryption
 - daemon: split out error response code from response*.go to
   errors*.go
 - interfaces/dsp: fix typo in udev rule
 - daemon,o/devicestate: have DeviceManager.SystemMode take an
   expectation on the system
 - o/snapstate: add helpers for setting and querying holding time for
   snaps
 - many: fix quota groups for centos 7, amazon linux 2 w/ workaround
   for buggy systemd
 - overlord/servicestate: mv ensureSnapServicesForGroup to new file
 - overlord/snapstate: lock the mutex before returning from stop snap
   services undo
 - daemon: drop resp completely in favor of using respJSON
   consistently
 - overlord/devicestate: support for snap downloads in recovery
   system handlers
 - daemon: introduce a separate findResponse, simplify SyncRespone
   and drop Meta
 - overlord/snapstate, overlord/devicestate: exclusive change
   conflict check
 - wrappers, packaging, snap-mgmt: handle removing slices on purge
   too
 - services: remember if acting on the entire snap
 - store: extend context and action objects of SnapAction with
   validation-sets
 - o/snapstate: refresh control - autorefresh phase2
 - cmd/snap/quota: refactor quota CLI as per new design
 - interfaces: opengl: change path for Xilinx zocl driver
 - tests: update spread images for ubuntu-core-20 and ubuntu-21.04
 - o/servicestate/quota_control_test.go: change helper escaping
 - o/configstate/configcore: support snap set system swap.size=...
 - o/devicestate: require serial assertion before remodeling can be
   started
 - systemd: improve systemctl error reporting
 - tests/core/remodel: use model assertions signed with valid keys
 - daemon: use apiError for more of the code
 - store: fix typo in snapActionResult struct json tag
 - userd: mock `systemd --version` in privilegedDesktopLauncherSuite
 - packaging/fedora: sync with downstream packaging
 - daemon/api_quotas.go: include current memory usage information in
   results
 - daemon: introduce StructuredResponse and apiError
 - o/patch: check if we have snapd snap with correct snap type
   already in snapstate
 - tests/main/snapd-snap: build the snapd snap on all platforms with
   lxd
 - tests: new commands for snaps-state tool
 - tests/main/snap-quota-groups: add functional spread test for quota
   groups
 - interfaces/dsp: add /dev/cavalry into dsp interface
 - cmd/snap/cmd_info_test.go: make test robust against TZ changes
 - tests: moving to tests directories snaps built locally - part 2
 - usersession/userd: fix unit tests on systems using /var/lib/snapd
 - sandbox/cgroup: wait for pid to be moved to the desired cgroup
 - tests: fix snap-user-dir-perms-fixed vs format checks
 - interfaces/desktop-launch: support confined snaps launching other
   snaps
 - features: enable dbus-activation by default
 - usersession/autostart: change ~/snap perms to 0700 on startup
 - cmd/snap-bootstrap/initramfs-mounts: mount ubuntu-data nosuid
 - tests: new test static checker
 - release-tool/changelog.py: misc fixes from real world usage
 - release-tools/changelog.py: add function to generate github
   release template
 - spread, tests: Fedora 32 is EOL, drop it
 - o/snapstate: bump max postponement from 60 to 95 days
 - interfaces/apparmor: limit the number of jobs when running with a
   single CPU
 - packaging/fedora/snapd.spec: correct date format in changelog
 - packaging: merge 2.51 changelog back to master
 - packaging/ubuntu-16.04/changelog: add 2.50 and 2.50.1 changelogs,
   placeholder for 2.51
 - interfaces: allow read access to /proc/tty/drivers to modem-
   manager and ppp/dev/tty

* Fri Aug 27 2021 Ian Johnson <ian.johnson@canonical.com>
- New upstream release 2.51.7
 - cmd/snap-seccomp/syscalls: update syscalls list to libseccomp
   v2.2.0-428-g5c22d4b1
 - tests: cherry-pick shellcheck fix `bd730fd4`
 - interfaces/dsp: add /dev/ambad into dsp interface
 - many: shellcheck fixes
 - snapstate: abort kernel refresh if no gadget update can be found
 - overlord: add manager test for "assumes" checking
 - store: deal correctly with "assumes" from the store raw yaml

* Thu Aug 19 2021 Ian Johnson <ian.johnson@canonical.com>
- New upstream release 2.51.6
 - secboot: use half the mem for KDF in AddRecoveryKey
 - secboot: switch main key KDF memory cost to 32KB

* Mon Aug 16 2021 Ian Johnson <ian.johnson@canonical.com>
- New upstream release 2.51.5
 - snap/squashfs: handle squashfs-tools 4.5+
 - tests/core20-install-device-file-install-via-hook-hack: adjust
   test for 2.51
 - o/devicestate/handlers_install.go: add workaround to create dirs
   for install
 - tests: fix linter warning
 - tests: update other spread tests for new behaviour
 - tests: ack assertions by default, add --noack option
 - release-tools/changelog.py: also fix opensuse changelog date
   format
 - release-tools/changelog.py: fix typo in function name
 - release-tools/changelog.py: fix fedora date format
 - release-tools/changelog.py: handle case where we don't have a TZ
 - release-tools/changelog.py: fix line length check
 - release-tools/changelog.py: specify the LP bug for the release as
   an arg too
 - interface/modem-manager: add support for MBIM/QMI proxy
   clients
 - .github/workflows/test.yaml: use snapcraft 4.x to build the snapd
   snap

* Mon Aug 09 2021 Ian Johnson <ian.johnson@canonical.com>
- New upstream release 2.51.4
 - {device,snap}state: skip kernel extraction in seeding
 - vendor: move to snapshot-4c814e1 branch and set fixed KDF options
 - tests/interfaces/tee: fix HasLen check for udev snippets
 - interfaces/tee: add support for Qualcomm qseecom device node
 - gadget: check for system-save with multi volumes if encrypting
   correctly
 - gadget: drive-by: drop unnecessary/supported passthrough in test
   gadget.yaml

* Wed Jul 14 2021 Ian Johnson <ian.johnson@canonical.com>
- New upstream release 2.51.3
 - interfaces/builtin: add sd-control interface
 - store: set ResponseHeaderTimeout on the default transport

* Wed Jul 07 2021 Michael Vogt <michael.vogt@ubuntu.com>
- New upstream release 2.51.2
 - snapstate: remove temporary snap file for local revisions early
 - interface: allows reading sd cards internal info from block-
   devices interface
 - o/ifacestate: do not visit same halt tasks in waitChainSearch to
   avoid slow convergence (or unlikely cycles)
 - corecfg: allow using `# snapd-edit: no` header to disable pi-
   config
 - configcore: ignore system.pi-config.* setting on measured kernels
 - many: pass device/model info to configcore via sysconfig.Device
   interface
 - o/configstate/configcore: support snap set system swap.size=...
 - store: make the log with download size a debug one
 - interfaces/opengl: add support for Imagination PowerVR

* Tue Jun 15 2021 Michael Vogt <michael.vogt@ubuntu.com>
- New upstream release 2.51.1
 - interfaces: add netlink-driver interface
 - interfaces: builtin: add dm-crypt interface to support external
   storage encryption
 - interfaces/dsp: fix typo in udev rule
 - overlord/snapstate: lock the mutex before returning from stop
   snap services undo
 - interfaces: opengl: change path for Xilinx zocl driver
 - interfaces/dsp: add /dev/cavalry into dsp interface
 - packaging/fedora/snapd.spec: correct date format in changelog

* Thu May 27 2021 Ian Johnson <ian.johnson@canonical.com>
- New upstream release 2.51
 - cmd/snap: stacktraces debug endpoint
 - secboot: deactivate volume again when model checker fails
 - store: extra log message, a few minor cleanups
 - packaging/debian-sid: update systemd patch
 - snapstate: adjust update-gadget-assets user visible message
 - tests/nested/core/core20-create-recovery: verify that recovery
   system can be created at runtime
 - gadget: support creating vfat partitions during bootstrap
 - daemon/api_quotas.go: support updating quotas with ensure action
 - daemon: tighten access to a couple of POST endpoints that should
   be really be root-only
 - seed/seedtest, overlord/devicestate: move seed validation helper
   to seedtest
 - overlord/hookstate/ctlcmd: remove unneeded parameter
 - snap/quota: add CurrentMemoryUsage for current memory usage of a
   quota group
 - systemd: add CurrentMemoryUsage to get current memory usage for a
   unit
 - o/snapstate: introduce minimalInstallInfo interface
 - o/hookstate: print pending info (ready, inhibited or none)
 - osutil: a helper to find out the total amount of memory in the
   system
 - overlord, overlord/devicestate: allow for reloading modeenv in
   devicemgr when testing
 - daemon: refine access testing
 - spread: disable unattended-upgrades on debian
 - tests/lib/reset: make nc exit after a while when connection is
   idle
 - daemon: replace access control flags on commands with access
   checkers
 - release-tools/changelog.py: refactor regexp + file reading/writing
 - packaging/debian-sid: update locale patch for the latest master
 - overlord/devicestate: tasks for creating recovery systems at
   runtime
 - release-tools/changelog.py: implement script to update all the
   changelog files
 - tests: change machine type used for nested testsPrices:
 - cmd/snap: include locale when linting description being lower case
 - o/servicestate: add RemoveSnapFromQuota
 - interfaces/serial-port: add Qualcomm serial port devices to
   allowed list
 - packaging: merge 2.50.1 changelog back
 - interfaces/builtin: introduce raw-input interface
 - tests: remove tests.cleanup prepare from nested test
 - cmd/snap-update-ns: fix linter errors
 - asserts: fix errors reported by linter
 - o/hookstate/ctlcmd: allow system-mode for non-root
 - overlord/devicestate: comment why explicit system mode check is
   needed in ensuring tried recovery systems (#10275)
 - overlord/devicesate: observe snap writes when creating recovery
   systems
 - packaging/ubuntu-16.04/changelog: add placeholder for 2.50.1
 - tests: moving to tests directories snaps built locally - part 1
 - seed/seedwriter: fail early when system seed directory exists
 - o/snapstate: autorefresh phase1 for refresh-control
 - c/snap: more precise message for ErrorKindSystemRestart op !=
   reboot
 - tests: simplify the tests.cleanup tool
 - boot: helpers for manipulating current and good recovery systems
   list
 - o/hookstate, o/snapstate: print revision, version, channel with
   snapctl --pending
 - overlord:  unit test tweaks, use well known snap IDs, setup snap
   declarations for most common snaps
 - tests/nested/manual: add test for install-device + snapctl reboot
 - o/servicestate: restart slices + services on modifications
 - tests: update mount-ns test to support changes in the distro
 - interfaces: fix linter issues
 - overlord: mock logger in managers unit tests
 - tests: adding support for fedora-34
 - tests: adding support for debian 10 on gce
 - boot: reseal given keys when the respective boot chain has changed
 - secboot: switch encryption key size to 32 byte (thanks to Chris)
 - interfaces/dbus: allow claiming 'well-known' D-Bus names with a
   wildcard suffix
 - spread: bump delta reference version
 - interfaces: builtin: update permitted paths to be compatible with
   UC20
 - overlord: fix errors reported by linter
 - tests: remove old fedora systems from tests
 - tests: update spread url
 - interfaces/camera: allow devices in /sys/devices/platform/**/usb*
 - interfaces/udisks2: Allow access to the login manager via dbus
 - cmd/snap: exit normally if "snap changes" has no changes
   (LP #1823974)
 - tests: more fixes for spread suite on openSUSE
 - tests: fix tests expecting cgroup v1/hybrid on openSUSE Tumbleweed
 - daemon: fix linter errors
 - spread: add Fedora 34, leave a TODO about dropping Fedora 32
 - interfaces: fix linter errors
 - tests: use op.paths tools instead of dirs.sh helper - part 2
 - client: Fix linter errors
 - cmd/snap: Fix errors reported by linter
 - cmd/snap-repair: fix linter issues
 - cmd/snap-bootstrap: Fix linter errors
 - tests: update permission denied message for test-snapd-event on
   ubuntu 2104
 - cmd/snap: small tweaks based on previous reviews
 - snap/snaptest: helper that mocks both the squashfs file and a snap
   directory
 - overlord/devicestate: tweak comment about creating recovery
   systems, formatting tweaks
 - overlord/devicestate: move devicemgr base suite helpers closer to
   test suite struct
 - overlord/devicestate: keep track of tried recovery system
 - seed/seedwriter: clarify in the diagram when SetInfo is called
 - overlord/devicestate: add helper for creating recovery systems at
   runtime
 - snap-seccomp: update syscalls.go list
 - boot,image: support image.Customizations.BootFlags
 - overlord: support snapctl --halt|--poweroff in gadget install-
   device
 - features,servicestate: add experimental.quota-groups flag
 - o/servicestate: address comments from previous PR
 - tests: basic spread test for snap quota commands
 - tests: moving the snaps which are not locally built to the store
   directory
 - image,c/snap: implement prepare-image --customize
 - daemon: implement REST API for quota groups (create / list / get)
 - cmd/snap, client: snap quotas command
 - o/devicestate,o/hookstate/ctlcmd: introduce SystemModeInfo methods
   and snapctl system-mode
 - o/servicestate/quota_control.go: introduce (very) basic group
   manipulation methods
 - cmd/snap, client: snap remove-quota command
 - wrappers, quota: implement quota groups slice generation
 - snap/quotas: followups from previous PR
 - cmd/snap: introduce 'snap quota' command
 - o/configstate/configcore/picfg.go: use ubuntu-seed config.txt in
   uc20 run mode
 - o/servicestate: test has internal ordering issues, consider both
   cases
 - o/servicestate/quotas: add functions for getting and setting
   quotas in state
 - tests: new buckets for snapd-spread project on gce
 - spread.yaml: update the gce project to start using snapd-spread
 - quota: new package for managing resource groups
 - many: bind and check keys against models when using FDE hooks v2
 - many: move responsibilities down seboot -> kernel/fde and boot ->
   secboot
 - packaging: add placeholder changelog
 - o/configstate/configcore/vitality: fix RequireMountedSnapdSnap
   bug
 - overlord: properly mock usr-lib-snapd tests to mimic an Ubuntu
   Core system
 - many: hide EncryptionKey size and refactors for fde hook v2 next
   steps
 - tests: adding debug info for create user tests
 - o/hookstate: add "refresh" command to snapctl (hidden, not
   complete yet)
 - systemd: wait for zfs mounts (LP #1922293)
 - testutil: support referencing files in FileEquals checker
 - many: refactor to kernel/fde and allow `fde-setup initial-setup`
   to return json
 - o/snapstate: store refresh-candidates in the state
 - o/snapstate: helper for creating gate-auto-refresh hooks
 - bootloader/bootloadertest: provide interface implementation as
   mixins, provide a mock for recovery-aware-trusted-asses bootloader
 - tests/lib/nested: do not compress images, return early when
   restored from pristine image
 - boot: split out a helper for making recovery system bootable
 - tests: update os.query check to match new bullseye codename used
   on sid images
 - o/snapstate: helper for getting snaps affected by refresh, define
   new hook
 - wrappers: support in EnsureSnapServices a callback to observe
   changes (#10176)
 - gadget: multi line support in gadget's cmdline file
 - daemon: test that requesting restart from (early) Ensure works
 - tests: use op.paths tools instead of dirs.sh helper - part 1
 - tests: add new command to snaps-state to get current core, kernel
   and gadget
 - boot, gadget: move opening the snap container into the gadget
   helper
 - tests, overlord: extend unit tests, extend spread tests to cover
   full command line support
 - interfaces/builtin: introduce dsp interface
 - boot, bootloader, bootloader/assets: support for full command line
   override from gadget
 - overlord/devicestate, overlord/snapstate: add task for updating
   kernel command lines from gadget
 - o/snapstate: remove unused DeviceCtx argument of
   ensureInstallPreconditions
 - tests/lib/nested: proper status return for tpm/secure boot checks
 - cmd/snap, boot: add snapd_full_cmdline_args to dumped boot vars
 - wrappers/services.go: refactor helper lambda function to separate
   function
 - boot/flags.go: add HostUbuntuDataForMode
 - boot: handle updating of components that contribute to kernel
   command line
 - tests: add 20.04 to systems for nested/core
 - daemon: add new accessChecker implementations
 - boot, overlord/devicestate: consider gadget command lines when
   updating boot config
 - tests: fix prepare-image-grub-core18 for arm devices
 - tests: fix gadget-kernel-refs-update-pc test on arm and when
   $TRUST_TEST_KEY is false
 - tests: enable help test for all the systems
 - boot: set extra command line arguments when preparing run mode
 - boot: load bits of kernel command line from gadget snaps
 - tests: update layout for tests - part 2
 - tests: update layout for tests - part 1
 - tests: remove the snap profiler from the test suite
 - boot: drop gadget snap yaml which is already defined elsewhere in
   the tests
 - boot: set extra kernel command line arguments when making a
   recovery system bootable
 - boot: pass gadget path to command line helpers, load gadget from
   seed
 - tests: new os.paths tool
 - daemon: make ucrednetGet() return a *ucrednet structure
 - boot: derive boot variables for kernel command lines
 - cmd/snap-bootstrap/initramfs-mounts: fix boot-flags location from
   initramfs

* Wed May 19 2021 Ian Johnson <ian.johnson@canonical.com>
- New upstream release 2.50.1
 - interfaces: update permitted /lib/.. paths to be compatible with 
   UC20
 - interfaces: builtin: update permitted paths to be compatible with
   UC20
 - interfaces/greengrass-support: delete white spaces at the end of
   lines
 - snap-seccomp: update syscalls.go list
 - many: backport kernel command line for 2.50
 - interfaces/dbus: allow claiming 'well-known' D-Bus names with a
   wildcard suffix
 - interfaces/camera: allow devices in /sys/devices/platform/**/usb*
 - interfaces/builtin: introduce dsp interface

* Sat Apr 24 2021 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.50
 - overlord: properly mock usr-lib-snapd tests to mimic an Ubuntu
   Core system
 - o/configstate/configcore/vitality: fix RequireMountedSnapdSnap bug
 - o/servicestate/servicemgr.go: add ensure loop for snap service
   units
 - wrappers/services.go: introduce EnsureSnapServices()
 - snapstate: add "kernel-assets" to featureSet
 - systemd: wait for zfs mounts
 - overlord: make servicestate responsible to compute
   SnapServiceOptions
 - boot,tests: move where we write boot-flags one level up
 - o/configstate: don't pass --root=/ when
   masking/unmasking/enabling/disabling services
 - cmd/snap-bootstrap/initramfs-mounts: write active boot-flags to
   /run
 - gadget: be more flexible with kernel content resolving
 - boot, cmd/snap: include extra cmdline args in debug boot-vars
   output
 - boot: support read/writing boot-flags from userspace/initramfs
 - interfaces/pwm: add PWM interface
 - tests/lib/prepare-restore.sh: clean out snapd changes and snaps
   before purging
 - systemd: enrich UnitStatus returned by systemd.Status() with
   Installed flag
 - tests: updated restore phase of spread tests - part 1
 - gadget: add support for kernel command line provided by the gadget
 - tests: Using GO111MODULE: "off" in spread.yaml
 - features: add gate-auto-refresh-hook feature flag
 - spread: ignore linux kernel upgrade in early stages for arch
   preparation
 - tests: use snaps-state commands and remove them from the snaps
   helper
 - o/configstate: fix panic with a sequence of config unset ops over
   same path
 - api: provide meaningful error message on connect/disconnect for
   non-installed snap
 - interfaces/u2f-devices: add HyperFIDO Pro
 - tests: add simple sanity check for systemctl show
   --property=UnitFileState for unknown service
 - tests: use tests.session tool on interfaces-desktop-document-
   portal test
 - wrappers: install D-Bus service activation files for snapd session
   tools on core
 - many: add x-gvfs-hide option to mount units
 - interfaces/builtin/gpio_test.go: actually test the generated gpio
   apparmor
 - spread: tentative workaround for arch failure caused by libc
   upgrade and cgroups v2
 - tests: add spread test for snap validate against store assertions
 - tests: remove snaps which are not used in any test
 - ci: set the accept-existing-contributors parameter for the cla-
   check action
 - daemon: introduce apiBaseSuite.(json|sync|async|error)Req (and
   some apiBaseSuite cosmetics)
 - o/devicestate/devicemgr: register install-device hook, run if
   present in install
 - o/configstate/configcore: simple refactors in preparation for new
   function
 - tests: unifying the core20 nested suite with the core nested suite
 - tests: uboot-unpacked-assets updated to reflect the real path used
   to find the kernel
 - daemon: switch api_test.go to daemon_test and various other
   cleanups
 - o/configstate/configcore/picfg.go: add hdmi_cvt support
 - interfaces/apparmor: followup cleanups, comments and tweaks
 - boot: cmd/snap-bootstrap: handle a candidate recovery system v2
 - overlord/snapstate: skip catalog refresh when snappy testing is
   enabled
 - overlord/snapstate, overlord/ifacestate: move late security
   profile removal to ifacestate
 - snap-seccomp: fix seccomp test on ppc64el
 - interfaces, interfaces/apparmor, overlord/snapstate: late removal
   of snap-confine apparmor profiles
 - cmd/snap-bootstrap/initramfs-mounts: move time forward using
   assertion times
 - tests: reset the system while preparing the test suite
 - tests: fix snap-advise-command check for 429
 - gadget: policy for gadget/kernel refreshes
 - o/configstate: deal with no longer valid refresh.timer=managed
 - interfaces/udisks2: allow locking /run/mount/utab for udisks 2.8.4
 - cla-check: Use has-signed-canonical-cla GitHub Action
 - tests: validation sets spread test
 - tests: simplify the reset.sh logic by removing not needed command
 - overlord/snapstate: make sure that snapd current symlink is not
   removed during refresh
 - tests/core/fsck-on-boot: unmount /run/mnt/snapd directly on uc20
 - tests/lib/fde-setup-hook: also verify that fde-reveal-key key data
   is base64
 - o/devicestate: split off ensuring next boot goes to run mode into
   new task
 - tests: fix cgroup-tracking test
 - boot: export helper for clearing tried system state, add tests
 - cmd/snap: use less aggressive client timeouts in unit tests
 - daemon: fix signing key validity timestamp in unit tests
 - o/{device,hook}state: encode fde-setup-request key as base64
   string
 - packaging: drop dh-systemd from build-depends on ubuntu-16.04+
 - cmd/snap/pack: unhide the compression option
 - boot: extend set try recovery system unit tests
 - cmd/snap-bootstrap: refactor handling of ubuntu-save, do not use
   secboot's implicit fallback
 - o/configstate/configcore: add hdmi_timings to pi-config
 - snapstate: reduce reRefreshRetryTimeout to 1/2 second
 - interfaces/tee: add TEE/OPTEE interface
 - o/snapstate: update validation sets assertions with auto-refresh
 - vendor: update go-tpm2/secboot to latest version
 - seed: ReadSystemEssentialAndBetterEarliestTime
 - tests: replace while commands with the retry tool
 - interfaces/builtin: update unit tests to use proper distro's
   libexecdir
 - tests: run the reset.sh helper and check test invariants while the
   test is restored
 - daemon: switch preexisting daemon_test tests to apiBaseSuite and
   .req
 - boot, o/devicestate: split makeBootable20 into two parts
 - interfaces/docker-support: add autobind unix rules to docker-
   support
 - interfaces/apparmor: allow reading
   /proc/sys/kernel/random/entropy_avail
 - tests: use retry tool instead a loops
 - tests/main/uc20-create-partitions: fix tests cleanup
 - asserts: mode where Database only assumes cur time >= earliest
   time
 - daemon: validation sets/api tests cleanup
 - tests: improve tests self documentation for nested test suite
 - api: local assertion fallback when it's not in the store
 - api: validation sets monitor mode
 - tests: use fs-state tool in interfaces tests
 - daemon:  move out /v2/login|logout and errToResponse tests from
   api_test.go
 - boot: helper for inspecting the outcome of a recovery system try
 - o/configstate, o/snapshotstate: fix handling of nil snap config on
   snapshot restore
 - tests: update documentation and checks for interfaces tests
 - snap-seccomp: add new `close_range` syscall
 - boot: revert #10009
 - gadget: remove `device-tree{,-origin}` from gadget tests
 - boot: simplify systems test setup
 - image: write resolved-content from snap prepare-image
 - boot: reseal the run key for all recovery systems, but recovery
   keys only for the good ones
 - interfaces/builtin/network-setup-{control,observe}: allow using
   netplan directly
 - tests: improve sections prepare and restore - part 1
 - tests: update details on task.yaml files
 - tests: revert os.query usage in spread.yaml
 - boot: export bootAssetsMap as AssetsMap
 - tests/lib/prepare: fix repacking of the UC20 kernel snap for with
   ubuntu-core-initramfs 40
 - client: protect against reading too much data from stdin
 - tests: improve tests documentation - part 2
 - boot: helper for setting up a try recover system
 - tests: improve tests documentation - part 1
 - tests/unit/go: use tests.session wrapper for running tests as a
   user
 - tests: improvements for snap-seccomp-syscalls
 - gadget: simplify filterUpdate (thanks to Maciej)
 - tests/lib/prepare.sh: use /etc/group and friends from the core20
   snap
 - tests: fix tumbleweed spread tests part 2
 - tests: use new commands of os.query tool on tests
 - o/snapshotstate: create snapshots directory on import
 - tests/main/lxd/prep-snapd-in-lxd.sh: dump contents of sources.list
 - packaging: drop 99-snapd.conf via dpkg-maintscript-helper
 - osutil: add SetTime() w/ 32-bit and 64-bit implementations
 - interfaces/wayland: rm Xwayland Xauth file access from wayland
   slot
 - packaging/ubuntu-16.04/rules: turn modules off explicitly
 - gadget,devicestate: perform kernel asset update for $kernel: style
   refs
 - cmd/recovery: small fix for `snap recovery` tab output
 - bootloader/lkenv: add recovery systems related variables
 - tests: fix new tumbleweed image
 - boot: fix typo, should be systems
 - o/devicestate: test that users.create.automatic is configured
   early
 - asserts: use Fetcher in AddSequenceToUpdate
 - daemon,o/c/configcore: introduce users.create.automatic
 - client, o/servicestate: expose enabled state of user daemons
 - boot: helper for checking and marking tried recovery system status
   from initramfs
 - asserts: pool changes for validation-sets (#9930)
 - daemon: move the last api_foo_test.go to daemon_test
 - asserts: include the assertion timestamp in error message when
   outside of signing key validity range
 - ovelord/snapshotstate: keep a few of the last line tar prints
   before failing
 - gadget/many: rm, delay sector size + structure size checks to
   runtime
 - cmd/snap-bootstrap/triggerwatch: fix returning wrong errors
 - interfaces: add allegro-vcu and media-control interfaces
 - interfaces: opengl: add Xilinx zocl bits
 - mkversion: check that version from changelog is set before
   overriding the output version
 - many: fix new ineffassign warnings
 - .github/workflows/labeler.yaml: try work-around to not sync
   labels
 - cmd/snap, boot: add debug set-boot-vars
 - interfaces: allow reading the Xauthority file KDE Plasma writes
   for Wayland sessions
 - tests/main/snap-repair: test running repair assertion w/ fakestore
 - tests: disable lxd tests for 21.04 until the lxd images are
   published for the system
 - tests/regression/lp-1910456: cleanup the /snap symlink when done
 - daemon: move single snap querying and ops to api_snaps.go
 - tests: fix for preseed and dbus tests on 21.04
 - overlord/snapshotstate: include the last message printed by tar in
   the error
 - interfaces/system-observe: Allow reading /proc/zoneinfo
 - interfaces: remove apparmor downgrade feature
 - snap: fix unit tests on Go 1.16
 - spread: disable Go modules support in environment
 - tests: use new path to find kernel.img in uc20 for arm devices
 - tests: find files before using cat command when checking broadcom-
   asic-control interface
 - boot: introduce good recovery systems, provide compatibility
   handling
 - overlord: add manager gadget refresh test
 - tests/lib/fakestore: support repair assertions too
 - github: temporarily disable action labeler due to issues with
   labels being removed
 - o/devicestate,many: introduce DeviceManager.preloadGadget for
   EarlyConfig
 - tests: enable ubuntu 21.04 for spread tests
 - snap: provide a useful error message if gdbserver is not installed
 - data/selinux: allow system dbus to watch /var/lib/snapd/dbus-1
 - tests/lib/prepare.sh: split reflash.sh into two parts
 - packaging/opensuse: sync with openSUSE packaging
 - packaging: disable Go modules in snapd.mk
 - snap: add deprecation noticed to "snap run --gdb"
 - daemon: add API for checking and installing available theme snaps
 - tests: using labeler action to add automatically a label to run
   nested tests
 - gadget: improve error handling around resolving content sources
 - asserts: repeat the authority cross-check in CheckSignature as
   well
 - interfaces/seccomp/template.go: allow copy_file_range
 - o/snapstate/check_snap.go: add support for many subversions in
   assumes snapdX..
 - daemon: move postSnap and inst.dispatch tests to api_snaps_test.go
 - wrappers: use proper paths for mocked mount units in tests
 - snap: rename gdbserver option to `snap run --gdbserver`
 - store: support validation sets with fetch-assertions action
 - snap-confine.apparmor.in: support tmp and log dirs on Yocto/Poky
 - packaging/fedora: sync with downstream packaging in Fedora
 - many: add Delegate=true to generated systemd units for special
   interfaces (master)
 - boot: use a common helper for mocking boot assets in cache
 - api: validate snaps against validation set assert from the store
 - wrappers: don't generate an [Install] section for timer or dbus
   activated services
 - tests/nested/core20/boot-config-update: skip when snapd was not
   built with test features
 - o/configstate,o/devicestate: introduce devicestate.EarlyConfig
   implemented by configstate.EarlyConfig
 - cmd/snap-bootstrap/initramfs-mounts: fix typo in func name
 - interfaces/builtin: mock distribution in fontconfig cache unit
   tests
 - tests/lib/prepare.sh: add another console= to the reflash magic
   grub entry
 - overlord/servicestate: expose dbus activators of a service
 - desktop/notification: test against a real session bus and
   notification server implementation
 - cmd/snap-bootstrap/initramfs-mounts: write realistic modeenv for
   recover+install
 - HACKING.md: explain how to run UC20 spread tests with QEMU
 - asserts: introduce AtSequence
 - overlord/devicestate: task for updating boot configs, spread test
 - gadget: fix documentation/typos
 - gadget: cleanup MountedFilesystem{Writer,Updater}
 - gadget: use ResolvedSource in MountedFilesystemWriter
 - snap/info.go: add doc-comment for SortServices
 - interfaces: add an optional mount-host-font-cache plug attribute
   to the desktop interface
 - osutil: skip TestReadBuildGo inside sbuild
 - o/hookstate/ctlcmd: add optional --pid and --apparmor-label
   arguments to "snapctl is-connected"
 - data/env/snapd: use quoting in case PATH contains spaces
 - boot: do not observe successful boot assets if not in run mode
 - tests: fix umount for snapd snap on fsck-on-boot testumount:
   /run/mnt/ubuntu-seed/systems/*/snaps/snapd_*.snap: no mount
 - misc: little tweaks
 - snap/info.go: ignore unknown daemons in SortSnapServices
 - devicestate: keep log from install-mode on installed system
 - seed: add LoadEssentialMeta to seed16 and allow all of its
   implementations to be called multiple times
 - cmd/snap-preseed: initialize snap.SanitizePlugsSlots for gadget in
   seeds
 - tests/core/uc20-recovery: move recover mode helpers to generic
   testslib script
 - interfaces/fwupd: allow any distros to access fw files via fwupd
 - store: method for fetching validation set assertion
 - store: switch to v2/assertions api
 - gadget: add new ResolvedContent and populate from LayoutVolume()
 - spread: use full format when listing processes
 - osutil/many: make all test pkgs osutil_test instead of "osutil"
 - tests/unit/go: drop unused environment variables, skip coverage
 - OpenGL interface: Support more Tegra libs
 - gadget,overlord: pass kernelRoot to install.Run()
 - tests: run unit tests in Focal instead of Xenial
 - interfaces/browser-support: allow sched_setaffinity with browser-
   sandbox: true
 - daemon: move query /snaps/<name> tests to api_snaps_test.go
 - cmd/snap-repair/runner.go: add SNAP_SYSTEM_MODE to env of repair
   runner
 - systemd/systemd.go: support journald JSON messages with arrays for
   values
 - cmd: make string/error code more robust against errno leaking
 - github, run-checks: do not collect coverage data on subsequent
   test runs
 - boot: boot config update & reseal
 - o/snapshotstate: handle conflicts between snapshot forget, export
   and import
 - osutil/stat.go: add RegularFileExists
 - cmd/snapd-generator: don't create mount overrides for snap-try
   snaps inside lxc
 - gadget/gadget.go: rename ubuntu-* to system-* in doc-comment
 - tests: use 6 spread workers for centos8
 - bootloader/assets: support injecting bootloader assets in testing
   builds of snapd
 - gadget: enable multi-volume uc20 gadgets in
   LaidOutSystemVolumeFromGadget; rename too
 - overlord/devicestate, sysconfig: do nothing when cloud-init is not
   present
 - cmd/snap-repair: filter repair assertions based on bases + modes
 - snap-confine: make host /etc/ssl available for snaps on classic

* Fri Mar 26 2021 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.49.2
 - interfaces/tee: add TEE/OPTEE interface
 - o/configstate/configcore: add hdmi_timings to pi-config
 - interfaces/udisks2: allow locking /run/mount/utab for udisks 2.8.4
 - snap-seccomp: fix seccomp test on ppc64el
 - interfaces{,/apparmor}, overlord/snapstate:
   late removal of snap-confine apparmor profiles
 - overlord/snapstate, wrappers: add dependency on usr-lib-
   snapd.mount for services on core with snapd snap
 - o/configstate: deal with no longer valid refresh.timer=managed
 - overlord/snapstate: make sure that snapd current symlink is not
   removed during refresh
 - packaging: drop dh-systemd from build-depends on ubuntu-16.04+
 - o/{device,hook}state: encode fde-setup-request key as base64
 - snapstate: reduce reRefreshRetryTimeout to 1/2 second
 - tests/main/uc20-create-partitions: fix tests cleanup
 - o/configstate, o/snapshotstate: fix handling of nil snap config on
   snapshot restore
 - snap-seccomp: add new `close_range` syscall

* Mon Mar 08 2021 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.49.1
 - tests: turn modules off explicitly in spread go unti test
 - o/snapshotstate: create snapshots directory on import
 - cmd/snap-bootstrap/triggerwatch: fix returning wrong errors
 - interfaces: add allegro-vcu and media-control interfaces
 - interfaces: opengl: add Xilinx zocl bits
 - many: fix new ineffassign warnings
 - interfaces/seccomp/template.go: allow copy_file_range
 - interfaces: allow reading the Xauthority file KDE Plasma writes
   for Wayland sessions
 - data/selinux: allow system dbus to watch
   /var/lib/snapd/dbus-1
 - Remove apparmor downgrade feature
 - Support tmp and log dirs on Yocto/Poky

* Wed Feb 10 2021 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.49
 - many: add Delegate=true to generated systemd units for special
   interfaces
 - cmd/snap-bootstrap: rename ModeenvFromModel to
   EphemeralModeenvForModel
 - cmd/snap-bootstrap/initramfs-mounts: write realistic modeenv for
   recover+install
 - osutil: skip TestReadBuildGo inside sbuild
 - tests: fix umount for snapd snap on fsck-on-boot test
 - snap/info_test.go: add unit test cases for bug
 - tests/main/services-after-before: add regression spread test
 - snap/info.go: ignore unknown daemons in SortSnapServices
 - cmd/snap-preseed: initialize snap.SanitizePlugsSlots for gadget in
   seeds
 - OpenGL interface: Support more Tegra libs
 - interfaces/browser-support: allow sched_setaffinity with browser-
   sandbox: true
 - cmd: make string/error code more robust against errno leaking
 - o/snapshotstate: handle conflicts between snapshot forget, export
   and import
 - cmd/snapd-generator: don't create mount overrides for snap-try
   snaps inside lxc
 - tests: update test pkg for fedora and centos
 - gadget: pass sector size in to mkfs family of functions, use to
   select block sz
 - o/snapshotstate: fix returning of snap names when duplicated
   snapshot is detected
 - tests/main/snap-network-errors: skip flushing dns cache on
   centos-7
 - interfaces/builtin: Allow DBus property access on
   org.freedesktop.Notifications
 - cgroup-support.c: fix link to CGROUP DELEGATION
 - osutil: update go-udev package
 - packaging: fix arch-indep build on debian-sid
 - {,sec}boot: pass "key-name" to the FDE hooks
 - asserts: sort by revision with Sort interface
 - gadget: add gadget.ResolveContentPaths()
 - cmd/snap-repair: save base snap and mode in device info; other
   misc cleanups
 - tests: cleanup the run-checks script
 - asserts: snapasserts method to validate installed snaps against
   validation sets
 - tests: normalize test tools - part 1
 - snapshotstate: detect duplicated snapshot imports
 - interfaces/builtin: fix unit test expecting snap-device-helper at
   /usr/lib/snapd
 - tests: apply workaround done for snap-advise-command to apt-hooks
   test
 - tests: skip main part of snap-advise test if 429 error is
   encountered
 - many: clarify gadget role-usage consistency checks for UC16/18 vs
   UC20
 - sandbox/cgroup, tess/main: fix unit tests on v2 system, disable
   broken tests on sid
 - interfaces/builtin: more drive by fixes, import ordering, removing
   dead code
 - tests: skip interfaces-openvswitch spread test on debian sid
 - interfaces/apparmor: drive by comment fix
 - cmd/libsnap-confine-private/cleanup-funcs-test.c: rm g_autofree
   usage
 - cmd/libsnap-confine-private: make unit tests execute happily in a
   container
 - interfaces, wrappers: misc comment fixes, etc.
 - asserts/repair.go: add "bases" and "modes" support to the repair
   assertion
 - interfaces/opengl: allow RPi MMAL video decoding
 - snap: skip help output tests for go-flags v1.4.0
 - gadget: add validation for "$kernel:ref" style content
 - packaging/deb, tests/main/lxd-postrm-purge: fix purge inside
   containers
 - spdx: update to SPDX license list version: 3.11 2020-11-25
 - tests: improve hotplug test setup on classic
 - tests: update check to verify is the current system is arm
 - tests: use os-query tool to check debian, trusty and tumbleweed
 - daemon: start moving implementation to api_snaps.go
 - tests/main/snap-validate-basic: disable test on Fedora due to go-
   flags panics
 - tests: fix library path used for tests.pkgs
 - tests/main/cohorts: replace yq with a Python snippet
 - run-checks: update to match new argument syntax of ineffassign
 - tests: use apiBaseSuite for snapshots tests, fix import endpoint
   path
 - many: separate consistency/content validation into
   gadget.Validate|Content
 - o/{device,snap}state: enable devmode snaps with dangerous model
   assertions
   secboot: add test for when systemd-run does not honor
   RuntimeMaxSec
 - secboot: add workaround for snapcore/core-initrd issue #13
 - devicestate: log checkEncryption errors via logger.Noticef
 - o/daemon: validation sets api and basic spread test
 - gadget: move BuildPartitionList to install and make it unexported
 - tests: add nested spread end-to-end test for fde-hooks
 - devicestate: implement checkFDEFeatures()
 - boot: tweak resealing with fde-setup hooks
 - tests: add os query commands for subsystems and architectures
 - o/snapshotstate: don't set auto flag in the snapshot file
 - tests: use os.query tool instead of comparing the system var
 - testutil: use the original environment when calling shellcheck
 - sysconfig/cloudinit.go: add "manual_cache_clean: true" to cloud-
   init restrict file
 - gadget,o/devicestate,tests: drop EffectiveFilesystemLabel and
   instead set the implicit labels when loading the yaml
 - secboot: add new LockSealedKeys() that uses either TPM/fde-reveal-
   key
 - gadget/quantity: introduce Offset, start using it for offset
   related fields in the gadget
 - gadget: use "sealed-keys" to determine what method to use for
   reseal
 - tests/main/fake-netplan-apply: disable test on xenial for now
 - daemon: start splitting snaps op tests out of api_test.go
 - testutil: make DBusTest use a custom bus configuration file
 - tests: replace pkgdb.sh (library) with tests.pkgs (program)
 - gadget: prepare gadget kernel refs (0/N)
 - interfaces/builtin/docker-support: allow /run/containerd/s/...
 - cmd/snap-preseed: reset run inhibit locks on --reset.
 - boot: add sealKeyToModeenvUsingFdeSetupHook()
 - daemon: reorg snap.go and split out sections and icons support
   from api.go
 - sandbox/seccomp: use snap-seccomp's stdout for getting version
   info
 - daemon: split find support to its own api_*.go files and move some
   helpers
 - tests: move snapstate config defaults tests to a separate file.
 - bootloader/{lk,lkenv}: followups from #9695
 - daemon: actually move APIBaseSuite to daemon_test.apiBaseSuite
 - gadget,o/devicestate: set implicit values for schema and role
   directly instead of relying on Effective* accessors
 - daemon: split aliases support to its own api_*.go files
 - gadget: start separating rule/convention validation from basic
   soundness
 - cmd/snap-update-ns: add better unit test for overname sorting
 - secboot: use `fde-reveal-key` if available to unseal key
 - tests: fix lp-1899664 test when snapd_x1 is not installed in the
   system
 - tests: fix the scenario when the "$SRC".orig file does not exist
 - cmd/snap-update-ns: fix sorting of overname mount entries wrt
   other entries
 - devicestate: add runFDESetupHook() helper
 - bootloader/lk: add support for UC20 lk bootloader with V2 lkenv
   structs
 - daemon: split unsupported buy implementation to its own api_*.go
   files
 - tests: download timeout spread test
 - gadget,o/devicestate: hybrid 18->20 ready volume setups should be
   valid
 - o/devicestate: save model with serial in the device save db
 - bootloader: add check for prepare-image time and more tests
   validating options
 - interfaces/builtin/log_observe.go: allow controlling apparmor
   audit levels
 - hookstate: refactor around EphemeralRunHook
 - cmd/snap: implement 'snap validate' command
 - secboot,devicestate: add scaffoling for "fde-reveal-key" support
 - boot: observe successful command line update, provide a default
 - tests: New queries for the os tools
 - bootloader/lkenv: specify backup file as arg to NewEnv(), use ""
   as path+"bak"
 - osutil/disks: add FindMatchingPartitionUUIDWithPartLabel to Disk
   iface
 - daemon: split out snapctl support and snap configuration support
   to their own api_*.go files
 - snapshotstate: improve handling of multiple errors
 - tests: sign new nested-18|20* models to allow for generic serials
 - bootloader: remove installableBootloader interface and methods
 - seed: cleanup/drop some no longer valid TODOS, clarify some other
   points
 - boot: set kernel command line in modeenv during install
 - many: rename disks.FindMatching... to FindMatching...WithFsLabel
   and err type
 - cmd/snap: suppress a case of spurious stdout logging from tests
 - hookstate: add new HookManager.EphemeralRunHook()
 - daemon: move some more api tests from daemon to daemon_test
 - daemon: split apps and logs endpoints to api_apps.go and tests
 - interfaces/utf: Add Ledger to U2F devices
 - seed/seedwriter: consider modes when checking for deps
   availability
 - o/devicestate,daemon: fix reboot system action to not require a
   system label
 - cmd/snap-repair,store: increase initial retry time intervals,
   stalling TODOs
 - daemon: split interfacesCmd to api_interfaces.go
 - github: run nested suite when commit is pushed to release branch
 - client: reduce again the /v2/system-info timeout
 - tests: reset fakestore unit status
 - update-pot: fix typo in plural keyword spec
 - tests: remove workarounds that add "ubuntu-save" if missing
 - tests: add unit test for auto-refresh with validate-snap failure
 - osutil: add helper for getting the kernel command line
 - tests/main/uc20-create-partitions: verify ubuntu-save encryption
   keys, tweak not MATCH
 - boot: add kernel command lines to the modeenv file
 - spread: bump delta ref, tweak repacking to make smaller delta
   archives
 - bootloader/lkenv: add v2 struct + support using it
 - snapshotstate: add cleanup of abandonded snapshot imports
 - tests: fix uc20-create-parition-* tests for updated gadget
 - daemon: split out /v2/interfaces tests to api_interfaces_test.go
 - hookstate: implement snapctl fde-setup-{request,result}
 - wrappers, o/devicestate: remove EnableSnapServices
 - tests: enable nested on 20.10
 - daemon: simplify test helpers Get|PostReq into Req
 - daemon: move general api to api_general*.go
 - devicestate: make checkEncryption fde-setup hook aware
 - client/snapctl, store: fix typos
 - tests/main/lxd/prep-snapd-in-lxd.sh: wait for valid apt files
   before doing apt ops
 - cmd/snap-bootstrap: update model cross-check considerations
 - client,snapctl: add naive support for "stdin"
 - many: add new "install-mode: disable" option
 - osutil/disks: allow building on mac os
 - data/selinux: update the policy to allow operations on non-tmpfs
   /tmp
 - boot: add helper for generating candidate kernel lines for
   recovery system
 - wrappers: generate D-Bus service activation files
 - bootloader/many: rm ConfigFile, add Present for indicating
   presence of bloader
 - osutil/disks: allow mocking DiskFromDeviceName
 - daemon: start cleaning up api tests
 - packaging/arch: sync with AUR packaging
 - bootloader: indicate when boot config was updated
 - tests: Fix snap-debug-bootvars test to make it work on arm devices
   and core18
 - tests/nested/manual/core20-save: verify handling of ubuntu-save
   with different system variants
 - snap: use the boot-base for kernel hooks
 - devicestate: support "storage-safety" defaults during install
 - bootloader/lkenv: mv v1 to separate file,
   include/lk/snappy_boot_v1.h: little fixups
 - interfaces/fpga: add fpga interface
 - store: download timeout
 - vendor: update secboot repo to avoid including secboot.test binary
 - osutil: add KernelCommandLineKeyValue
 - gadget/gadget.go: allow system-recovery-{image,select} as roles in
   gadget.yaml
 - devicestate: implement boot.HasFDESetupHook
 - osutil/disks: add DiskFromName to get a disk using a udev name
 - usersession/agent: have session agent connect to the D-Bus session
   bus
 - o/servicestate: preserve order of services on snap restart
 - o/servicestate: unlock state before calling wrappers in
   doServiceControl
 - spread: disable unattended-upgrades on ubuntu
 - tests: testing new fedora 33 image
 - tests: fix fsck on boot on arm devices
 - tests: skip boot state test on arm devices
 - tests: updated the systems to run prepare-image-grub test
 - interfaces/raw_usb: allow read access to /proc/tty/drivers
 - tests: unmount /boot/efi in fsck-on-boot test
 - strutil/shlex,osutil/udev/netlink: minimally import go-check
 - tests: fix basic20 test on arm devices
 - seed: make a shared seed system label validation helper
 - tests/many: enable some uc20 tests, delete old unneeded tests or
   TODOs
 - boot/makebootable.go: set snapd_recovery_mode=install at image-
   build time
 - tests: migrate test from boot.sh helper to boot-state tool
 - asserts: implement "storage-safety" in uc20 model assertion
 - bootloader: use ForGadget when installing boot config
 - spread: UC20 no longer needs 2GB of mem
 - cmd/snap-confine: implement snap-device-helper internally
 - bootloader/grub: replace old reference to Managed...Blr... with
   Trusted...Blr...
 - cmd/snap-bootstrap: add readme for snap-bootstrap + real state
   diagram
 - interfaces: fix greengrass attr namingThe flavor attribute names
   are now as follows:
 - tests/lib/nested: poke the API to get the snap revisions
 - tests: compare options of mount units created by snapd and snapd-
   generator
 - o/snapstate,servicestate: use service-control task for service
   actions
 - sandbox: track applications unconditionally
 - interfaces/greengrass-support: add additional "process" flavor for
   1.11 update
 - cmd/snap-bootstrap, secboot, tests: misc cleanups, add spread test

* Tue Dec 15 2020 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.48.2
 - tests: sign new nested-18|20* models to allow for generic serials
 - secboot: add extra paranoia when waiting for that fde-reveal-key
 - tests: backport netplan workarounds from #9785
 - secboot: add workaround for snapcore/core-initrd issue #13
 - devicestate: log checkEncryption errors via logger.Noticef
 - tests: add nested spread end-to-end test for fde-hooks
 - devicestate: implement checkFDEFeatures()
 - boot: tweak resealing with fde-setup hooks
 - sysconfig/cloudinit.go: add "manual_cache_clean: true" to cloud-
   init restrict file
 - secboot: add new LockSealedKeys() that uses either TPM or
   fde-reveal-key
 - gadget: use "sealed-keys" to determine what method to use for
   reseal
 - boot: add sealKeyToModeenvUsingFdeSetupHook()
 - secboot: use `fde-reveal-key` if available to unseal key
 - cmd/snap-update-ns: fix sorting of overname mount entries wrt
   other entries
 - o/devicestate: save model with serial in the device save db
 - devicestate: add runFDESetupHook() helper
 - secboot,devicestate: add scaffoling for "fde-reveal-key" support
 - hookstate: add new HookManager.EphemeralRunHook()
 - update-pot: fix typo in plural keyword spec
 - store,cmd/snap-repair: increase initial expontential time
   intervals
 - o/devicestate,daemon: fix reboot system action to not require a
   system label
 - github: run nested suite when commit is pushed to release branch
 - tests: reset fakestore unit status
 - tests: fix uc20-create-parition-* tests for updated gadget
 - hookstate: implement snapctl fde-setup-{request,result}
 - devicestate: make checkEncryption fde-setup hook aware
 - client,snapctl: add naive support for "stdin"
 - devicestate: support "storage-safety" defaults during install
 - snap: use the boot-base for kernel hooks
 - vendor: update secboot repo to avoid including secboot.test binary

* Thu Dec 03 2020 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.48.1
 - gadget: disable ubuntu-boot role validation check

* Thu Nov 19 2020 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.48
 - osutil: add KernelCommandLineKeyValue
 - devicestate: implement boot.HasFDESetupHook
 - boot/makebootable.go: set snapd_recovery_mode=install at image-
   build time
 - bootloader: use ForGadget when installing boot config
 - interfaces/raw_usb: allow read access to /proc/tty/drivers
 - boot: add scaffolding for "fde-setup" hook support for sealing
 - tests: fix basic20 test on arm devices
 - seed: make a shared seed system label validation helper
 - snap: add new "fde-setup" hooktype
 - cmd/snap-bootstrap, secboot, tests: misc cleanups, add spread test
 - secboot,cmd/snap-bootstrap: fix degraded mode cases with better
   device handling
 - boot,dirs,c/snap-bootstrap: avoid InstallHost* at the cost of some
   messiness
 - tests/nested/manual/refresh-revert-fundamentals: temporarily
   disable secure boot
 - snap-bootstrap,secboot: call BlockPCRProtectionPolicies in all
   boot modes
 - many: address degraded recover mode feedback, cleanups
 - tests: Use systemd-run on tests part2
 - tests: set the opensuse tumbleweed system as manual in spread.yaml
 - secboot: call BlockPCRProtectionPolicies even if the TPM is
   disabled
 - vendor: update to current secboot
 - cmd/snap-bootstrap,o/devicestate: use a secret to pair data and
   save
 - spread.yaml: increase number of workers on 20.10
 - snap: add new `snap recovery --show-keys` option
 - tests: minor test tweaks suggested in the review of 9607
 - snapd-generator: set standard snapfuse options when generating
   units for containers
 - tests: enable lxd test on ubuntu-core-20 and 16.04-32
 - interfaces: share /tmp/.X11-unix/ from host or provider
 - tests: enable main lxd test on 20.10
 - cmd/s-b/initramfs-mounts: refactor recover mode to implement
   degraded mode
 - gadget/install: add progress logging
 - packaging: keep secboot/encrypt_dummy.go in debian
 - interfaces/udev: use distro specific path to snap-device-helper
 - o/devistate: fix chaining of tasks related to regular snaps when
   preseeding
 - gadget, overlord/devicestate: validate that system supports
   encrypted data before install
 - interfaces/fwupd: enforce the confined fwupd to align Ubuntu Core
   ESP layout
 - many: add /v2/system-recovery-keys API and client
 - secboot, many: return UnlockMethod from Unlock* methods for future
   usage
 - many: mv keys to ubuntu-boot, move model file, rename keyring
   prefix for secboot
 - tests: using systemd-run instead of manually create a systemd unit
   - part 1
 - secboot, cmd/snap-bootstrap: enable or disable activation with
   recovery key
 - secboot: refactor Unlock...IfEncrypted to take keyfile + check
   disks first
 - secboot: add LockTPMSealedKeys() to lock access to keys
   independently
 - gadget: correct sfdisk arguments
 - bootloader/assets/grub: adjust fwsetup menuentry label
 - tests: new boot state tool
 - spread: use the official image for Ubuntu 20.10, no longer an
   unstable system
 - tests/lib/nested: enable snapd logging to console for core18
 - osutil/disks: re-implement partition searching for disk w/ non-
   adjacent parts
 - tests: using the nested-state tool in nested tests
 - many: seal a fallback object to the recovery boot chain
 - gadget, gadget/install: move helpers to install package, refactor
   unit tests
 - dirs: add "gentoo" to altDirDistros
 - update-pot: include file locations in translation template, and
   extract strings from desktop files
 - gadget/many: drop usage of gpt attr 59 for indicating creation of
   partitions
 - gadget/quantity: tweak test name
 - snap: fix failing unittest for quantity.FormatDuration()
 - gadget/quantity: introduce a new package that captures quantities
 - o/devicestate,a/sysdb: make a backup of the device serial to save
 - tests: fix rare interaction of tests.session and specific tests
 - features: enable classic-preserves-xdg-runtime-dir
 - tests/nested/core20/save: check the bind mount and size bump
 - o/devicetate,dirs: keep device keys in ubuntu-save/save for UC20
 - tests: rename hasHooks to hasInterfaceHooks in the ifacestate
   tests
 - o/devicestate: unit test tweaks
 - boot: store the TPM{PolicyAuthKey,LockoutAuth}File in ubuntu-save
 - testutil, cmd/snap/version: fix misc little errors
 - overlord/devicestate: bind mount ubuntu-save under
   /var/lib/snapd/save on startup
 - gadget/internal: tune ext4 setting for smaller filesystems
 - tests/nested/core20/save: a test that verifies ubuntu-save is
   present and set up
 - tests: update google sru backend to support groovy
 - o/ifacestate: handle interface hooks when preseeding
 - tests: re-enable the apt hooks test
 - interfaces,snap: use correct type: {os,snapd} for test data
 - secboot: set metadata and keyslots sizes when formatting LUKS2
   volumes
 - tests: improve uc20-create-partitions-reinstall test
 - client, daemon, cmd/snap: cleanups from #9489 + more unit tests
 - cmd/snap-bootstrap: mount ubuntu-save during boot if present
 - secboot: fix doc comment on helper for unlocking volume with key
 - tests: add spread test for refreshing from an old snapd and core18
 - o/snapstate: generate snapd snap wrappers again after restart on
   refresh
 - secboot: version bump, unlock volume with key
 - tests/snap-advise-command: re-enable test
 - cmd/snap, snapmgr, tests: cleanups after #9418
 - interfaces: deny connected x11 plugs access to ICE
 - daemon,client: write and read a maintenance.json file for when
   snapd is shut down
 - many: update to secboot v1 (part 1)
 - osutil/disks/mockdisk: panic if same mountpoint shows up again
   with diff opts
 - tests/nested/core20/gadget,kernel-reseal: add sanity checks to the
   reseal tests
 - many: implement snap routine console-conf-start for synchronizing
   auto-refreshes
 - dirs, boot: add ubuntu-save directories and related locations
 - usersession: fix typo in test name
 - overlord/snapstate: refactor ihibitRefresh
 - overlord/snapstate: stop warning about inhibited refreshes
 - cmd/snap: do not hardcode snapshot age value
 - overlord,usersession: initial notifications of pending refreshes
 - tests: add a unit test for UpdateMany where a single snap fails
 - o/snapstate/catalogrefresh.go: don't refresh catalog in install
   mode uc20
 - tests: also check snapst.Current in undo-unlink tests
 - tests: new nested tool
 - o/snapstate: implement undo handler for unlink-snap
 - tests: clean systems.sh helper and migrate last set of tests
 - tests: moving the lib section from systems.sh helper to os.query
   tool
 - tests/uc20-create-partitions: don't check for grub.cfg
 - packaging: make sure that static binaries are indeed static, fix
   openSUSE
 - many: have install return encryption keys for data and save,
   improve tests
 - overlord: add link participant for linkage transitions
 - tests: lxd smoke test
 - tests: add tests for fsck; cmd/s-b/initramfs-mounts: fsck ubuntu-
   seed too
 - tests: moving main suite from systems.sh to os.query tool
 - tests: moving the core test suite from systems.sh to os.query tool
 - cmd/snap-confine: mask host's apparmor config
 - o/snapstate: move setting updated SnapState after error paths
 - tests: add value to INSTANCE_KEY/regular
 - spread, tests: tweaks for openSUSE
 - cmd/snap-confine: update path to snap-device-helper in AppArmor
   profile
 - tests: new os.query tool
 - overlord/snapshotstate/backend: specify tar format for snapshots
 - tests/nested/manual/minimal-smoke: use 384MB of RAM for nested
   UC20
 - client,daemon,snap: auto-import does not error on managed devices
 - interfaces: PTP hardware clock interface
 - tests: use tests.backup tool
 - many: verify that unit tests work with nosecboot tag and without
   secboot package
 - wrappers: do not error out on read-only /etc/dbus-1/session.d
   filesystem on core18
 - snapshots: import of a snapshot set
 - tests: more output for sbuild test
 - o/snapstate: re-order remove tasks for individual snap revisions
   to remove current last
 - boot: skip some unit tests when running as root
 - o/assertstate: introduce
   ValidationTrackingKey/ValidationSetTracking and basic methods
 - many: allow ignoring running apps for specific request
 - tests: allow the searching test to fail under load
 - overlord/snapstate: inhibit startup while unlinked
 - seed/seedwriter/writer.go: check DevModeConfinement for dangerous
   features
 - tests/main/sudo-env: snap bin is available on Fedora
 - boot, overlord/devicestate: list trusted and managed assets
   upfront
 - gadget, gadget/install: support for ubuntu-save, create one during
   install if needed
 - spread-shellcheck: temporary workaround for deadlock, drop
   unnecessary test
 - snap: support different exit-code in the snap command
 - logger: use strutil.KernelCommandLineSplit in
   debugEnabledOnKernelCmdline
 - logger: fix snapd.debug=1 parsing
 - overlord: increase refresh postpone limit to 14 days
 - spread-shellcheck: use single thread pool executor
 - gadget/install,secboot: add debug messages
 - spread-shellcheck: speed up spread-shellcheck even more
 - spread-shellcheck: process paths from arguments in parallel
 - tests: tweak error from tests.cleanup
 - spread: remove workaround for openSUSE go issue
 - o/configstate: create /etc/sysctl.d when applying early config
   defaults
 - tests: new tests.backup tool
 - tests: add tests.cleanup pop sub-command
 - tests: migration of the main suite to snaps-state tool part 6
 - tests: fix journal-state test
 - cmd/snap-bootstrap/initramfs-mounts: split off new helper for misc
   recover files
 - cmd/snap-bootstrap/initramfs-mounts: also copy /etc/machine-id for
   same IP addr
 - packaging/{ubuntu,debian}: add liblzo2-dev as a dependency for
   building snapd
 - boot, gadget, bootloader: observer preserves managed bootloader
   configs
 - tests/nested/manual: add uc20 grade signed cloud-init test
 - o/snapstate/autorefresh.go: eliminate race when launching
   autorefresh
 - daemon,snapshotstate: do not return "size" from Import()
 - daemon: limit reading from snapshot import to Content-Length
 - many: set/expect Content-Length header when importing snapshots
 - github: switch from ::set-env command to environment file
 - tests: migration of the main suite to snaps-state tool part 5
 - client: cleanup the Client.raw* and Client.do* method families
 - tests: moving main suite to snaps-state tool part 4
 - client,daemon,snap: use constant for snapshot content-type
 - many: fix typos and repeated "the"
 - secboot: fix tpm connection leak when it's not enabled
 - many: scaffolding for snapshots import API
 - run-checks: run spread-shellcheck too
 - interfaces: update network-manager interface to allow
   ObjectManager access from unconfined clients
 - tests: move core and regression suites to snaps-state tool
 - tests: moving interfaces tests to snaps-state tool
 - gadget: preserve files when indicated by content change observer
 - tests: moving smoke test suite and some tests from main suite to
   snaps-state tool
 - o/snapshotstate: pass set id to backend.Open, update tests
 - asserts/snapasserts: introduce ValidationSets
 - o/snapshotstate: improve allocation of new set IDs
 - boot: look at the gadget for run mode bootloader when making the
   system bootable
 - cmd/snap: allow snap help vs --all to diverge purposefully
 - usersession/userd: separate bus name ownership from defining
   interfaces
 - o/snapshotstate: set snapshot set id from its filename
 - o/snapstate: move remove-related tests to snapstate_remove_test.go
 - desktop/notification: switch ExpireTimeout to time.Duration
 - desktop/notification: add unit tests
 - snap: snap help output refresh
 - tests/nested/manual/preseed: include a system-usernames snap when
   preseeding
 - tests: fix sudo-env test
 - tests: fix nested core20 shellcheck bug
 - tests/lib: move to new directory when restoring PWD, cleanup
   unpacked unpacked snap directories
 - desktop/notification: add bindings for FDO notifications
 - dbustest: fix stale comment references
 - many: move ManagedAssetsBootloader into TrustedAssetsBootloader,
   drop former
 - snap-repair: add uc20 support
 - tests: print all the serial logs for the nested test
 - o/snapstate/check_snap_test.go: mock osutil.Find{U,G}id to avoid
   bug in test
 - cmd/snap/auto-import: stop importing system user assertions from
   initramfs mnts
 - osutil/group.go: treat all non-nil errs from user.Lookup{Group,}
   as Unknown*
 - asserts: deserialize grouping only once in Pool.AddBatch if needed
 - gadget: allow content observer to have opinions about a change
 - tests: new snaps-state command - part1
 - o/assertstate: support refreshing any number of snap-declarations
 - boot: use test helpers
 - tests/core/snap-debug-bootvars: also check snap_mode
 - many/apparmor: adjust rules for reading profile/ execing new
   profiles for new kernel
 - tests/core/snap-debug-bootvars: spread test for snap debug boot-
   vars
 - tests/lib/nested.sh: more little tweaks
 - tests/nested/manual/grade-signed-above-testkeys-boot: enable kvm
 - cmd/s-b/initramfs-mounts: use ConfigureTargetSystem for install,
   recover modes
 - overlord: explicitly set refresh-app-awareness in tests
 - kernel: remove "edition" from kernel.yaml and add "update"
 - spread: drop vendor from the packed project archive
 - boot: fix debug bootloader variables dump on UC20 systems
 - wrappers, systemd: allow empty root dir and conditionally do not
   pass --root to systemctl
 - tests/nested/manual: add test for grades above signed booting with
   testkeys
 - tests/nested: misc robustness fixes
 - o/assertstate,asserts: use bulk refresh to refresh snap-
   declarations
 - tests/lib/prepare.sh: stop patching the uc20 initrd since it has
   been updated now
 - tests/nested/manual/refresh-revert-fundamentals: re-enable test
 - update-pot: ignore .go files inside .git when running xgettext-go
 - tests: disable part of the lxd test completely on 16.04.
 - o/snapshotstate: tweak comment regarding snapshot filename
 - o/snapstate: improve snapshot iteration
 - bootloader: lk cleanups
 - tests: update to support nested kvm without reboots on UC20
 - tests/nested/manual/preseed: disable system-key check for 20.04
   image
 - spread.yaml: add ubuntu-20.10-64 to qemu
 - store: handle v2 error when fetching assertions
 - gadget: resolve device mapper devices for fallback device lookup
 - tests/nested/cloud-init-many: simplify tests and unify
   helpers/seed inputs
 - tests: copy /usr/lib/snapd/info to correct directory
 - check-pr-title.py * : allow "*" in the first part of the title
 - many: typos and small test tweak
 - tests/main/lxd: disable cgroup combination for 16.04 that is
   failing a lot
 - tests: make nested signing helpers less confusing
 - tests: misc nested changes
 - tests/nested/manual/refresh-revert-fundamentals: disable
   temporarily
 - tests/lib/cla_check: default to Python 3, tweaks, formatting
 - tests/lib/cl_check.py: use python3 compatible code

* Thu Oct 08 2020 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.47.1
 - o/configstate: create /etc/sysctl.d when applying early config
   defaults
 - cmd/snap-bootstrap/initramfs-mounts: also copy /etc/machine-id for
   same IP addr
 - packaging/{ubuntu,debian}: add liblzo2-dev as a dependency for
   building snapd
 - cmd/snap: allow snap help vs --all to diverge purposefully
 - snap: snap help output refresh

* Tue Sep 29 2020 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.47
 - tests: fix nested core20 shellcheck bug
 - many/apparmor: adjust rule for reading apparmor profile for new
   kernel
 - snap-repair: add uc20 support
 - cmd/snap/auto-import: stop importing system user assertions from
   initramfs mnts
 - cmd/s-b/initramfs-mounts: use ConfigureTargetSystem for install,
   recover modes
 - gadget: resolve device mapper devices for fallback device lookup
 - secboot: add boot manager profile to pcr protection profile
 - sysconfig,o/devicestate: mv DisableNoCloud to
   DisableAfterLocalDatasourcesRun
 - tests: make gadget-reseal more robust
 - tests: skip nested images pre-configuration by default
 - tests: fix for basic20 test running on external backend and rpi
 - tests: improve kernel reseal test
 - boot: adjust comments, naming, log success around reseal
 - tests/nested, fakestore: changes necessary to run nested uc20
   signed/secured tests
 - tests: add nested core20 gadget reseal test
 - boot/modeenv: track unknown keys in Read and put back into modeenv
   during Write
 - interfaces/process-control: add sched_setattr to seccomp
 - boot: with unasserted kernels reseal if there's a hint modeenv
   changed
 - client: bump the default request timeout to 120s
 - configcore: do not error in console-conf.disable for install mode
 - boot: streamline bootstate20.go reseal and tests changes
 - boot: reseal when changing kernel
 - cmd/snap/model: specify grade in the model command output
 - tests: simplify
   repack_snapd_snap_with_deb_content_and_run_mode_first_boot_tweaks
 - test: improve logging in nested tests
 - nested: add support to telnet to serial port in nested VM
 - secboot: use the snapcore/secboot native recovery key type
 - tests/lib/nested.sh: use more focused cloud-init config for uc20
 - tests/lib/nested.sh: wait for the tpm socket to exist
 - spread.yaml, tests/nested: misc changes
 - tests: add more checks to disk space awareness spread test
 - tests: disk space awareness spread test
 - boot: make MockUC20Device use a model and MockDevice more
   realistic
 - boot,many: reseal only when meaningful and necessary
 - tests/nested/core20/kernel-failover: add test for failed refresh
   of uc20 kernel
 - tests: fix nested to work with qemu and kvm
 - boot: reseal when updating boot assets
 - tests: fix snap-routime-portal-info test
 - boot: verify boot chain file in seal and reseal tests
 - tests: use full path to test-snapd-refresh.version binary
 - boot: store boot chains during install, helper for checking
   whether reseal is needed
 - boot: add call to reseal an existing key
 - boot: consider boot chains with unrevisioned kernels incomparable
 - overlord: assorted typos and miscellaneous changes
 - boot: group SealKeyModelParams by model, improve testing
 - secboot: adjust parameters to buildPCRProtectionProfile
 - strutil: add SortedListsUniqueMergefrom the doc comment:
 - snap/naming: upgrade TODO to TODO:UC20
 - secboot: add call to reseal an existing key
 - boot: in seal.go adjust error message and function names
 - o/snapstate: check available disk space in RemoveMany
 - boot: build bootchains data for sealing
 - tests: remove "set -e" from function only shell libs
 - o/snapstate: disk space check on UpdateMany
 - o/snapstate: disk space check with snap update
 - snap: implement new `snap reboot` command
 - boot: do not reorder boot assets when generating predictable boot
   chains and other small tweaks
 - tests: some fixes and improvements for nested execution
 - tests/core/uc20-recovery: fix check for at least specific calls to
   mock-shutdown
 - boot: be consistent using bootloader.Role* consts instead of
   strings
 - boot: helper for generating secboot load chains from a given boot
   asset sequence
 - boot: tweak boot chains to support a list of kernel command lines,
   keep track of model and kernel boot file
 - boot,secboot: switch to expose and use snapcore/secboot load event
   trees
 - tests: use `nested_exec` in core{20,}-early-config test
 - devicestate: enable cloud-init on uc20 for grade signed and
   secured
 - boot: add "rootdir" to baseBootenvSuite and use in tests
 - tests/lib/cla_check.py: don't allow users.noreply.github.com
   commits to pass CLA
 - boot: represent boot chains, helpers for marshalling and
   equivalence checks
 - boot: mark successful with boot assets
 - client, api: handle insufficient space error
 - o/snapstate: disk space check with single snap install
 - configcore: "service.console-conf.disable" is gadget defaults only
 - packaging/opensuse: fix for /usr/libexec on TW, do not hardcode
   AppArmor profile path
 - tests: skip udp protocol in nfs-support test on ubuntu-20.10
 - packaging/debian-sid: tweak code preparing _build tree
 - many: move seal code from gadget/install to boot
 - tests: remove workaround for cups on ubuntu-20.10
 - client: implement RebootToSystem
 - many: seed.Model panics now if called before LoadAssertions
 - daemon: add /v2/systems "reboot" action API
 - github: run tests also on push to release branches
 - interfaces/bluez: let slot access audio streams
 - seed,c/snap-bootstrap: simplify snap-bootstrap seed reading with
   new seed.ReadSystemEssential
 - interfaces: allow snap-update-ns to read /proc/cmdline
 - tests: new organization for nested tests
 - o/snapstate, features: add feature flags for disk space awareness
 - tests: workaround for cups issue on 20.10 where default printer is
   not configured.
 - interfaces: update cups-control and add cups for providing snaps
 - boot: keep track of the original asset when observing updates
 - tests: simplify and fix tests for disk space checks on snap remove
 - sysconfig/cloudinit.go: add AllowCloudInit and use GadgetDir for
   cloud.conf
 - tests/main: mv core specific tests to core suite
 - tests/lib/nested.sh: reset the TPM when we create the uc20 vm
 - devicestate: rename "mockLogger" to "logbuf"
 - many: introduce ContentChange for tracking gadget content in
   observers
 - many: fix partion vs partition typo
 - bootloader: retrieve boot chains from bootloader
 - devicestate: add tests around logging in RequestSystemAction
 - boot: handle canceled update
 - bootloader: tweak doc comments (thanks Samuele)
 - seed/seedwriter: test local asserted snaps with UC20 grade signed
 - sysconfig/cloudinit.go: add DisableNoCloud to
   CloudInitRestrictOptions
 - many: use BootFile type in load sequences
 - boot,bootloader: clarifications after the changes to introduce
   bootloader.Options.Role
 - boot,bootloader,gadget: apply new bootloader.Options.Role
 - o/snapstate, features: add feature flag for disk space check on
   remove
 - testutil: add checkers for symbolic link target
 - many: refactor tpm seal parameter setting
 - boot/bootstate20: reboot to rollback to previous kernel
 - boot: add unit test helpers
 - boot: observe update & rollback of trusted assets
 - interfaces/utf: Add MIRKey to u2f devices
 - o/devicestate/devicestate_cloudinit_test.go: test cleanup for uc20
   cloud-init tests
 - many: check that users of BaseTest don't forget to consume
   cleanups
 - tests/nested/core20/tpm: verify trusted boot assets tracking
 - github: run macOS job with Go 1.14
 - many: misc doc-comment changes and typo fixes
 - o/snapstate: disk space check with InstallMany
 - many: cloud-init cleanups from previous PR's
 - tests: running tests on opensuse leap 15.2
 - run-checks: check for dirty build tree too
 - vendor: run ./get-deps.sh to update the secboot hash
 - tests: update listing test for "-dirty" versions
 - overlord/devicestate: do not release the state lock when updating
   gadget assets
 - secboot: read kernel efi image from snap file
 - snap: add size to the random access file return interface
 - daemon: correctly parse Content-Type HTTP header.
 - tests: account for apt-get on core18
 - cmd/snap-bootstrap/initramfs-mounts: compute string outside of
   loop
 - mkversion.sh: simple hack to include dirty in version if the tree
   is dirty
 - cgroup,snap: track hooks on system bus only
 - interfaces/systemd: compare dereferenced Service
 - run-checks: only check files in git for misspelling
 - osutil: add a package doc comment (via doc.go)
 - boot: complain about reused asset name during initial install
 - snapstate: installSize helper that calculates total size of snaps
   and their prerequisites
 - snapshots: export of snapshots
 - boot/initramfs_test.go: reset boot vars on the bootloader for each
   iteration

* Fri Sep 04 2020 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.46.1
 - interfaces: allow snap-update-ns to read
   /proc/cmdline
 - github: run macOS job with Go 1.14
 - o/snapstate, features: add feature flag for disk space check on
   remove
 - tests: account for apt-get on core18
 - mkversion.sh: include dirty in version if the tree
   is dirty
 - interfaces/systemd: compare dereferenced Service
 - vendor.json: update mysterious secboot SHA again

* Tue Aug 25 2020 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.46
 - logger: add support for setting snapd.debug=1 on kernel cmdline
 - o/snapstate: check disk space before creating automatic snapshot
   on remove
 - boot, o/devicestate: observe existing recovery bootloader trusted
   boot assets
 - many: use transient scope for tracking apps and hooks
 - features: add HiddenSnapFolder feature flag
 - tests/lib/nested.sh: fix partition typo, unmount the image on uc20
   too
 - runinhibit: open the lock file in read-only mode in IsLocked
 - cmd/s-b/initramfs-mounts: make recover -> run mode transition
   automatic
 - tests: update spread test for unknown plug/slot with snapctl is-
   connected
 - osutil: add OpenExistingLockForReading
 - kernel: add kernel.Validate()
 - interfaces: add vcio interface
 - interfaces/{docker,kubernetes}-support: load overlay and support
   systemd cgroup driver
 - tests/lib/nested.sh: use more robust code for finding what loop
   dev we mounted
 - cmd/snap-update-ns: detach all bind-mounted file
 - snap/snapenv: set SNAP_REAL_HOME
 - packaging: umount /snap on purge in containers
 - interfaces: misc policy updates xlvi
 - secboot,cmd/snap-bootstrap: cross-check partitions before
   unlocking, mounting
 - boot: copy boot assets cache to new root
 - gadget,kernel: add new kernel.{Info,Asset} struct and helpers
 - o/hookstate/ctlcmd: make is-connected check whether the plug or
   slot exists
 - tests: find -ignore_readdir_race when scanning cgroups
 - interfaces/many: deny arbitrary desktop files and misc from
   /usr/share
 - tests: use "set -ex" in prep-snapd-in-lxd.sh
 - tests: re-enable udisks test on debian-sid
 - cmd/snapd-generator: use PATH fallback if PATH is not set
 - tests: disable udisks2 test on arch linux
 - github: use latest/stable go, not latest/edge
 - tests: remove support for ubuntu 19.10 from spread tests
 - tests: fix lxd test wrongly tracking 'latest'
 - secboot: document exported functions
 - cmd: compile snap gdbserver shim correctly
 - many: correctly calculate the desktop file prefix everywhere
 - interfaces: add kernel-crypto-api interface
 - corecfg: add "system.timezone" setting to the system settings
 - cmd/snapd-generator: generate drop-in to use fuse in container
 - cmd/snap-bootstrap/initramfs-mounts: tweak names, add comments
   from previous PR
 - interfaces/many: miscellaneous updates for strict microk8s
 - secboot,cmd/snap-bootstrap: don't import boot package from secboot
 - cmd/snap-bootstrap/initramfs-mounts: call systemd-mount instead of
   the-tool
 - tests: work around broken update of systemd-networkd
 - tests/main/install-fontconfig-cache-gen: enhance test by
   verifying, add fonts to test
 - o/devicestate: wrap asset update observer error
 - boot: refactor such that bootStateUpdate20 mainly carries Modeenv
 - mkversion.sh: disallow changelog versions that have git in it, if
   we also have git version
 - interfaces/many: miscellaneous updates for strict microk8s
 - snap: fix repeated "cannot list recovery system" and add test
 - boot: track trusted assets during initial install, assets cache
 - vendor: update secboot to fix key data validation
 - tests: unmount FUSE file-systems from XDG runtime dir
 - overlord/devicestate: workaround non-nil interface with nil struct
 - sandbox/cgroup: remove temporary workaround for multiple cgroup
   writers
 - sandbox/cgroup: detect dangling v2 cgroup
 - bootloader: add helper for creating a bootloader based on gadget
 - tests: support different images on nested execution
 - many: reorg cmd/snapinfo.go into snap and new client/clientutil
 - packaging/arch: use external linker when building statically
 - tests: cope with ghost cgroupv2
 - tests: fix issues related to restarting systemd-logind.service
 - boot, o/devicestate: TrustedAssetUpdateObserver stubs, hook up to
   gadget updates
 - vendor: update github.com/kr/pretty to fix diffs of values with
   pointer cycles
 - boot: move bootloaderKernelState20 impls to separate file
 - .github/workflows: move snap building to test.yaml as separate
   cached job
 - tests/nested/manual/minimal-smoke: run core smoke tests in a VM
   meeting minimal requirements
 - osutil: add CommitAs to atomic file
 - gadget: introduce content update observer
 - bootloader: introduce TrustedAssetsBootloader, implement for grub
 - o/snapshotstate: helpers for calculating disk space needed for an
   automatic snapshot
 - gadget/install: retrieve command lines from bootloader
 - boot/bootstate20: unify commit method impls, rm
   bootState20MarkSuccessful
 - tests: add system information and image information when debug
   info is displayed
 - tests/main/cgroup-tracking: try to collect some information about
   cgroups
 - boot: introduce current_boot_assets and
   current_recovery_boot_assets to modeenv
 - tests: fix for timing issues on journal-state test
 - many: remove usage and creation of hijacked pid cgroup
 - tests: port regression-home-snap-root-owned to tests.session
 - tests: run as hightest via tests.session
 - github: run CLA checks on self-hosted workers
 - github: remove Ubuntu 19.10 from actions workflow
 - tests: remove End-Of-Life opensuse/fedora releases
 - tests: remove End-Of-Life releases from spread.yaml
 - tests: fix debug section of appstream-id test
 - interfaces: check !b.preseed earlier
 - tests: work around bug in systemd/debian
 - boot: add deepEqual, Copy helpers for Modeenv to simplify
   bootstate20 refactor
 - cmd: add new "snap recovery" command
 - interfaces/systemd: use emulation mode when preseeding
 - interfaces/kmod: don't load kernel modules in kmod backend when
   preseeding
 - interfaces/udev: do not reload udevadm rules when preseeding
 - cmd/snap-preseed: use snapd from the deb if newer than from seeds
 - boot: fancy marshaller for modeenv values
 - gadget, osutil: use atomic file copy, adjust tests
 - overlord: use new tracking cgroup for refresh app awareness
 - github: do not skip gofmt with Go 1.9/1.10
 - many: introduce content write observer, install mode glue, initial
   seal stubs
 - daemon,many: switch to use client.ErrorKind and drop the local
   errorKind...
 - tests: new parameters for nested execution
 - client: move all error kinds into errors.go and add doc strings
 - cmd/snap: display the error in snap debug seeding if seeding is in
   error
 - cmd/snap/debug/seeding: use unicode for proper yaml
 - tests/cmd/snap-bootstrap/initramfs-mounts: add test case for empty
   recovery_mode
 - osutil/disks: add mock disk and tests for happy path of mock disks
 - tests: refresh/revert snapd in uc20
 - osutil/disks: use a dedicated error to indicate a fs label wasn't
   found
 - interfaces/system-key: in WriteSystemKey during tests, don't call
   ParserFeatures
 - boot: add current recovery systems to modeenv
 - bootloader: extend managed assets bootloader interface to compose
   a candidate command line
 - interfaces: make the unmarshal test match more the comment
 - daemon/api: use pointers to time.Time for debug seeding aspect
 - o/ifacestate: update security profiles in connect undo handler
 - interfaces: add uinput interface
 - cmd/snap-bootstrap/initramfs-mounts: add doSystemdMount + unit
   tests
 - o/devicestate: save seeding/preseeding times for use with debug
   seeding api
 - cmd/snap/debug: add "snap debug seeding" command for preseeding
   debugging
 - tests/main/selinux-clean: workaround SELinux denials triggered by
   linger setup on Centos8
 - bootloader: compose command line with mode and extra arguments
 - cmd/snap, daemon: detect and bail purge on multi-snap
 - o/ifacestate: fix bug in snapsWithSecurityProfiles
 - interfaces/builtin/multipass: replace U+00A0 no-break space with
   simple space
 - bootloader/assets: generate bootloader assets from files
 - many/tests/preseed: reset the preseeded images before preseeding
   them
 - tests: drop accidental accents from e
 - secboot: improve key sealing tests
 - tests: replace _wait_for_file_change with retry
 - tests: new fs-state which replaces the files.sh helper
 - sysconfig/cloudinit_test.go: add test for initramfs case, rm "/"
   from path
 - cmd/snap: track started apps and hooks
 - tests/main/interfaces-pulseaudio: disable start limit checking for
   pulseaudio service
 - api: seeding debug api
 - .github/workflows/snap-build.yaml: build the snapd snap via GH
   Actions too
 - tests: moving journalctl.sh to a new journal-state tool
 - tests/nested/manual: add spread tests for cloud-init vuln
 - bootloader/assets: helpers for registering per-edition snippets,
   register snippets for grub
 - data,packaging,wrappers: extend D-Bus service activation search
   path
 - spread: add opensuse 15.2 and tumbleweed for qemu
 - overlord,o/devicestate: restrict cloud-init on Ubuntu Core
 - sysconfig/cloudinit: add RestrictCloudInit
 - cmd/snap-preseed: check that target path exists and is a directory
   on --reset
 - tests: check for pids correctly
 - gadget,gadget/install: refactor partition table update
 - sysconfig/cloudinit: add CloudInitStatus func + CloudInitState
   type
 - interface/fwupd: add more policies for making fwupd upstream
   strict
 - tests: new to-one-line tool which replaces the strings.sh helper
 - interfaces: new helpers to get and compare system key, for use
   with seeding debug api
 - osutil, many: add helper for checking whether the process is a go
   test binary
 - cmd/snap-seccomp/syscalls: add faccessat2
 - tests: adjust xdg-open after launcher changes
 - tests: new core config helper
 - usersession/userd: do not modify XDG_DATA_DIRS when calling xdg-
   open
 - cmd/snap-preseed: handle relative chroot path
 - snapshotstate: move sizer to osutil.Sizer()
 - tests/cmd/snap-bootstrap/initramfs-mounts: rm duplicated env ref
   kernel tests
 - gadget/install,secboot: use snapcore/secboot luks2 api
 - boot/initramfs_test.go: add Commentf to more Assert()'s
 - tests/lib: account for changes in arch package file name extension
 - bootloader/bootloadertest: fix comment typo
 - bootloader: add helper for getting recovery system environment
   variables
 - tests: preinstall shellcheck and run tests on focal
 - strutil: add a helper for parsing kernel command line
 - osutil: add CheckFreeSpace helper
 - secboot: update tpm connection error handling
 - packaging, cmd/snap-mgmt, tests: remove modules files on purge
 - tests: add tests.cleanup helper
 - packaging: add "ca-certificates" to build-depends
 - tests: more checks in core20 early config spread test
 - tests: fix some snapstate tests to use pointers for
   snapmgrTestSuite
 - boot: better naming of helpers for obtaining kernel command line
 - many: use more specific check for unit test mocking
 - systemd/escape: fix issues with "" and "\t" handling
 - asserts: small improvements and corrections for sequence-forming
   assertions' support
 - boot, bootloader: query kernel command line of run mod and
   recovery mode systems
 - snap/validate.go: disallow snap layouts with new top-level
   directories
 - tests: allow to add a new label to run nested tests as part of PR
   validation
 - tests/core/gadget-update-pc: port to UC20
 - tests: improve nested tests flexibility
 - asserts: integer headers: disallow prefix zeros and make parsing
   more uniform
 - asserts: implement Database.FindSequence
 - asserts: introduce SequenceMemberAfter in the asserts backstores
 - spread.yaml: remove tests/lib/tools from PATH
 - overlord: refuse to install snaps whose activatable D-Bus services
   conflict with installed snaps
 - tests: shorten lxd-state undo-mount-changes
 - snap-confine: don't die if a device from sysfs path cannot be
   found by udev
 - tests: fix argument handling of apt-state
 - tests: rename lxd-tool to lxd-state
 - tests: rename user-tool to user-state, fix --help
 - interfaces: add gconf interface
 - sandbox/cgroup: avoid parsing security tags twice
 - tests: rename version-tool to version-compare
 - cmd/snap-update-ns: handle anomalies better
 - tests: fix call to apt.Package.mark_install(auto_inst=True)
 - tests: rename mountinfo-tool to mountinfo.query
 - tests: rename memory-tool to memory-observe-do
 - tests: rename invariant-tool to tests.invariant
 - tests: rename apt-tool to apt-state
 - many: managed boot config during run mode setup
 - asserts: introduce the concept of sequence-forming assertion types
 - tests: tweak comments/output in uc20-recovery test
 - tests/lib/pkgdb: do not use quiet when purging debs
 - interfaces/apparmor: allow snap-specific /run/lock
 - interfaces: add system-source-code for access to /usr/src
 - sandbox/cgroup: extend SnapNameFromPid with tracking cgroup data
 - gadget/install: move udev trigger to gadget/install
 - many: make nested spread tests more reliable
 - tests/core/uc20-recovery: apply hack to get gopath in recover mode
   w/ external backend
 - tests: enable tests on uc20 which now work with the real model
   assertion
 - tests: enable system-snap-refresh test on uc20
 - gadget, bootloader: preserve managed boot assets during gadget
   updates
 - tests: fix leaked dbus-daemon in selinux-clean
 - tests: add servicestate.Control tests
 - tests: fix "restart.service"
 - wrappers: helper for enabling services - extract and move enabling
   of services into a helper
 - tests: new test to validate refresh and revert of kernel and
   gadget on uc20
 - tests/lib/prepare-restore: collect debug info when prepare purge
   fails
 - bootloader: allow managed bootloader to update its boot config
 - tests: Remove unity test from nightly test suite
 - o/devicestate: set mark-seeded to done in the task itself
 - tests: add spread test for disconnect undo caused by failing
   disconnect hook
 - sandbox/cgroup: allow discovering PIDs of given snap
 - osutil/disks: support IsDecryptedDevice for mountpoints which are
   dm devices
 - osutil: detect autofs mounted in /home
 - spread.yaml: allow amazon-linux-2-64 qemu with
   ec2-user/ec2-user
 - usersession: support additional zoom URL schemes
 - overlord: mock timings.DurationThreshold in TestNewWithGoodState
 - sandbox/cgroup: add tracking helpers
 - tests: detect stray dbus-daemon
 - overlord: refuse to install snaps providing user daemons on Ubuntu
   14.04
 - many: move encryption and installer from snap-boostrap to gadget
 - o/ifacestate: fix connect undo handler
 - interfaces: optimize rules of multiple connected iio/i2c/spi plugs
 - bootloader: introduce managed bootloader, implement for grub
 - tests: fix incorrect check in smoke/remove test
 - asserts,seed: split handling of essential/not essential model
   snaps
 - gadget: fix typo in mounted filesystem updater
 - gadget: do only one mount point lookup in mounted fs updater
 - tests/core/snap-auto-mount: try to make the test more robust
 - tests: adding ubuntu-20.04 to google-sru backend
 - o/servicestate: add updateSnapstateServices helper
 - bootloader: pull recovery grub config from internal assets
 - tests/lib/tools: apply linger workaround when needed
 - overlord/snapstate: graceful handling of denied "managed" refresh
   schedule
 - snapstate: fix autorefresh from classic->strict
 - overlord/configstate: add system.kernel.printk.console-loglevel
   option
 - tests: fix assertion disk handling for nested UC systems
 - snapstate: use testutil.HostScaledTimeout() in snapstate tests
 - tests: extra worker for google-nested backend to avoid timeout
   error on uc20
 - snapdtool: helper to check whether the current binary is reexeced
   from a snap
 - tests: mock servicestate in api tests to avoid systemctl checks
 - many: rename back snap.Info.GetType to Type
 - tests/lib/cla_check: expect explicit commit range
 - osutil/disks: refactor diskFromMountPointImpl a bit
 - o/snapstate: service-control task handler
 - osutil: add disks pkg for associating mountpoints with
   disks/partitions
 - gadget,cmd/snap-bootstrap: move partitioning to gadget
 - seed: fix LoadEssentialMeta when gadget is not loaded
 - cmd/snap: Debian does not allow $SNAP_MOUNT_DIR/bin in sudo
   secure_path
 - asserts: introduce new assertion validation-set
 - asserts,daemon: add support for "serials" field in system-user
   assertion
 - data/sudo: drop a failed sudo secure_path workaround
 - gadget: mv encodeLabel to osutil/disks.EncodeHexBlkIDFormat
 - boot, snap-bootstrap: move initramfs-mounts logic to boot pkg
 - spread.yaml: update secure boot attribute name
 - interfaces/block_devices: add NVMe subsystem devices, support
   multipath paths
 - tests: use the "jq" snap from the edge channel
 - tests: simplify the tpm test by removing the test-snapd-mokutil
   snap
 - boot/bootstate16.go: clean snap_try_* vars when not in Trying
   status too
 - tests/main/sudo-env: check snap path under sudo
 - tests/main/lxd: add test for snaps inside nested lxd containers
   not working
 - asserts/internal: expand errors about invalid serialized grouping
   labels
 - usersession/userd: add msteams url support
 - tests/lib/prepare.sh: adjust comment about sgdisk
 - tests: fix how gadget pc is detected when the snap does not exist
   and ls fails
 - tests: move a few more tests to snapstate_update_test.go
 - tests/main: add spread test for running svc from install hook
 - tests/lib/prepare: increase the size of the uc16/uc18 partitions
 - tests/special-home-can-run-classic-snaps: re-enable
 - workflow: test PR title as part of the static checks again
 - tests/main/xdg-open-compat: backup and restore original xdg-open
 - tests: move update-related tests to snapstate_update_test.go
 - cmd,many: move Version and bits related to snapd tools to
   snapdtool, merge cmdutil
 - tests/prepare-restore.sh: reset-failed systemd-journald before
   restarting
 - interfaces: misc small interface updates
 - spread: use find rather than recursive ls, skip mounted snaps
 - tests/lib/prepare-restore.sh: if we failed to purge snapd deb, ls
   /var/lib/snapd
 - tests: enable snap-auto-mount test on core20
 - cmd/snap: do not show $PATH warning when executing under sudo on a
   known distro
 - asserts/internal: add some iteration benchmarks
 - sandbox/cgroup: improve pid parsing code
 - snap: add new `snap run --experimental-gdbserver` option
 - asserts/internal: limit Grouping size switching to a bitset
   representationWe don't always use the bit-set representation
   because:
 - snap: add an activates-on property to apps for D-Bus activation
 - dirs: delete unused Cloud var, fix typo
 - sysconfig/cloudinit: make callers of DisableCloudInit use
   WritableDefaultsDir
 - tests: fix classic ubuntu core transition auth
 - tests: fail in setup_reflash_magic() if there is snapd state left
 - tests: port interfaces-many-core-provided to tests.session
 - tests: wait after creating partitions with sfdisk
 - bootloader: introduce bootloarder assets, import grub.cfg with an
   edition marker
 - riscv64: bump timeouts
 - gadget: drop dead code, hide exports that are not used externally
 - tests: port 2 uc20 part1
 - tests: fix bug waiting for snap command to be ready
 - tests: move try-related tests to snapstate_try_test.go
 - tests: add debug for 20.04 prepare failure
 - travis.yml: removed, all our checks run in GH actions now
 - tests: clean up up the use of configcoreSuite in the configcore
   tests
 - sandbox/cgroup: remove redundant pathOfProcPidCgroup
 - sandbox/cgroup: add tests for ParsePids
 - tests: fix the basic20 test for uc20 on external backend
 - tests: use configcoreSuite in journalSuite and remove some
   duplicated code
 - tests: move a few more tests to snapstate_install_test
 - tests: assorted small patches
 - dbusutil/dbustest: separate license from package
 - interfaces/builtin/time-control: allow POSIX clock API
 - usersession/userd: add "slack" to the white list of URL schemes
   handled by xdg-open
 - tests: check that host settings like hostname are settable on core
 - tests: port xdg-settings test to tests.session
 - tests: port snap-handle-link test to tests.session
 - arch: add riscv64
 - tests: core20 early defaults spread test
 - tests: move install tests from snapstate_test.go to
   snapstate_install_test.go
 - github: port macOS sanity checks from travis
 - data/selinux: allow checking /var/cache/app-info
 - o/devicestate: core20 early config from gadget defaults
 - tests: autoremove after removing lxd in preseed-lxd test
 - secboot,cmd/snap-bootstrap: add tpm sealing support to secboot
 - sandbox/cgroup: move FreezerCgroupDir from dirs.go
 - tests: update the file used to detect the boot path on uc20
 - spread.yaml: show /var/lib/snapd in debug
 - cmd/snap-bootstrap/initramfs-mounts: also copy systemd clock +
   netplan files
 - snap/naming: add helpers to parse app and hook security tags
 - tests: modernize retry tool
 - tests: fix and trim debug section in xdg-open-portal
 - tests: modernize and use snapd.tool
 - vendor: update to latest github.com/snapcore/bolt for riscv64
 - cmd/snap-confine: add support for libc6-lse
 - interfaces: miscellaneous policy updates xlv
 - interfaces/system-packages-doc: fix typo in variable names
 - tests: port interfaces-calendar-service to tests.session
 - tests: install/run the lzo test snap too
 - snap: (small) refactor of `snap download` code for
   testing/extending
 - data: fix shellcheck warnings in snapd.sh.in
 - packaging: disable buildmode=pie for riscv64
 - tests: install test-snapd-rsync snap from edge channel
 - tests: modernize tests.session and port everything using it
 - tests: add ubuntu 20.10 to spread tests
 - cmd/snap/remove: mention snap restore/automatic snapshots
 - dbusutil: move all D-Bus helpers and D-Bus test helpers
 - wrappers: pass 'disable' flag to StopServices wrapper
 - osutil: enable riscv64 build
 - snap/naming: add ParseSecurityTag and friends
 - tests: port document-portal-activation to session-tool
 - bootloader: rename test helpers to reflect we are mocking EFI boot
   locations
 - tests: disable test of nfs v3 with udp proto on debian-sid
 - tests: plan to improve the naming and uniformity of utilities
 - tests: move *-tool tests to their own suite
 - snap-bootstrap: remove sealed key file on reinstall
 - bootloader/ubootenv: don't panic with an empty uboot env
 - systemd: rename actualFsTypeAndMountOptions to
   hostFsTypeAndMountOptions
 - daemon: fix filtering of service-control changes for snap.app
 - tests: spread test for preseeding in lxd container
 - tests: fix broken snapd.session agent.socket
 - wrappers: add RestartServices function and ReloadOrRestart to
   systemd
 - o/cmdstate: handle ignore flag on exec-command tasks
 - gadget: make ext4 filesystems with or without metadata checksum
 - tests: update statx test to run on all LTS releases
 - configcore: show better error when disabling services
 - interfaces: add hugepages-control
 - interfaces-ssh-keys: Support reading /etc/ssh/ssh_config.d/
 - tests: run ubuntu-20.04-* tests on all ubuntu-2* releases
 - tests: skip interfaces-openvswitch for centos 8 in nightly suite
 - tests: reload systemd --user for root, if present
 - tests: reload systemd after editing /etc/fstab
 - tests: add missing dependencies needed for sbuild test on debian
 - tests: reload systemd after removing pulseaudio
 - image, tests: core18 early config.
 - interfaces: add system-packages-doc interface
 - cmd/snap-preseed, systemd: fix handling of fuse.squashfuse when
   preseeding
 - interfaces/fwupd: allow bind mount to /boot on core
 - tests: improve oom-vitality tests
 - tests: add fedora 32 to spread.yaml
 - config: apply vitality-hint immediately when the config changes
 - tests: port snap-routine-portal-info to session-tool
 - configcore: add "service.console-conf.disable" config option
 - tests: port xdg-open to session-tool
 - tests: port xdg-open-compat to session-tool
 - tests: port interfaces-desktop-* to session-tool
 - spread.yaml: apply yaml formatter/linter
 - tests: port interfaces-wayland to session-tool
 - o/devicestate: refactor current system handling
 - snap-mgmt: perform cleanup of user services
 - snap/snapfile,squashfs: followups from 8729
 - boot, many: require mode in modeenv
 - data/selinux: update policy to allow forked processes to call
   getpw*()
 - tests: log stderr from dbus-monitor
 - packaging: build cmd/snap and cmd/snap-bootstrap with nomanagers
   tag
 - snap/squashfs: also symlink snap Install with uc20 seed snap dir
   layout
 - interfaces/builtin/desktop: do not mount fonts cache on distros
   with quirks
 - data/selinux: allow snapd to remove/create the its socket
 - testutil/exec.go: set PATH after running shellcheck
 - tests: silence stderr from dbus-monitor
 - snap,many: mv Open to snapfile pkg to support add'l options to
   Container methods
 - devicestate, sysconfig: revert support for cloud.cfg.d/ in the
   gadget
 - github: remove workaround for bug 133 in actions/cache
 - tests: remove dbus.sh
 - cmd/snap-preseed: improve mountpoint checks of the preseeded
   chroot
 - spread.yaml: add ps aux to debug section
 - github: run all spread systems in a single go with cached results
 - test: session-tool cli tweaks
 - asserts: rest of the Pool API
 - tests: port interfaces-network-status-classic to session-tool
 - packaging: remove obsolete 16.10,17.04 symlinks
 - tests: setup portals before starting user session
 - o/devicestate: typo fix
 - interfaces/serial-port: add NXP SC16IS7xx (ttySCX) to allowed
   devices
 - cmd/snap/model: support store, system-user-authority keys in
   --verbose
 - o/devicestate: raise conflict when requesting system action while
   seeding
 - tests: detect signs of crashed snap-confine
 - tests: sign kernel and gadget to run nested tests using current
   snapd code
 - tests: remove gnome-online-accounts we install
 - tests: fix the issue where all the tests were executed on secboot
   system
 - tests: port interfaces-accounts-service to session-tool
 - interfaces/network-control: bring /var/lib/dhcp from host
 - image,cmd/snap,tests: add support for store-wide cohort keys
 - configcore: add nomanagers buildtag for conditional build
 - tests: port interfaces-password-manager-service to session-tool
 - o/devicestate: cleanup system actions supported by recover mode
 - snap-bootstrap: remove create-partitions and update tests
 - tests: fix nested tests
 - packaging/arch: update PKGBUILD to match one in AUR
 - tests: port interfaces-location-control to session-tool
 - tests: port interfaces-contacts-service to session-tool
 - state: log task errors in the journal too
 - o/devicestate: change how current system is reported for different
   modes
 - devicestate: do not report "ErrNoState" for seeded up
 - tests: add a note about broken test sequence
 - tests: port interfaces-autopilot-introspection to session-tool
 - tests: port interfaces-dbus to session-tool
 - packaging: update sid packaging to match 16.04+
 - tests: enable degraded test on uc20
 - c/snaplock/runinhibit: add run inhibition operations
 - tests: detect and report root-owned files in /home
 - tests: reload root's systemd --user after snapd tests
 - tests: test registration with serial-authority: [generic]
 - cmd/snap-bootstrap/initramfs-mounts: copy auth.json and macaroon-
   key in recover
 - tests/mount-ns: stop binfmt_misc mount unit
 - cmd/snap-bootstrap/initramfs-mounts: use booted kernel partition
   uuid if available
 - daemon, tests: indicate system mode, test switching to recovery
   and back to run
 - interfaces/desktop: silence more /var/lib/snapd/desktop/icons
   denials
 - tests/mount-ns: update to reflect new UEFI boot mode
 - usersession,tests: clean ups for userd/settings.go and move
   xdgopenproxy under usersession
 - tests: disable mount-ns test
 - tests: test user belongs to systemd-journald, on core20
 - tests: run core/snap-set-core-config on uc20 too
 - tests: remove generated session-agent units
 - sysconfig: use new _writable_defaults dir to create cloud config
 - cmd/snap-bootstrap/initramfs-mounts: cosmetic changes in prep for
   future work
 - asserts: make clearer that with label we mean a serialized label
 - cmd/snap-bootstrap: tweak recovery trigger log messages
 - asserts: introduce PoolTo
 - userd: allow setting default-url-scheme-handler
 - secboot: append uuid to ubuntu-data when decrypting
 - o/configcore: pass extra options to FileSystemOnlyApply
 - tests: add dbus-user-session to bionic and reorder package names
 - boot, bootloader: adjust comments, expand tests
 - tests: improve debugging of user session agent tests
 - packaging: add the inhibit directory
 - many: add core.resiliance.vitality-hint config setting
 - tests: test adjustments and fixes for recently published images
 - cmd/snap: coldplug auto-import assertions from all removable
   devices
 - secboot,cmd/snap-bootstrap: move initramfs-mounts tpm access to
   secboot
 - tests: not fail when boot dir cannot be determined
 - tests: new directory used to store the cloud images on gce
 - tests: inject snapd from edge into seeds of the image in manual
   preseed test
 - usersession/agent,wrappers: fix races between Shutdown and Serve
 - tests: add dependency needed for next upgrade of bionic
 - tests: new test user is used for external backend
 - cmd/snap: fix the order of positional parameters in help output
 - tests: don't create root-owned things in ~test
 - tests/lib/prepare.sh: delete patching of the initrd
 - cmd/snap-bootstrap/initramfs-mounts: add sudoers to dirs to copy
   as well
 - progress: tweak multibyte label unit test data
 - o/devicestate,cmd/snap-bootstrap: seal to recover mode cmdline
 - gadget: fix fallback device lookup for 'mbr' type structures
 - configcore: only reload journald if systemd is new enough
 - cmd/snap-boostrap, boot: use /run/mnt/data instead of ubuntu-data
 - wrappers: allow user mode systemd daemons
 - progress: fix progress bar with multibyte duration units
 - tests: fix raciness in pulseaudio test
 - asserts/internal: introduce Grouping and Groupings
 - tests: remove user.sh
 - tests: pair of follow-ups from earlier reviews
 - overlord/snapstate: warn of refresh/postpone events
 - configcore,tests: use daemon-reexec to apply watchdog config
 - c/snap-bootstrap: check mount states via initramfsMountStates
 - store: implement DownloadAssertions
 - tests: run smoke test with different bases
 - tests: port user-mounts test to session-tool
 - store: handle error-list in fetch-assertions results
 - tests: port interfaces-audio-playback-record to session-tool
 - data/completion: add `snap` command completion for zsh
 - tests/degraded: ignore failure in systemd-vconsole-setup.service
 - image: stub implementation of image.Prepare for darwin
 - tests: session-tool --restore -u stops user-$UID.slice
 - o/ifacestate/handlers.go: fix typo
 - tests: port pulseaudio test to session-tool
 - tests: port user-session-env to session-tool
 - tests: work around journald bug in core16
 - tests: add debug to core-persistent-journal test
 - tests: port selinux-clean to session-tool
 - tests: port portals test to session-tool, fix portal tests on sid
 - tests: adding option --no-install-recommends option also when
   install all the deps
 - tests: add session-tool --has-systemd-and-dbus
 - packaging/debian-sid: add gcc-multilib to build deps
 - osutil: expand FileLock to support shared locks and more
 - packaging: stop depending on python-docutils
 - store,asserts,many: support the new action fetch-assertions
 - tests: port snap-session-agent-* to session-tool
 - packaging/fedora: disable FIPS compliant crypto for static
   binaries
 - tests: fix for preseeding failures

* Tue Jul 28 2020 Samuele Pedroni <pedronis@lucediurna.net>
- New upstream release, LP: #1875071
  - o/ifacestate: fix bug in snapsWithSecurityProfiles
  - tests/main/selinux-clean: workaround SELinux denials triggered by
    linger setup on Centos8

* Mon Jul 27 2020 Zygmunt Krynicki <me@zygoon.pl>
- New upstream release, LP: #1875071
  - many: backport _writable_defaults dir changes
  - tests: fix incorrect check in smoke/remove test
  - cmd/snap-bootstrap,seed: backport of uc20 PRs
  - tests: avoid exit when nested type var is not defined
  - cmd/snap-preseed: backport fixes
  - interfaces: optimize rules of multiple connected iio/i2c/spi plugs
  - many: cherry-picks for 2.45, gh-action, test fixes
  - tests/lib: account for changes in arch package file name extension
  - postrm, snap-mgmt: cleanup modules and other cherry-picks
  - snap-confine: don't die if a device from sysfs path cannot be
    found by udev
  - data/selinux: update policy to allow forked processes to call
    getpw*()
  - tests/main/interfaces-time-control: exercise setting time via date
  - interfaces/builtin/time-control: allow POSIX clock API
  - usersession/userd: add "slack" to the white list of URL schemes
    handled by xdg-open

* Fri Jul 10 2020 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.45.2
 - SECURITY UPDATE: sandbox escape vulnerability on snapctl xdg-open
   implementation
   - usersession/userd/launcher.go: remove XDG_DATA_DIRS environment
     variable modification when calling the system xdg-open. Patch
     thanks to James Henstridge
   - packaging/ubuntu-16.04/snapd.postinst: ensure "snap userd" is
     restarted. Patch thanks to Michael Vogt
   - CVE-2020-11934
 - SECURITY UPDATE: arbitrary code execution vulnerability on core
   devices with access to physical removable media
   - devicestate: Disable/restrict cloud-init after seeding.
   - CVE-2020-11933

* Fri Jun 05 2020 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.45.1
 - data/selinux: allow checking /var/cache/app-info
 - cmd/snap-confine: add support for libc6-lse
 - interfaces: miscellaneous policy updates xlv
 - snap-bootstrap: remove sealed key file on reinstall
 - interfaces-ssh-keys: Support reading /etc/ssh/ssh_config.d/
 - gadget: make ext4 filesystems with or without metadata checksum
 - interfaces/fwupd: allow bind mount to /boot on core
 - tests: cherry-pick test fixes from master
 - snap/squashfs: also symlink snap Install with uc20 seed snap dir
   layout
 - interfaces/serial-port: add NXP SC16IS7xx (ttySCX) to allowed
   devices
 - snap,many: mv Open to snapfile pkg to support add'l options to
   Container methods
 - interfaces/builtin/desktop: do not mount fonts cache on distros
   with quirks
 - devicestate, sysconfig: revert support for cloud.cfg.d/ in the
   gadget
 - data/completion, packaging: cherry-pick zsh completion
 - state: log task errors in the journal too
 - devicestate: do not report "ErrNoState" for seeded up
 - interfaces/desktop: silence more /var/lib/snapd/desktop/icons
   denials
 - packaging/fedora: disable FIPS compliant crypto for static
   binaries
 - packaging: stop depending on python-docutils

* Tue May 12 2020 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.45
 - o/devicestate: support doing system action reboots from recover
   mode
 - vendor: update to latest secboot
 - tests: not fail when boot dir cannot be determined
 - configcore: only reload journald if systemd is new enough
 - cmd/snap-bootstrap/initramfs-mounts: append uuid to ubuntu-data
   when decrypting
 - tests/lib/prepare.sh: delete patching of the initrd
 - cmd/snap: coldplug auto-import assertions from all removable
   devices
 - cmd/snap: fix the order of positional parameters in help output
 - c/snap-bootstrap: port mount state mocking to the new style on
   master
 - cmd/snap-bootstrap/initramfs-mounts: add sudoers to dirs to copy
   as well
 - o/devicestate,cmd/snap-bootstrap: seal to recover mode cmdline,
   unlock in recover mode initramfs
 - progress: tweak multibyte label unit test data
 - gadget: fix fallback device lookup for 'mbr' type structures
 - progress: fix progress bar with multibyte duration units
 - many: use /run/mnt/data over /run/mnt/ubuntu-data for uc20
 - many: put the sealed keys in a directory on seed for tidiness
 - cmd/snap-bootstrap: measure epoch and model before unlocking
   encrypted data
 - o/configstate: core config handler for persistent journal
 - bootloader/uboot: use secondary ubootenv file boot.sel for uc20
 - packaging: add "$TAGS" to dh_auto_test for debian packaging
 - tests: ensure $cache_dir is actually available
 - secboot,cmd/snap-bootstrap: add model to pcr protection profile
 - devicestate: do not use snap-boostrap in devicestate to install
 - tests: fix a typo in nested.sh helper
 - devicestate: add support for cloud.cfg.d config from the gadget
 - cmd/snap-bootstrap: cleanups, naming tweaks
 - testutil: add NewDBusTestConn
 - snap-bootstrap: lock access to sealed keys
 - overlord/devicestate: preserve the current model inside ubuntu-
   boot
 - interfaces/apparmor: use differently templated policy for non-core
   bases
 - seccomp: add get_tls, io_pg* and *time64/*64 variants for existing
   syscalls
 - cmd/snap-bootstrap/initramfs-mounts: mount ubuntu-seed first,
   other misc changes
 - o/snapstate: tweak "waiting for restart" message
 - boot: store model model and grade information in modeenv
 - interfaces/firewall-control: allow -legacy and -nft for core20
 - boot: enable makeBootable20RunMode for EnvRefExtractedKernel
   bootloaders
 - boot/bootstate20: add EnvRefExtractedKernelBootloader bootstate20
   implementation
 - daemon: fix error message from `snap remove-user foo` on classic
 - overlord: have a variant of Mock that can take a state.State
 - tests: 16.04 and 18.04 now have mediating pulseaudio (again)
 - seed: clearer errors for missing essential snapd or core snap
 - cmd/snap-bootstrap/initramfs-mounts: support
   EnvRefExtractedKernelBootloader's
 - gadget, cmd/snap-bootstrap: MBR schema support
 - image: improve/adjust DownloadSnap doc comment
 - asserts: introduce ModelGrade.Code
 - tests: ignore user-12345 slice and service
 - image,seed/seedwriter: support redirect channel aka default
   tracks
 - bootloader: use binary.Read/Write
 - tests: uc20 nested suite part II
 - tests/boot: refactor to make it easier for new
   bootloaderKernelState20 impl
 - interfaces/openvswitch: support use of ovs-appctl
 - snap-bootstrap: copy auth data from real ubuntu-data in recovery
   mode
 - snap-bootstrap: seal and unseal encryption key using tpm
 - tests: disable special-home-can-run-classic-snaps due to jenkins
   repo issue
 - packaging: fix build on Centos8 to support BUILDTAGS
 - boot/bootstate20: small changes to bootloaderKernelState20
 - cmd/snap: Implement a "snap routine file-access" command
 - spread.yaml: switch back to latest/candidate for lxd snap
 - boot/bootstate20: re-factor kernel methods to use new interface
   for state
 - spread.yaml,tests/many: use global env var for lxd channel
 - boot/bootstate20: fix bug in try-kernel cleanup
 - config: add system.store-certs.[a-zA-Z0-9] support
 - secboot: key sealing also depends on secure boot enabled
 - httputil: fix client timeout retry tests
 - cmd/snap-update-ns: handle EBUSY when unlinking files
 - cmd/snap/debug/boot-vars: add opts for setting dir and/or uc20
   vars
 - secboot: add tpm support helpers
 - tests/lib/assertions/developer1-pi-uc20.model: use 20/edge for
   kernel and gadget
 - cmd/snap-bootstrap: switch to a 64-byte key for unlocking
 - tests: preserve size for centos images on spread.yaml
 - github: partition the github action workflows
 - run-checks: use consistent "Checking ..." style messages
 - bootloader: add efi pkg for reading efi variables
 - data/systemd: do not run snapd.system-shutdown if finalrd is
   available
 - overlord: update tests to work with latest go
 - cmd/snap: do not hide debug boot-vars on core
 - cmd/snap-bootstrap: no error when not input devices are found
 - snap-bootstrap: fix partition numbering in create-partitions
 - httputil/client_test.go: add two TLS version tests
 - tests: ignore user@12345.service hierarchy
 - bootloader, gadget, cmd/snap-bootstrap: misc cosmetic things
 - tests: rewrite timeserver-control test
 - tests: fix racy pulseaudio tests
 - many: fix loading apparmor profiles on Ubuntu 20.04 with ZFS
 - tests: update snap-preseed --reset logic to accommodate for 2.44
   change
 - cmd/snap: don't wait for system key when stopping
 - sandbox/cgroup: avoid making arrays we don't use
 - osutil: mock proc/self/mountinfo properly everywhere
 - selinux: export MockIsEnforcing; systemd: use in tests
 - tests: add 32 bit machine to GH actions
 - tests/session-tool: kill cron session, if any
 - asserts: it should be possible to omit many snap-ids if allowed,
   fix
 - boot: cleanup more things, simplify code
 - github: skip spread jobs when corresponding label is set
 - dirs: don't depend on osutil anymore, mv apparmor vars to apparmor
   pkg
 - tests/session-tool: add session-tool --dump
 - github: allow cached debian downloads to restore
 - tests/session-tool: session ordering is non-deterministic
 - tests: enable unit tests on debian-sid again
 - github: move spread to self-hosted workers
 - secboot: import secboot on ubuntu, provide dummy on !ubuntu
 - overlord/devicestate: support for recover and run modes
 - snap/naming: add validator for snap security tag
 - interfaces: add case for rootWritableOverlay + NFS
 - tests/main/uc20-create-partitions: tweaks, renames, switch to
   20.04
 - github: port CLA check to Github Actions
 - interfaces/many: miscellaneous policy updates xliv
 - configcore,tests: fix setting watchdog options on UC18/20
 - tests/session-tool: collect information about services on startup
 - tests/main/uc20-snap-recovery: unbreak, rename to uc20-create-
   partitions
 - state: add state.CopyState() helper
 - tests/session-tool: stop anacron.service in prepare
 - interfaces: don't use the owner modifier for files shared via
   document portal
 - systemd: move the doc comments to the interface so they are
   visible
 - cmd/snap-recovery-chooser: tweaks
 - interfaces/docker-support: add overlayfs file access
 - packaging: use debian/not-installed to ignore snap-preseed
 - travis.yml: disable unit tests on travis
 - store: start splitting store.go and store_test.go into subtopic
   files
 - tests/session-tool: stop cron/anacron from meddling
 - github: disable fail-fast as spread cannot be interrupted
 - github: move static checks and spread over
 - tests: skip "/etc/machine-id" in "writablepaths" test
 - snap-bootstrap: store encrypted partition recovery key
 - httputil: increase testRetryStrategy max timelimit to 5s
 - tests/session-tool: kill leaking closing session
 - interfaces: allow raw access to USB printers
 - tests/session-tool: reset failed session-tool units
 - httputil: increase httpclient timeout in
   TestRetryRequestTimeoutHandling
 - usersession: extend timerange in TestExitOnIdle
 - client: increase timeout in client tests to 100ms
 - many: disentagle release and snapdenv from sandbox/*
 - boot: simplify modeenv mocking to always write a modeenv
 - snap-bootstrap: expand data partition on install
 - o/configstate: add backlight option for core config
 - cmd/snap-recovery-chooser: add recovery chooser
 - features: enable robust mount ns updates
 - snap: improve TestWaitRecovers test
 - sandbox/cgroup: add ProcessPathInTrackingCgroup
 - interfaces/policy: fix comment in recent new test
 - tests: make session tool way more robust
 - interfaces/seccomp: allow passing an address to setgroups
 - o/configcore: introduce core config handlers (3/N)
 - interfaces: updates to login-session-observe, network-manager and
   modem-manager interfaces
 - interfaces/policy/policy_test.go: add more tests'allow-
   installation: false' and we grant based on interface attributes
 - packaging: detect/disable broken seed in the postinst
 - cmd/snap-confine/mount-support-nvidia.c: add libnvoptix as nvidia
   library
 - tests: remove google-tpm backend from spread.yaml
 - tests: install dependencies with apt using --no-install-recommends
 - usersession/userd: add zoommtg url support
 - snap-bootstrap: fix disk layout sanity check
 - snap: add `snap debug state --is-seeded` helper
 - devicestate: generate warning if seeding fails
 - config, features: move and rename config.GetFeatureFlag helper to
   features.Flag
 - boot, overlord/devicestate, daemon:  implement requesting boot
   into a given recovery system
 - xdgopenproxy: forward requests to the desktop portal
 - many: support immediate reboot
 - store: search v2 tweaks
 - tests: fix cross build tests when installing dependencies
 - daemon: make POST /v2/systems/<label> root only
 - tests/lib/prepare.sh: use only initrd from the kernel snap
 - cmd/snap,seed: validate full seeds (UC 16/18)
 - tests/main/user-session-env: stop the user session before deleting
   the test-zsh user
 - overlord/devicestate, daemon: record the seed current system was
   installed from
 - gadget: SystemDefaults helper function to convert system defaults
   config into a flattened map suitable for FilesystemOnlyApply.
 - many: comment or avoid cryptic snap-ids in tests
 - tests: add LXD_CHANNEL environment
 - store: support for search API v2
 - .github: register a problem matcher to detect spread failures
 - seed: add Info() method for seed.Snap
 - github: always run the "Discard spread workers" step, even if the
   job fails
 - github: offload self-hosted workers
 - cmd/snap: the model command needs just a client, no waitMixin
 - github: combine tests into one workflow
 - github: fix order of go get caches
 - tests: adding more workers for ubuntu 20.04
 - boot,overlord: rename operating mode to system mode
 - config: add new Transaction.GetPristine{,Maybe}() function
 - o/devicestate: rename readMaybe* to maybeRead*
 - github: cache Debian dependencies for unit tests
 - wrappers: respect pre-seeding in error path
 - seed: validate UC20 seed system label
 - client, daemon, overlord/devicestate: request system action API
   and stubs
 - asserts,o/devicestate: support model specified alternative serial-
   authority
 - many: introduce naming.WellKnownSnapID
 - o/configcore: FilesystemOnlyApply method for early configuration
   of core (1/N)
 - github: run C unit tests
 - github: run spread tests on PRs only
 - interfaces/docker-support: make containerd abstract socket more
   generic
 - tests: cleanup security-private-tmp properly
 - overlord/devicestate,boot: do not hold to the originally read
   modeenv
 - dirs: rm RunMnt; boot: add vars for early boot env layout;
   sysconfig: take targetdir arg
 - cmd/snap-bootstrap/initramfs-mounts/tests: use dirs.RunMnt over
   s.runMnt
 - tests: add regression test for MAAS refresh bug
 - errtracker: add missing mocks
 - github: apt-get update before installing build-deps
 - github: don't fail-fast
 - github: run spread via github actions
 - boot,many: add modeenv.WriteTo, make Write take no args
 - wrappers: fix timer schedules that are days only
 - tests/main/snap-seccomp-syscalls: install gperf
 - github: always checkout to snapcore/snapd
 - github: add prototype workflow running unit tests
 - many: improve comments, naming, a possible TODO
 - client: use Assert when checking for error
 - tests: ensure sockets target is ready in session agent spread
   tests
 - osutil: do not leave processes behind after the test run
 - tests: update proxy-no-core to match latest CDN changes
 - devicestate,sysconfig: support "cloud.cfg.d" in uc20 for grade:
   dangerous
 - cmd/snap-failure,tests: try to make snap-failure more robust
 - many: fix packages having mistakenly their copyright as doc
 - many: enumerate system seeds, return them on the /v2/systems API
   endpoint
 - randutil: don't consume kernel entropy at init, just mix more info
   to try to avoid fleet collisions
 - snap-bootstrap: add creationSupported predicate for partition
   types
 - tests: umount partitions which are not umounted after remount
   gadget
 - snap: run gofmt -s
 - many: improve environment handling, fixing duplicate entries
 - boot_test: add many boot robustness tests for UC20 kernel
   MarkBootSuccessul and SetNextBoot
 - overlord: remove unneeded overlord.MockPruneInterval() mocks
 - interfaces/greengrass-support: fix typo
 - overlord,timings,daemon: separate timings from overlord/state
 - tests: enable nested on core20 and test current branch
 - snap-bootstrap: remove created partitions on reinstall
 - boot: apply Go 1.10 formatting
 - apparmor: use rw for uuidd request to default and remove from
   elsewhere
 - packaging: add README.source for debian
 - tests: cleanup various uc20 boot tests from previous PR
 - devicestate: disable cloud-init by default on uc20
 - run-checks: tweak formatting checks
 - packaging,tests: ensure debian-sid builds without vendor/
 - travis.yml: run unit tests with go/master as well* travis.yml: run
   unit tests with go/master as well
 - seed: make Brand() part of the Seed interface
 - cmd/snap-update-ns: ignore EROFS from rmdir/unlink
 - daemon: do a forceful server shutdown if we hit a deadline
 - tests/many: don't use StartLimitInterval anymore, unify snapd-
   failover variants, build snapd snap for UC16 tests
 - snap-seccomp: robustness improvements
 - run-tests: disable -v for go test to avoid spaming the logs
 - snap: whitelist lzo as support compression for snap pack
 - snap: tweak comment in Install() for overlayfs detection
 - many: introduce snapdenv.Preseeding instead of release.PreseedMode
 - client, daemon, overlord/devicestate: structures and stubs for
   systems API
 - o/devicestate: delay the creation of mark-seeded task until
   asserts are loaded
 - data/selinux, tests/main/selinux: cleanup tmpfs operations in the
   policy, updates
 - interfaces/greengrass-support: add new 1.9 access
 - snap: do not hardlink on overlayfs
 - boot,image: ARM kernel extract prepare image
 - interfaces: make gpio robust against not-existing gpios in /sys
 - cmd/snap-preseed: handle --reset flag
 - many: introduce snapdenv to present common snapd env options
 - interfaces/kubernetes-support: allow autobind to journald socket
 - snap-seccomp: allow mprotect() to unblock the tests
 - tests/lib/reset: workaround unicode dot in systemctl output
 - interfaces/udisks2: also allow Introspection on
   /org/freedesktop/UDisks/**
 - snap: introduce Container.RandomAccessFile
 - o/ifacestate, api: implementation of snap disconnect --forget
 - cmd/snap: make the portal-info command search for the network-
   status interface
 - interfaces: work around apparmor_parser slowness affecting uio
 - tests: fix/improve failing spread tests
 - many: clean separation of bootenv mocking vs mock bootloader kinds
 - tests: mock prune ticker in overlord tests to reduce wait times
 - travis: disable arm64 again
 - httputil: add support for extra snapd certs
 - travis.yml: run unit tests on arm64 as well
 - many: fix a pair of ineffectual assignments
 - tests: add uc20 kernel snap upgrade managers test, fix
   bootloadertest bugs
 - o/snapstate: set base in SnapSetup on snap revert
 - interfaces/{docker,kubernetes}-support: updates for lastest k8s
 - cmd/snap-exec: add test case for LP bug 1860369
 - interfaces: make the network-status interface implicit on
   classic
 - interfaces: power control interfaceIt is documented in the
   kernel
 - interfaces: miscellaneous policy updates
 - cmd/snap: add a "snap routine portal-info" command
 - usersession/userd: add "apt" to the white list of URL schemes
   handled by xdg-open
 - interfaces/desktop: allow access to system prompter interface
 - devicestate: allow encryption regardless of grade
 - tests: run ipv6 network-retry test too
 - tests: test that after "remove-user" the system is unmanaged
 - snap-confine: unconditionally add /dev/net/tun to the device
   cgroup
 - snapcraft.yaml: use sudo -E and remove workaround
 - interfaces/audio_playback: Fix pulseaudio config access
 - ovelord/snapstate: update only system wide fonts cache
 - wrappers: import /etc/environment in all services
 - interfaces/u2f: Add Titan USB-C key
 - overlord, taskrunner: exit on task/ensure error when preseeding
 - tests: add session-tool, a su / sudo replacement
 - wrappers: add mount unit dependency for snapd services on core
   devices
 - tests: just remove user when the system is not managed on create-
   user-2 test
 - snap-preseed: support for preseeding of snapd and core18
 - boot: misc UC20 changes
 - tests: adding arch-linux execution
 - packaging: revert "work around review-tools and snap-confine"
 - netlink: fix panic on arm64 with the new rawsockstop codewith a
   nil Timeval panics
 - spread, data/selinux: add CentOS 8, update policy
 - tests: updating checks to new test account for snapd-test snaps
 - spread.yaml: mv opensuse 15.1 to unstable
 - cmd/snap-bootstrap,seed: verify only in-play snaps
 - tests: use ipv4 in retry-network to unblock failing master
 - data/systemd: improve the description
 - client: add "Resume" to DownloadOptions and new test
 - tests: enable snapd-failover on uc20
 - tests: add more debug output to the snapd-failure handling
 - o/devicestate: unset recovery_system when done seeding

* Fri Apr 10 2020 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.44.3
 - tests: fix racy pulseaudio tests
 - many: fix loading apparmor profiles on Ubuntu 20.04 with ZFS
 - tests: update snap-preseed --reset logic
 - tests: backport partition fixes
 - cmd/snap: don't wait for system key when stopping
 - interfaces/many: miscellaneous policy updates xliv
 - tests/main/uc20-snap-recovery: use 20.04 system
 - tests: skip "/etc/machine-id" in "writablepaths
 - interfaces/docker-support: add overlays file access

* Thu Apr 2 2020 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.44.2
 - packaging: detect/disable broken seeds in the postinst
 - cmd/snap,seed: validate full seeds (UC 16/18)
 - snap: add `snap debug state --is-seeded` helper
 - devicestate: generate warning if seeding fails
 - store: support for search API v2
 - cmd/snap-seccomp/syscalls: update the list of known syscalls
 - snap/cmd: the model command needs just a client, no waitMixin
 - tests: cleanup security-private-tmp properly
 - wrappers: fix timer schedules that are days only
 - tests: update proxy-no-core to match latest CDN changes
 - cmd/snap-failure,tests: make snap-failure more robust
 - tests, many: don't use StartLimitInterval anymore, unify snapd-
   failover variants, build snapd snap for UC16 tests

* Sat Mar 21 2020 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.44.1
 - randutil: switch back to setting up seed with lower entropy data
 - interfaces/greengrass-support: fix typo
 - packaging,tests: ensure debian-sid builds without vendor/
 - travis.yml: run unit tests with go/master as well
 - cmd/snap-update-ns: ignore EROFS from rmdir/unlink

* Tue Mar 17 2020 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.44
 - daemon: do a forceful serer shutdown if we hit a deadline
 - snap: whitelist lzo as support compression for snap pack
 - data/selinux: update policy to allow more ops
 - interfaces/greengrass-support: add new 1.9 access
 - snap: do not hardlink on overlayfs
 - cmd/snap-preseed: handle --reset flag
 - interfaces/kubernetes-support: allow autobind to journald socket
 - snap-seccomp: allow mprotect() to unblock the tests
 - tests/lib/reset: workaround unicode dot in systemctl output
 - interfaces: work around apparmor_parser slowness affecting uio
 - interfaces/udisks2: also allow Introspection on
   /org/freedesktop/UDisks2/**
 - tests: mock prune ticker in overlord tests to reduce wait times
 - interfaces/{docker,kubernetes}-support: updates for lastest k8s
 - interfaces: miscellaneous policy updates
 - interfaces/audio_playback: Fix pulseaudio config access
 - overlord: disable Test..AbortShortlyAfterStartOfOperation for 2.44
 - ovelord/snapstate: update only system wide fonts cache
 - wrappers: import /etc/environment in all services
 - interfaces/u2f: Add Titan USB-C key
 - overlord, taskrunner: exit on task/ensure error when preseeding
 - overlord/snapstate/backend: update snapd services contents in unit
   tests
 - wrappers: add mount unit dependency for snapd services on core
   devices
 - Revert "tests: remove /tmp/snap.* left over by other tests"
 - Revert "packaging: work around review-tools and snap-confine"
 - netlink: fix panic on arm64 with the new rawsockstop code
 - spread, data/selinux: add CentOS 8, update policy
 - spread.yaml: mv opensuse tumbleweed to unstable too
 - spread.yaml: mv opensuse 15.1 to unstable
 - tests: use ipv4 in retry-network to unblock failing master
 - data/systemd: improve the description
 - tests/lib/prepare.sh: simplify, combine code paths
 - tests/main/user-session-env: add test verifying environment
   variables inside the user session
 - spread.yaml: make qemu ubuntu-core-20-64 use ubuntu-20.04-64
 - run-checks: SKIP_GMFMT really skips formatting checks
 - tests: enable more tests for UC20/UC18
 - tests: remove tmp dir for snap not-test-snapd-sh on security-
   private-tmp test
 - seed,cmd/snap-bootstrap: introduce seed.Snap.EssentialType,
   simplify bootstrap code
 - snapstate: do not restart in undoLinkSnap unless on first install
 - cmd/snap-bootstrap: subcommand to detect UC chooser trigger
 - cmd/snap-bootstrap/initramfs-mounts: mount the snapd snap in run-
   mode too
 - cmd/libsnap, tests: fix C unit tests failing as non-root
 - cmd/snap-bootstrap: verify kernel snap is in modeenv before
   mounting it
 - tests: adding amazon linux to google backend
 - cmd/snap-failure/snapd: rm snapd.socket, reset snapd.socket failed
   status
 - client: add support for "ResumeToken", "HeaderPeek" to download
 - build: enable type: snapd
 - tests: rm -rf /tmp/snap.* in restore
 - cmd/snap-confine: deny snap-confine to load nss libs
 - snapcraft.yaml: add comments, rename snapd part to snapd-deb
 - boot: write current_kernels in bootstate20, makebootable
 - packaging: work around review-tools and snap-confine
 - tests: skipping interfaces-openvswitch on centos due to package is
   not available
 - packaging,snap-confine: stop being setgid root
 - cmd/snap-confine: bring /var/lib/dhcp from host, if present
 - store: rely on CommandFromSystemSnap to find xdelta3
 - tests: bump sleep time of the new overlord tests
 - cmd/snap-preseed: snapd version check for the target
 - netlink: fix/support stopping goroutines reading netlink raw
   sockets
 - tests: reset PS1 before possibly interactive dash
 - overlord, state: don't abort changes if spawn time before
   StartOfOperationTime (2/2)
 - snapcraft.yaml: add python3-apt, tzdata as build-deps for the
   snapd snap
 - tests: ask tar to speak English
 - tests: using google storage when downloading ubuntu cloud images
   from gce
 - Coverity produces false positives for code like this:
 - many: maybe restart & security backend options
 - o/standby: add SNAPD_STANDBY_WAIT to control standby in
   development
 - snap: use the actual staging snap-id for snapd
 - cmd/snap-bootstrap: create a new parser instance
 - snapcraft.yaml: use build-base and adopt-info, rm builddeb
   plugin
 - tests: set StartLimitInterval in snapd failover test
 - tests: disable archlinux system
 - tests: add preseed test for classic
 - many, tests: integrate all preseed bits and add spread tests
 - daemon: support resuming downloads
 - tests: use Filename() instead of filepath.Base(sn.MountFile())
 - tests/core: add swapfiles test
 - interfaces/cpu-control: allow to control cpufreq tunables
 - interfaces: use commonInteface for desktopInterface
 - interfaces/{desktop-legacy,unity7}: adjust for new ibus socket
   location
 - snap/info: add Filename
 - bootloader: make uboot a RecoveryAwareBootloader
 - gadget: skip update when mounted filesystem content is identical
 - systemd: improve is-active check for 'failed' services
 - boot: add current_kernels to modeenv
 - o/devicestate: StartOfOperationTime helper for Prune (1/2)
 - tests: detect LXD launching i386 containers
 - tests: move main/ubuntu-core-* tests to core/ suite
 - tests: remove snapd in ubuntu-core-snapd
 - boot: enable base snap updates in bootstate20
 - tests: Fix core revert channel after 2.43 has been released to
   stable
 - data/selinux: unify tabs/spaces
 - o/ifacestate: move ResolveDisconnect to ifacestate
 - spread: move centos to stable systems
 - interfaces/opengl: allow datagrams to nvidia-driver
 - httputil: add NoNetwork(err) helper, spread test and use in serial
   acquire
 - store: detect if server does not support http range headers
 - test/lib/user: add helper lib for doing things for and as a user
 - overlord/snapstate, wrappers: undo of snapd on core
 - tests/main/interfaces-pulseaudio: use custom pulseaudio script,
   set kill timeout
 - store: add support for resume in DownloadStream
 - cmd/snap: implement 'snap remove-user'
 - overlord/devicestate: fix preseed unit tests on systems not using
   /snap
 - tests/main/static: ldd in glibc 2.31 logs to stderr now
 - run-checks, travis: allow skipping spread jobs by adding a label
 - tests: add new backend which includes images with tpm support
 - boot: use constants for boot status values
 - tests: add "core" suite for UC specific tests
 - tests/lib/prepare: use a local copy of uc20 initramfs skeleton
 - tests: retry mounting the udisk2 device due to timing issue
 - usersession/client: add a client library for the user session
   agent
 - o/devicestate: Handle preseed mode in the firstboot mode (core16
   only for now).
 - boot: add TryBase and BaseStatus to modeenv; use in snap-bootstrap
 - cmd/snap-confine: detect base transitions on core16
 - boot: don't use "kernel" from the modeenv anymore
 - interfaces: add uio interface
 - tests: repack the initramfs + kernel snap for UC20 spread tests
 - interfaces/greengrass-support: add /dev/null ->
   /proc/latency_stats mount
 - httputil: remove workaround for redirect handling in go1.7
 - httputil: remove go1.6 transport workaround
 - snap: add `snap pack --compression=<comp>` options
 - tests/lib/prepare: fix hardcoded loopback device names for UC
   images
 - timeutil: add a unit test case for trivial schedule
 - randutil,o/snapstate,-mkauthors.sh: follow ups to randutil
   introduction
 - dirs: variable with distros using alternate snap mount
 - many,randutil: centralize and streamline our random value
   generation
 - tests/lib/prepare-restore: Revert "Continue on errors updating or
   installing dependencies"
 - daemon: Allow clients to call /v2/logout via Polkit
 - dirs: manjaro-arm is like manjaro
 - data, packaging: Add sudoers snippet to allow snaps to be run with
   sudo
 - daemon, store: better expose single action errors
 - tests: switch mount-ns test to differential data set
 - snapstate: refactor things to add the re-refresh task last
 - daemon: drop support for the DELETE method
 - client: move to /v2/users; implement RemoveUser
 - boot: enable UC20 kernel extraction and bootState20 handling
 - interfaces/policy: enforce plug-names/slot-names constraints
 - asserts: parse plug-names/slot-names constraints
 - daemon: make users result more consistent
 - cmd/snap-confine,tests: support x.y.z nvidia version
 - dirs: fixlet for XdgRuntimeDirGlob
 - boot: add bootloader options to coreKernel
 - o/auth,daemon: do not remove unknown user
 - tests: tweak and enable tests on ubuntu 20.04
 - daemon: implement user removal
 - cmd/snap-confine: allow snap-confine to link to libpcre2
 - interfaces/builtin: Allow NotificationReplied signal on
   org.freedesktop.Notifications
 - overlord/auth: add RemoveUserByName
 - client: move user-related things to their own files
 - boot: tweak kernel cmdline helper docstring
 - osutil: implement deluser
 - gadget: skip update when raw structure content is unchanged
 - boot, cmd/snap, cmd/snap-bootstrap: move run mode and system label
   detection to boot
 - tests: fix revisions leaking from snapd-refresh test
 - daemon: refactor create-user to a user action & hide behind a flag
 - osutil/tests: check there are no leftover symlinks with
   AtomicSymlink
 - grub: support atomically renaming kernel symlinks
 - osutil: add helpers for creating symlinks and renaming in an
   atomic manner
 - tests: add marker tag for core 20 test failure
 - tests: fix gadget-update-pc test leaking snaps
 - tests: remove revision leaking from ubuntu-core-refresh
 - tests: remove revision leaking from remodel-kernel
 - tests: disable system-usernames test on core20
 - travis, tests, run-checks: skip nakedret
 - tests: run `uc20-snap-recovery-encrypt` test on 20.04-64 as well
 - tests: update mount-ns test tables
 - snap: disable auto-import in uc20 install-mode
 - tests: add a command-chain service test
 - tests: use test-snapd-upower instead of upower
 - data/selinux: workaround incorrect fonts cache labeling on RHEL7
 - spread.yaml: fix ubuntu 19.10 and 20.04 names
 - debian: check embedded keys for snap-{bootstrap,preseed} too
 - interfaces/apparmor: fix doc-comments, unnecessary code
 - o/ifacestate,o/devicestatate: merge gadget-connect logic into
   auto-connect
 - bootloader: add ExtractedRunKernelImageBootloader interface,
   implement in grub
 - tests: add spread test for hook permissions
 - cmd/snap-bootstrap: check device size before boostrapping and
   produce a meaningful error
 - cmd/snap: add ability to register "snap routine" commands
 - tests: add a test demonstrating that snaps can't access the
   session agent socket
 - api: don't return connections referring to non-existing
   plugs/slots
 - interfaces: refactor path() from raw-volume into utils with
   comments for old
 - gitignore: ignore snap files
 - tests: skip interfaces-network-manager on arm devices
 - o/devicestate: do not create perfTimings if not needed inside
   ensureSeed/Operational
 - tests: add ubuntu 20.04 to the tests execution and remove
   tumbleweed from unstable
 - usersession: add systemd user instance service control to user
   session agent
 - cmd/snap: print full channel in 'snap list', 'snap info'
 - tests: remove execution of ubuntu 19.04 from google backend
 - cmd/snap-boostrap: add mocking for fakeroot
 - tests/core18/snapd-failover: collect more debug info
 - many: run black formatter on all python files
 - overlord: increase settle timeout for slow machines
 - httputil: use shorter timeout in TestRetryRequestTimeoutHandling
 - store, o/snapstate: send default-tracks header, use
   RedirectChannel
 - overlord/standby: fix possible deadlock in standby test
 - cmd/snap-discard-ns: fix pattern for .info files
 - boot: add HasModeenv to Device
 - devicestate: do not allow remodel between core20 models
 - bootloader,snap: misc tweaks
 - store, overlord/snapstate, etc: SnapAction now returns a []…Result
 - snap-bootstrap: create encrypted partition
 - snap: remove "host" output from `snap version`
 - tests: use snap remove --purge flag in most of the spread tests
 - data/selinux, test/main/selinux-clean: update the test to cover
   more scenarios
 - many: drop NameAndRevision, use snap.PlaceInfo instead
 - boot: split MakeBootable tests into their own file
 - travis-ci: add go import path
 - boot: split MakeBootable implementations into their own file
 - tests: enable a lot of the tests of main on uc20
 - packaging, tests: stop services in prerm
 - tests: enable regression suite on core20
 - overlord/snapstate: improve snapd snap backend link unit tests
 - boot: implement SetNextBoot in terms of bootState.setNext
 - wrappers: write and undo snapd services on core
 - boot,o/devicestate: refactor MarkBootSuccessful over bootState
 - snap-bootstrap: mount the correct snapd snap to /run/mnt/snapd
 - snap-bootstrap: refactor partition creation
 - tests: use new snapd.spread-tests-run-mode-tweaks.service unit
 - tests: add core20 tests
 - boot,o/snapstate: SetNextBoot/LinkSnap return whether to reboot,
   use the information
 - tests/main/snap-sign: add test for non-stdin signing
 - snap-bootstrap: trigger udev after filesystem creation
 - boot,overlord: introduce internal abstraction bootState and use it
   for InUse/GetCurrentBoot
 - overlord/snapstate: tracks are now sticky
 - cmd: sign: add filename param
 - tests: remove "test-snapd-tools" in smoke/sandbox on restore
 - cmd/snap, daemon: stop over-normalising channels
 - tests: fix classic-ubuntu-core-transition-two-cores after refactor
   of MATCH -v
 - packaging: ship var/lib/snapd/desktop/applications in the pkg
 - spread: drop copr repo with F30 build dependencies
 - tests: use test-snapd-sh snap instead of test-snapd-tools - Part 3
 - tests: fix partition creation test
 - tests: unify/rename services-related spread tests to start with
   services- prefix
 - test: extract code that modifies "writable" for test prep
 - systemd: handle preseed mode
 - snap-bootstrap: read only stdout when parsing the sfdisk json
 - interfaces/browser-support: add more product/vendor paths
 - boot: write compat UC16 bootvars in makeBootable20RunMode
 - devicestate: avoid adding mockModel to deviceMgrInstallModeSuite
 - devicestate: request reboot after successful doSetupRunSystem()
 - snapd.core-fixup.sh: do not run on UC20 at all
 - tests: unmount automounted snap-bootstrap devices
 - devicestate: run boot.MakeBootable in doSetupRunSystem
 - boot: copy kernel/base to data partition in makeBootable20RunMode
 - tests: also check nested lxd container
 - run-checks: complain about MATCH -v
 - boot: always return the trivial boot participant in ephemeral mode
 - o/devicestate,o/snapstate: move the gadget.yaml checkdrive-by: use
   gadget.ReadInfoFromSnapFile in checkGadgetRemodelCompatible
 - snap-bootstrap: append new partitions
 - snap-bootstrap: mount filesystems after creation
 - snapstate: do not try to detect rollback in ephemeral modes
 - snap-bootstrap: trigger udev for new partitions
 - cmd/snap-bootstrap: xxx todos about kernel cross-checks
 - tests: avoid mask rsyslog service in case is not enabled on the
   system
 - tests: fix use of MATCH -v
 - cmd/snap-preseed: update help strings
 - cmd/snap-bootstrap: actually parse snapd_recovery_system label
 - bootstrap: reduce runmode mounts from 5 to 2 steps.
 - lkenv.go: adjust for new location of include file
 - snap: improve squashfs.ReadFile() error
 - systemd: fix uc20 shutdown
 - boot: write modeenv when creating the run mode
 - boot,image: add skeleton boot.makeBootable20RunMode
 - cmd/snap-preseed: add snap-preseed executable
 - overlord,boot: follow ups to #7889 and #7899
 - interfaces/wayland: Add access to Xwayland's shm files
 - o/hookstate/ctlcmd: fix command name in snapctl -h
 - daemon,snap: remove screenshot deprecation notice
 - overlord,o/snapstate: make sure we never leave config behind
 - many: pass consistently boot.Device state to boot methods
 - run-checks: check multiline string blocks in
   restore/prepare/execute sections of spread tests
 - intrefaces: login-session-control - added missing dbus commands
 - tests/main/parallel-install-remove-after: parallel installs should
   not break removal
 - overlord/snapstate: tweak assumes error hint
 - overlord: replace DeviceContext.OldModel with GroundContext
 - devicestate: use httputil.ShouldRetryError() in
   prepareSerialRequest
 - tests: replace "test-snapd-base-bare" with real "bare" base snap
 - many: pass a Model to the gadget info reading functions
 - snapstate: relax gadget constraints in ConfigDefaults Et al.
 - devicestate: only run ensureBootOk() in "run" mode
 - tests/many: quiet lxc launching, file pushing
 - tests: disable apt-hooks test until it can be properly fixed
 - tests: 16.04 and 18.04 now have mediating pulseaudio

* Wed Feb 12 2020 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.43.3
 - interfaces/opengl: allow datagrams to nvidia-driver
 - httputil: add NoNetwork(err) helper, spread test and use
   in serial acquire
 - interfaces: add uio interface
 - interfaces/greengrass-support: 'aws-iot-greengrass' snap fails to
   start due to apparmor deny on mounting of "/proc/latency_stats".
 - data, packaging: Add sudoers snippet to allow snaps to be run with
   sudo

* Tue Jan 28 2020 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.43.2
 - cmd/snap-confine: Revert #7421 (unmount /writable from snap view)
 - overlord/snapstate: fix for re-refresh bug
 - tests, run-checks, many: fix nakedret issues
 - data/selinux: workaround incorrect fonts cache labeling on RHEL7
 - tests: use test-snapd-upower instead of upower
 - overlord: increase overall settle timeout for slow arm boards

* Tue Jan 14 2020 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.43.1
 - devicestate: use httputil.ShouldRetryError() in prepareSerialRequest
 - overlord/standby: fix possible deadlock in standby test
 - cmd/snap-discard-ns: fix pattern for .info files
 - overlord,o/snapstate: make sure we never leave config behind
 - data/selinux: update policy to cover more cases
 - snap: remove "host" output from `snap version`

* Thu Jan 09 2020 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.43
 - snap: default to "--direct" in `snap known`
 - packaging: ship var/lib/snapd/desktop/applications in the
   pkg
 - tests: cherry-pick fixes for  snap-set-core-config/ubuntu-core-
   config-defaults-once
 - tests: use test-snapd-sh snap instead of test-snapd-tools
 - tests: rename "test-snapd-sh" in smoke test to test-snapd-sandbox
 - tests: fix partition creation test
 - packaging: fix incorrect changelog entry
 - Revert "tests: 16.04 and 18.04 now have mediating pulseaudio"
 - tests: 16.04 and 18.04 now have mediating pulseaudio
 - interfaces: include hooks in plug/slot apparmor label
 - interfaces: add raw-volume interface for access to partitions
 - image: set recovery system label when creating the image
 - cmd/snapd-generator: fix unit name for non /snap mount locations
 - boot,bootloader: setup the snap recovery system bootenv
 - seed: support ModeSnaps(mode) for mode != "run"
 - seed: fix seed location of local but asserted snaps
 - doc: HACKING.md change autopkgtest-trusty-amd64.img name
 - interfaces/seccomp: parallelize seccomp backend setup
 - cmd/snap-bootstrap: mount ubuntu-data tmpfs, in one go with kernel
   & base
 - interfaces: add audio-playback/record and pulseaudio spread tests
 - apparmor: allow 'r'
   /sys/kernel/mm/transparent_hugepage/hpage_pmd_size
 - cmd/snap-mgmt, packaging/postrm: stop and remove socket units when
   purging
 - tests: use test-snapd-sh snap instead of test-snapd-tools
 - snap-confine: raise egid before calling setup_private_mount()
 - tests: fix fwupd version regular expression
 - snap-bootstrap: parse seed if either kernel or base are not
   mounted
 - tests: check for SELinux denials in interfaces-kvm spread test
 - tests: run snap-set-core-config on all core devices
 - selinux: update policy to allow modifications related to kmod
   backend
 - o/hookstate/ctlcmd: snapctl is-connected command
 - devicestate: add missing test for failing task setup-run-system
 - gadget: add missing test for duplicate detection of roles
 - tests/cmd/snapctl: unset SNAP_CONTEXT for the suite
 - snap/pack, cmd_pack: 'snap pack --check-skeleton' checks
   interfaces
 - gitignore: ignore visual studio code directory
 - snap-bootstrap: implement "run" mode in snap-bootstrap initramfs-
   mounts
 - interfaces/apparmor: handle pre-seeding mode
 - devicestate: implement creating partitions in "install" mode
 - seed: support extra snaps on top of Core 20 dangerous models
 - tests: cache snaps also for ubuntu core and add new snaps to cache
 - snap-bootstrap: support auto-detect device in create-partitions
 - tests: fix partitioning test debug message
 - tests: prevent partitioning test errors
 - cmd/snap-bootstrap: stub out snap.SanitizePlugsSlots for real
 - gadget: extract and export new DiskFromPartition() helper
 - snap-bootstrap: force partition table operations
 - HACKING.md: add nvidia options to configure example
 - tests: move the watchdog timeout to 2s to make the tests work in
   rpi
 - tests: demand silence from check_journalctl_log
 - tests: fix the channels checks done on nested tests
 - tests: reduce the complexity of the test-snapd-sh snap
 - snap/squashfs, osutil: verify files/dirs can be accessed by
   mksquashfs when building a snap
 - boot: add boot.Modeenv.Kernel support
 - devicestate: ensure system installation
 - tests: apply change on permissions to serial port on hotplug test
 - cmd/snap-update-ns: adjust debugging output for usability
 - devicestate: add reading of modeenv to uc20 firstboot code
 - tests/lib/prepare: drop workarounds for rpmbuild rewriting /bin/sh
 - cmd/snap-bootstrap: write /var/lib/snapd/modeenv to the right
   place
 - boot: add boot.Modeenv.Base support
 - overlord/snapstate: install task edges
 - cmd/snap-bootstrap: some small naming and code org tweaks
 - snap-bootstrap: remove SNAPPY_TESTING check, we use it for real
   now
 - interfaces: remove leftover reservedForOS
 - snap-bootstrap: write /run/mnt/ubuntu-data/var/lib/snapd/modeenv
 - osutil/mount: optimize flagOptSearch some more
 - devicestate: read modeenv early and store in devicestate
 - interfaces: add login-session-observe for who, {fail,last}log and
   loginctl
 - tests: add Ubuntu Eoan to google-sru backend
 - osutil/mount: de-duplicate code to use a list
 - interfaces: remove reservedForOS from commonInterface
 - interfaces/browser-support: allow reading status of huge pages
 - interfaces: update system-backup tests to not check for sanitize
   errors related to os
 - interfaces: add system-backup interface
 - osutil/mount: add {Unm,M}outFlagsToOpts helpers
 - snap-bootstrap: make cmdline parsing robust
 - overlord/patch: normalize tracking channel in state
 - boot: add boot.Modeenv that can read/write the UC20 modeenv files
 - bootloader: add new bootloader.InstallBootConfig()
 - many: share single implementation to list needed default-providers
 - snap-bootstrap: implement "snap-bootstrap initramfs-mounts"
 - seccomp: allow chown 'snap_daemon:root' and 'root:snap_daemon'
 - osutil: handle "rw" mount flag in ParseMountEntry
 - overlord/ifacestate: report bad plug/slots with warnings on snap
   install
 - po: sync translations from launchpad
 - tests: cleanup most test snaps icons, they were anyway in the
   wrong place
 - seed: fix confusing pre snapd dates in tests
 - many: make ValidateBasesAndProviders signature simpler/canonical
 - snap-bootstrap: set expected filesystem labels
 - testutil, many: make MockCommand() create prefix of absolute paths
 - tests: improve TestDoPrereqRetryWhenBaseInFlight to fix occasional
   flakiness.
 - seed: proper support for optional snaps for Core 20 models
 - many: test various kinds of overriding for the snapd snap in Core
   20
 - cmd/snap-failure: passthrough snapd logs, add informational
   logging
 - cmd/snap-failure: fallback to snapd from core, extend tests
 - configcore: fix missing error propagation
 - devicestate: rename ensureSeedYaml -> ensureSeeded
 - tests: adding fedora 31
 - tests: restart the snapd service in the snapd-failover test
 - seed: Core 20 seeds channel overrides support for grade dangerous
 - cmd: fix the get command help message
 - tests: enable degraded test on arch linux after latest image
   updates
 - overlord/snapstate: don't re-enable and start disabled services on
   refresh, etc.
 - seed: support in Core 20 seeds local unasserted snaps for model
   snaps
 - snap-bootstrap: add go-flags cmdline parsing and tests
 - gadget: skip fakeroot if not needed
 - overlord/state: panic in MarkEdge() if task is nil
 - spread: fix typo in spread suite
 - overlord: mock device serial in gadget remodel unit tests
 - tests: fix spread shellcheck and degraded tests to unbreak master
 - spread, tests: openSUSE Tumbleweed to unstable systems, update
   system-usernames on Amazon Linux 2
 - snap: extract printInstallHint in cmd_download.go
 - cmd: fix a pair of typos
 - release: preseed mode flag
 - cmd/snap-confine: tracking processes with classic confinement
 - overlord/ifacestate: remove automatic connections if plug/slot
   missing
 - o/ifacestate,interfaces,interfaces/policy: slots-per-plug: *
 - tests/lib/state: snapshot and restore /var/snap during the tests
 - overlord: add base->base remodel undo tests and fixes
 - seed: test and improve Core 20 seed handling errors
 - asserts: add "snapd" type to valid types in the model assertion
 - snap-bootstrap: check gadget versus disk partitions
 - devicestate: add support for gadget->gadget remodel
 - snap/snapenv: preserve XDG_RUNTIME_DIR for classic confinement
 - daemon: parse and reject invalid channels in snap ops
 - overlord: add kernel remodel undo tests and fix undo
 - cmd/snap: support (but warn) using deprecated multi-slash channel
 - overlord: refactor mgrsSuite and extract kernelSuite
 - tests/docker-smoke: add minimal docker smoke test
 - interfaces: extend the fwupd slot to be implicit on classic
 - cmd/snap: make 'snap list' shorten latest/$RISK to $RISK
 - tests: fix for journalctl which is failing to restart
 - cmd/snap,image: initial support for Core 20 in prepare-image with
   test
 - cmd/snap-confine: add support for parallel instances of classic
   snaps, global mount ns initialization
 - overlord: add kernel rollback across reboots manager test and
   fixes
 - o/devicestate: the basics of Core 20 firstboot support with test
 - asserts: support and parsing for slots-per-plug/plugs-per-slotSee
   https://forum.snapcraft.io/t/plug-slot-declaration-rules-greedy-
   plugs/12438
 - parts/plugins: don't xz-compress a deb we're going to discard
 - cmd/snap: make completion skip hidden commands (unless overridden)
 - many: load/consume Core 20 seeds (aka recovery systems)
 - tests: add netplan test on ubuntu core
 - seed/internal: doc comment fix and drop handled TODOs
 - o/ifacestate: unify code into
   autoConnectChecker.addAutoConnectionsneed to change to support
   slots-per-plugs: *
 - many: changes to testing in preparation of Core 20 seed consuming
   code
 - snapstate,devicestate: make OldModel() available in DeviceContext
 - tests: opensuse tumbleweed has similar issue than arch linux with
   snap --strace
 - client,daemon: pass sha3-384 in /v2/download to the client
 - builtin/browser_support.go: allow monitoring process memory
   utilization (used by chromium)
 - overlord/ifacestate: use SetupMany in setupSecurityByBackend
 - tests: add 14.04 canonical-livepatch test
 - snap: make `snap known --remote` use snapd if available
 - seed: share auxInfo20 and makeSystemSnap via internal
 - spread: disable secondary compression for deltas
 - interfaces/content: workaround for renamed target
 - tests/lib/gendevmodel: helper tool for generating developer model
   assertions
 - tests: tweak wording in mount-ns test
 - tests: don't depend on GNU time
 - o/snapstate, etc: SnapState.Channel -> TrackingChannel, and a
   setter
 - seed/seedwriter: support writing Core 20 seeds (aka recovery
   systems)
 - snap-recovery: rename to "snap-bootstrap"
 - managers: add remodel undo test for new required snaps case
 - client: add xerrors and wrap errors coming from "client"
 - tests: verify host is not affected by mount-ns tests
 - tests: configure the journald service for core systems
 - cmd/snap, store: include snapcraft.io page URL in snap info output
 - cmd/cmdutil: version helper
 - spread: enable bboozzoo/snapd-devel-deps COPR repo for getting
   golang-x-xerrors
 - interfaces: simplify AddUpdateNS and emit
 - interfaces/policy: expand cstrs/cstrs1 to
   altConstraints/constraints
 - overlord/devicestate: check snap handler for gadget remodel
   compatibility
 - snap-recovery: deploy gadget content when creating partitions
 - gadget: skip structures with MBR role during remodel
 - tests: do not use lsblk in uc20-snap-recovery test
 - overlord/snapstate: add LastActiveDisabledServices,
   missingDisabledServices
 - overlord/devicestate: refactor and split into per-functionality
   files, drop dead code
 - tests: update mount-ns after addition of /etc/systemd/user
 - interfaces/pulseaudio: adjust to manually connect by default
 - interfaces/u2f-devices: add OnlyKey to devices list
 - interfaces: emit update-ns snippets to function
 - interfaces/net-setup-{observe,control}: add Info D-Bus method
   accesses
 - tests: moving ubuntu-19.10-64 from google-unstable to google
   backend
 - gadget: rename existing and add new helpers for checking
   filesystem/partition presence
 - gadget, overlord/devicestate: add support for customized update
   policy, add remodel policy
 - snap-recovery: create filesystems as defined in the gadget
 - tests: ignore directories for go modules
 - policy: implement CanRemove policy for the snapd type
 - overlord/snapstate: skip catalog refresh if unseeded
 - strutil: add OrderedSet
 - snap-recovery: add minimal binary so that we can use spread on it
 - gadget, snap/pack: perform extended validation of gadget metadata
   and contents
 - timeutil: fix schedules with ambiguous nth weekday spans
 - interfaces/many: allow k8s/systemd-run to mount volume subPaths
   plus cleanups
 - client: add KnownOptions to Know() and support remote assertions
 - tests: check the apparmor_parser when the file exists on snap-
   confine test
 - gadget: helper for volume compatibility checks
 - tests: update snap logs to match for multiple lines for "running"
 - overlord: add checks for bootvars in
   TestRemodelSwitchToDifferentKernel
 - snap-install: add ext4,vfat creation support
 - snap-recovery: remove "usedPartitions" from sfdisk.Create()
 - image,seed: hide Seed16/Snap16, use seed.Open in image_test.go
 - cmd/snap: Sort tasks in snap debug timings output by lanes and
   ready-time.
 - snap-confine.apparmor.in: harden pivot_root until we have full
   mediation
 - gadget: refactor ensureVolumeConsistency
 - gadget: add a public helper for parsing gadget metadata
 - many: address issues related to explicit/implicit channels for
   image building
 - overlord/many: switch order of check snap parameters
 - cmd/snap-confine: remove leftover condition from capability world
 - overlord: set fake serial in TestRemodelSwitchToDifferentKernel
 - overlord/many: extend check snap callback to take snap container
 - recovery-tool: add sfdisk wrapper
 - tests: launch the lxd images following the pattern
   ubuntu:${VERSION_ID}
 - sandbox/cgroup: move freeze/thaw code
 - gadget: accept system-seed role and ubuntu-data label
 - test/lib/names.sh: make backslash escaping explicit
 - spread: generate delta when using google backend
 - cmd/snap-confine: remove loads of dead code
 - boot,dirs,image: various refinements in the prepare-image code
   switched to seedwriter
 - spread: include mounts list in task debug output
 - .gitignore: pair of trivial changes
 - image,seed/seedwriter: switch image to use seedwriter.Writer
 - asserts: introduce explicit support for grade for Core 20 models
 - usersession: drive by fixes for things flagged by unused or
   gosimple
 - spread.yaml: exclude vendor dir
 - sandbox/cgroup, overlord/snapstate: move helper for listing pids
   in group to the cgroup package
 - sandbox/cgroup: refactor process cgroup helper to support v2 and
   named hierarchies
 - snap-repair: error if run as non-root
 - snap: when running `snap repair` without arguments, show hint
 - interfaces: add cgroup-version to system-key
 - snap-repair: add missing check in TestRepairBasicRun
 - tests: use `snap model` instead of `snap known model` in tests
 - daemon: make /v2/download take snapRevisionOptions
 - snap-repair: add additional comment about trust in runner.Verify()
 - client: add support to use the new "download" API
 - interfaces: bump system-key version (and keep on bumping)
 - interfaces/mount: account for cgroup version when reporting
   supported features
 - tests: change regex to validate access to cdn during snap
   download
 - daemon: change /v2/download API to take "snap-name" as input
 - release: make forced dev mode look at cgroupv2 support
 - seed/seedwriter: support for extra snaps
 - wrappers/services.go: add disabled svc list arg to AddSnapServices
 - overlord/snapstate: add SetTaskSnapSetup helper + unit tests
 - cmd/libsnap: use cgroup.procs instead of tasks
 - tests: fix snapd-failover test for core18 tests on boards
 - overlord/snapstate/policy, etc: introduce policy, move canRemove
   to it
 - seed/seedwriter: cleanups and small left over todos* drive-by: use
   testutil.FilePresent consistently
 - cmd/snap: update 'snap find' help because it's no longer narrow
 - seed/seedwriter,snap/naming: support classic models
 - cmd/snap-confine: unmount /writable from snap view
 - spread.yaml: exclude automake cacheThe error message is looks like
   this:dpkg-source: info: local changes detected, the modified files
   are:
 - interfaces/openvswitch: allow access to other openvswitch sockets
 - cmd/model: don't show model with display-name inline w/ opts
 - daemon: add a 'prune' debug action
 - client: add doTimeout to http.Client{Timeout}
 - interfaces/seccomp: query apparmor sandbox helper rather than
   aggregate info
 - sandbox/cgroup: avoid dependency on dirs
 - seed/seedwriter,snap: support local snaps
 - overlord/snapstate: fix undo on firstboot seeding.
 - usersession: track connections to session agent for exit on idle
   and peer credential checks
 - tests: fix ubuntu-core-device-reg test for arm devices on core18
 - sandbox/seccomp: move the remaining sandbox bits to a
   corresponding sandbox package
 - osutil: generalize SyncDir with FileState interface
 - daemon, client, cmd/snap: include architecture in 'snap version'
 - daemon: allow /v2/assertions/{assertType} to query store
 - gadget: do not fail the update when old gadget snap is missing
   bare content
 - sandbox/selinux: move SELinux related bits from 'release' to
   'sandbox/selinux'
 - tests: add unit test for gadget defaults with a multiline string
 - overlord/snapstate: have more context in the errors about
   prerequisites
 - httputil: set user agent for CONNECT
 - seed/seedwriter: resolve channels using channel.Resolve* for snaps
 - run-checks: allow overriding gofmt binary, show gofmt diff
 - asserts,seed/seedwriter: follow snap type sorting in the model
   assertion snap listings
 - daemon: return "snapname_rev.snap" style when using /v2/download
 - tests: when the backend is external skip the loop waiting for snap
   version
 - many: move AppArmor probing code under sandbox/apparmor
 - cmd: add `snap debug boot-vars` that dumps the current bootvars
 - tests: skip the ubuntu-core-upgrade on arm devices on core18
 - seed/seedwriter: implement WriteMeta and tree16 corresponding code
 - interfaces/docker-support,kubernetes-support: misc updates for
   strict k8s
 - tests: restart the journald service while preparing the test
 - tests/cmd/debug_state: make the test output TZ independent
 - interfaces/kubernetes-support: allow use of /run/flannel
 - seed/seedwriter: start of Writer and internal policy16/tree16
 - sandbox/cgroup, usersession/userd: move cgroup related helper to a
   dedicated package
 - tests: move "centos-7" to unstable systems
 - snapstate: add missing tests for checkGadgetOrKernel
 - docs: Update README.md
 - snapcraft: set license to GPL-3.0
 - interfaces/wayland: allow a confined server running in a user
   session to work with Qt, GTK3 & SDL2 clients
 - selinux: move the package under sandbox/selinux
 - interfaces/udev: account for cgroup version when reporting
   supported features
 - store, ..., client: add a "website" field
 - sanity: sanity check cgroup probing
 - snapstate: increase settleTimeout in
   TestRemodelSwitchToDifferentKernel
 - packaging: remove obsolete usr.lib.snapd.snap-confine in postinst
 - data/selinux: allow snapd/snap to do statfs() on the cgroup
   mountpoint
 - usersession/userd: make sure to export DBus interfaces before
   requesting a name
 - data/selinux: allow snapd to issue sigkill to journalctl
 - docs: Add Code of Conduct
 - store: download propagates options to delta download
 - tests/main/listing: account for dots in ~pre suffix

* Fri Dec 06 2019 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.42.5
 - snap-confine: revert, with comment, explicit unix deny for nested
   lxd
 - Disable mount-ns test on 16.04. It is too flaky currently.

* Thu Nov 28 2019 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.42.4
 - overlord/snapstate: make sure configuration defaults are applied
   only once

* Wed Nov 27 2019 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.42.3
 - overlord/snapstate: pick up system defaults when seeding the snapd
   snap
 - cmd/snap-update-ns: fix overlapping, nested writable mimic
   handling
 - interfaces: misc updates for u2f-devices, browser-support,
   hardware-observe, et al
 - tests: reset failing "fwupd-refresh.service" if needed
 - tests/main/gadget-update-pc: use a program to modify gadget yaml
 - snap-confine: suppress noisy classic snap file_inherit denials

* Wed Nov 20 2019 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.42.2
 - interfaces/lxd-support: Fix on core18
 - tests/main/system-usernames: Amazon Linux 2 comes with libseccomp
   2.4.1 now
 - snap-seccomp: add missing clock_getres_time64
 - cmd/snap-seccomp/syscalls: update the list of known
   syscalls
 - sandbox/seccomp: accept build ID generated by Go toolchain
 - interfaces: allow access to ovs bridge sockets

* Wed Oct 30 2019 Michael Vogt <mvo@ubuntu.com>
- New upstream release 2.42.1
 - interfaces: de-duplicate emitted update-ns profiles
 - packaging: tweak handling of usr.lib.snapd.snap-confine
 - interfaces: allow introspecting network-manager on core
 - tests/main/interfaces-contacts-service: disable on openSUSE
   Tumbleweed
 - tests/lib/lxd-snapfuse: restore mount changes introduced by LXD
 - snap: fix default-provider in seed validation
 - tests: update system-usernames test now that opensuse-15.1 works
 - overlord: set fake sertial in TestRemodelSwitchToDifferentKernel
 - gadget: rename "boot{select,img}" -> system-boot-{select,image}
 - tests: listing test, make accepted snapd/core versions consistent

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
