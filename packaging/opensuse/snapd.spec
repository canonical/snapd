# spec file for package snapd
#
# Copyright (c) 2017 Zygmunt Krynicki <zygmunt.krynicki@canonical.com>
# Copyright (c) 2018 Neal Gompa <ngompa13@gmail.com>
#
# All modifications and additions to the file contributed by third parties
# remain the property of their copyright owners, unless otherwise agreed
# upon. The license for this file, and modifications and additions to the
# file, is the same license as for the pristine package itself (unless the
# license for the pristine package is not an Open Source License, in which
# case the license is the MIT License). An "Open Source License" is a
# license that conforms to the Open Source Definition (Version 1.9)
# published by the Open Source Initiative.

# Please submit bugfixes or comments via http://bugs.opensuse.org/

%bcond_with testkeys

# Enable AppArmor on openSUSE Tumbleweed (post 15.0) or higher
# N.B.: Prior to openSUSE Tumbleweed in May 2018, the AppArmor userspace in SUSE
# did not support what we needed to be able to turn on basic integration.
%if 0%{?suse_version} >= 1550
%bcond_without apparmor
%else
%bcond_with apparmor
%endif

# Compat macros
%{!?make_build: %global make_build %{__make} %{?_smp_mflags}}
%{?!_environmentdir: %global _environmentdir %{_prefix}/lib/environment.d}

# Define the variable for systemd generators, if missing.
%{?!_systemdgeneratordir: %global _systemdgeneratordir %{_prefix}/lib/systemd/system-generators}
%{?!_systemdusergeneratordir: %global _systemdusergeneratordir %{_prefix}/lib/systemd/user-generators}
%{?!_systemd_system_env_generator_dir: %global _systemd_system_env_generator_dir %{_prefix}/lib/systemd/system-environment-generators}
%{?!_systemd_user_env_generator_dir: %global _systemd_user_env_generator_dir %{_prefix}/lib/systemd/user-environment-generators}

# This is fixed in SUSE Linux 15
# Cf. https://build.opensuse.org/package/rdiff/Base:System/rpm?linkrev=base&rev=396
%if 0%{?suse_version} < 1500
%global _sharedstatedir %{_localstatedir}/lib
%endif

%global provider        github
%global provider_tld    com
%global project         snapcore
%global repo            snapd
%global provider_prefix %{provider}.%{provider_tld}/%{project}/%{repo}
%global import_path     %{provider_prefix}

# Additional entry of $GOPATH during the build process.
# This is designed to be a sub-directory of {_builddir}/{name}-{version}
# because that directory is automatically cleaned-up by the build process.
%global         indigo_gopath   %{_builddir}/%{name}-%{version}/gopath

# Directory where "name-version" directory from upstream taball is unpacked to.
# This directory is arranged so that it is already contained inside the future
# GOPATH so that nothing needs to be moved or copied for "go build" to work.
%global         indigo_srcdir   %{indigo_gopath}/src/%{import_path}

%global with_test_keys  0

%if %{with testkeys}
%global with_test_keys 1
%else
%global with_test_keys 0
%endif

# Set if multilib is enabled for supported arches
%ifarch x86_64 aarch64 %{power64} s390x
%global with_multilib 1
%endif

%global systemd_services_list snapd.socket snapd.service snapd.seeded.service %{?with_apparmor:snapd.apparmor.service}

%global snap_mount_dir /snap

