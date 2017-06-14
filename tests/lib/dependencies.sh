#!/bin/bash

# shellcheck source=tests/lib/pkgdb.sh
. "$TESTSLIB/pkgdb.sh"

export DEPENDENCY_PACKAGES=

add_pkgs(){
  DEPENDENCY_PACKAGES="$DEPENDENCY_PACKAGES $@"
}

get_dependency_ubuntu_packages(){
  add_pkgs automake
  add_pkgs autotools-dev
  add_pkgs dbus-x11
  add_pkgs gccgo-6
  add_pkgs indent
  add_pkgs jq
  add_pkgs kpartx
  add_pkgs libapparmor-dev
  add_pkgs libglib2.0-dev
  add_pkgs libseccomp-dev
  add_pkgs libvirt-bin
  add_pkgs libudev-dev
  add_pkgs linux-image-extra-$(uname -r)
  add_pkgs man
  add_pkgs pkg-config
  add_pkgs pollinate rng-tools
  add_pkgs python3-docutils
  add_pkgs python3-yaml
  add_pkgs udev
  add_pkgs upower 
  add_pkgs x11-utils 
  add_pkgs xvfb
  case "$SPREAD_SYSTEM" in
      ubuntu-14.04-*)
          add_pkgs cups-pdf
          ;;
      *)
          add_pkgs printer-driver-cups-pdf
          ;;
  esac 
}

get_dependency_fedora_packages(){
  echo "Fedora dependencies not ready yet"
}

get_dependency_opensuse_packages(){
  echo "Opensuse dependencies not ready yet"
}

get_dependency_packages(){
  case "$SPREAD_SYSTEM" in
      ubuntu-*|debian-*)
          get_dependency_ubuntu_packages
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

install_dependencies(){
  get_dependency_packages
  echo "Installing the following packages: $DEPENDENCY_PACKAGES"
  distro_install_package $DEPENDENCY_PACKAGES  
}
