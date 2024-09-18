#!/bin/sh

set -x
set -e

# check for triggers that indicate a build on LP of the snapd-fips snap
# recipe https://launchpad.net/~fips-cc-stig/fips-cc-stig/+snap/snapd-fips
#
# - git origin https://git.launchpad.net/~snappy-dev/snapd/+git/snapd
# - build path: /build/snapd-fips

if ! git remote get-url origin | grep "git.launchpad.net" >&2 ; then
    echo "false"
    exit 0
fi

# when building from https://launchpad.net/~fips-cc-stig/fips-cc-stig/+snap/snapd-fips
# recipe, the code is cloned at /build/snapd-fips/
if ! echo "$PWD" | grep '^/build/snapd-fips/' >&2 ; then
    echo "false"
    exit 0
fi

# TODO check if a FIPS PPA is enabled?

echo "true"
