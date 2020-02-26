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

// For AT_EMPTY_PATH and O_PATH
#define _GNU_SOURCE

#include "cgroup-support.h"

#include <errno.h>
#include <stdio.h>
#include <string.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <sys/vfs.h>
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
    // Since we may be running from a setuid but not setgid executable, switch
    // to the effective group to root so that the mkdirat call creates a cgroup
    // that is always owned by root.root.
    sc_identity old = sc_set_effective_identity(sc_root_group_identity());
    if (mkdirat(parent_fd, name, 0755) < 0 && errno != EEXIST) {
        die("cannot create cgroup hierarchy %s/%s", parent, name);
    }
    (void)sc_set_effective_identity(old);
    int hierarchy_fd SC_CLEANUP(sc_cleanup_close) = -1;
    hierarchy_fd = openat(parent_fd, name, O_PATH | O_DIRECTORY | O_NOFOLLOW | O_CLOEXEC);
    if (hierarchy_fd < 0) {
        die("cannot open cgroup hierarchy %s/%s", parent, name);
    }
    // Open the cgroup.procs file.
    int procs_fd SC_CLEANUP(sc_cleanup_close) = -1;
    procs_fd = openat(hierarchy_fd, "cgroup.procs", O_WRONLY | O_NOFOLLOW | O_CLOEXEC);
    if (procs_fd < 0) {
        die("cannot open file %s/%s/cgroup.procs", parent, name);
    }
    // Write the process (task) number to the procs file. Linux task IDs are
    // limited to 2^29 so a long int is enough to represent it.
    // See include/linux/threads.h in the kernel source tree for details.
    char buf[22] = {0};  // 2^64 base10 + 2 for NUL and '-' for long
    int n = sc_must_snprintf(buf, sizeof buf, "%ld", (long)pid);
    if (write(procs_fd, buf, n) < n) {
        die("cannot move process %ld to cgroup hierarchy %s/%s", (long)pid, parent, name);
    }
    debug("moved process %ld to cgroup hierarchy %s/%s", (long)pid, parent, name);
}

static const char *cgroup_dir = "/sys/fs/cgroup";

// from statfs(2)
#ifndef CGRUOP2_SUPER_MAGIC
#define CGROUP2_SUPER_MAGIC 0x63677270
#endif

// Detect if we are running in cgroup v2 unified mode (as opposed to
// hybrid or legacy) The algorithm is described in
// https://systemd.io/CGROUP_DELEGATION.html
bool sc_cgroup_is_v2() {
    static bool did_warn = false;
    struct statfs buf;

    if (statfs(cgroup_dir, &buf) != 0) {
        if (errno == ENOENT) {
            return false;
        }
        die("cannot statfs %s", cgroup_dir);
    }
    if (buf.f_type == CGROUP2_SUPER_MAGIC) {
        if (!did_warn) {
            fprintf(stderr, "WARNING: cgroup v2 is not fully supported yet, proceeding with partial confinement\n");
            did_warn = true;
        }
        return true;
    }
    return false;
}
