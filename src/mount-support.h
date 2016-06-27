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

#ifndef SNAP_MOUNT_SUPPORT_H
#define SNAP_MOUNT_SUPPORT_H

void setup_private_mount(const char *appname);
void setup_private_pts();
void setup_snappy_os_mounts();
void setup_slave_mount_namespace();

/**
 * Setup mount profiles as described by snapd.
 *
 * This function reads /var/lib/snapd/mount/$appname.fstab as a fstab(5) file
 * and executes the mount requests described there.
 *
 * Currently only bind mounts are allowed. All bind mounts are read only by
 * default though the `rw` flag can be used.
 *
 * This function is called with the rootfs being "consistent" so that it is
 * either the core snap on an all-snap system or the core snap + punched holes
 * on a classic system.
 **/
void sc_setup_mount_profiles(const char *appname);

#endif
