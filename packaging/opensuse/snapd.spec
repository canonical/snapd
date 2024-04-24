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

# takes an absolute path with slashes and turns it into an AppArmor profile path
%define as_apparmor_path() %(echo "%1" | tr / . | cut -c2-)

# Test keys: used for internal testing in snapd.
%bcond_with testkeys

# Enable apparmor on Tumbleweed and Leap 15.3+
%if 0%{?suse_version} >= 1550 || 0%{?sle_version} >= 150300
%bcond_without apparmor
%else
%bcond_with apparmor
%endif

# The list of systemd services we are expected to ship. Note that this does
# not include services that are only required on core systems.
%global systemd_services_list snapd.socket snapd.service snapd.seeded.service snapd.failure.service %{?with_apparmor:snapd.apparmor.service} snapd.mounts.target snapd.mounts-pre.target
%global systemd_user_services_list snapd.session-agent.socket

# Alternate snap mount directory: not used by openSUSE.
# If this spec file is integrated into Fedora then consider
# adding global with_alt_snap_mount_dir 1 then.
%global snap_mount_dir /snap

# Compat macros
%{!?make_build: %global make_build %{__make} %{?_smp_mflags}}
%{?!_environmentdir: %global _environmentdir %{_prefix}/lib/environment.d}
%{?!_userunitdir: %global _userunitdir %{_prefix}/lib/systemd/user}

# Define the variable for systemd generators, if missing.
%{?!_systemdgeneratordir: %global _systemdgeneratordir %{_prefix}/lib/systemd/system-generators}
%{?!_systemdusergeneratordir: %global _systemdusergeneratordir %{_prefix}/lib/systemd/user-generators}
%{?!_systemd_system_env_generator_dir: %global _systemd_system_env_generator_dir %{_prefix}/lib/systemd/system-environment-generators}
%{?!_systemd_user_env_generator_dir: %global _systemd_user_env_generator_dir %{_prefix}/lib/systemd/user-environment-generators}
%{!?_tmpfilesdir: %global _tmpfilesdir %{_prefix}/lib/tmpfiles.d}

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
%global indigo_gopath   %{_builddir}/%{name}-%{version}/gopath

# Directory where "name-version" directory from upstream taball is unpacked to.
# This directory is arranged so that it is already contained inside the future
# GOPATH so that nothing needs to be moved or copied for "go build" to work.
%global indigo_srcdir   %{indigo_gopath}/src/%{import_path}

# path to snap-confine encoded as AppArmor profile
%define apparmor_snapconfine_profile %as_apparmor_path %{_libexecdir}/snapd/snap-confine

# Set if multilib is enabled for supported arches
%ifarch x86_64 aarch64 %{power64} s390x
%global with_multilib 1
%endif


Name:           snapd
Version:        2.63
Release:        0
Summary:        Tools enabling systems to work with .snap files
License:        GPL-3.0
Group:          System/Packages
Url:            https://%{import_path}
Source0:        https://github.com/snapcore/snapd/releases/download/%{version}/%{name}_%{version}.vendor.tar.xz
Source1:        snapd-rpmlintrc
BuildRequires:  autoconf
BuildRequires:  autoconf-archive
BuildRequires:  automake
BuildRequires:  fakeroot
BuildRequires:  glib2-devel
BuildRequires:  glibc-devel-static
BuildRequires:  go >= 1.18
BuildRequires:  gpg2
BuildRequires:  indent
BuildRequires:  libcap-devel
BuildRequires:  libseccomp-devel
BuildRequires:  libtool
BuildRequires:  libudev-devel
BuildRequires:  libuuid-devel
BuildRequires:  make
BuildRequires:  openssh
BuildRequires:  pkg-config
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
BuildRequires:  ca-certificates
BuildRequires:  ca-certificates-mozilla

%if %{with apparmor}
BuildRequires:  libapparmor-devel
BuildRequires:  apparmor-rpm-macros
BuildRequires:  apparmor-parser
%endif

PreReq:         permissions

Requires(post): permissions
%if %{with apparmor}
Requires:       apparmor-parser
Requires:       apparmor-profiles
%endif
Requires:       gpg2
Requires:       openssh
Requires:       squashfs
Requires:       system-user-daemon