Name:           snapd
Version:        2.37
Release:        0
Summary:        Tools enabling systems to work with .snap files
License:        GPL-3.0
Group:          System/Packages
Url:            https://%{import_path}
Source0:        https://github.com/snapcore/snapd/releases/download/%{version}/%{name}_%{version}.vendor.tar.xz
Source1:        snapd-rpmlintrc
%if (0%{?sle_version} >= 120200 || 0%{?suse_version} >= 1500) && 0%{?is_opensuse}
BuildRequires:  ShellCheck
%endif
BuildRequires:  autoconf
BuildRequires:  automake
BuildRequires:  glib2-devel
BuildRequires:  glibc-devel-static
BuildRequires:  go
BuildRequires:  gpg2
BuildRequires:  indent
BuildRequires:  libapparmor-devel
BuildRequires:  libcap-devel
BuildRequires:  libseccomp-devel
BuildRequires:  libtool
BuildRequires:  libudev-devel
BuildRequires:  libuuid-devel
BuildRequires:  make
BuildRequires:  openssh
BuildRequires:  pkg-config
BuildRequires:  python-docutils
BuildRequires:  python3-docutils
BuildRequires:  squashfs
# Due to: rpm -q --whatprovides /usr/share/pkgconfig/systemd.pc
BuildRequires:  systemd
BuildRequires:  systemd-rpm-macros
BuildRequires:  timezone
BuildRequires:  udev
BuildRequires:  xfsprogs-devel
BuildRequires:  xz
%ifarch x86_64
# This is needed for seccomp tests
BuildRequires:  glibc-devel-32bit
BuildRequires:  glibc-devel-static-32bit
BuildRequires:  gcc-32bit
%endif

%if %{with apparmor}
BuildRequires:  apparmor-rpm-macros
%endif

PreReq:         permissions

Requires(post): permissions
Requires:       apparmor-parser
Requires:       apparmor-profiles
Requires:       gpg2
Requires:       openssh
Requires:       squashfs

# Old versions of xdg-document-portal can expose data belonging to
# other confied apps.  Older OpenSUSE releases are unlikely to change,
# so for now limit this to Tumbleweed.
%if 0%{?suse_version} >= 1550
Conflicts:      xdg-desktop-portal < 0.11
%endif

%{?systemd_requires}

%description
This package contains that snapd daemon and the snap command line tool.
Together they can be used to install, refresh (update), remove and configure
snap packages on a system. Snap packages are a novel format based on simple
principles. Bundle your dependencies, run in a predictable environment, use
modern kernel features for setting up the execution environment and security.
The same binary snap package can be installed and used on many diverse systems
such as Debian, Fedora and OpenSUSE as well as their multiple derivatives.

This package contains the official build, endorsed by snapd developers. It is
updated as soon as new upstream releases are made and is designed to live in
the system:snappy repository.

%prep
# NOTE: Instead of using setup -q we are unpacking a subdirectory of the source
# tarball into a directory that is automatically on the future GOPATH. This
# means that while go doesn't care at all the current working directory is not
# the top-level directory of the source tarball which some people may find
# unusual.

# Create indigo compatible build layout.
mkdir -p %{indigo_srcdir}
tar -axf %{_sourcedir}/%{name}_%{version}.vendor.tar.xz --strip-components=1 -C %{indigo_srcdir}

# Patch the source in the place it got extracted to.
pushd %{indigo_srcdir}
# Add patch0 -p1 ... as appropriate here.
popd

# Set the version that is compiled into the various executables/
pushd %{indigo_srcdir}
./mkversion.sh %{version}-%{release}
popd

# Sanity check, ensure that systemd system generator directory is in agreement between the build system and packaging.
if [ "$(pkg-config --variable=systemdsystemgeneratordir systemd)" != "%{_systemdgeneratordir}" ]; then
  echo "pkg-confing and rpm macros disagree about the location of systemd system generator directory"
  exit 1
fi

# Enable hardening; Also see https://bugzilla.redhat.com/show_bug.cgi?id=1343892
CFLAGS="$RPM_OPT_FLAGS -fPIC -Wl,-z,relro -Wl,-z,now"
CXXFLAGS="$RPM_OPT_FLAGS -fPIC -Wl,-z,relro -Wl,-z,now"
LDFLAGS=""

# On openSUSE Leap 15 or more recent build position independent executables.
# For a helpful guide about the versions and macros used below, please see:
# https://en.opensuse.org/openSUSE:Build_Service_cross_distribution_howto
%if 0%{?suse_version} >= 1500
CFLAGS="$CFLAGS -fPIE"
CXXFLAGS="$CXXFLAGS -fPIE"
LDFLAGS="$LDFLAGS -pie"
%endif

export CFLAGS
export CXXFLAGS
export LDFLAGS

