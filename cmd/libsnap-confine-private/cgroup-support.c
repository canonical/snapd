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
#if 0
static const char *cgroup_unified_dir = "/sys/fs/cgroup/unified";
static const char *cgroup_systemd_dir = "/sys/fs/cgroup/systemd";
#endif

// from statfs(2)
#ifndef CGRUOP2_SUPER_MAGIC
#define CGROUP2_SUPER_MAGIC 0x63677270
#endif

#ifndef CGROUP_SUPER_MAGIC
#define CGROUP_SUPER_MAGIC 0x27e0eb
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

#if 0
/**
 * sc_find_tracking_hierarchy produces the full /sys/fs/cgroup/ path the
 * selected cgroup hierarchy of the given process. The result is written to
 * path buffer. If the location cannot be found or any other error occurs, the
 * process dies.
 *
 * The hierarchy selected can be used for process tracking. It is picked based
 * on the following criteria:
 *
 * 1) A unified or v2 cgroup is preferred. If one exists it is selected.
 * 2) A tracking name=systemd v1 cgroup is supported.
 **/
static void sc_find_tracking_hierarchy(pid_t pid, char *path, size_t path_size) {
    /* Find the path of the unified hierarchy of the given process. */
    char proc_pid_cgroup[PATH_MAX + 1] = {};
    sc_must_snprintf(proc_pid_cgroup, sizeof proc_pid_cgroup, "/proc/%d/cgroup", (int)pid);
    int fd = open(proc_pid_cgroup, O_RDONLY | O_NOFOLLOW | O_CLOEXEC);
    if (fd < 0) {
        die("cannot open %s", proc_pid_cgroup);
    }
    /* The ownership of the descriptor is now passed to the stream. */
    FILE *stream = fdopen(fd, "r");
    if (stream == NULL) {
        die("cannot open stream from %d", fd);
    }
    /* The format of the file is: set of lines representing records. Each
     * record is a tuple (hierarchy-id,controller-list,cgroup-path) using
     * colons as element separators. Controllers is in turn a list using commas
     * as separators. See cgroups(7) for authoritative reference. */
    char *line = NULL;
    size_t size = 0;
    bool found = false;
    while (true) {
        /* Read the next line, chomp the trailing newline and parse it. */
        errno = 0;
        ssize_t len = getline(&line, &size, stream);
        if (len < 0) {
            if (errno != 0) {
                die("cannot read subsequent line from %s", proc_pid_cgroup);
            }
            break;
        }
        if (len > 0 && line[len - 1] == '\n') {
            line[len - 1] = '\0';
            len--;
        }
        char *attr_id, *attr_ctrl_list, *attr_path, *tmp;

        attr_id = line;

        tmp = strchr(attr_id, ':');
        if (tmp == NULL) {
            die("cannot parse cgroup, expected hierarchy id: %s", line);
        }
        attr_ctrl_list = tmp + 1;

        tmp = strchr(attr_ctrl_list, ':');
        if (tmp == NULL) {
            die("cannot parse cgroup, expected controller list: %s", line);
        }
        attr_path = tmp + 1;

        tmp = strchr(attr_path, ':');
        if (tmp != NULL) {
            die("cannot parse cgroup, expected end of line: %s", line);
        }
        attr_ctrl_list[-1] = '\0';
        attr_path[-1] = '\0';

        debug("cgroup presence: id:%s, controllers:%s, path:%s", attr_id, attr_ctrl_list, attr_path);

        /* Find preferred tracking cgroup, either v1 name=systemd or v2. */
        if (strcmp(attr_id, "0") == 0) {
            /* We found it but is it mounted for us to see?
             *
             * Note that in reality we ought to parse /proc/self/mountinfo and
             * look but in practice it's either only /sys/fs/cgroup in pure v2
             * mode or /sys/fs/cgroup/unified in hybrid mode (available since
             * kernel 4.5). */
            struct statfs stat_buf;
            if (statfs(cgroup_dir, &stat_buf) < 0) {
                if (errno != ENOENT) {
                    die("cannot statfs %s", cgroup_dir);
                }
            } else {
                if (stat_buf.f_type == CGROUP2_SUPER_MAGIC) {
                    sc_must_snprintf(path, path_size, "%s%s", cgroup_dir, attr_path);
                    found = true;
                    debug("cgroup pure v2 path is %s (preferred)", path);
                    break;
                }
            }

            if (statfs(cgroup_unified_dir, &stat_buf) < 0) {
                if (errno != ENOENT) {
                    die("cannot statfs %s", cgroup_unified_dir);
                }
            } else {
                if (stat_buf.f_type == CGROUP2_SUPER_MAGIC) {
                    sc_must_snprintf(path, path_size, "%s%s", cgroup_unified_dir, attr_path);
                    debug("cgroup hybrid v2 path is %s (preferred)", path);
                    found = true;
                    break;
                }
            }

            /* v2 is preferred, don't look more if we found it. */
            break;
        } else if (strcmp(attr_ctrl_list, "name=systemd") == 0) {
            /* We found it but is it mounted for us to see? */
            struct statfs stat_buf;
            if (statfs(cgroup_systemd_dir, &stat_buf) < 0) {
                if (errno != ENOENT) {
                    die("cannot statfs %s", cgroup_systemd_dir);
                }
            } else {
                if (stat_buf.f_type == CGROUP_SUPER_MAGIC) {
                    sc_must_snprintf(path, path_size, "%s%s", cgroup_systemd_dir, attr_path);
                    debug("cgroup v1 name=systemd is %s", path);
                    found = true;
                    /* keep looking, maybe there is more */
                }
            }
        }
    }
    free(line);
    fclose(stream);
    if (!found) {
        die("cannot find tracking cgroup path");
    }
}
#endif

void sc_join_sub_cgroup(const char *security_tag, pid_t pid) {
#if 0
    char current_hierarchy_path[PATH_MAX + 1] = {};
    sc_find_tracking_hierarchy(pid, current_hierarchy_path, sizeof current_hierarchy_path);
    sc_cgroup_create_and_join(current_hierarchy_path, security_tag, pid);
#else
    // TODO: reimplement this in C
    char cmd[1000];
    for (int attempt=0; attempt<3; ++attempt) {
        snprintf(cmd, sizeof cmd, "busctl %s call org.freedesktop.systemd1 /org/freedesktop/systemd1 org.freedesktop.systemd1.Manager StartTransientUnit 'ssa(sv)a(sa(sv))' %s.scope fail 2 PIDs au 1 %d Delegate b true 0",
                getuid() == 0 ? "--system" : "--user", security_tag, pid);
        debug("staring: %s", cmd);
        if (system(cmd) == 0) {
            return;
        }

        snprintf(cmd, sizeof cmd, "busctl %s call org.freedesktop.systemd1 /org/freedesktop/systemd1 org.freedesktop.systemd1.Manager AttachProcessesToUnit 'ssau' %s.scope '' 1 %d",
                getuid() == 0 ? "--system" : "--user", security_tag, pid);
        debug("staring: %s", cmd);
        if (system(cmd) == 0) {
            return;
        }
    }
    die("cannot start or join transient unit");
#endif
}
