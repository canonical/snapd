#!/bin/bash

set -e

snapd_dir=$1
user=$2
buildopt=$3

cd "$snapd_dir"
go mod vendor
su -c "cd $snapd_dir/c-vendor && ./vendor.sh" "$user"

newver="$(dpkg-parsechangelog --show-field Version)"
dch --newversion "1337.$newver" "testing build"
unshare -n -- \
    su -l -c "cd $snapd_dir && DEB_BUILD_OPTIONS='nocheck testkeys ${buildopt}' dpkg-buildpackage -tc -b -Zgzip -uc -us" "$user"