# Old versions of xdg-document-portal can expose data belonging to
# other confied apps.  Older OpenSUSE releases are unlikely to change,
# so for now limit this to Tumbleweed.
%if 0%{?suse_version} >= 1550 || 0%{?sle_version} >= 150300
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
%autopatch -p1
popd

# Generate snapd.defines.mk, this file is included by snapd.mk. It contains a
# number of variable definitions that are set based on their RPM equivalents.
# Since we can apply any conditional overrides here in the spec file we can
# maintain one consistent set of variables across the spec and makefile worlds.
cat >snapd.defines.mk <<__DEFINES__
# This file is generated by openSUSE's snapd.spec
# Directory variables.
prefix = %{_prefix}
bindir = %{_bindir}
sbindir = %{_sbindir}
libexecdir = %{_libexecdir}
mandir = %{_mandir}
datadir = %{_datadir}
localstatedir = %{_localstatedir}
sharedstatedir = %{_sharedstatedir}
unitdir = %{_unitdir}
builddir = %{_builddir}
# Build configuration
with_core_bits = 0
with_alt_snap_mount_dir = %{!?with_alt_snap_mount_dir:0}%{?with_alt_snap_mount_dir:1}
with_apparmor = %{with apparmor}
with_testkeys = %{with_testkeys}
__DEFINES__

# Set the version that is compiled into the various executables/
pushd %{indigo_srcdir}
./mkversion.sh %{version}
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
    %{?with_apparmor:--enable-apparmor} \
    --libexecdir=%{_libexecdir}/snapd \
    --enable-nvidia-biarch \
    %{?with_multilib:--with-32bit-libdir=%{_prefix}/lib} \
    --with-snap-mount-dir=%{snap_mount_dir} \
    --enable-merged-usr

popd

%build
%make_build -C %{indigo_srcdir}/cmd
# Use the common packaging helper for building.
#
# NOTE: indigo_gopath takes priority over GOPATH. This ensures that we
# build the code that we intended in case GOPATH points to another copy.
%make_build -C %{indigo_srcdir} -f %{indigo_srcdir}/packaging/snapd.mk \
            GOPATH=%{indigo_gopath}:$GOPATH SNAPD_DEFINES_DIR=%{_builddir} \
            all

%check
for binary in snap-exec snap-update-ns snapctl; do
    ldd $binary 2>&1 | grep 'not a dynamic executable'
done

%make_build -C %{indigo_srcdir}/cmd check
# Use the common packaging helper for testing.
%make_build -C %{indigo_srcdir} -f %{indigo_srcdir}/packaging/snapd.mk \
            GOPATH=%{indigo_gopath}:$GOPATH SNAPD_DEFINES_DIR=%{_builddir} \
            check

%install
# Install all systemd and dbus units, and env files.
%make_install -C %{indigo_srcdir}/data \
		BINDIR=%{_bindir} \
		LIBEXECDIR=%{_libexecdir} \
		DATADIR=%{_datadir} \
		SYSTEMDSYSTEMUNITDIR=%{_unitdir} \
		TMPFILESDIR=%{_tmpfilesdir} \
		SNAP_MOUNT_DIR=%{snap_mount_dir}
# Install all the C executables.
%make_install -C %{indigo_srcdir}/cmd
# Use the common packaging helper for bulk of installation.
%make_install -f %{indigo_srcdir}/packaging/snapd.mk \
            GOPATH=%{indigo_gopath}:$GOPATH SNAPD_DEFINES_DIR=%{_builddir} \
            install

# Undo special permissions of the void directory. We handle that in RPM files
# section below.
chmod 755 %{buildroot}%{_localstatedir}/lib/snapd/void

# Install local permissions policy for snap-confine. This should be removed
# once snap-confine is added to the permissions package. This is done following
# the recommendations on
# https://en.opensuse.org/openSUSE:Package_security_guidelines
install -m 644 -D %{indigo_srcdir}/packaging/opensuse/permissions %{buildroot}%{_sysconfdir}/permissions.d/snapd
install -m 644 -D %{indigo_srcdir}/packaging/opensuse/permissions.paranoid %{buildroot}%{_sysconfdir}/permissions.d/snapd.paranoid

# See https://en.opensuse.org/openSUSE:Packaging_checks#suse-missing-rclink for details
install -d %{buildroot}%{_sbindir}
ln -sf %{_sbindir}/service %{buildroot}%{_sbindir}/rcsnapd
ln -sf %{_sbindir}/service %{buildroot}%{_sbindir}/rcsnapd.seeded
%if %{with apparmor}
ln -sf %{_sbindir}/service %{buildroot}%{_sbindir}/rcsnapd.apparmor
%endif

