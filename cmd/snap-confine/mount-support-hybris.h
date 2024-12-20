/*
 * Copyright (C) 2023 Canonical Ltd
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

#ifndef SNAP_CONFINE_MOUNT_SUPPORT_HYBRIS_H
#define SNAP_CONFINE_MOUNT_SUPPORT_HYBRIS_H

/**
 * Make the libhybris drivers from the classic distribution available in the snap
 * execution environment.
 *
 * libhybris allows for ABI guarantees as long as their wrappers can be linked or
 * dlopen()'ed because it is the library loader, it resolves the symbols and links them.
 * /android needs to live inside the Snap environment too for the actual bionic-built
 * libraries to be found, loaded and their functions executed.
 *
 * /android and the respective compatibility symlinks from /system to /android/system
 * etc. allow for loading the appropriate userspace components for proper use
 * (assuming AppArmor plays along).
 **/
void sc_mount_hybris_driver(const char *rootfs_dir, const char *base_snap_name);

#endif
