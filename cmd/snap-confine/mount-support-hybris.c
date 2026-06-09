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

#include "mount-support-hybris.h"
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
#include "mount-support.h"

#define SC_HYBRIS_ROOTFS "/android"

#define SC_HYBRIS_SYSTEM_SYMLINK "/system"
#define SC_HYBRIS_VENDOR_SYMLINK "/vendor"
#define SC_HYBRIS_ODM_SYMLINK "/odm"
#define SC_HYBRIS_APEX_SYMLINK "/apex"
#define SC_HYBRIS_LINKERCONFIG_SYMLINK "/linkerconfig"

#define SC_HYBRIS_SYSTEM_SYMLINK_TARGET "/android/system"
#define SC_HYBRIS_VENDOR_SYMLINK_TARGET "/android/vendor"
#define SC_HYBRIS_ODM_SYMLINK_TARGET "/android/odm"
#define SC_HYBRIS_APEX_SYMLINK_TARGET "/android/apex"
#define SC_HYBRIS_LINKERCONFIG_SYMLINK_TARGET "/android/linkerconfig"

static void sc_hybris_mount_android_rootfs(const char *rootfs_dir) {
    // Bind mount the Halium rootfs as set up by the host, in /android
    char path_buf[PATH_MAX] = {0};
    sc_must_snprintf(path_buf, sizeof(path_buf), "%s%s", rootfs_dir, SC_HYBRIS_ROOTFS);
    const char *android_rootfs_dir = path_buf;

    int res = mkdir(android_rootfs_dir, 0755);
    if (res != 0 && errno != EEXIST) {
        die("cannot create bind-mount target %s", android_rootfs_dir);
    }
    if (res == 0 && (chown(android_rootfs_dir, 0, 0) < 0)) {
        // Adjust the ownership only if we created the directory.
        die("cannot change ownership of %s", android_rootfs_dir);
    }

    if (mount(SC_HYBRIS_ROOTFS, android_rootfs_dir, NULL, MS_BIND | MS_REC | MS_RDONLY, NULL)) {
        die("Cannot mount Halium environment into target");
    }

    sc_must_snprintf(path_buf, sizeof(path_buf), "%s%s", rootfs_dir, SC_HYBRIS_SYSTEM_SYMLINK);
    const char *android_system_symlink = path_buf;
    if (symlink(SC_HYBRIS_SYSTEM_SYMLINK_TARGET, android_system_symlink)) {
        die("Cannot set symlink for %s", SC_HYBRIS_SYSTEM_SYMLINK);
    }

    sc_must_snprintf(path_buf, sizeof(path_buf), "%s%s", rootfs_dir, SC_HYBRIS_VENDOR_SYMLINK);
    const char *android_vendor_symlink = path_buf;
    if (symlink(SC_HYBRIS_VENDOR_SYMLINK_TARGET, android_vendor_symlink)) {
        die("Cannot set symlink for %s", SC_HYBRIS_VENDOR_SYMLINK);
    }

    sc_must_snprintf(path_buf, sizeof(path_buf), "%s%s", rootfs_dir, SC_HYBRIS_ODM_SYMLINK);
    const char *android_odm_symlink = path_buf;
    if (symlink(SC_HYBRIS_ODM_SYMLINK_TARGET, android_odm_symlink)) {
        die("Cannot set symlink for %s", SC_HYBRIS_ODM_SYMLINK);
    }

    sc_must_snprintf(path_buf, sizeof(path_buf), "%s%s", rootfs_dir, SC_HYBRIS_APEX_SYMLINK);
    const char *android_apex_symlink = path_buf;
    if (symlink(SC_HYBRIS_APEX_SYMLINK_TARGET, android_apex_symlink)) {
        die("Cannot set symlink for %s", SC_HYBRIS_APEX_SYMLINK);
    }

    sc_must_snprintf(path_buf, sizeof(path_buf), "%s%s", rootfs_dir, SC_HYBRIS_LINKERCONFIG_SYMLINK);
    const char *android_linkerconfig_symlink = path_buf;
    if (symlink(SC_HYBRIS_LINKERCONFIG_SYMLINK_TARGET, android_linkerconfig_symlink)) {
        die("Cannot set symlink for %s", SC_HYBRIS_LINKERCONFIG_SYMLINK);
    }
}

int sc_mount_is_halium_system(void) {
    // Halium-typical paths to check for
    // snapd's "opengl" interface takes care of exposing it all
    // to the confined environment
    static const char *halium_paths[] = {
        "/system/build.prop",
#ifdef __LP64__
        "/system/lib64/libEGL.so",
#else
        "/system/lib/libEGL.so"
#endif
    };
    static const char *halium_mountpoints[] = {"/android/vendor", "/android/data", "/android/cache"};
    static const char *halium_symlinks[] = {"/system", "/vendor", "/odm", "/data"};
    static const char *binder_paths[] = {"/dev/binderfs/binder", "/dev/binderfs/hwbinder", "/dev/binder",
                                         "/dev/hwbinder"};

    // Check if this is running on a system with binder devices, which all Halium systems do.
    bool has_binder = false;
    for (long unsigned int i = 0; i < sizeof(binder_paths) / sizeof(binder_paths[0]); i++) {
        struct stat info;
        if (stat(binder_paths[i], &info) == 0) {
            has_binder = true;
            break;
        }
    }
    if (!has_binder) {
        return 0;
    }

    // These are required mountpoints which are used by our Halium LXC container
    for (long unsigned int i = 0; i < sizeof(halium_mountpoints) / sizeof(halium_mountpoints[0]); i++) {
        struct stat info;
        if (stat(halium_mountpoints[i], &info) != 0) {
            return 0;
        }
    }

    // Next check for commonly required host-side files we want to pass
    for (long unsigned int i = 0; i < sizeof(halium_paths) / sizeof(halium_paths[0]); i++) {
        struct stat info;
        if (stat(halium_paths[i], &info) != 0) {
            return 0;
        }
    }

    // These symlinks must exist in GNU/Linux Land for hybris to work
    for (long unsigned int i = 0; i < sizeof(halium_symlinks) / sizeof(halium_symlinks[0]); i++) {
        struct stat info;
        if (lstat(halium_symlinks[i], &info) != 0) {
            return 0;
        }
    }

    return 1;
}

void sc_mount_hybris_driver(const char *rootfs_dir, const char *base_snap_name) {
    // Only proceed if this has been identified as a Halium system, on Ubuntu Touch
    // we require access to a lot of unvetted libraries and unmediated IPC, and
    // misc required files. No executable bits allowed, this is the most fine-grained
    // while future-resiliant as patched-in, shipped and used by Ubuntu Touch users.
    if (!sc_mount_is_halium_system()) {
        return;
    }

    sc_hybris_mount_android_rootfs(rootfs_dir);
}
