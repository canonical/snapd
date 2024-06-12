/*
 * Copyright (C) 2024 Canonical Ltd
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

#include "snap-dir.h"

#include <errno.h>
#include <fcntl.h>
#include <limits.h>
#include <stddef.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>

#include "snap.h"  // For SC_SNAP_MOUNT_DIR_UNSUPPORTED

static const char *_snap_mount_dir = NULL;

// Function is exported only for tests.
void sc_set_snap_mount_dir(const char *dir);
void sc_set_snap_mount_dir(const char *dir) { _snap_mount_dir = dir; }

const char *sc_snap_mount_dir(sc_error **errorp) {
    sc_error *err = NULL;

    if (_snap_mount_dir == NULL) {
        err = sc_error_init_api_misuse("sc_probe_snap_mount_dir_from_pid_1_mount_ns was not called yet");
    }

    sc_error_forward(errorp, err);
    return _snap_mount_dir;
}

void sc_probe_snap_mount_dir_from_pid_1_mount_ns(int root_fd, sc_error **errorp) {
    char target[PATH_MAX] = {0};
    struct stat sb;
    ssize_t n = 0;
    sc_error *err = NULL;

    const char *const probe_dir =
        /* Depending on the value of the root_fd descriptor, the probe path is either absolute or relative.*/
        root_fd == AT_FDCWD
            /* If we are given the special AT_FDCWD descriptor then probe the path "/proc/1/root/snap". */
            ? "/proc/1/root" SC_CANONICAL_SNAP_MOUNT_DIR
            /* If we are given any other descriptor the probe a relative path "proc/1/root/snap",
             * which is relative to root_fd. */
            : "proc/1/root" SC_CANONICAL_SNAP_MOUNT_DIR;

    // If /snap does not exist, then we assume the fallback directory is used.
    if (fstatat(root_fd, probe_dir, &sb, AT_SYMLINK_NOFOLLOW) != 0) {
        if (errno != ENOENT) {
            err = sc_error_init_from_errno(errno, "cannot fstatat canonical snap directory");
            goto out;
        }

        _snap_mount_dir = SC_ALTERNATE_SNAP_MOUNT_DIR;
        return;
    }

    // If /snap exists it must be either a directory or a symbolic link.
    switch (sb.st_mode & S_IFMT) {
        case S_IFDIR:
            _snap_mount_dir = SC_CANONICAL_SNAP_MOUNT_DIR;
            break;

        case S_IFLNK:
            // If /snap exits and is a symbolic link it must point to the fallback directory.
            n = readlinkat(root_fd, probe_dir, target, sizeof target);
            if (n < 0 || n == sizeof(target)) {
                err = sc_error_init_from_errno(errno, "cannot readlinkat canonical snap directory");
                goto out;
            }

            if (strncmp(target, SC_ALTERNATE_SNAP_MOUNT_DIR, n) != 0) {
                err = sc_error_init(SC_SNAP_DOMAIN, SC_SNAP_MOUNT_DIR_UNSUPPORTED,
                                    SC_CANONICAL_SNAP_MOUNT_DIR
                                    " must be a symbolic link to " SC_ALTERNATE_SNAP_MOUNT_DIR);
                goto out;
            }

            _snap_mount_dir = SC_ALTERNATE_SNAP_MOUNT_DIR;
            break;

        default:
            err = sc_error_init(SC_SNAP_DOMAIN, SC_SNAP_MOUNT_DIR_UNSUPPORTED,
                                SC_CANONICAL_SNAP_MOUNT_DIR
                                " must be a directory or a symbolic link to " SC_ALTERNATE_SNAP_MOUNT_DIR);
            goto out;
            break;
    }

out:
    sc_error_forward(errorp, err);
}
