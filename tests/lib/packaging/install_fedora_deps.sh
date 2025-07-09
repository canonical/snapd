pkgs="
rpm-build
rpmdevtools
"

dnf makecache
dnf -y --refresh install --setopt=install_weak_deps=False $pkgs

rpm_dir=$(rpm --eval "%_topdir")
base_version="$(head -1 debian/changelog | awk -F '[()]' '{print $2}')"
version="1337.$base_version"
packaging_path=packaging/fedora-42
sed -i -e "s/^Version:.*$/Version: $version/g" "$packaging_path/snapd.spec"
dnf -y --refresh install --setopt=install_weak_deps=False $(rpmspec -q --buildrequires "$packaging_path/snapd.spec")
mkdir -p "$rpm_dir/SOURCES"
cp "$packaging_path"/* "$rpm_dir/SOURCES/"
go mod vendor
./packaging/pack-source -v "$version" -o "$rpm_dir/SOURCES"
rm -rf "$rpm_dir"/BUILD/*
rpmbuild --with testkeys -bs "$rpm_dir/SOURCES/snapd.spec"
rpmbuild \
    --with testkeys \
    --nocheck \
    -ba \
    "$rpm_dir/SOURCES/snapd.spec"