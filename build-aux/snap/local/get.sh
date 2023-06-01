#!/bin/bash

set -eu

craftctl default
selected_branch=$(git -C "${CRAFT_PART_SRC}" branch --show-current)

case "${selected_branch}" in
    applied/ubuntu/jammy-updates|ubuntu/jammy-updates)
    ;;
    applied/ubuntu/jammy|ubuntu/jammy)
        if git -C "${CRAFT_PART_SRC}"  show-ref --quiet refs/remotes/origin/"${selected_branch}-updates"; then
            echo "We should use ${selected_branch}-updates instead" 1>&2
            exit 1
        fi
    ;;
    *)
    ;;
esac

case "${selected_branch}" in
    applied/*)
        ;;
    *)
        (cd "${CRAFT_PART_SRC}"; QUILT_PATCHES=debian/patches quilt push -a)
        ;;
esac
