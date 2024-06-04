/*
 * Copyright (C) 2023 Canonical Ltd
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

#define SC_HYBRIS_PROPERTY_FILE "/system/build.prop"

#define SC_LIBGL_DIR   SC_EXTRA_LIB_DIR "/gl"
#define SC_VULKAN_DIR  SC_EXTRA_LIB_DIR "/vulkan"
#define SC_GLVND_DIR  SC_EXTRA_LIB_DIR "/glvnd"

#define SC_VULKAN_SOURCE_DIR "/usr/share/vulkan"
#define SC_EGL_VENDOR_SOURCE_DIR "/usr/share/glvnd"

#define SC_HYBRIS_ROOTFS "/android"
#define SC_HYBRIS_SYSTEM_SYMLINK "/system"
#define SC_HYBRIS_VENDOR_SYMLINK "/vendor"
#define SC_HYBRIS_ODM_SYMLINK "/odm"
#define SC_HYBRIS_APEX_SYMLINK "/apex"
#define SC_HYBRIS_SYSTEM_SYMLINK_TARGET "/android/system"
#define SC_HYBRIS_VENDOR_SYMLINK_TARGET "/android/vendor"
#define SC_HYBRIS_ODM_SYMLINK_TARGET "/android/odm"
#define SC_HYBRIS_APEX_SYMLINK_TARGET "/android/apex"

static const char *hybris_globs[] = {
	"libEGL_libhybris.so*",
	"libGLESv1_CM_libhybris.so*",
	"libGLESv2_libhybris.so*",
	"libhybris-common.so*",
	"libhybris-platformcommon.so*",
	"libhybris-eglplatformcommon.so*",
	"libgralloc.so*",
	"libsync.so*",
	"libhardware.so*",
	"libui.so*",
	"libhybris/eglplatform_*.so",
	"libhybris/linker/*.so"
};

static const size_t hybris_globs_len =
    sizeof hybris_globs / sizeof *hybris_globs;

// Location for libhybris vulkan files (including _wayland)
static const char *hybris_vulkan_globs[] = {
	"icd.d/*hybris*.json",
};

static const size_t hybris_vulkan_globs_len =
    sizeof hybris_vulkan_globs / sizeof *hybris_vulkan_globs;

// Location of EGL vendor files
static const char *hybris_egl_vendor_globs[] = {
	"egl_vendor.d/*hybris*.json",
};

static const size_t hybris_egl_vendor_globs_len =
    sizeof hybris_egl_vendor_globs / sizeof *hybris_egl_vendor_globs;

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

static void sc_hybris_mount_main(const char *rootfs_dir)
{
	const char *main_libs[] = {
		NATIVE_LIBDIR "/" HOST_ARCH_TRIPLET,
	};
	const size_t main_libs_len =
	    sizeof main_libs / sizeof *main_libs;

	sc_mkdir_and_mount_and_glob_files(rootfs_dir, main_libs,
					  main_libs_len, SC_LIBGL_DIR,
					  hybris_globs, hybris_globs_len);
}

static void sc_hybris_mount_vulkan(const char *rootfs_dir)
{
	const char *vulkan_sources[] = {
		SC_VULKAN_SOURCE_DIR,
	};
	const size_t vulkan_sources_len =
	    sizeof vulkan_sources / sizeof *vulkan_sources;

	sc_mkdir_and_mount_and_glob_files(rootfs_dir, vulkan_sources,
					  vulkan_sources_len, SC_VULKAN_DIR,
					  hybris_vulkan_globs, hybris_vulkan_globs_len);
}

static void sc_hybris_mount_egl(const char *rootfs_dir)
{
	const char *egl_vendor_sources[] = { SC_EGL_VENDOR_SOURCE_DIR };
	const size_t egl_vendor_sources_len =
	    sizeof egl_vendor_sources / sizeof *egl_vendor_sources;

	sc_mkdir_and_mount_and_glob_files(rootfs_dir, egl_vendor_sources,
					  egl_vendor_sources_len, SC_GLVND_DIR,
					  hybris_egl_vendor_globs,
					  hybris_egl_vendor_globs_len);
}

void sc_mount_hybris_driver(const char *rootfs_dir, const char *base_snap_name)
{
	/* If a hybris-typical property file doesn't exist, don't attempt to mount the drivers */
	if (access(SC_HYBRIS_PROPERTY_FILE, F_OK) != 0) {
		return;
	}

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
	sc_hybris_mount_main(rootfs_dir);
	sc_hybris_mount_vulkan(rootfs_dir);
	sc_hybris_mount_egl(rootfs_dir);
}