# Generate autotools build system files.
pushd %{indigo_srcdir}/cmd
autoreconf -i -f
%configure \
    %{!?with_apparmor:--disable-apparmor} \
    --libexecdir=%{_libexecdir}/snapd \
    --enable-nvidia-biarch \
    %{?with_multilib:--with-32bit-libdir=%{_prefix}/lib} \
    --with-snap-mount-dir=%{snap_mount_dir} \
    --enable-merged-usr
popd

%build
# Build golang executables, with the following exceptions everything is built the same way:
# - snap-exec and snap-update-ns is built statically.
# - snapd has a variant that uses test keys instead of production keys.
#
# NOTE: indigo_gopath takes priority over GOPATH. This ensures that we
# build the code that we intended in case GOPATH points to another copy.
GOPATH=%{indigo_gopath}:$GOPATH go build -buildmode=pie %{import_path}/cmd/snap
GOPATH=%{indigo_gopath}:$GOPATH go build -buildmode=pie %{import_path}/cmd/snapctl
GOPATH=%{indigo_gopath}:$GOPATH go build -buildmode=pie %{import_path}/cmd/snap-seccomp
GOPATH=%{indigo_gopath}:$GOPATH go build -buildmode=default -ldflags '-extldflags "-static"' %{import_path}/cmd/snap-update-ns
GOPATH=%{indigo_gopath}:$GOPATH go build -buildmode=default -ldflags '-extldflags "-static"' %{import_path}/cmd/snap-exec
%if 0%{?with_test_keys}
GOPATH=%{indigo_gopath}:$GOPATH go build -buildmode=pie -tags withtestkeys %{import_path}/cmd/snapd
%else
GOPATH=%{indigo_gopath}:$GOPATH go build -buildmode=pie %{import_path}/cmd/snapd
%endif

# Build C executables
%make_build -C %{indigo_srcdir}/cmd

%check
# Run tests with fixed locale that is expected by unicode/color code.
LC_ALL=C.UTF-8 GOPATH=%{indigo_gopath}:$GOPATH go test %{import_path}/...
%make_build -C %{indigo_srcdir}/cmd check

%install
# Install all systemd and dbus units, and env files.
%make_install -C %{indigo_srcdir}/data \
		BINDIR=%{_bindir} \
		LIBEXECDIR=%{_libexecdir} \
		SYSTEMDSYSTEMUNITDIR=%{_unitdir} \
		SNAP_MOUNT_DIR=%{snap_mount_dir}
# Install all the C executables.
%make_install -C %{indigo_srcdir}/cmd
# Install all the Go executables.
install -d -m 755 %{buildroot}%{_bindir}
install -m 755 snap %{buildroot}%{_bindir}
# Ensure /usr/bin/snapctl is a symlink to /usr/libexec/snapd/snapctl
install -m 755 snapctl %{buildroot}%{_libexecdir}/snapd
ln -s %{_libexecdir}/snapd/snapctl  %{buildroot}%{_bindir}/snapctl
install -d -m 755 %{buildroot}%{_libexecdir}/snapd
install -m 755 snapd %{buildroot}%{_libexecdir}/snapd/
install -m 755 snap-exec %{buildroot}%{_libexecdir}/snapd/
install -m 755 snap-update-ns %{buildroot}%{_libexecdir}/snapd/
install -m 755 snap-seccomp %{buildroot}%{_libexecdir}/snapd/
# Generate and install man page for snap command
install -d -m 755 %{buildroot}%{_mandir}/man8
./snap help --man > %{buildroot}%{_mandir}/man8/snap.8
# Undo special permissions of the void directory
chmod 755 %{buildroot}%{_sharedstatedir}/snapd/void
# Remove traces of ubuntu-core-launcher. It is a phased-out executable that is
# still partially present in the tree but should be removed in the subsequent
# release.
rm -f %{buildroot}%{_bindir}/ubuntu-core-launcher
# NOTE: we don't want to ship system-shutdown helper, it is just a helper on
# ubuntu-core systems that exclusively use snaps. It is used during the
# shutdown process and thus can be left out of the distribution package.
rm -f %{buildroot}%{_libexecdir}/snapd/system-shutdown
# Install the directories that snapd creates by itself so that they can be a part of the package
install -d %{buildroot}%{_sharedstatedir}/snapd/{assertions,cookie,desktop/applications,device,hostfs,mount,apparmor/profiles,seccomp/bpf,snaps}

