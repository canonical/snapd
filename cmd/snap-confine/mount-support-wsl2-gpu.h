/*
 * Copyright (C) 2022 Canonical Ltd
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

#ifndef SNAP_CONFINE_MOUNT_SUPPORT_WSL2_GPU_H
#define SNAP_CONFINE_MOUNT_SUPPORT_WSL2_GPU_H

/**
 * Make the WSL2 OpenGL driver from the classic distribution available in the snap
 * execution environment.
 *
 * This function is designed to be called before pivot_root() switched the root
 * filesystem.
 *
 * On WSL2, the host GPU libraries and drivers are mounted to /usr/lib/wsl. This
 * directory is bind mounted to /var/lib/snapd/lib/wsl relative to the location of
 * the root filesystem directory provided as an argument.
 **/
void sc_mount_wsl2_gpu_driver(const char *rootfs_dir);

#endif
