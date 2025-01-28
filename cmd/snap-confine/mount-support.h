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

#include <sys/types.h>
#include "../libsnap-confine-private/apparmor-support.h"
#include "snap-confine-invocation.h"

/* Base location where extra libraries might be made available to the snap.
 * This is currently used for graphics drivers, but could pontentially be used
 * for other goals as well.
 *
 * NOTE: do not bind-mount anything directly onto this directory! This is only
 * a *base* directory: for exposing drivers and libraries, create a
 * sub-directory in SC_EXTRA_LIB_DIR and use that one as the bind mount target.
 */
#define SC_EXTRA_LIB_DIR "/var/lib/snapd/lib"

/**
 * Assuming a new mountspace, populate it accordingly.
 *
 * This function performs many internal tasks:
 * - prepares and chroots into the core snap (on classic systems)
 * - creates private /tmp
 * - creates private /dev/pts
 * - processes mount profiles
 **/
void sc_populate_mount_ns(struct sc_apparmor *apparmor, int snap_update_ns_fd, const sc_invocation *inv,
                          const gid_t real_gid, const gid_t saved_gid);

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
void sc_setup_user_mounts(struct sc_apparmor *apparmor, int snap_update_ns_fd, const char *snap_name);

/**
 * Ensure that SNAP_MOUNT_DIR and /var/snap are mount points.
 *
 * Create bind mounts and set up shared propagation for SNAP_MOUNT_DIR and
 * /var/snap as needed. This allows for further propagation changes after the
 * initial mount namespace is unshared.
 */
void sc_ensure_snap_dir_shared_mounts(void);

/**
 * Set up mount namespace for parallel installed classic snap
 *
 * Create bind mounts from instance specific locations to non-instance ones.
 */
void sc_setup_parallel_instance_classic_mounts(const char *snap_name, const char *snap_instance_name);
#endif
