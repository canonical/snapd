#!/bin/sh

set -x
set -e

# check for triggers that indicate a build on LP of the snapd-fips snap
# recipe https://launchpad.net/~fips-cc-stig/fips-cc-stig/+snap/snapd-fips
#
# - git origin https://git.launchpad.net/~snappy-dev/snapd/+git/snapd
# - one of the following is true:
#   - build path: /build/snapd-fips (but not always)
#   - openssl-fips-module-3 package is available (meaning FIPS PPA is present)
#
# TODO: we really need knobs on LP/snapcraft side to make this explicit

if ! git remote get-url origin | grep "git.launchpad.net" >&2 ; then
    echo "false"
    exit 0
fi

# LP has no means of injecting additional configuration to the snap build, so
# try a couple of simple checks that let us identify whether we are building a
# FIPS target
if echo "$PWD" | grep '^/build/snapd-fips/' >&2 ; then
    # when building from
    # https://launchpad.net/~fips-cc-stig/fips-cc-stig/+snap/snapd-fips recipe, the
    # code is cloned at /build/snapd-fips/. However if the build is published to the
    # store using 'snapd' as the store package name, LP clones the source tree into
    # directory named 'snapd', so the check is not always successful.
    echo "true"
    exit 0
elif apt show openssl-fips-module-3 > /dev/null; then
    echo ":: openssl-fips-module-3 package is available" >&2
    # when building with Pro FIPS Updates PPA
    # https://launchpad.net/~ubuntu-advantage/+archive/ubuntu/pro-fips-updates, the
    # openssl-fips-module-3 package will be available
    echo "true"
    exit 0
fi

# TODO check if a FIPS PPA is enabled?

echo "false"
