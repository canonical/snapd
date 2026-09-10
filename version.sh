#!/bin/bash

set -eu

case "${1}" in
     get)
         if ! [ -f "${MESON_SOURCE_ROOT}/cmd/VERSION" ]; then
             (cd "${MESON_SOURCE_ROOT}"; ./mkversion.sh)
         fi
         cat "${MESON_SOURCE_ROOT}/cmd/VERSION"
         ;;
     set)
         echo "${2}" >"${MESON_DIST_ROOT}/cmd/VERSION"
         ;;
     *)
         exit 1
         ;;
esac
