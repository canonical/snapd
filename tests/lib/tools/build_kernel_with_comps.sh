#!/bin/bash

set -uxe

# Modify kernel and create a component
build_kernel_with_comp() {
  # shellcheck source=tests/lib/prepare.sh
  . "$TESTSLIB/prepare.sh"
  #shellcheck source=tests/lib/nested.sh
  . "$TESTSLIB"/nested.sh

  VERSION="$(tests.nested show version)"
  snap download --channel="$VERSION"/beta --basename=pc-kernel pc-kernel
  unsquashfs -d kernel pc-kernel.snap
  kern_ver=$(find kernel/modules/* -maxdepth 0 -printf "%f\n")
  comp_ko_dir=wifi-comp/modules/"$kern_ver"/wireless/
  mkdir -p "$comp_ko_dir"
  mkdir -p wifi-comp/meta/
  cat << 'EOF' > wifi-comp/meta/component.yaml
component: pc-kernel+wifi-comp
type: kernel-modules
version: 1.0
summary: wifi simulator
description: wifi simulator for testing purposes
EOF
  hwsim_path=$(find kernel -name mac80211_hwsim.ko\*)
  cp "$hwsim_path" "$comp_ko_dir"
  snap pack --filename=pc-kernel+wifi-comp.comp wifi-comp

  # Create kernel without the kernel module
  rm "$hwsim_path"
  # depmod wants a lib subdir, fake it and remove after invocation
  mkdir kernel/lib
  ln -s ../modules kernel/lib/modules
  depmod -b kernel/ "$kern_ver"
  rm -rf kernel/lib
  rm pc-kernel.snap
  # append component meta-information
  printf 'components:\n  wifi-comp:\n    type: kernel-modules\n' >> kernel/meta/snap.yaml
  snap pack --filename=pc-kernel.snap kernel
}

build_kernel_with_comp "$@"
