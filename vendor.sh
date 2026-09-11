#!/bin/bash

set -eu

go -C "${MESON_DIST_ROOT}" mod vendor

(cd "${MESON_DIST_ROOT}/c-vendor" && ./vendor.sh)
