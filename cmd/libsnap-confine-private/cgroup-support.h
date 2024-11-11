/*
 * Copyright (C) 2019-2021 Canonical Ltd
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

#ifndef SC_CGROUP_SUPPORT_H
#define SC_CGROUP_SUPPORT_H

#include <fcntl.h>
#include <stdbool.h>

/**
 * sc_cgroup_create_and_join joins, perhaps creating, a cgroup hierarchy.
 *
 * The code assumes that an existing hierarchy rooted at "parent". It follows
 * up with a sub-hierarchy called "name", creating it if necessary. The created
 * sub-hierarchy is made to belong to root:root and the specified process is
 * moved there.
 **/
void sc_cgroup_create_and_join(const char *parent, const char *name, pid_t pid);

/**
 * sc_cgroup_is_v2() returns true if running on cgroups v2
 *
 **/
bool sc_cgroup_is_v2(void);

/**
 * sc_cgroup_is_tracking_snap checks whether any snap process other than the
 * caller are currently being tracked in a cgroup.
 *
 * Note that this call will traverse the cgroups hierarchy looking for a group
 * name with a specific prefix corresponding to the snap name. This is
 * inherently racy. The caller must have taken the per snap instance lock to
 * prevent new applications of that snap from being started. However, it is
 * still possible that the application may exit but the cgroup has not been
 * cleaned up yet, in which case this call will return a false positive.
 *
 * It is possible that the current process is already being tracked in cgroup,
 * in which case the code will skip its own group.
 */
bool sc_cgroup_v2_is_tracking_snap(const char *snap_instance);

/**
 * sc_cgroup_v2_own_path_full return the full path of the owning cgroup as
 * reported by the kernel.
 *
 * Returns the full path of the group in the unified hierarchy relative to its
 * root. The string is owned by the caller.
 */
char *sc_cgroup_v2_own_path_full(void);

#endif