# Install Polkit configuration.
# TODO: This should be handled by data makefile.
install -m 644 -D %{indigo_srcdir}/data/polkit/io.snapcraft.snapd.policy %{buildroot}%{_datadir}/polkit-1/actions

# Install the "info" data file with snapd version
# TODO: This should be handled by data makefile.
install -m 644 -D %{indigo_srcdir}/data/info %{buildroot}%{_libexecdir}/snapd/info

# Install bash completion for "snap"
# TODO: This should be handled by data makefile.
install -m 644 -D %{indigo_srcdir}/data/completion/bash/snap %{buildroot}%{_datadir}/bash-completion/completions/snap
install -m 644 -D %{indigo_srcdir}/data/completion/bash/complete.sh %{buildroot}%{_libexecdir}/snapd
install -m 644 -D %{indigo_srcdir}/data/completion/bash/etelpmoc.sh %{buildroot}%{_libexecdir}/snapd
# Install zsh completion for "snap"
install -d -p %{buildroot}%{_datadir}/zsh/site-functions
install -m 644 -D %{indigo_srcdir}/data/completion/zsh/_snap %{buildroot}%{_datadir}/zsh/site-functions/_snap

%verifyscript
%verify_permissions -e %{_libexecdir}/snapd/snap-confine

%pre
%service_add_pre %{systemd_services_list}

%post
%set_permissions %{_libexecdir}/snapd/snap-confine
%if %{with apparmor}
%apparmor_reload /etc/apparmor.d/%{apparmor_snapconfine_profile}
%endif
%service_add_post %{systemd_services_list}
%systemd_user_post %{systemd_user_services_list}
%if %{with apparmor}
if [ -x /usr/bin/systemctl ]; then
    if systemctl is-enabled snapd.service >/dev/null 2>&1 || systemctl is-enabled snapd.socket >/dev/null 2>&1; then
        # either the snapd.service or the snapd.socket are enabled, meaning snapd is
        # being actively used
        if systemctl is-enabled apparmor.service >/dev/null 2>&1 && ! systemctl is-enabled snapd.apparmor.service >/dev/null 2>&1; then
            # also apparmor appears to be enabled, but loading of apparmor profiles
            # of the snaps is not, so enable that now so that the snaps continue to
            # work after the update
            systemctl enable --now snapd.apparmor.service || :
        fi
    fi
fi
%endif

case ":$PATH:" in
    *:/snap/bin:*)
        ;;
    *)
        echo "Please reboot, logout/login or source /etc/profile to have /snap/bin added to PATH."
        echo "On a Tumbleweed and Leap 15.3+ systems you need to run: systemctl enable snapd.apparmor.service"
        ;;
esac

%preun
%service_del_preun %{systemd_services_list}
%systemd_user_preun %{systemd_user_services_list}
if [ $1 -eq 0 ]; then
    %{_libexecdir}/snapd/snap-mgmt --purge || :
fi

%postun
%service_del_postun %{systemd_services_list}
%systemd_user_postun %{systemd_user_services_list}

%files

# Configuration files
%config %{_sysconfdir}/permissions.d/snapd
%config %{_sysconfdir}/permissions.d/snapd.paranoid
%config %{_sysconfdir}/profile.d/snapd.sh

# Directories
%dir %attr(0111,root,root) %{_sharedstatedir}/snapd/void
%dir %{_datadir}/dbus-1
%dir %{_datadir}/dbus-1/services
%dir %{_datadir}/dbus-1/session.d
%dir %{_datadir}/dbus-1/system.d
%dir %{_datadir}/polkit-1
%dir %{_datadir}/polkit-1/actions
%dir %{_environmentdir}
%dir %{_libexecdir}/snapd
%dir %{_localstatedir}/cache/snapd
%dir %{_sharedstatedir}/snapd
%dir %{_sharedstatedir}/snapd/apparmor
%dir %{_sharedstatedir}/snapd/apparmor/profiles
%dir %{_sharedstatedir}/snapd/apparmor/snap-confine
%dir %{_sharedstatedir}/snapd/assertions
%dir %{_sharedstatedir}/snapd/cache
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
%dir %{_sharedstatedir}/snapd/sequence
%dir %{_sharedstatedir}/snapd/snaps
%dir %{_systemd_system_env_generator_dir}
%dir %{_tmpfilesdir}
%dir %{_systemdgeneratordir}
%dir %{_userunitdir}
%dir %{snap_mount_dir}
%dir %{snap_mount_dir}/bin
# this is typically owned by zsh, but we do not want to explicitly require zsh
%dir %{_datadir}/zsh
%dir %{_datadir}/zsh/site-functions
# similar case for fish
%dir %{_datadir}/fish
%dir %{_datadir}/fish/vendor_conf.d

