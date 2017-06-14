#!/bin/bash

# shellcheck source=tests/lib/pkgdb.sh
. "$TESTSLIB/pkgdb.sh"

export DEPENDENCY_PACKAGES=
export DISTRO_BUILD_DEPS=

add_pkgs(){
  DEPENDENCY_PACKAGES="$DEPENDENCY_PACKAGES $@"
}

get_apt_dependencies_generic(){
  add_pkgs autoconf
  add_pkgs automake
  add_pkgs autotools-dev
  add_pkgs build-essential
  add_pkgs indent
  add_pkgs libapparmor-dev
  add_pkgs libglib2.0-dev
  add_pkgs libseccomp-dev
  add_pkgs libudev-dev
  add_pkgs pkg-config
  add_pkgs python3-docutils
  add_pkgs udev
}

get_apt_dependencies_classic(){
  add_pkgs dbus-x11
  add_pkgs jq
  add_pkgs man
  add_pkgs python3-yaml
  add_pkgs rng-tools
  add_pkgs upower

  case "$SPREAD_SYSTEM" in
      ubuntu-14.04-*)
          add_pkgs cups-pdf
          add_pkgs linux-image-extra-$(uname -r)
          add_pkgs pollinate
          ;;
      ubuntu-16.04-32)
          add_pkgs linux-image-extra-$(uname -r)
          add_pkgs pollinate
          add_pkgs printer-driver-cups-pdf
          ;;
      ubuntu-16.04-64)
          add_pkgs gccgo-6
          add_pkgs kpartx
          add_pkgs libvirt-bin
          add_pkgs linux-image-extra-$(uname -r)
          add_pkgs pollinate
          add_pkgs printer-driver-cups-pdf
          add_pkgs qemu
          add_pkgs x11-utils
          add_pkgs xvfb
          ;;
      debian-*)
          add_pkgs printer-driver-cups-pdf
          ;;
  esac 
}

get_apt_dependencies_core(){
  add_pkgs linux-image-extra-$(uname -r)
  add_pkgs pollinate
}

get_dependency_fedora_packages(){
  echo "Fedora dependencies not ready yet"
}

get_dependency_opensuse_packages(){
  echo "Opensuse dependencies not ready yet"
}

get_test_dependencies(){
  case "$SPREAD_SYSTEM" in
      ubuntu-16.04-*|ubuntu-14.04-64|debian-*)
          get_apt_dependencies_generic
          get_apt_dependencies_classic
          ;;
      ubuntu-core-16-*)
          get_apt_dependencies_generic
          get_apt_dependencies_core
          ;;
      fedora-*)
          get_dependency_fedora_packages
          ;;
      opensuse-*)
          get_dependency_opensuse_packages
          ;;
      *)
          echo "ERROR: Unsupported distribution $SPREAD_SYSTEM"
          exit 1
          ;;
  esac  
}

get_build_dependencies(){
  case "$SPREAD_SYSTEM" in
    debian-*|ubuntu-*)
        DISTRO_BUILD_DEPS="build-essential curl devscripts expect gdebi-core jq rng-tools git netcat-openbsd"
        ;;
    fedora-*)
        DISTRO_BUILD_DEPS="mock git expect curl golang rpm-build redhat-lsb-core"
        ;;
    opensuse-*)
        DISTRO_BUILD_DEPS="osc git expect curl golang-packaging lsb-release netcat-openbsd jq rng-tools"
        ;;
    *)
        ;;
  esac
}

install_build_dependencies(){
  # Specify necessary packages which need to be installed on a
  # system to provide a basic build environment for snapd.
  get_build_dependencies
  echo "Installing the following packages: $DISTRO_BUILD_DEPS"
  distro_install_package $DISTRO_BUILD_DEPS
}

install_test_dependencies(){
  get_test_dependencies
  echo "Installing the following packages: $DEPENDENCY_PACKAGES"
  distro_install_package $DEPENDENCY_PACKAGES
}
