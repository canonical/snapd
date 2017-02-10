/*
 * Copyright (C) 2015 Canonical Ltd
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

#ifndef SNAP_CONFINE_MOUNT_SUPPORT_NVIDIA_H
#define SNAP_CONFINE_MOUNT_SUPPORT_NVIDIA_H

/**
 * Make the Nvidia driver from the classic distribution available in the snap
 * execution environment.
 *
 * This function may be a no-op, depending on build-time configuration options.
 * If enabled the behavior differs from one distribution to another because of
 * differences in classic packaging and perhaps version of the Nvidia driver.
 * This function is designed to be called before pivot_root() switched the root
 * filesystem.
 *
 * On Ubuntu, there are several versions of the binary Nvidia driver. The
 * drivers are all installed in /usr/lib/nvidia-$MAJOR_VERSION where
 * MAJOR_VERSION is an integer like 304, 331, 340, 346, 352 or 361.  The driver
 * is located by inspecting /sys/modules/nvidia/version which contains the
 * string "$MAJOR_VERSION.$MINOR_VERSION". The appropriate directory is then
 * bind mounted to /var/lib/snapd/lib/gl relative relative to the location of
 * the root filesystem directory provided as an argument.
 *
 * On Arch another approach is used. Because the actual driver installs a
 * number of shared objects into /usr/lib, they cannot be bind mounted
 * directly. Instead a tmpfs is mounted on /var/lib/snapd/lib/gl. The tmpfs is
 * subsequently populated with symlinks that point to a number of files in the
 * /usr/lib directory on the classic filesystem. After the pivot_root() call
 * those symlinks rely on the /var/lib/snapd/hostfs directory as a "gateway".
 **/
void sc_mount_nvidia_driver(const char *rootfs_dir);

#endif
