#!/bin/sh

set -e

# XXX: provide a nice declarative format
# XXX2: reuse vendor.json and write some python3 code?
if [ ! -d ./squashfuse ]; then
    git clone https://github.com/vasi/squashfuse
fi

# This is the commit that was tagged as 0.5.2, released on 22 February 2024:
# https://github.com/vasi/squashfuse/releases/tag/0.5.2
# It contains bug fixes and enables multithreading support to squashfuse_ll
# by default.
# It still should work with both "libfuse-dev" and "libfuse3-dev" which
# is important as 16.04 only has libfuse-dev and 21.10 only has libfuse3-dev
SQUASHFUSE_REF=775b4cc72ab47641637897f11ce0da15d5c1f115

if [ -d ./squashfuse/.git ]; then
		cd squashfuse

		# shellcheck disable=SC1083
		if ! git rev-parse --verify $SQUASHFUSE_REF^{commit}; then
			# if the pinned commit isn't known, update the repo
			git checkout master
			git pull
		fi

		git checkout "$SQUASHFUSE_REF"
fi

