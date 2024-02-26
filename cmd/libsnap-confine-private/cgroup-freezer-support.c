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

#include "cgroup-freezer-support.h"

#include <errno.h>
#include <fcntl.h>
#include <limits.h>
#include <string.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <unistd.h>

#include "cgroup-support.h"
#include "cleanup-funcs.h"
#include "string-utils.h"
#include "utils.h"

static const char *freezer_cgroup_dir = "/sys/fs/cgroup/freezer";

void sc_cgroup_freezer_join(const char *snap_name, pid_t pid) {
    char buf[PATH_MAX] = {0};
    sc_must_snprintf(buf, sizeof buf, "snap.%s", snap_name);
    sc_cgroup_create_and_join(freezer_cgroup_dir, buf, pid);
}

bool sc_cgroup_freezer_occupied(const char *snap_name) {
    // Format the name of the cgroup hierarchy.
    char buf[PATH_MAX] = {0};
    sc_must_snprintf(buf, sizeof buf, "snap.%s", snap_name);

    // Open the freezer cgroup directory.
    int cgroup_fd SC_CLEANUP(sc_cleanup_close) = -1;
    cgroup_fd = open(freezer_cgroup_dir, O_PATH | O_DIRECTORY | O_NOFOLLOW | O_CLOEXEC);
    if (cgroup_fd < 0) {
        die("cannot open freezer cgroup (%s)", freezer_cgroup_dir);
    }
    // Open the proc directory.
    int proc_fd SC_CLEANUP(sc_cleanup_close) = -1;
    proc_fd = open("/proc", O_PATH | O_DIRECTORY | O_NOFOLLOW | O_CLOEXEC);
    if (proc_fd < 0) {
        die("cannot open /proc");
    }
    // Open the hierarchy directory for the given snap.
    int hierarchy_fd SC_CLEANUP(sc_cleanup_close) = -1;
    hierarchy_fd = openat(cgroup_fd, buf, O_PATH | O_DIRECTORY | O_NOFOLLOW | O_CLOEXEC);
    if (hierarchy_fd < 0) {
        if (errno == ENOENT) {
            return false;
        }
        die("cannot open freezer cgroup hierarchy for snap %s", snap_name);
    }
    // Open the "cgroup.procs" file. Alternatively we could open the "tasks"
    // file and see per-thread data but we don't need that.
    int cgroup_procs_fd SC_CLEANUP(sc_cleanup_close) = -1;
    cgroup_procs_fd = openat(hierarchy_fd, "cgroup.procs", O_RDONLY | O_NOFOLLOW | O_CLOEXEC);
    if (cgroup_procs_fd < 0) {
        die("cannot open cgroup.procs file for freezer cgroup hierarchy for "
            "snap %s",
            snap_name);
    }

    FILE *cgroup_procs SC_CLEANUP(sc_cleanup_file) = NULL;
    cgroup_procs = fdopen(cgroup_procs_fd, "r");
    if (cgroup_procs == NULL) {
        die("cannot convert cgroups.procs file descriptor to FILE");
    }
    cgroup_procs_fd = -1;  // cgroup_procs_fd will now be closed by fclose.

    char *line_buf SC_CLEANUP(sc_cleanup_string) = NULL;
    size_t line_buf_size = 0;
    ssize_t num_read;
    struct stat statbuf;
    for (;;) {
        num_read = getline(&line_buf, &line_buf_size, cgroup_procs);
        if (num_read < 0 && errno != 0) {
            die("cannot read next PID belonging to snap %s", snap_name);
        }
        if (num_read <= 0) {
            break;
        } else {
            if (line_buf[num_read - 1] == '\n') {
                line_buf[num_read - 1] = '\0';
            } else {
                die("could not find newline in cgroup.procs");
            }
        }
        debug("found process id: %s\n", line_buf);

        if (fstatat(proc_fd, line_buf, &statbuf, AT_SYMLINK_NOFOLLOW) < 0) {
            // The process may have died already.
            if (errno != ENOENT) {
                die("cannot stat /proc/%s", line_buf);
            }
            continue;
        }
        debug("found live process %s belonging to user %d", line_buf, statbuf.st_uid);
        return true;
    }

    return false;
}
