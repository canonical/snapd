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

#include "config.h"
#include "mount-support-hybris.h"

#include <errno.h>
#include <fcntl.h>
#include <glob.h>
#include <stdlib.h>
#include <string.h>
#include <sys/mount.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <stdint.h>
#include <unistd.h>
/* POSIX version of basename() and dirname() */
#include <libgen.h>

#include "../libsnap-confine-private/classic.h"
#include "../libsnap-confine-private/cleanup-funcs.h"
#include "../libsnap-confine-private/string-utils.h"
#include "../libsnap-confine-private/utils.h"
#include "mount-support.h"

#define SC_LIBGL_DIR   SC_EXTRA_LIB_DIR "/gl"
#define SC_VULKAN_DIR  SC_EXTRA_LIB_DIR "/vulkan"
#define SC_GLVND_DIR  SC_EXTRA_LIB_DIR "/glvnd"

#define SC_HYBRIS_ROOTFS "/android"
#define SC_HYBRIS_SYSTEM_SYMLINK "/system"
#define SC_HYBRIS_VENDOR_SYMLINK "/vendor"
#define SC_HYBRIS_ODM_SYMLINK "/odm"
#define SC_HYBRIS_APEX_SYMLINK "/apex"
#define SC_HYBRIS_SYSTEM_SYMLINK_TARGET "/android/system"
#define SC_HYBRIS_VENDOR_SYMLINK_TARGET "/android/vendor"
#define SC_HYBRIS_ODM_SYMLINK_TARGET "/android/odm"
#define SC_HYBRIS_APEX_SYMLINK_TARGET "/android/apex"

static void sc_hybris_mount_android_rootfs(const char *rootfs_dir)
{
	// Bind mount a tmpfs on $rootfs_dir/$tgt_dir (i.e. /var/lib/snapd/lib/gl)
	char path_buf[PATH_MAX] = { 0 };
	sc_must_snprintf(path_buf, sizeof(path_buf), "%s%s", rootfs_dir, SC_HYBRIS_ROOTFS);
	const char *android_rootfs_dir = path_buf;

	sc_identity old = sc_set_effective_identity(sc_root_group_identity());
	int res = mkdir(android_rootfs_dir, 0755);
	if (res != 0 && errno != EEXIST) {
		die("cannot create bind-mount target %s", android_rootfs_dir);
	}
	if (res == 0 && (chown(android_rootfs_dir, 0, 0) < 0)) {
		// Adjust the ownership only if we created the directory.
		die("cannot change ownership of %s", android_rootfs_dir);
	}
	(void)sc_set_effective_identity(old);

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
}

void sc_mount_hybris_driver(const char *rootfs_dir, const char *base_snap_name)
{
	sc_identity old = sc_set_effective_identity(sc_root_group_identity());
	int res = sc_nonfatal_mkpath(SC_EXTRA_LIB_DIR, 0755);
	if (res != 0) {
		die("cannot create " SC_EXTRA_LIB_DIR);
	}
	if (res == 0 && (chown(SC_EXTRA_LIB_DIR, 0, 0) < 0)) {
		// Adjust the ownership only if we created the directory.
		die("cannot change ownership of " SC_EXTRA_LIB_DIR);
	}
	(void)sc_set_effective_identity(old);

	sc_hybris_mount_android_rootfs(rootfs_dir);
}
