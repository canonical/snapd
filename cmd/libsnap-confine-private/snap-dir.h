/*
 * Copyright (C) 2024 Canonical Ltd
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

#ifndef SNAP_CONFINE_SNAP_DIR_H
#define SNAP_CONFINE_SNAP_DIR_H

#include "error.h"

/**
 * Canonical location of the mount tree where snaps are visible on the system or
 * the location of the symbolic link to the fallback location.
 **/
#define SC_CANONICAL_SNAP_MOUNT_DIR "/snap"

/**
 * Alternate location of the mount tree where snaps are visible on the system.
 * Used if distribution policy disallows the use of the preferred location.
 **/
#define SC_ALTERNATE_SNAP_MOUNT_DIR "/var/lib/snapd/snap"

/**
 * Return the value probed by sc_probe_snap_mount_dir_from_pid_1_mount_ns.
 *
 * The function fails if the directory was not probed yet.
 *
 * The error protocol is observed so if the caller doesn't provide an outgoing
 * error pointer the function will die on any error.
 **/
const char *sc_snap_mount_dir(sc_error **errorp);

/**
 * Probe the system to decide which of the two possible mount locations to use.
 *
 * The function is safe to call from the any mount namespace. The function
 * internally stores the value later returned by sc_snap_mount_dir(), making the
 * result stable during each execution.
 *
 * The root_fd argument is either an AT_FDCWD or a descriptor to a O_PATH
 * representing an alternative root directory during tests.
 *
 * The error protocol is observed so if the caller doesn't provide an outgoing
 * error pointer the function will die on any error.
 **/
void sc_probe_snap_mount_dir_from_pid_1_mount_ns(int root_fd, sc_error **errorp);

#endif
