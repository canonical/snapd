/*
 * Copyright (C) 2019-2021 Canonical Ltd
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

#define _GNU_SOURCE

#include "cgroup-support.h"

#include <dirent.h>
#include <errno.h>
#include <fcntl.h>
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
// https://systemd.io/CGROUP_DELEGATION/
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

static bool traverse_looking_for_prefix_in_dir(DIR *root, const char *prefix, const char *skip) {
    while (true) {
        errno = 0;
        struct dirent *ent = readdir(root);
        if (ent == NULL) {
            if (errno != 0) {
                die("cannot read directory entry");
            }
            break;
        }
        if (ent->d_type != DT_DIR) {
            continue;
        }
        if (sc_streq(ent->d_name, "..") || sc_streq(ent->d_name, ".")) {
            /* we don't want to go up or process the current directory again */
            continue;
        }
        if (sc_streq(ent->d_name, skip)) {
            continue;
        }
        if (sc_startswith(ent->d_name, prefix)) {
            debug("found matching prefix in \"%s\"", ent->d_name);
            /* the directory starts with our prefix */
            return true;
        }
        int entfd = openat(dirfd(root), ent->d_name, O_DIRECTORY | O_NOFOLLOW | O_CLOEXEC);
        if (entfd == -1) {
            die("cannot open directory entry \"%s\"", ent->d_name);
        }
        debug("got directory fd: %d", entfd);
        /* takes ownership o the file descriptor? */
        DIR *entdir SC_CLEANUP(sc_cleanup_closedir) = fdopendir(entfd);
        if (entdir == NULL) {
            die("cannot fdopendir directory \"%s\"", ent->d_name);
        }
        debug("descend into %s", ent->d_name);
        int found = traverse_looking_for_prefix_in_dir(entdir, prefix, skip);
        if (found == true) {
            return true;
        }
    }
    return false;
}

bool sc_cgroup_v2_is_tracking_snap(const char *snap_instance) {
    debug("is cgroup tracking snap %s?", snap_instance);
    char tracking_group_name[PATH_MAX] = {0};
    sc_must_snprintf(tracking_group_name, sizeof tracking_group_name, "snap.%s.", snap_instance);

    char *own_group SC_CLEANUP(sc_cleanup_string) = sc_cgroup_v2_own_path_full();
    if (own_group == NULL) {
        die("cannot obtain own group path");
    }
    debug("own group: %s", own_group);
    char *just_leaf = strrchr(own_group, '/');
    if (just_leaf == NULL) {
        die("cannot obtain the leaf group path");
    }
    /* pointing at /, advance to the next char */
    just_leaf += 1;
    debug("leap group: %s", just_leaf);

    // this would otherwise be inherently racy, but the caller is expected to
    // keep the snap instance lock, thus preventing new apps of that snap from
    // starting; not we can still return false positive if the currently running
    // process exits but we look at the hierarchy before systemd has cleaned up
    // the group

    bool found = false;
    debug("opening %s", cgroup_dir);
    DIR *root SC_CLEANUP(sc_cleanup_closedir) = opendir(cgroup_dir);
    if (root == NULL) {
        die("cannot open cgroup root dir");
    }
    found = traverse_looking_for_prefix_in_dir(root, tracking_group_name, just_leaf);
    return found;
}

static const char *self_cgroup = "/proc/self/cgroup";

char *sc_cgroup_v2_own_path_full(void) {
    FILE *in SC_CLEANUP(sc_cleanup_file) = fopen(self_cgroup, "r");
    if (in == NULL) {
        die("cannot open %s", self_cgroup);
    }

    char *own_group = NULL;

    while (true) {
        char *line SC_CLEANUP(sc_cleanup_string) = NULL;
        size_t linesz = 0;
        ssize_t sz = getline(&line, &linesz, in);
        if (sz == -1) {
            if (feof(in)) {
                break;
            }
            if (ferror(in)) {
                die("cannot read line from %s", self_cgroup);
            }
        }
        debug("got line: \'%s\'", line);
        if (!sc_startswith(line, "0::")) {
            continue;
        }
        size_t len = strlen(line);
        if (len <= 3) {
            die("unexpected content of group entry %s", line);
        }
        /* \n does not normally appear inside the group path, but if it did, it
         * would be escaped anyway */
        char *newline = strchr(line, '\n');
        if (newline != NULL) {
            *newline = '\0';
        }
        own_group = sc_strdup(line + 3);
        break;
    }
    return own_group;
}
