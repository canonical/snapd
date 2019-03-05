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

#ifndef SC_CGROUP_PIDS_SUPPORT_H
#define SC_CGROUP_PIDS_SUPPORT_H

#include <sys/types.h>

/**
 * Join the pid cgroup for the given snap application.
 *
 * This function adds the specified task to the pid cgroup specific to the
 * given snap. The name of the cgroup is "snap.$snap_name.$app_name" for apps
 * or "snap.$snap_name.hook.$hook_name" for hooks.
 *
 * The "tasks" file belonging to the cgroup contains the set of all the
 * threads that originate from the given snap app or hook. Examining that
 * file one can reliably determine if the set is empty or not.
 *
 * Similarly the "cgroup.procs" file belonging to the same directory contains
 * the set of all the processes that originate from the given snap app or
 * hook.
 *
 * For more details please review:
 * https://www.kernel.org/doc/Documentation/cgroup-v1/pids.txt
 **/
void sc_cgroup_pids_join(const char *snap_security_tag, pid_t pid);

#endif
