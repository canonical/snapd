#!/bin/bash

set -eu

rootfs="$1"

mkdir -p "${rootfs}/etc/systemd/system/snapd.service.d"
cat <<EOF >"${rootfs}/etc/systemd/system/snapd.service.d/snapd-override.conf"
[Service]
Environment=SNAPPY_FORCE_API_URL=http://10.0.2.2:11028
Environment=SNAPD_DEBUG=1
Environment=SNAPPY_USE_STAGING_STORE=0
Environment=SNAPPY_TESTING=1
EOF
