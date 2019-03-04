#ifndef SC_CGROUP_SUPPORT_H
#define SC_CGROUP_SUPPORT_H

#include <fcntl.h>

/**
 * sc_cgroup_create_and_join joins, perhaps creating, a cgroup hierarchy.
 *
 * The code assumes that an existing hierarchy rooted at "parent". It follows
 * up with a sub-hierarchy called "name", creating it if necessary. The created
 * sub-hierarchy is made to belong to root.root and the specified process is
 * moved there.
 **/
void sc_cgroup_create_and_join(const char *parent, const char *name, pid_t pid);

#endif
