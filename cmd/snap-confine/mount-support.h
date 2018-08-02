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

#include "apparmor-support.h"

/**
 * Return a file descriptor referencing the snap-update-ns utility
 *
 * By calling this prior to changing the mount namespace, it is
 * possible to execute the utility even if a different version is now
 * mounted at the expected location.
 **/
int sc_open_snap_update_ns(void);

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
void sc_populate_mount_ns(struct sc_apparmor *apparmor, int snap_update_ns_fd,
			  const char *base_snap_name, const char *snap_name);

/**
 * Ensure that / or /snap is mounted with the SHARED option.
 *
 * If the system is found to be not having a shared mount for "/"
 * snap-confine will create a shared bind mount for "/snap" to
 * ensure that "/snap" is mounted shared. See LP:#1668659
 */
void sc_ensure_shared_snap_mount(void);

/**
 * Set up user mounts, private to this process.
 *
 * If any user mounts have been configured for this process, this does
 * the following:
 * - create a new mount namespace
 * - reconfigure all existing mounts to slave mode
 * - perform all user mounts
 */
void sc_setup_user_mounts(struct sc_apparmor *apparmor, int snap_update_ns_fd,
			  const char *snap_name);

/**
 * Set up mappings for snap instance.
 *
 * Perform setup of snap directory mappings of the snap being run is a parallel
 * installed one. This ensures that snap directories appear the same for snaps
 * with instance key and the ones without.
 */
void sc_setup_snap_instance_mapping(const char *snap_instance);

#endif
