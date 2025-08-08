#!/bin/bash

set -e

usage() {
    echo "Usage: $0 --user <user> --build-dir <build_dir>"
    exit 1
}

user=""
build_dir=""

# Parse arguments
while [[ $# -gt 0 ]]; do
    case "$1" in
        --user)
            user="$2"
            shift 2
            ;;
        --build-dir)
            build_dir="$2"
            shift 2
            ;;
        *)
            echo "Unknown option: $1"
            usage
            ;;
    esac
done

# Check required arguments
if [ -z "$user" ] || [ -z "$build_dir" ]; then
    usage
fi

cp -av packaging/arch/* "$build_dir"
chown -R "$user":"$user" "$build_dir"
unshare -n -- \
        su -l -c "cd $build_dir && WITH_TEST_KEYS=1 makepkg -f --nocheck" "$user"