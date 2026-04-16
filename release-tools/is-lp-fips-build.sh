#!/bin/sh

set -x
set -e

# check for triggers that indicate a build on LP of the snapd-fips snap
# recipe https://launchpad.net/~fips-cc-stig/fips-cc-stig/+snap/snapd-fips
#
# - git origin https://git.launchpad.net/~snappy-dev/snapd/+git/snapd
# - openssl-fips-module-3 package is available (meaning FIPS PPA is present)
#
# TODO: we really need knobs on LP/snapcraft side to make this explicit

if ! git remote get-url origin | grep "git.launchpad.net" >&2 ; then
    echo "false"
    exit 0
fi

if apt show openssl-fips-module-3 > /dev/null; then
    echo ":: openssl-fips-module-3 package is available" >&2
    # when building with Pro FIPS Updates PPA
    # https://launchpad.net/~ubuntu-advantage/+archive/ubuntu/pro-fips-updates, the
    # openssl-fips-module-3 package will be available
    echo "true"
    exit 0
fi

# TODO check if a FIPS PPA is enabled?

echo "false"
