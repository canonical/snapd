#include "cgroup-pids-support.h"

#include "cgroup-support.h"

static const char *pids_cgroup_dir = "/sys/fs/cgroup/pids";

void sc_cgroup_pids_join(const char *snap_security_tag, pid_t pid) {
    sc_cgroup_create_and_join(pids_cgroup_dir, snap_security_tag, pid);
}
