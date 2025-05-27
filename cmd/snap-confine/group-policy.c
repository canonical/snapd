/*
 * Copyright (C) 2025 Canonical Ltd
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

#include <errno.h>
#include <fcntl.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>

#include "group-policy.h"

#include "../libsnap-confine-private/cleanup-funcs.h"
#include "../libsnap-confine-private/string-utils.h"
#include "../libsnap-confine-private/tools-dir.h"
#include "../libsnap-confine-private/utils.h"

static void sc_cleanup_gid_ts(gid_t **groups) {
    if (groups != NULL && *groups != NULL) {
        free(*groups);
        *groups = NULL;
    }
}

static bool sc_fstatat_host_snap_confine(int root_fd, struct stat *buf, sc_error **errorp) {
    char target[PATH_MAX] = {0};

    const char *pid_1_root = (root_fd == AT_FDCWD) ? "/proc/1/root" : "proc/1/root";

    memset(buf, 0, sizeof(*buf));
    /* let's try the default path */
    sc_must_snprintf(target, sizeof(target), "%s" SC_CANONICAL_HOST_TOOLS_DIR "/snap-confine", pid_1_root);

    if (fstatat(root_fd, target, buf, AT_SYMLINK_NOFOLLOW) != 0) {
        if (errno != ENOENT) {
            sc_error_forward(errorp, sc_error_init_from_errno(errno, "cannot fstatat() in canonical tools directory"));
            return false;
        }
    } else {
        debug("snap-confine found at %s", target);
        return true;
    }

    /* no dice, try the alternate path */
    memset(buf, 0, sizeof(*buf));
    sc_must_snprintf(target, sizeof(target), "%s" SC_ALTERNATE_HOST_TOOLS_DIR "/snap-confine", pid_1_root);
    debug("checking at %s", target);
    if (fstatat(root_fd, target, buf, AT_SYMLINK_NOFOLLOW) != 0) {
        if (errno != ENOENT) {
            sc_error_forward(errorp,
                             sc_error_init_from_errno(errno, "cannot fstatat() in alternative tools directory"));
            return false;
        }
    } else {
        debug("snap-confine found at %s", target);
        return true;
    }

    debug("s-c not found");
    sc_error_forward(errorp, sc_error_init_from_errno(ENOENT, "cannot locate snap-confine in host root filesystem"));
    return false;
}

/* lower level API to facilitate testing */
static bool _sc_assert_host_local_group_policy(int root_fd, gid_t real_gid, gid_t *groups, size_t groups_cnt,
                                               sc_error **errorp) {
    struct stat buf;
    sc_error *err = NULL;

    if (real_gid == 0) {
        debug("the user is member of root group");
        return true;
    }

    if (!sc_fstatat_host_snap_confine(root_fd, &buf, &err)) {
        sc_error_forward(errorp, err);
        return false;
    }

    if (buf.st_gid == 0) {
        /* owned by root */
        debug("host snap-confine is owned by root");
        return true;
    }

    if (real_gid == buf.st_gid) {
        debug("current user is a member of group owning snap-confine");
        return true;
    }

    for (size_t i = 0; i < groups_cnt; i++) {
        if (groups[i] == buf.st_gid) {
            debug("current user is a member of supplementary group owning snap-confine");
            return true;
        }
    }

    if (errorp != NULL) {
        *errorp = sc_error_init(
            SC_GROUP_DOMAIN, SC_NO_GROUP_PRIVS,
            "user is not a member of group owning snap-confine, check you distribution's policy for running snaps");
    }
    return false;
}

bool sc_assert_host_local_group_policy(int root_fd, sc_error **errorp) {
    /* check supplementary groups */
    int cnt = getgroups(0, NULL);
    if (cnt < 0) {
        sc_error_forward(errorp, sc_error_init_from_errno(errno, "cannot list supplementary groups"));
        return false;
    }

    gid_t *groups SC_CLEANUP(sc_cleanup_gid_ts) = NULL;

    if (cnt > 0) {
        groups = calloc(cnt, sizeof(gid_t));

        cnt = getgroups(cnt, groups);
        if (cnt < 0) {
            sc_error_forward(errorp, sc_error_init_from_errno(errno, "cannot list supplementary groups"));
            return false;
        }
    }

    return _sc_assert_host_local_group_policy(root_fd, getgid(), groups, cnt, errorp);
}
