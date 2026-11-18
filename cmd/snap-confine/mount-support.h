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

#include <stdbool.h>
#include <sys/types.h>
#include "../libsnap-confine-private/apparmor-support.h"
#include "../libsnap-confine-private/classic.h"
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
 * Paths for the trust store view that snaps should see.
 *
 * snapd generates a bundle plus auxiliary certificate entries under
 * SC_MANAGED_CA_CERTS_DIR, so snap-confine mounts the directory as a unit to
 * keep bundle-based and directory-based trust lookups on the same generated
 * trust store.
 **/
#define SC_MANAGED_CA_CERTS_DIR "/var/lib/snapd/pki/v1/merged"
#define SC_MANAGED_CA_GENERATION_DIR "/var/lib/snapd/pki/v1/published"
#define SC_SYSTEM_CA_CERTS_DIR "/etc/ssl/certs"

/**
 * Resolve the host-managed CA certificate view to the immutable generation
 * directory currently active (/var/lib/snapd/pki/v1/merged). The merged folder
 * must be a symlink (after generations was introduced), and must be within the
 * published generations directory.
 *
 * This function returns the path to the managed CA certificates directory,
 * taking into account the managed CA generation directory if necessary.
 */
char *sc_resolve_managed_ca_certs_dir(const char *managed_ca_certs_dir, const char *managed_ca_generation_dir);

struct sc_populate_mount_ns_options {
    struct sc_apparmor *apparmor;
    int snap_update_ns_fd;
    const sc_invocation *inv;
    sc_distro distro;
    const gid_t real_gid;
    const gid_t saved_gid;
};

/**
 * Assuming a new mountspace, populate it accordingly.
 *
 * This function performs many internal tasks:
 * - prepares and chroots into the core snap (on classic systems)
 * - creates private /tmp
 * - creates private /dev/pts
 * - processes mount profiles
 *
 * Returns the mounted managed CA generation ID in managed_ca_generation_id. The returned
 * generation ID string must be freed by the caller if not NULL.
 **/
void sc_populate_mount_ns(struct sc_populate_mount_ns_options *options, char **managed_ca_generation_id);

/**
 * Check whether the given directory is a mount point.
 *
 * This inspects /proc/self/mountinfo and returns true if dir is listed there
 * as a mount point (regardless of its propagation type). It does not create
 * or modify any mounts.
 */
bool sc_is_mount_point(const char *dir) __attribute__((nonnull(1)));

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
 * Set up mount namespace for parallel installed classic snap
 *
 * Create bind mounts from instance specific locations to non-instance ones.
 */
void sc_setup_parallel_instance_classic_mounts(const char *snap_name, const char *snap_instance_name);
#endif
