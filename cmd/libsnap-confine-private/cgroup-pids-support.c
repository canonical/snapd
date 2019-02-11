// For AT_EMPTY_PATH and O_PATH
#define _GNU_SOURCE

#include "cgroup-pids-support.h"

#include <errno.h>
#include <fcntl.h>
#include <limits.h>
#include <string.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <unistd.h>

#include "cleanup-funcs.h"
#include "string-utils.h"
#include "utils.h"

static const char *pids_cgroup_dir = "/sys/fs/cgroup/pids";

void sc_cgroup_pids_join(const char *snap_security_tag, pid_t pid) {
    // Open the pids cgroup directory.
    int cgroup_fd SC_CLEANUP(sc_cleanup_close) = -1;
    cgroup_fd = open(pids_cgroup_dir, O_PATH | O_DIRECTORY | O_NOFOLLOW | O_CLOEXEC);
    if (cgroup_fd < 0) {
        die("cannot open pids cgroup (%s)", pids_cgroup_dir);
    }
    // Create the pid hierarchy for the given snap security tag.
    if (mkdirat(cgroup_fd, snap_security_tag, 0755) < 0 && errno != EEXIST) {
        die("cannot create pids cgroup hierarchy for snap security tag %s", snap_security_tag);
    }
    // Open the hierarchy directory for the given snap security tag.
    int hierarchy_fd SC_CLEANUP(sc_cleanup_close) = -1;
    hierarchy_fd = openat(cgroup_fd, snap_security_tag, O_PATH | O_DIRECTORY | O_NOFOLLOW | O_CLOEXEC);
    if (hierarchy_fd < 0) {
        die("cannot open pids cgroup hierarchy for snap security tag %s", snap_security_tag);
    }
    // Since we may be running from a setuid but not setgid executable, ensure
    // that the group and owner of the hierarchy directory is root.root.
    if (fchownat(hierarchy_fd, "", 0, 0, AT_EMPTY_PATH) < 0) {
        die("cannot change owner of pids cgroup hierarchy for snap security tag %s to root.root", snap_security_tag);
    }
    // Open the tasks file.
    int tasks_fd SC_CLEANUP(sc_cleanup_close) = -1;
    tasks_fd = openat(hierarchy_fd, "tasks", O_WRONLY | O_NOFOLLOW | O_CLOEXEC);
    if (tasks_fd < 0) {
        die("cannot open tasks file for pids cgroup hierarchy for snap security tag %s", snap_security_tag);
    }
    // Write the process (task) number to the tasks file. Linux task IDs are
    // limited to 2^29 so a long int is enough to represent it.
    // See include/linux/threads.h in the kernel source tree for details.
    char buf[PATH_MAX] = {0};
    int n = sc_must_snprintf(buf, sizeof buf, "%ld", (long)pid);
    if (write(tasks_fd, buf, n) < n) {
        die("cannot move process %ld to pids cgroup hierarchy for snap security tag %s", (long)pid, snap_security_tag);
    }
    debug("moved process %ld to pids cgroup hierarchy for snap security tag %s", (long)pid, snap_security_tag);
}
