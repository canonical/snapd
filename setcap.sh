#!/bin/bash

set -eu

setcap -q - "${MESON_INSTALL_DESTDIR_PREFIX}/usr/lib/snapd/snap-confine" <"${MESON_SOURCE_ROOT}/cmd/snap-confine/snap-confine.caps"
