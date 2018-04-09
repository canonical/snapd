# spec file for package snapd
#

# A switch to allow building the package with support for testkeys which
# are used for the spread test suite of snapd.
%bcond_with testkeys

%global with_debug 1
%global with_check 0
%global with_test_keys 0

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

%define systemd_services_list snapd.socket snapd.service snapd.autoimport.service

%define gobuild(o:) go build -compiler gc -tags=rpm_crashtraceback -ldflags "${LDFLAGS:-} -B ${BUILDID:-0x0} -extldflags '%__global_ldflags'" -a -v -x %{?**};
%define gobuild_static(o:) go build -compiler gc -tags=rpm_crashtraceback -ldflags "${LDFLAGS:-} -B ${BUILDID:-0x0} -extldflags '%__global_ldflags -static'" -a -v -x %{?**};
%define gotest() go test -compiler gc -ldflags "${LDFLAGS:-}" %{?**};

Name:           snapd
Version:        2.32.3
Release:        0%{?dist}
Summary:        Tools enabling systems to work with .snap files
License:        GPLv3
Group:          System Environment/Base
Url:            https://%{provider_prefix}
# Use the version with the vendored go packages
Source0:  https://%{provider_prefix}/releases/download/%{version}/%{name}_%{version}.vendor.tar.xz
BuildRequires:  autoconf
BuildRequires:  automake
BuildRequires:  gcc
BuildRequires:  gettext
BuildRequires:  gnupg
BuildRequires:  pkgconfig(glib-2.0)
BuildRequires:  pkgconfig(libcap)
BuildRequires:  pkgconfig(libseccomp)
BuildRequires:  pkgconfig(libudev)
BuildRequires:  pkgconfig(systemd)
BuildRequires:  libseccomp-static
BuildRequires:  glibc-static
BuildRequires:  golang
BuildRequires:  libtool
BuildRequires:  make
BuildRequires:  pkgconfig
BuildRequires:  squashfs-tools
BuildRequires:  xfsprogs-devel
BuildRequires:  xz
BuildRequires:  valgrind

%{?systemd_requires}
Requires: squashfs-tools
# TODO: enable squashfuse once it's packaged
#Requires:  squashfuse
#Requires:  fuse
Requires: bash-completion

BuildRoot:      %{_tmppath}/%{name}-%{version}-build

%description
This package contains that snapd daemon and the snap command line tool.
Together they can be used to install, refresh (update), remove and configure
snap packages on a system. Snap packages are a novel format based on simple
principles. Bundle your dependencies, run in a predictable environment, use
moder kernel features for setting up the execution environment and security.
The same binary snap package can be installed and used on many diverse systems
such as Debian, Fedora and OpenSUSE as well as their multiple derivatives.

%prep
%setup -q -n %{name}-%{version}

%build
# Set the version that is compiled into the various executables
./mkversion.sh %{version}-%{release}

# Build golang executables
mkdir -p src/github.com/snapcore
ln -s ../../../ src/github.com/snapcore/snapd

export GOPATH=$(pwd):$(pwd)/Godeps/_workspace:%{gopath}

GOFLAGS=
%if 0%{?with_test_keys}
GOFLAGS="$GOFLAGS -tags withtestkeys"
%endif

BUILDID="0x$(head -c20 /dev/urandom|od -An -tx1|tr -d ' \\\n')"

%gobuild -o bin/snapd $GOFLAGS github.com/snapcore/snapd/cmd/snapd
%gobuild -o bin/snap $GOFLAGS github.com/snapcore/snapd/cmd/snap
%gobuild -o bin/snapctl $GOFLAGS github.com/snapcore/snapd/cmd/snapctl
%gobuild_static -o bin/snap-exec $GOFLAGS github.com/snapcore/snapd/cmd/snap-exec
%gobuild_static -o bin/snap-update-ns $GOFLAGS github.com/snapcore/snapd/cmd/snap-update-ns
%gobuild -o bin/snap-seccomp $GOFLAGS github.com/snapcore/snapd/cmd/snap-seccomp

# Build C executables

# Generate autotools build system files
pushd cmd
autoreconf -i -f

%configure  \
    --disable-apparmor \
    --libexecdir=/usr/libexec/snapd \
    --with-snap-mount-dir=%{_sharedstatedir}/snapd/snap \
    --with-merged-usr

%make_build
popd

pushd ./data
# Build data
make BINDIR="%{_bindir}" LIBEXECDIR="%{_libexecdir}" \
     SYSTEMDSYSTEMUNITDIR="%{_unitdir}" \
     SNAP_MOUNT_DIR="%{_sharedstatedir}/snapd/snap" \
     SNAPD_ENVIRONMENT_FILE="%{_sysconfdir}/sysconfig/snapd"
popd

%check
%if 0%{?with_check}
export GOPATH=$(pwd):$(pwd)/Godeps/_workspace:%{gopath}
# TODO: running unit tests will requires a patch to disable snap-seccomp
# multilib tests, the patch is already in master and 2.32 branches, will be
# included in 2.32.4 release
%gotest %{import_path}/...
%endif

