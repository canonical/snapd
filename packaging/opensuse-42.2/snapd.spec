# spec file for package snapd
#
# Copyright (c) 2017 Zygmunt Krynicki <zygmunt.krynicki@canonical.com>
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

%global provider        github
%global provider_tld    com
%global project         snapcore
%global repo            snapd
%global provider_prefix %{provider}.%{provider_tld}/%{project}/%{repo}
%global import_path     %{provider_prefix}

%global with_test_keys  0

%if %{with testkeys}
%global with_test_keys 1
%else
%global with_test_keys 0
%endif

%define systemd_services_list snapd.refresh.timer snapd.refresh.service snapd.socket snapd.service snapd.autoimport.service snapd.system-shutdown.service
Name:           snapd
Version:        2.27
Release:        1
Summary:        Tools enabling systems to work with .snap files
License:        GPL-3.0
Group:          System/Packages
Url:            https://%{import_path}
Source0:        https://github.com/snapcore/snapd/releases/download/%{version}/%{name}_%{version}.vendor.tar.xz
Source1:        snapd-rpmlintrc
# TODO: make this enabled only on Leap 42.2+
# BuildRequires:  ShellCheck
BuildRequires:  autoconf
BuildRequires:  automake
BuildRequires:  glib2-devel
BuildRequires:  glibc-devel-static
BuildRequires:  golang-packaging
BuildRequires:  gpg2
BuildRequires:  indent
BuildRequires:  libcap-devel
BuildRequires:  libseccomp-devel
BuildRequires:  libtool
BuildRequires:  libudev-devel
BuildRequires:  libuuid-devel
BuildRequires:  make
BuildRequires:  pkg-config
BuildRequires:  python-docutils
BuildRequires:  python3-docutils
BuildRequires:  squashfs
BuildRequires:  openssh
BuildRequires:  timezone
BuildRequires:  udev
BuildRequires:  xfsprogs-devel
BuildRequires:  xz

# Make sure we are on Leap 42.2/SLE 12 SP2 or higher
%if 0%{?sle_version} >= 120200
BuildRequires: systemd-rpm-macros
%endif

PreReq:         permissions

Requires(post): permissions
Requires:       gpg2
Requires:       openssh
Requires:       squashfs

%systemd_requires

BuildRoot:      %{_tmppath}/%{name}-%{version}-build

# TODO strip the C executables but don't strip the go executables
# as that breaks the world in some ways.
# reenable {go_nostrip}
%{go_provides}

%description
This package contains that snapd daemon and the snap command line tool.
Together they can be used to install, refresh (update), remove and configure
snap packages on a system. Snap packages are a novel format based on simple
principles. Bundle your dependencies, run in a predictable environment, use
moder kernel features for setting up the execution environment and security.
The same binary snap package can be installed and used on many diverse systems
such as Debian, Fedora and OpenSUSE as well as their multiple derivatives.
.
This package contains the official build, endorsed by snapd developers. It is
updated as soon as new upstream releases are made and is designed to live in
the system:snappy repository.

%prep
%setup -q -n %{name}-%{version}

# Set the version that is compiled into the various executables
./mkversion.sh %{version}-%{release}

# Generate autotools build system files
cd cmd && autoreconf -i -f

# Enable hardening; We can't use -pie here as this conflicts with
# our build of static binaries for snap-confine. Also see
# https://bugzilla.redhat.com/show_bug.cgi?id=1343892
CFLAGS="$RPM_OPT_FLAGS -fPIC -Wl,-z,relro -Wl,-z,now"
CXXFLAGS="$RPM_OPT_FLAGS -fPIC -Wl,-z,relro -Wl,-z,now"
export CFLAGS
export CXXFLAGS

# NOTE: until snapd and snap-confine have the improved communication mechanism
# we need to disable apparmor as snapd doesn't yet support the version of
# apparmor kernel available in SUSE and Debian. The generated apparmor profiles
# cannot be loaded into a vanilla kernel. As a temporary measure we just switch
# it all off.
%configure --disable-apparmor --libexecdir=/usr/lib/snapd

