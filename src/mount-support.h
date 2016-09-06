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

/**
 * Unshare the mount namespace.
 *
 * Ensure we run in our own slave mount namespace, this will create a new mount
 * namespace and make it a slave of "/"
 *
 * Note that this means that no mount actions inside our namespace are
 * propagated to the main "/". We need this both for the private /tmp we create
 * and for the bind mounts we do on a classic distribution system.
 *
 * This also means you can't run an automount daemon under this launcher.
 **/
void sc_unshare_mount_ns();

/**
 * Assuming a new mountspace, populate it accordingly.
 *
 * This function performs many internal tasks:
 * - prepares and chroots into the core snap (on classic systems)
 * - creates private /tmp
 * - creates private /dev/pts
 * - applies quirks for specific snaps (like LXD)
 * - processes mount profiles
 *
 * The function will also try to preserve the current working directory but if
 * this is impossible it will chdir to SC_VOID_DIR.
 **/
void sc_populate_mount_ns(const char *security_tag);

#endif