install -d %{buildroot}%{_sharedstatedir}/snapd/{lib/gl,lib/gl32,lib/vulkan}
install -d %{buildroot}%{_localstatedir}/cache/snapd
install -d %{buildroot}%{_datadir}/polkit-1/actions
install -d %{buildroot}%{snap_mount_dir}/bin
# Install local permissions policy for snap-confine. This should be removed
# once snap-confine is added to the permissions package. This is done following
# the recommendations on
# https://en.opensuse.org/openSUSE:Package_security_guidelines
install -m 644 -D %{indigo_srcdir}/packaging/opensuse/permissions %{buildroot}%{_sysconfdir}/permissions.d/snapd
install -m 644 -D %{indigo_srcdir}/packaging/opensuse/permissions.paranoid %{buildroot}%{_sysconfdir}/permissions.d/snapd.paranoid
# Remove unwanted systemd units
for s in snapd.autoimport.service snapd.system-shutdown.service snapd.snap-repair.timer snapd.snap-repair.service snapd.core-fixup.service; do
    rm -f %{buildroot}%{_unitdir}/$s
done
# Remove snappy core specific scripts
rm -f %{buildroot}%{_libexecdir}/snapd/snapd.core-fixup.sh

# Install Polkit configuration
install -m 644 -D %{indigo_srcdir}/data/polkit/io.snapcraft.snapd.policy %{buildroot}%{_datadir}/polkit-1/actions

# See https://en.opensuse.org/openSUSE:Packaging_checks#suse-missing-rclink for details
install -d %{buildroot}%{_sbindir}
ln -sf %{_sbindir}/service %{buildroot}%{_sbindir}/rcsnapd
ln -sf %{_sbindir}/service %{buildroot}%{_sbindir}/rcsnapd.seeded
%if %{with apparmor}
ln -sf %{_sbindir}/service %{buildroot}%{_sbindir}/rcsnapd.apparmor
%endif
# Install the "info" data file with snapd version
install -m 644 -D %{indigo_srcdir}/data/info %{buildroot}%{_libexecdir}/snapd/info
# Install bash completion for "snap"
install -m 644 -D %{indigo_srcdir}/data/completion/snap %{buildroot}%{_datadir}/bash-completion/completions/snap
install -m 644 -D %{indigo_srcdir}/data/completion/complete.sh %{buildroot}%{_libexecdir}/snapd
install -m 644 -D %{indigo_srcdir}/data/completion/etelpmoc.sh %{buildroot}%{_libexecdir}/snapd

# Don't ship apparmor helper service when AppArmor is not enabled
%if ! %{with apparmor}
rm -f %{buildroot}%{_unitdir}/snapd.apparmor.service
rm -f %{buildroot}%{_libexecdir}/snapd/snapd-apparmor
%endif

%verifyscript
%verify_permissions -e %{_libexecdir}/snapd/snap-confine

%pre
%service_add_pre %{systemd_services_list}

%post
%set_permissions %{_libexecdir}/snapd/snap-confine
%if %{with apparmor}
%apparmor_reload /etc/apparmor.d/usr.lib.snapd.snap-confine
%endif
%service_add_post %{systemd_services_list}
case ":$PATH:" in
    *:/snap/bin:*)
        ;;
    *)
        echo "Please reboot, logout/login or source /etc/profile to have /snap/bin added to PATH."
        echo "On a Tumbleweed system you need to run: systemctl enable snapd.apparmor.service"
        ;;
esac

%preun
%service_del_preun %{systemd_services_list}
if [ $1 -eq 0 ]; then
    %{_libexecdir}/snapd/snap-mgmt --purge || :
fi

%postun
%service_del_postun %{systemd_services_list}

