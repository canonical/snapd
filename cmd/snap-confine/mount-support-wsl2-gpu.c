/*
 * Copyright (C) 2022 Canonical Ltd
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

#include "mount-support-wsl2-gpu.h"
#include "config.h"

#include <errno.h>
#include <fcntl.h>
#include <glob.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <sys/mount.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <unistd.h>
/* POSIX version of basename() and dirname() */
#include <libgen.h>

#include "../libsnap-confine-private/classic.h"
#include "../libsnap-confine-private/cleanup-funcs.h"
#include "../libsnap-confine-private/string-utils.h"
#include "../libsnap-confine-private/utils.h"

// note: if the parent dir changes to something other than
// the current /var/lib/snapd/lib then sc_mkdir_and_mount_and_bind
// and sc_mkdir_and_mount_and_bind need updating.
#define SC_LIB "/var/lib/snapd/lib"
#define SC_WSL_GPU_DIR SC_LIB "/wsl"

#define SC_HOST_WSL_DIR "/usr/lib/wsl/lib"

static void sc_mkdir_and_mount_and_bind_wsl_gpu(const char *rootfs_dir, const char *src_dir, const char *dst_dir) {
    // If there is no userspace driver available then don't try to mount it.
    if (access(src_dir, F_OK) != 0) {
        return;
    }
    sc_identity old = sc_set_effective_identity(sc_root_group_identity());
    int res = mkdir(dst_dir, 0755);
    if (res != 0 && errno != EEXIST) {
        die("cannot create directory %s", dst_dir);
    }
    if (res == 0 && (chown(dst_dir, 0, 0) < 0)) {
        // Adjust the ownership only if we created the directory.
        die("cannot change ownership of %s", dst_dir);
    }
    (void)sc_set_effective_identity(old);
    // Bind mount the binary WSL2 GPU driver into $dst_dir (i.e. /var/lib/snapd/lib/wsl).
    debug("bind mounting WSL2 GPU driver %s -> %s", src_dir, dst_dir);
    if (mount(src_dir, dst_dir, NULL, MS_BIND, NULL) != 0) {
        die("cannot bind mount WSL2 GPU driver %s -> %s", src_dir, dst_dir);
    }
}

void sc_mount_wsl2_gpu_driver(const char *rootfs_dir) {
    /* If WSL2 GPU libraries aren't mounted in the host, don't attempt to mount the drivers */
    if (access(SC_HOST_WSL_DIR, F_OK) != 0) {
        return;
    }

    sc_identity old = sc_set_effective_identity(sc_root_group_identity());
    int res = sc_nonfatal_mkpath(SC_LIB, 0755);
    if (res != 0) {
        die("cannot create " SC_LIB);
    }
    if (res == 0 && (chown(SC_LIB, 0, 0) < 0)) {
        // Adjust the ownership only if we created the directory.
        die("cannot change ownership of " SC_LIB);
    }
    (void)sc_set_effective_identity(old);
    sc_mkdir_and_mount_and_bind_wsl_gpu(rootfs_dir, SC_HOST_WSL_DIR, SC_WSL_GPU_DIR);
}
