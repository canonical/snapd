#!/bin/bash

set -euxo pipefail

version="$1"

if not tests.info is-reexec-in-use; then
    snap version --verbose | MATCH "snapd-bin-from.*native-package$"
    snap version --verbose | MATCH "snap-bin-from.*native-package$"
else
    snap version --verbose | MATCH "snapd-bin-from.*snap$"
    snap version --verbose | MATCH "snap-bin-from.*snap$"
fi

if not tests.info is-reexec-in-use && tests.info is-snapd-from-archive; then
    . /etc/os-release
    version_id_re="${VERSION_ID//./\\.}"
    snap version | grep -E '^snap[[:space:]]' | tee /dev/stderr | MATCH "^snap[[:space:]]+[0-9]+[.][0-9]+([.][0-9]+)?([.][0-9]+)?[+]ubuntu${version_id_re}([.][0-9]+)?$"
    snap version | grep -E '^snapd[[:space:]]' | tee /dev/stderr | MATCH "^snapd[[:space:]]+[0-9]+[.][0-9]+([.][0-9]+)?([.][0-9]+)?[+]ubuntu${version_id_re}([.][0-9]+)?$"
fi

snap version | grep -E '^snap[[:space:]]' | tee /dev/stderr | grep -qF "$version"
snap version | grep -E '^snapd[[:space:]]' | tee /dev/stderr | grep -qF "$version"