%build
# Build golang executables
%goprep %{import_path}

%if 0%{?with_test_keys}
# The %gobuild macro doesn't allow us to pass any additional parameters
# so we we have to invoke `go install` here manually.
export GOPATH=%{_builddir}/go:%{_libdir}/go/contrib
export GOBIN=%{_builddir}/go/bin
# Options used are the same as the %gobuild macro does but as it
# doesn't allow us to amend new flags we have to repeat them here:
# -s: tell long running tests to shorten their build time
# -v: be verbose
# -p 4: allow parallel execution of tests
# -x: print commands
go install -s -v -p 4 -x -tags withtestkeys github.com/snapcore/snapd/cmd/snapd
%else
%gobuild cmd/snapd
%endif

%gobuild cmd/snap
%gobuild cmd/snap-exec
%gobuild cmd/snapctl
%gobuild cmd/snap-update-ns

# This is ok because snap-seccomp only requires static linking when it runs from the core-snap via re-exec.
sed -e "s/-Bstatic -lseccomp/-Bstatic/g" -i %{_builddir}/go/src/%{provider_prefix}/cmd/snap-seccomp/main.go
# build snap-seccomp
%gobuild cmd/snap-seccomp

# Build C executables
make %{?_smp_mflags} -C cmd

%check
%{gotest} %{import_path}/...
make %{?_smp_mflags} -C cmd check

%install
# Install all the go stuff
%goinstall
# TODO: instead of removing it move this to a dedicated golang package
rm -rf %{buildroot}/usr/lib64/go
rm -rf %{buildroot}/usr/lib/go
find %{buildroot}
# Move snapd, snap-exec, snap-seccomp and snap-update-ns into /usr/lib/snapd
install -m 755 -d %{buildroot}/usr/lib/snapd
mv %{buildroot}/usr/bin/snapd %{buildroot}/usr/lib/snapd/snapd
mv %{buildroot}/usr/bin/snap-exec %{buildroot}/usr/lib/snapd/snap-exec
mv %{buildroot}/usr/bin/snap-update-ns %{buildroot}/usr/lib/snapd/snap-update-ns
mv %{buildroot}/usr/bin/snap-seccomp %{buildroot}/usr/lib/snapd/snap-seccomp
# Install profile.d-based PATH integration for /snap/bin
install -m 755 -d %{buildroot}/etc/profile.d/
install -m 644 etc/profile.d/apps-bin-path.sh %{buildroot}/etc/profile.d/snapd.sh

# Generate and install man page for snap command
install -m 755 -d %{buildroot}%{_mandir}/man1
%{buildroot}/usr/bin/snap help --man >  %{buildroot}%{_mandir}/man1/snap.1

# TODO: enable gosrc
# TODO: enable gofilelist

# Install all the C executables
%{make_install} -C cmd
# Undo special permissions of the void directory
chmod 755 %{?buildroot}/var/lib/snapd/void
# Remove traces of ubuntu-core-launcher. It is a phased-out executable that is
# still partially present in the tree but should be removed in the subsequent
# release.
rm -f %{?buildroot}/usr/bin/ubuntu-core-launcher
# NOTE: we don't want to ship system-shutdown helper, it is just a helper on
# ubuntu-core systems that exclusively use snaps. It is used during the
# shutdown process and thus can be left out of the distribution package.
rm -f %{?buildroot}/usr/lib/snapd/system-shutdown
# Install the directories that snapd creates by itself so that they can be a part of the package
install -d %buildroot/var/lib/snapd/{assertions,desktop/applications,device,hostfs,mount,apparmor/profiles,seccomp/bpf,snaps}
install -d %buildroot/snap/bin
# Install local permissions policy for snap-confine. This should be removed
# once snap-confine is added to the permissions package. This is done following
# the recommendations on
# https://en.opensuse.org/openSUSE:Package_security_guidelines
install -m 644 -D packaging/opensuse-42.2/permissions %buildroot/%{_sysconfdir}/permissions.d/snapd
install -m 644 -D packaging/opensuse-42.2/permissions.paranoid %buildroot/%{_sysconfdir}/permissions.d/snapd.paranoid
# Install the systemd units
make -C data/systemd install DESTDIR=%{buildroot} SYSTEMDSYSTEMUNITDIR=%{_unitdir}
for s in snapd.autoimport.service snapd.system-shutdown.service snap-repair.timer snap-repair.service snapd.core-fixup.service; do
    rm -f %buildroot/%{_unitdir}/$s
