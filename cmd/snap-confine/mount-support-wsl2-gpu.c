/*
 * Copyright (C) 2026 Canonical Ltd
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
#include "../libsnap-confine-private/mount-opt.h"
#include "../libsnap-confine-private/string-utils.h"
#include "../libsnap-confine-private/utils.h"
#include "mount-support.h"

#define SC_WSL_GPU_DIR SC_EXTRA_LIB_DIR "/wsl"

#define SC_HOST_WSL_DIR "/usr/lib/wsl/lib"

void sc_mount_wsl2_gpu_driver(const char *rootfs) {
    /* If WSL2 GPU libraries aren't mounted in the host, don't attempt to mount the drivers */
    if (access(SC_HOST_WSL_DIR, F_OK) != 0) {
        return;
    }
    if (sc_nonfatal_mkpath(SC_EXTRA_LIB_DIR, 0755, 0, 0) != 0) {
        die("cannot create " SC_EXTRA_LIB_DIR);
    }
    if (sc_ensure_mkdir(SC_WSL_GPU_DIR, 0755, 0, 0) != 0) {
        die("cannot create directory %s", SC_WSL_GPU_DIR);
    }
    // Bind mount the binary WSL2 GPU driver into $dst_dir (i.e. /var/lib/snapd/lib/wsl).
    debug("bind mounting WSL2 GPU driver %s -> %s", SC_HOST_WSL_DIR, SC_WSL_GPU_DIR);
    sc_do_mount(SC_HOST_WSL_DIR, SC_WSL_GPU_DIR, NULL, MS_BIND, NULL);
}
