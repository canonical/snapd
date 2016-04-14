// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package apparmor

// defaultTemplate contains default apparmor template.
//
// It can be overridden for testing using MockTemplate().
//
// http://bazaar.launchpad.net/~ubuntu-security/ubuntu-core-security/trunk/view/head:/data/apparmor/templates/ubuntu-core/16.04/default
var defaultTemplate = []byte(`
# Description: Allows access to app-specific directories and basic runtime
# Usage: common

# vim:syntax=apparmor

#include <tunables/global>

###VAR###

###PROFILEATTACH### (attach_disconnected) {
  #include <abstractions/base>
  #include <abstractions/consoles>
  #include <abstractions/openssl>

  # for python apps/services
  #include <abstractions/python>
  /usr/bin/python{,2,2.[0-9]*,3,3.[0-9]*} ixr,
  deny /usr/lib/python3*/{,**/}__pycache__/ w,              # noisy
  deny /usr/lib/python3*/{,**/}__pycache__/**.pyc.[0-9]* w,

  # for perl apps/services
  #include <abstractions/perl>
  /usr/bin/perl{,5*} ixr,

# TODO: we must remove these since things like 'container-management' will be
# broken if we have explicit denies. However, the development tools need to be
# clear that these can't be allowed.
  # Explicitly deny ptrace for now since it can be abused to break out of the
  # seccomp sandbox. https://lkml.org/lkml/2015/3/18/823
#  audit deny ptrace (trace),

  # Explicitly deny capability mknod so apps can't create devices
#  audit deny capability mknod,

  # Explicitly deny mount, remount and umount so apps can't modify things in
  # their namespace
#  audit deny mount,
#  audit deny remount,
#  audit deny umount,

  # for bash 'binaries' (do *not* use abstractions/bash)
  # user-specific bash files
  /bin/bash ixr,
  /bin/dash ixr,
  /etc/bash.bashrc r,
  /etc/{passwd,group,nsswitch.conf} r,  # very common
  /etc/libnl-3/{classid,pktloc} r,      # apps that use libnl
  /var/lib/extrausers/{passwd,group} r,
  /etc/profile r,
  /usr/share/terminfo/** r,
  /etc/inputrc r,
  deny @{HOME}/.inputrc r,
  # Common utilities for shell scripts
  /{,usr/}bin/{,g,m}awk ixr,
  /{,usr/}bin/basename ixr,
  /{,usr/}bin/bunzip2 ixr,
  /{,usr/}bin/bzcat ixr,
  /{,usr/}bin/bzdiff ixr,
  /{,usr/}bin/bzgrep ixr,
  /{,usr/}bin/bzip2 ixr,
  /{,usr/}bin/cat ixr,
  /{,usr/}bin/chmod ixr,
  /{,usr/}bin/cmp ixr,
  /{,usr/}bin/cp ixr,
  /{,usr/}bin/cpio ixr,
  /{,usr/}bin/cut ixr,
  /{,usr/}bin/date ixr,
  /{,usr/}bin/dd ixr,
  /{,usr/}bin/diff{,3} ixr,
  /{,usr/}bin/dir ixr,
  /{,usr/}bin/dirname ixr,
  /{,usr/}bin/echo ixr,
  /{,usr/}bin/{,e,f,r}grep ixr,
  /{,usr/}bin/env ixr,
  /{,usr/}bin/expr ixr,
  /{,usr/}bin/false ixr,
  /{,usr/}bin/find ixr,
  /{,usr/}bin/fmt ixr,
  /{,usr/}bin/getopt ixr,
  /{,usr/}bin/groups ixr,
  /{,usr/}bin/gzip ixr,
  /{,usr/}bin/head ixr,
  /{,usr/}bin/hostname ixr,
  /{,usr/}bin/id ixr,
  /{,usr/}bin/igawk ixr,
  /{,usr/}bin/kill ixr,
  /{,usr/}bin/ldd ixr,
  /{,usr/}bin/less{,file,pipe} ixr,
  /{,usr/}bin/ln ixr,
  /{,usr/}bin/line ixr,
  /{,usr/}bin/link ixr,
  /{,usr/}bin/logger ixr,
  /{,usr/}bin/ls ixr,
  /{,usr/}bin/md5sum ixr,
  /{,usr/}bin/mkdir ixr,
  /{,usr/}bin/mktemp ixr,
  /{,usr/}bin/more ixr,
  /{,usr/}bin/mv ixr,
  /{,usr/}bin/openssl ixr, # may cause harmless capability block_suspend denial
  /{,usr/}bin/pgrep ixr,
  /{,usr/}bin/printenv ixr,
  /{,usr/}bin/printf ixr,
  /{,usr/}bin/ps ixr,
  /{,usr/}bin/pwd ixr,
  /{,usr/}bin/readlink ixr,
  /{,usr/}bin/realpath ixr,
  /{,usr/}bin/rev ixr,
  /{,usr/}bin/rm ixr,
  /{,usr/}bin/rmdir ixr,
  /{,usr/}bin/sed ixr,
  /{,usr/}bin/seq ixr,
  /{,usr/}bin/sleep ixr,
  /{,usr/}bin/sort ixr,
  /{,usr/}bin/stat ixr,
  /{,usr/}bin/tac ixr,
  /{,usr/}bin/tail ixr,
  /{,usr/}bin/tar ixr,
  /{,usr/}bin/tee ixr,
  /{,usr/}bin/test ixr,
  /{,usr/}bin/tempfile ixr,
  /{,usr/}bin/tset ixr,
  /{,usr/}bin/touch ixr,
  /{,usr/}bin/tr ixr,
  /{,usr/}bin/true ixr,
  /{,usr/}bin/uname ixr,
  /{,usr/}bin/uniq ixr,
  /{,usr/}bin/unlink ixr,
  /{,usr/}bin/unxz ixr,
  /{,usr/}bin/unzip ixr,
  /{,usr/}bin/vdir ixr,
  /{,usr/}bin/wc ixr,
  /{,usr/}bin/which ixr,
  /{,usr/}bin/xargs ixr,
  /{,usr/}bin/xz ixr,
  /{,usr/}bin/yes ixr,
  /{,usr/}bin/zcat ixr,
  /{,usr/}bin/z{,e,f}grep ixr,
  /{,usr/}bin/zip ixr,
  /{,usr/}bin/zipgrep ixr,

  # uptime
  /{,usr/}bin/uptime ixr,
  @{PROC}/uptime r,
  @{PROC}/loadavg r,
  # this is an information leak
  deny /{,var/}run/utmp r,

  # java
  @{PROC}/@{pid}/ r,
  @{PROC}/@{pid}/fd/ r,
  owner @{PROC}/@{pid}/auxv r,
  @{PROC}/@{pid}/version_signature r,
  @{PROC}/@{pid}/version r,
  @{PROC}/sys/vm/zone_reclaim_mode r,
  /etc/lsb-release r,
  /sys/devices/**/read_ahead_kb r,
  /sys/devices/system/cpu/** r,
  /sys/kernel/mm/transparent_hugepage/enabled r,
  /sys/kernel/mm/transparent_hugepage/defrag r,
  # NOTE: this leaks running process and java seems to want it, but operates
  # ok without it. Deny for now to silence the denial but we could allow
  # owner match until AppArmor kernel var is available to solve this properly.
  deny @{PROC}/@{pid}/cmdline r,
  #owner @{PROC}/@{pid}/cmdline r,

  # Miscellaneous accesses
  /etc/mime.types r,
  @{PROC}/ r,
  /etc/{,writable/}hostname r,
  /etc/{,writable/}localtime r,
  /etc/{,writable/}timezone r,
  @{PROC}/@{pid}/stat r,
  @{PROC}/@{pid}/statm r,
  @{PROC}/@{pid}/status r,
  @{PROC}/sys/kernel/hostname r,
  @{PROC}/sys/kernel/osrelease r,
  @{PROC}/sys/fs/file-max r,
  @{PROC}/sys/kernel/pid_max r,
  @{PROC}/sys/kernel/random/uuid r,

  # Eases hardware assignment (doesn't give anything away)
  /etc/udev/udev.conf r,
  /sys/       r,
  /sys/bus/   r,
  /sys/class/ r,

  # this leaks interface names and stats, but not in a way that is traceable
  # to the user/device
  @{PROC}/net/dev r,

  # Read-only for the install directory
  @{INSTALL_DIR}/@{SNAP_NAME}/                   r,
  @{INSTALL_DIR}/@{SNAP_NAME}/@{SNAP_REVISION}/    r,
  @{INSTALL_DIR}/@{SNAP_NAME}/@{SNAP_REVISION}/**  mrklix,

  # Don't log noisy python denials (see LP: #1496895 for more details)
  deny @{INSTALL_DIR}/@{SNAP_NAME}/**/__pycache__/             w,
  deny @{INSTALL_DIR}/@{SNAP_NAME}/**/__pycache__/*.pyc.[0-9]* w,

  # Read-only home area for other versions
  owner @{HOME}/snap/@{SNAP_NAME}/                  r,
  owner @{HOME}/snap/@{SNAP_NAME}/**                mrkix,

  # Writable home area for this version.
  owner @{HOME}/snap/@{SNAP_NAME}/@{SNAP_REVISION}/** wl,

  # Read-only system area for other versions
  /var/snap/@{SNAP_NAME}/   r,
  /var/snap/@{SNAP_NAME}/** mrkix,

  # Writable system area only for this version
  /var/snap/@{SNAP_NAME}/@{SNAP_REVISION}/** wl,

  # The ubuntu-core-launcher creates an app-specific private restricted /tmp
  # and will fail to launch the app if something goes wrong. As such, we can
  # simply allow full access to /tmp.
  /tmp/   r,
  /tmp/** mrwlkix,

  # Also do the same for shm
  /{dev,run}/shm/snap/@{SNAP_NAME}/                  r,
  /{dev,run}/shm/snap/@{SNAP_NAME}/**                rk,
  /{dev,run}/shm/snap/@{SNAP_NAME}/@{SNAP_REVISION}/   r,
  /{dev,run}/shm/snap/@{SNAP_NAME}/@{SNAP_REVISION}/** mrwlkix,

  # Allow apps from the same package to communicate with each other via an
  # abstract or anonymous socket
  unix peer=(label=snap.@{SNAP_NAME}.*),

  # Allow apps from the same package to signal each other via signals
  signal peer=snap.@{SNAP_NAME}.*,

  # for 'udevadm trigger --verbose --dry-run --tag-match=snappy-assign'
  /{,s}bin/udevadm ixr,
  /etc/udev/udev.conf r,
  /{,var/}run/udev/tags/snappy-assign/ r,
  @{PROC}/cmdline r,
  /sys/devices/**/uevent r,

  # LP: #1447237: adding '--property-match=SNAPPY_APP=<pkgname>' to the above
  # requires:
  #   /run/udev/data/* r,
  # but that reveals too much about the system and cannot be granted to apps
  # by default at this time.

  # For convenience, allow apps to see what is in /dev even though cgroups
  # will block most access
  /dev/ r,
  /dev/**/ r,

  # Do the same with /sys/devices and /sys/class to help people using hw-assign
  /sys/devices/ r,
  /sys/devices/**/ r,
  /sys/class/ r,
  /sys/class/**/ r,

###SNIPPETS###
}
`)