# Ghost entries for things created at runtime
%ghost %dir %{_localstatedir}/snap
%ghost %{_localstatedir}/cache/snapd/commands
%ghost %{_localstatedir}/cache/snapd/names
%ghost %{_localstatedir}/cache/snapd/sections
%ghost %{_sharedstatedir}/snapd/seccomp/bpf/global.bin
%ghost %{_sharedstatedir}/snapd/state.json
%ghost %{_sharedstatedir}/snapd/system-key
%ghost %{snap_mount_dir}/README
%verify(not user group mode) %attr(04755,root,root) %{_libexecdir}/snapd/snap-confine
%{_bindir}/snap
%{_bindir}/snapctl
%{_datadir}/applications/io.snapcraft.SessionAgent.desktop
%{_datadir}/applications/snap-handle-link.desktop
%{_datadir}/bash-completion/completions/snap
%{_datadir}/zsh/site-functions/_snap
%{_datadir}/dbus-1/services/io.snapcraft.Launcher.service
%{_datadir}/dbus-1/services/io.snapcraft.SessionAgent.service
%{_datadir}/dbus-1/services/io.snapcraft.Settings.service
%{_datadir}/dbus-1/session.d/snapd.session-services.conf
%{_datadir}/dbus-1/system.d/snapd.system-services.conf
%{_datadir}/polkit-1/actions/io.snapcraft.snapd.policy
%{_datadir}/fish/vendor_conf.d/snapd.fish
%{_datadir}/snapd/snapcraft-logo-bird.svg
%{_environmentdir}/990-snapd.conf
%{_libexecdir}/snapd/complete.sh
%{_libexecdir}/snapd/etelpmoc.sh
%{_libexecdir}/snapd/info
%{_libexecdir}/snapd/snap-device-helper
%{_libexecdir}/snapd/snap-discard-ns
%{_libexecdir}/snapd/snap-exec
%{_libexecdir}/snapd/snap-gdb-shim
%{_libexecdir}/snapd/snap-gdbserver-shim
%{_libexecdir}/snapd/snap-mgmt
%{_libexecdir}/snapd/snap-seccomp
%{_libexecdir}/snapd/snap-update-ns
%{_libexecdir}/snapd/snapctl
%{_libexecdir}/snapd/snapd
%{_libexecdir}/snapd/snapd.run-from-snap
%{_mandir}/man8/snap-confine.8*
%{_mandir}/man8/snap-discard-ns.8*
%{_mandir}/man8/snap.8*
%{_mandir}/man8/snapd-env-generator.8*
%{_sbindir}/rcsnapd
%{_sbindir}/rcsnapd.seeded
%{_sysconfdir}/xdg/autostart/snap-userd-autostart.desktop
%{_systemd_system_env_generator_dir}/snapd-env-generator
%{_systemdgeneratordir}/snapd-generator
%{_tmpfilesdir}/snapd.conf
%{_unitdir}/snapd.failure.service
%{_unitdir}/snapd.seeded.service
%{_unitdir}/snapd.service
%{_unitdir}/snapd.socket
%{_unitdir}/snapd.mounts.target
%{_unitdir}/snapd.mounts-pre.target
%{_userunitdir}/snapd.session-agent.service
%{_userunitdir}/snapd.session-agent.socket

# When apparmor is enabled there are some additional entries.
%if %{with apparmor}
%config %{_sysconfdir}/apparmor.d
%{_libexecdir}/snapd/snapd-apparmor
%{_sbindir}/rcsnapd.apparmor
%{_sysconfdir}/apparmor.d/%{apparmor_snapconfine_profile}
%{_unitdir}/snapd.apparmor.service
%endif

%changelog