%files
%if %{with apparmor}
%config %{_sysconfdir}/apparmor.d
%endif
%config %{_sysconfdir}/permissions.d/snapd
%config %{_sysconfdir}/permissions.d/snapd.paranoid
%config %{_sysconfdir}/profile.d/snapd.sh
%dir %attr(0000,root,root) %{_sharedstatedir}/snapd/void
%dir %{snap_mount_dir}
%dir %{snap_mount_dir}/bin
%dir %{_libexecdir}/snapd
%dir %{_sharedstatedir}/snapd
%dir %{_sharedstatedir}/snapd/apparmor
%dir %{_sharedstatedir}/snapd/apparmor/profiles
%dir %{_sharedstatedir}/snapd/apparmor/snap-confine
%dir %{_sharedstatedir}/snapd/assertions
%dir %{_sharedstatedir}/snapd/cookie
%dir %{_sharedstatedir}/snapd/desktop
%dir %{_sharedstatedir}/snapd/desktop/applications
%dir %{_sharedstatedir}/snapd/device
%dir %{_sharedstatedir}/snapd/hostfs
%dir %{_sharedstatedir}/snapd/mount
%dir %{_sharedstatedir}/snapd/seccomp
%dir %{_sharedstatedir}/snapd/seccomp/bpf
%dir %{_sharedstatedir}/snapd/snaps
%dir %{_sharedstatedir}/snapd/lib
%dir %{_sharedstatedir}/snapd/lib/gl
%dir %{_sharedstatedir}/snapd/lib/gl32
%dir %{_sharedstatedir}/snapd/lib/vulkan
%dir %{_localstatedir}/cache/snapd
%dir %{_environmentdir}
%dir %{_systemd_system_env_generator_dir}
%dir %{_systemdgeneratordir}
%dir %{_datadir}/dbus-1
%dir %{_datadir}/dbus-1/services
%dir %{_datadir}/polkit-1
%dir %{_datadir}/polkit-1/actions
%verify(not user group mode) %attr(06755,root,root) %{_libexecdir}/snapd/snap-confine
%{_mandir}/man8/snap-confine.8*
%{_mandir}/man8/snap-discard-ns.8*
%{_mandir}/man8/snapd-env-generator.8*
%{_unitdir}/snapd.service
%{_unitdir}/snapd.socket
%{_unitdir}/snapd.seeded.service
%{_unitdir}/snapd.failure.service
%if %{with apparmor}
%{_unitdir}/snapd.apparmor.service
%endif
%{_bindir}/snap
%{_bindir}/snapctl
%{_sbindir}/rcsnapd
%{_sbindir}/rcsnapd.seeded
%if %{with apparmor}
%{_sbindir}/rcsnapd.apparmor
%endif
%{_libexecdir}/snapd/info
%{_libexecdir}/snapd/snap-discard-ns
%{_libexecdir}/snapd/snap-update-ns
%{_libexecdir}/snapd/snap-exec
%{_libexecdir}/snapd/snap-seccomp
%{_libexecdir}/snapd/snapd
%{_libexecdir}/snapd/snapctl
%if %{with apparmor}
%{_libexecdir}/snapd/snapd-apparmor
%endif
%{_libexecdir}/snapd/snap-mgmt
%{_libexecdir}/snapd/snap-gdb-shim
%{_libexecdir}/snapd/snap-device-helper
%{_datadir}/bash-completion/completions/snap
%{_libexecdir}/snapd/complete.sh
%{_libexecdir}/snapd/etelpmoc.sh
%{_systemdgeneratordir}/snapd-generator
%{_mandir}/man8/snap.8*
%{_datadir}/applications/snap-handle-link.desktop
%{_datadir}/dbus-1/services/io.snapcraft.Launcher.service
%{_datadir}/dbus-1/services/io.snapcraft.Settings.service
%{_datadir}/polkit-1/actions/io.snapcraft.snapd.policy
%{_sysconfdir}/xdg/autostart/snap-userd-autostart.desktop
%{_libexecdir}/snapd/snapd.run-from-snap
%if %{with apparmor}
%{_sysconfdir}/apparmor.d/usr.lib.snapd.snap-confine
%endif
%{_environmentdir}/990-snapd.conf
%{_systemd_system_env_generator_dir}/snapd-env-generator

%changelog

