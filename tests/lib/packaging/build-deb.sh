#!/bin/bash

set -e


usage() {
    echo "Usage: $0 --user <user> --pkg-version <pkg_version>"
    exit 1
}

user=""
pkg_version=""

# Parse arguments
while [[ $# -gt 0 ]]; do
    case "$1" in
        --user)
            user="$2"
            shift 2
            ;;
        --pkg-version)
            pkg_version="$2"
            shift 2
            ;;
        *)
            echo "Unknown option: $1"
            usage
            ;;
    esac
done

# Check required arguments
if [ -z "$user" ] || [ -z "$pkg_version" ]; then
    usage
fi

snapd_dir=$(pwd)

dch --newversion "$pkg_version" "testing build"
unshare -n -- \
    su -l -c "cd $snapd_dir && DEB_BUILD_OPTIONS='nocheck testkeys' dpkg-buildpackage -tc -b -Zgzip -uc -us" "$user"
