#!/bin/bash

set -eu

setcap -q - "${DESTDIR-}${1}/snap-confine" <"${MESON_SOURCE_ROOT}/cmd/snap-confine/snap-confine.caps"
