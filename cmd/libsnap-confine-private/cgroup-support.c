// For AT_EMPTY_PATH and O_PATH
#define _GNU_SOURCE

#include "cgroup-support.h"

#include <errno.h>
#include <stdio.h>
#include <string.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <unistd.h>

#include "cleanup-funcs.h"
#include "string-utils.h"
#include "utils.h"

void sc_cgroup_create_and_join(const char *parent, const char *name, pid_t pid) {
    int parent_fd SC_CLEANUP(sc_cleanup_close) = -1;
    parent_fd = open(parent, O_PATH | O_DIRECTORY | O_NOFOLLOW | O_CLOEXEC);
    if (parent_fd < 0) {
        die("cannot open cgroup hierarchy %s", parent);
    }
    if (mkdirat(parent_fd, name, 0755) < 0 && errno != EEXIST) {
        die("cannot create cgroup hierarchy %s/%s", parent, name);
    }
    int hierarchy_fd SC_CLEANUP(sc_cleanup_close) = -1;
    hierarchy_fd = openat(parent_fd, name, O_PATH | O_DIRECTORY | O_NOFOLLOW | O_CLOEXEC);
    if (hierarchy_fd < 0) {
        die("cannot open cgroup hierarchy %s/%s", parent, name);
    }
    // Since we may be running from a setuid but not setgid executable, ensure
    // that the group and owner of the hierarchy directory is root.root.
    if (fchownat(hierarchy_fd, "", 0, 0, AT_EMPTY_PATH) < 0) {
        die("cannot change owner of cgroup hierarchy %s/%s to root.root", parent, name);
    }
    // Open the tasks file.
    int tasks_fd SC_CLEANUP(sc_cleanup_close) = -1;
    tasks_fd = openat(hierarchy_fd, "tasks", O_WRONLY | O_NOFOLLOW | O_CLOEXEC);
    if (tasks_fd < 0) {
        die("cannot open file %s/%s/tasks", parent, name);
    }
    // Write the process (task) number to the tasks file. Linux task IDs are
    // limited to 2^29 so a long int is enough to represent it.
    // See include/linux/threads.h in the kernel source tree for details.
    char buf[22] = {0};  // 2^64 base10 + 2 for NUL and '-' for long
    int n = sc_must_snprintf(buf, sizeof buf, "%ld", (long)pid);
    if (write(tasks_fd, buf, n) < n) {
        die("cannot move process %ld to cgroup hierarchy %s/%s", (long)pid, parent, name);
    }
    debug("moved process %ld to cgroup hierarchy %s/%s", (long)pid, parent, name);
}
