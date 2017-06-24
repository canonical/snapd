#ifndef SC_CGROUP_FREEZER_SUPPORT_H
#define SC_CGROUP_FREEZER_SUPPORT_H

#include <sys/types.h>
#include "error.h"

/**
 * Join the freezer cgroup of the given snap.
 *
 * This function adds the specified task to the freezer cgroup specific to the
 * given snap. The name of the cgroup is "snap.$snap_name".
**/
void sc_cgroup_freezer_join(const char *snap_name, pid_t pid);

#endif
