#!/bin/sh

set -e

# XXX: provide a nice declarative format
# XXX2: reuse vendor.json and write some python3 code?
if [ ! -d ./squashfuse ]; then
    git clone https://github.com/vasi/squashfuse
fi

# This is the commit that was tagged as 0.5.0, released on September 2023:
# https://github.com/vasi/squashfuse/releases/tag/v0.5.0
# It contains bug fixes and enables multithreading support to squashfuse_ll
# by default.
# It still should work with both "libfuse-dev" and "libfuse3-dev" which
# is important as 16.04 only has libfuse-dev and 21.10 only has libfuse3-dev
SQUASHFUSE_REF=3f4dd2928ab362f8b20eab2be864d8e622472df5

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

