#!/bin/bash

set -e

DISTRO=$(dpkg-parsechangelog --show-field=Distribution)
export DEB_BUILD_PROFILES="${DISTRO}"
exec dpkg-buildpackage "${@}"
