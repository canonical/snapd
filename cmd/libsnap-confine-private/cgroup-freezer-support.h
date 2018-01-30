#ifndef SC_CGROUP_FREEZER_SUPPORT_H
#define SC_CGROUP_FREEZER_SUPPORT_H

#include <sys/types.h>
#include "error.h"

/**
 * Join the freezer cgroup for the given snap.
 *
 * This function adds the specified task to the freezer cgroup specific to the
 * given snap. The name of the cgroup is "snap.$snap_name".
 *
 * Interestingly we don't need to actually freeze the processes. The group
 * allows us to track processes belonging to a given snap. This makes the
 * measurement "are any processes of this snap still alive" very simple.
 *
 * The "tasks" file belonging to the cgroup contains the set of all the
 * processes that originate from the given snap. Examining that file one can
 * reliably determine if the set is empty or not.
 *
 * For more details please review:
 * https://www.kernel.org/doc/Documentation/cgroup-v1/freezer-subsystem.txt
**/
void sc_cgroup_freezer_join(const char *snap_name, pid_t pid);

/**
 * Check if a freezer cgroup for given snap has any processes belonging to a given user.
 *
 * This function examines the freezer cgroup called "snap.$snap_name" and looks
 * at each of its processes. If any process exists then the function returns true.
**/
// TODO: Support per user filtering for eventual per-user mount namespaces
bool sc_cgroup_freezer_occupied(const char *snap_name);

#endif
