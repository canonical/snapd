%global commit0 6331fd4a058271b0246714bc6746ab1e7ce2aa09
%global shortcommit0 %(c=%{commit0}; echo ${c:0:7})
%global snapdate 20161101

Name:           snappy-selinux
Version:        0
Release:        1.git%{snapdate}.%{shortcommit0}%{?dist}
Summary:        SELinux module for snappy

License:        GPLv2+
URL:            https://gitlab.com/Conan_Kudo/snapcore-selinux
Source0:        %{url}/repository/archive.tar.gz?ref=%{commit0}#/%{name}-%{shortcommit0}.tar.gz
BuildArch:      noarch
BuildRequires:  selinux-policy, selinux-policy-devel
Requires(post): selinux-policy-base >= %{_selinux_policy_version}
Requires(post): policycoreutils
Requires(post): policycoreutils-python-utils
Requires(pre):  libselinux-utils
Requires(post): libselinux-utils

%description
This package provides the SELinux policy module
to ensure snapd runs properly under an environment
with SELinux enabled.

%prep
%setup -q -n snapcore-selinux-%{commit0}-%{commit0}


%build
make SHARE="%{_datadir}" TARGETS="snappy"

%install
# Install snappy interfaces
install -d %{buildroot}%{_datadir}/selinux/devel/include/contrib
install -pm 0644 snappy.if %{buildroot}%{_datadir}/selinux/devel/include/contrib

# Install snappy selinux module
install -d %{buildroot}%{_datadir}/selinux/packages
install -m 0644 snappy.pp.bz2 %{buildroot}%{_datadir}/selinux/packages

%pre
%selinux_relabel_pre

%post
%selinux_modules_install %{_datadir}/selinux/packages/snappy.pp.bz2
%selinux_relabel_post

%postun
%selinux_modules_uninstall snappy
if [ $1 -eq 0 ]; then
    %selinux_relabel_post
fi

%files
%license COPYING
%doc README.md
%{_datadir}/selinux/packages/snappy.pp.bz2
%{_datadir}/selinux/devel/include/contrib/snappy.if


%changelog
* Mon Oct 17 2016 Neal Gompa <ngompa13@gmail.com> - 0-1.git20161017
- Initial packaging
