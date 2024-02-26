/*
 * Copyright (C) 2018 Canonical Ltd
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

#include "feature.h"

#include <errno.h>
#include <fcntl.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <unistd.h>

#include "cleanup-funcs.h"
#include "utils.h"

static const char *feature_flag_dir = "/var/lib/snapd/features";

bool sc_feature_enabled(sc_feature_flag flag) {
    const char *file_name;
    switch (flag) {
        case SC_FEATURE_PER_USER_MOUNT_NAMESPACE:
            file_name = "per-user-mount-namespace";
            break;
        case SC_FEATURE_REFRESH_APP_AWARENESS:
            file_name = "refresh-app-awareness";
            break;
        case SC_FEATURE_PARALLEL_INSTANCES:
            file_name = "parallel-instances";
            break;
        case SC_FEATURE_HIDDEN_SNAP_FOLDER:
            file_name = "hidden-snap-folder";
            break;
        default:
            die("unknown feature flag code %d", flag);
    }

    int dirfd SC_CLEANUP(sc_cleanup_close) = -1;
    dirfd = open(feature_flag_dir, O_CLOEXEC | O_DIRECTORY | O_NOFOLLOW | O_PATH);
    if (dirfd < 0 && errno == ENOENT) {
        return false;
    }
    if (dirfd < 0) {
        die("cannot open path %s", feature_flag_dir);
    }

    struct stat file_info;
    if (fstatat(dirfd, file_name, &file_info, AT_SYMLINK_NOFOLLOW) < 0) {
        if (errno == ENOENT) {
            return false;
        }
        die("cannot inspect file %s/%s", feature_flag_dir, file_name);
    }

    return S_ISREG(file_info.st_mode);
}
