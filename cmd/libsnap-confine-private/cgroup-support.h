/*
 * Copyright (C) 2019 Canonical Ltd
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
 * sub-hierarchy is made to belong to root.root and the specified process is
 * moved there.
 **/
void sc_cgroup_create_and_join(const char *parent, const char *name, pid_t pid);

/**
 * sc_cgroup_is_v2() returns true if running on cgroups v2
 *
 **/
bool sc_cgroup_is_v2(void);

/**
 * sc_cgroup_mount_snapd_hierarchy mounts /run/snapd/cgroup if one is missing.
 *
 * The logic mounts an v1 cgroup hierarchy with name=snapd and without any
 * controllers. Currently no release agent is set and no release notification
 * is enabled.
 **/
void sc_cgroup_mount_snapd_hierarchy(void);

/**
 * Join the name=snapd cgroup for the given snap.
 *
 * This function adds the specified process to the named cgroup named after the
 * snap security tag.
 *
 * This hierarchy is designed for tracking processes and associating them with
 * a given executable portion of a snap (either an application or a hook).
 **/
void sc_cgroup_snapd_hierarchy_join(const char *snap_security_tag, pid_t pid);

#endif
