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
 * sc_join_sub_cgroup() joins a leaf tracking cgroup named by the security tag.
 *
 * The code scans /proc/[pid]/cgroup, finds the location in either the unified
 * hierarchy or the name=systemd hierarchy, and moves the given process to a
 * new leaf sub-hierarchy named like the security tag.
 **/
void sc_join_sub_cgroup(const char *security_tag, pid_t pid);

#endif
