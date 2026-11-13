#!/bin/bash

set -eu

env -C "${MESON_DIST_ROOT}" go mod vendor
env -C "${MESON_DIST_ROOT}/c-vendor"  ./vendor.sh