# snap-confine tests (these always run!)
pushd ./cmd
make check
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
install -d -p %{buildroot}%{_sharedstatedir}/snapd/lib/gl
install -d -p %{buildroot}%{_sharedstatedir}/snapd/lib/gl32
install -d -p %{buildroot}%{_sharedstatedir}/snapd/lib/vulkan
install -d -p %{buildroot}%{_sharedstatedir}/snapd/mount
install -d -p %{buildroot}%{_sharedstatedir}/snapd/seccomp/bpf
install -d -p %{buildroot}%{_sharedstatedir}/snapd/snaps
install -d -p %{buildroot}%{_sharedstatedir}/snapd/snap/bin
install -d -p %{buildroot}%{_localstatedir}/snap
install -d -p %{buildroot}%{_localstatedir}/cache/snapd
install -d -p %{buildroot}%{_datadir}/polkit-1/actions

# Install snap and snapd
install -p -m 0755 bin/snap %{buildroot}%{_bindir}
install -p -m 0755 bin/snap-exec %{buildroot}%{_libexecdir}/snapd
install -p -m 0755 bin/snapctl %{buildroot}%{_bindir}/snapctl
install -p -m 0755 bin/snapd %{buildroot}%{_libexecdir}/snapd
install -p -m 0755 bin/snap-update-ns %{buildroot}%{_libexecdir}/snapd
install -p -m 0755 bin/snap-seccomp %{buildroot}%{_libexecdir}/snapd

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
%make_install BINDIR="%{_bindir}" LIBEXECDIR="%{_libexecdir}" \
              SYSTEMDSYSTEMUNITDIR="%{_unitdir}" \
              SNAP_MOUNT_DIR="%{_sharedstatedir}/snapd/snap" \
              SNAPD_ENVIRONMENT_FILE="%{_sysconfdir}/sysconfig/snapd"

# Remove snappy core specific units
rm -fv %{buildroot}%{_unitdir}/snapd.system-shutdown.service
rm -fv %{buildroot}%{_unitdir}/snapd.snap-repair.*
rm -fv %{buildroot}%{_unitdir}/snapd.core-fixup.*
popd

# Remove snappy core specific scripts
rm %{buildroot}%{_libexecdir}/snapd/snapd.core-fixup.sh

# Install Polkit configuration
install -m 644 -D data/polkit/io.snapcraft.snapd.policy %{buildroot}%{_datadir}/polkit-1/actions

# empty env file for snapd
touch  %{buildroot}%{_sysconfdir}/sysconfig/snapd

%post
%systemd_post %{systemd_services_list}

# If install and snapd.socket is enabled, start it.
if [ $1 -eq 1 ] ; then
    systemctl start snapd.socket > /dev/null 2>&1 || :
fi

%preun
%systemd_preun %{systemd_services_list}
# Remove all Snappy content if snapd is being fully uninstalled
if [ $1 -eq 0 ]; then
    /usr/libexec/snapd/snap-mgmt --purge || :
fi

%postun
%systemd_postun_with_restart %{systemd_services_list}

%files
%defattr(-,root,root)
%license COPYING
%doc README.md docs/*
%{_bindir}/snap
%{_bindir}/snapctl
%dir %{_libexecdir}/snapd
%{_libexecdir}/snapd/snapd
%{_libexecdir}/snapd/snap-exec
%{_libexecdir}/snapd/info
%{_libexecdir}/snapd/snap-mgmt
%attr(6755,root,root) %{_libexecdir}/snapd/snap-confine
%{_libexecdir}/snapd/snap-discard-ns
%{_libexecdir}/snapd/snap-seccomp
%{_libexecdir}/snapd/snap-update-ns
%{_libexecdir}/snapd/snap-device-helper
%{_libexecdir}/snapd/system-shutdown
%{_libexecdir}/snapd/snap-gdb-shim
%{_libexecdir}/snapd/snapd-generator
%attr(0000,root,root) %{_sharedstatedir}/snapd/void
%{_mandir}/man1/snap.1*
%{_mandir}/man1/snap-confine.1*
%{_mandir}/man5/snap-discard-ns.5*
%{_datadir}/bash-completion/completions/snap
%{_libexecdir}/snapd/complete.sh
%{_libexecdir}/snapd/etelpmoc.sh
%{_sysconfdir}/profile.d/snapd.sh
%{_unitdir}/snapd.socket
%{_unitdir}/snapd.service
%{_unitdir}/snapd.autoimport.service
%{_datadir}/dbus-1/services/io.snapcraft.Launcher.service
%{_datadir}/dbus-1/services/io.snapcraft.Settings.service
%{_datadir}/polkit-1/actions/io.snapcraft.snapd.policy
%config(noreplace) %{_sysconfdir}/sysconfig/snapd
%dir %{_sharedstatedir}/snapd
%dir %{_sharedstatedir}/snapd/assertions
%dir %{_sharedstatedir}/snapd/desktop
%dir %{_sharedstatedir}/snapd/desktop/applications
%dir %{_sharedstatedir}/snapd/device
%dir %{_sharedstatedir}/snapd/hostfs
%dir %{_sharedstatedir}/snapd/lib
%dir %{_sharedstatedir}/snapd/lib/gl
%dir %{_sharedstatedir}/snapd/lib/gl32
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

%changelog
* Mon Apr  9 2018 Maciej Zenon Borzecki <maciej.zenon.borzecki@canonical.com> - 2.32.3-0.amzn2
- Initial packaging of 2.32.3
