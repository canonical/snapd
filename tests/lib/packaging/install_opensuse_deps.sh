#!/bin/bash

pkgs="
python3 
apparmor-profiles 
audit 
bash-completion 
bpftool 
clang 
curl 
dbus-1-python3 
evolution-data-server 
expect 
fish 
fontconfig 
fwupd 
git 
golang-packaging 
iptables 
jq 
lsb-release 
lsof 
man 
man-pages 
nfs-kernel-server 
nss-mdns 
osc 
PackageKit 
procps 
python3-PyYAML 
strace 
sysvinit-tools 
netcat-openbsd 
rpm-build 
udisks2 
upower 
uuidd 
xdg-user-dirs 
xdg-utils 
zsh 
libcap-progs
"

pkgs="
rpm-build
rpmdevtools
awk
"

zypper --gpg-auto-import-keys refresh
zypper install -y --allow-downgrade --force-resolution --no-recommends $pkgs

rpm_dir=$(rpm --eval "%_topdir")
base_version="$(head -1 debian/changelog | awk -F '[()]' '{print $2}')"
version="1337.$base_version"
packaging_path=packaging/opensuse-15.6
sed -i -e "s/^Version:.*$/Version: $version/g" "$packaging_path/snapd.spec"
mkdir -p "$rpm_dir/SOURCES"
cp "$packaging_path"/* "$rpm_dir/SOURCES/"
pack_args=-s
./packaging/pack-source -v "$version" -o "$rpm_dir/SOURCES" $pack_args
rm -rf "$rpm_dir"/BUILD/*
zypper install -y --allow-downgrade --force-resolution --no-recommends $(rpmspec -q --buildrequires "$packaging_path/snapd.spec")
go mod vendor
rpmbuild --with testkeys -bs "$rpm_dir/SOURCES/snapd.spec"
rpmbuild \
    --with testkeys \
    --nocheck \
    -ba \
    "$rpm_dir/SOURCES/snapd.spec"

# find "$rpm_dir"/RPMS -name '*.rpm' -exec cp -v {} "${GOPATH%%:*}" \;