done
# Remove snappy core specific scripts
rm -f %buildroot/usr/lib/snapd/snapd.core-fixup.sh

# See https://en.opensuse.org/openSUSE:Packaging_checks#suse-missing-rclink for details
install -d %{buildroot}/usr/sbin
ln -sf %{_sbindir}/service %{buildroot}/%{_sbindir}/rcsnapd
ln -sf %{_sbindir}/service %{buildroot}/%{_sbindir}/rcsnapd.refresh
# Install the "info" data file with snapd version
install -m 644 -D data/info %{buildroot}/usr/lib/snapd/info
# Install bash completion for "snap"
install -m 644 -D data/completion/snap %{buildroot}/usr/share/bash-completion/completions/snap

%verifyscript
%verify_permissions -e /usr/lib/snapd/snap-confine

%pre
%service_add_pre %{systemd_services_list}

%post
%set_permissions /usr/lib/snapd/snap-confine
%service_add_post %{systemd_services_list}
case ":$PATH:" in
    *:/snap/bin:*)
        ;;
    *)
        echo "Please reboot, logout/login or source /etc/profile to have /snap/bin added to PATH."
        ;;
esac

%preun
%service_del_preun %{systemd_services_list}

%postun
%service_del_postun %{systemd_services_list}

%files
%defattr(-,root,root)
%config %{_sysconfdir}/permissions.d/snapd
%config %{_sysconfdir}/permissions.d/snapd.paranoid
%config %{_sysconfdir}/profile.d/snapd.sh
%dir %attr(0000,root,root) /var/lib/snapd/void
%dir /snap
%dir /snap/bin
%dir /usr/lib/snapd
%dir /var/lib/snapd
%dir /var/lib/snapd/apparmor
%dir /var/lib/snapd/apparmor/profiles
%dir /var/lib/snapd/assertions
%dir /var/lib/snapd/desktop
%dir /var/lib/snapd/desktop/applications
%dir /var/lib/snapd/device
%dir /var/lib/snapd/hostfs
%dir /var/lib/snapd/mount
%dir /var/lib/snapd/seccomp
%dir /var/lib/snapd/seccomp/bpf
%dir /var/lib/snapd/snaps
%verify(not user group mode) %attr(04755,root,root) /usr/lib/snapd/snap-confine
%{_mandir}/man5/snap-confine.5.gz
%{_mandir}/man5/snap-discard-ns.5.gz
%{_udevrulesdir}/80-snappy-assign.rules
%{_unitdir}/snapd.refresh.service
%{_unitdir}/snapd.refresh.timer
%{_unitdir}/snapd.service
%{_unitdir}/snapd.socket
/usr/bin/snap
/usr/bin/snapctl
/usr/sbin/rcsnapd
/usr/sbin/rcsnapd.refresh
/usr/lib/snapd/info
/usr/lib/snapd/snap-discard-ns
/usr/lib/snapd/snap-update-ns
/usr/lib/snapd/snap-exec
/usr/lib/snapd/snap-seccomp
/usr/lib/snapd/snapd
/usr/lib/udev/snappy-app-dev
/usr/share/bash-completion/completions/snap
%{_mandir}/man1/snap.1.gz

%changelog

