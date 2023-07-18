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
	char rfsbuf[512] = { 0 };
	sc_must_snprintf(rfsbuf, sizeof(rfsbuf), "%s%s", rootfs_dir, SC_HYBRIS_ROOTFS);
	const char *android_rootfs_dir = rfsbuf;

	char sysbuf[512] = { 0 };
	sc_must_snprintf(sysbuf, sizeof(sysbuf), "%s%s", rootfs_dir, SC_HYBRIS_SYSTEM_SYMLINK);
	const char *android_system_symlink = sysbuf;

	char vndbuf[512] = { 0 };
	sc_must_snprintf(vndbuf, sizeof(vndbuf), "%s%s", rootfs_dir, SC_HYBRIS_VENDOR_SYMLINK);
	const char *android_vendor_symlink = vndbuf;

	char odmbuf[512] = { 0 };
	sc_must_snprintf(odmbuf, sizeof(odmbuf), "%s%s", rootfs_dir, SC_HYBRIS_ODM_SYMLINK);
	const char *android_odm_symlink = odmbuf;

	char apxbuf[512] = { 0 };
	sc_must_snprintf(apxbuf, sizeof(apxbuf), "%s%s", rootfs_dir, SC_HYBRIS_APEX_SYMLINK);
	const char *android_apex_symlink = apxbuf;

	sc_identity old = sc_set_effective_identity(sc_root_group_identity());
	int res = mkdir(android_rootfs_dir, 0755);
	if (res != 0 && errno != EEXIST) {
		die("cannot create tmpfs target %s", android_rootfs_dir);
	}
	if (res == 0 && (chown(android_rootfs_dir, 0, 0) < 0)) {
		// Adjust the ownership only if we created the directory.
		die("cannot change ownership of %s", android_rootfs_dir);
	}
	(void)sc_set_effective_identity(old);

	(void)mount(SC_HYBRIS_ROOTFS, android_rootfs_dir, NULL, MS_BIND | MS_REC | MS_RDONLY, NULL);

	if (symlink(SC_HYBRIS_SYSTEM_SYMLINK_TARGET, android_system_symlink)) {
		die("Cannot set symlink for %s", SC_HYBRIS_SYSTEM_SYMLINK);
	}
	if (symlink(SC_HYBRIS_VENDOR_SYMLINK_TARGET, android_vendor_symlink)) {
		die("Cannot set symlink for %s", SC_HYBRIS_VENDOR_SYMLINK);
	}
	if (symlink(SC_HYBRIS_ODM_SYMLINK_TARGET, android_odm_symlink)) {
		die("Cannot set symlink for %s", SC_HYBRIS_ODM_SYMLINK);
	}
	if (symlink(SC_HYBRIS_APEX_SYMLINK_TARGET, android_apex_symlink)) {
		die("Cannot set symlink for %s", SC_HYBRIS_APEX_SYMLINK);
	}
}

// Populate libgl_dir with a symlink farm to files matching glob_list.
//
// The symbolic links are made in one of two ways. If the library found is a
// file a regular symlink "$libname" -> "/path/to/hostfs/$libname" is created.
// If the library is a symbolic link then relative links are kept as-is but
// absolute links are translated to have "/path/to/hostfs" up front so that
// they work after the pivot_root elsewhere.
//
// The glob list passed to us is produced with paths relative to source dir,
// to simplify the various tie-in points with this function.
static void sc_hybris_populate_libgl_with_hostfs_symlinks(const char *libgl_dir,
							  const char *source_dir,
							  const char *glob_list[],
							  size_t glob_list_len)
{
	size_t source_dir_len = strlen(source_dir);
	glob_t glob_res SC_CLEANUP(globfree) = {
		.gl_pathv = NULL
	};
	// Find all the entries matching the list of globs
	for (size_t i = 0; i < glob_list_len; ++i) {
		const char *glob_pattern = glob_list[i];
		char glob_pattern_full[512] = { 0 };
		sc_must_snprintf(glob_pattern_full, sizeof glob_pattern_full,
				 "%s/%s", source_dir, glob_pattern);

		int err = glob(glob_pattern_full, i ? GLOB_APPEND : 0, NULL,
			       &glob_res);
		// Not all of the files have to be there (they differ depending on the
		// driver version used). Ignore all errors that are not GLOB_NOMATCH.
		if (err != 0 && err != GLOB_NOMATCH) {
			die("cannot search using glob pattern %s: %d",
			    glob_pattern_full, err);
		}
	}
	// Symlink each file found
	for (size_t i = 0; i < glob_res.gl_pathc; ++i) {
		char symlink_name[512] = { 0 };
		char symlink_target[512] = { 0 };
		char prefix_dir[512] = { 0 };
		const char *pathname = glob_res.gl_pathv[i];
		char *pathname_copy1
		    SC_CLEANUP(sc_cleanup_string) = sc_strdup(pathname);
		char *pathname_copy2
		    SC_CLEANUP(sc_cleanup_string) = sc_strdup(pathname);
		// POSIX dirname() and basename() may modify their input arguments
		char *filename = basename(pathname_copy1);
		char *directory_name = dirname(pathname_copy2);
		sc_must_snprintf(prefix_dir, sizeof prefix_dir, "%s",
				 libgl_dir);

		if (strlen(directory_name) > source_dir_len) {
			// Additional path elements between source_dir and dirname, meaning the
			// actual file is not placed directly under source_dir but under one or
			// more directories below source_dir. Make sure to recreate the whole
			// prefix
			sc_must_snprintf(prefix_dir, sizeof prefix_dir,
					 "%s%s", libgl_dir,
					 &directory_name[source_dir_len]);
			sc_identity old =
			    sc_set_effective_identity(sc_root_group_identity());
			if (sc_nonfatal_mkpath(prefix_dir, 0755) != 0) {
				die("failed to create prefix path: %s",
				    prefix_dir);
			}
			(void)sc_set_effective_identity(old);
		}

		struct stat stat_buf;
		int err = lstat(pathname, &stat_buf);
		if (err != 0) {
			die("cannot stat file %s", pathname);
		}
		switch (stat_buf.st_mode & S_IFMT) {
		case S_IFLNK:;
			// Read the target of the symbolic link
			char hostfs_symlink_target[512] = { 0 };
			ssize_t num_read;
			hostfs_symlink_target[0] = 0;
			num_read =
			    readlink(pathname, hostfs_symlink_target,
				     sizeof hostfs_symlink_target - 1);
			if (num_read == -1) {
				die("cannot read symbolic link %s", pathname);
			}
			hostfs_symlink_target[num_read] = 0;
			if (hostfs_symlink_target[0] == '/') {
				sc_must_snprintf(symlink_target,
						 sizeof symlink_target,
						 "/var/lib/snapd/hostfs%s",
						 hostfs_symlink_target);
			} else {
				// Keep relative symlinks as-is, so that they point to -> libfoo.so.0.123
				sc_must_snprintf(symlink_target,
						 sizeof symlink_target, "%s",
						 hostfs_symlink_target);
			}
			break;
		case S_IFREG:
			sc_must_snprintf(symlink_target,
					 sizeof symlink_target,
					 "/var/lib/snapd/hostfs%s", pathname);
			break;
		default:
			debug("ignoring unsupported entry: %s", pathname);
			continue;
		}
		sc_must_snprintf(symlink_name, sizeof symlink_name,
				 "%s/%s", prefix_dir, filename);
		debug("creating symbolic link %s -> %s", symlink_name,
		      symlink_target);

		// Make sure we don't have some link already (merged GLVND systems)
		if (lstat(symlink_name, &stat_buf) == 0) {
			if (unlink(symlink_name) != 0) {
				die("cannot remove symbolic link target %s",
				    symlink_name);
			}
		}

		if (symlink(symlink_target, symlink_name) != 0) {
			die("cannot create symbolic link %s -> %s",
			    symlink_name, symlink_target);
		}
	}
}

static void sc_hybris_mkdir_and_mount_and_glob_files(const char *rootfs_dir,
					      const char *source_dir[],
					      size_t source_dir_len,
					      const char *tgt_dir,
					      const char *glob_list[],
					      size_t glob_list_len)
{
	// Bind mount a tmpfs on $rootfs_dir/$tgt_dir (i.e. /var/lib/snapd/lib/gl)
	char buf[512] = { 0 };
	sc_must_snprintf(buf, sizeof(buf), "%s%s", rootfs_dir, tgt_dir);
	const char *libgl_dir = buf;

	sc_identity old = sc_set_effective_identity(sc_root_group_identity());
	int res = mkdir(libgl_dir, 0755);
	if (res != 0 && errno != EEXIST) {
		die("cannot create tmpfs target %s", libgl_dir);
	}
	if (res == 0 && (chown(libgl_dir, 0, 0) < 0)) {
		// Adjust the ownership only if we created the directory.
		die("cannot change ownership of %s", libgl_dir);
	}
	(void)sc_set_effective_identity(old);

	debug("mounting tmpfs at %s", libgl_dir);
	if (mount("none", libgl_dir, "tmpfs", MS_NODEV | MS_NOEXEC, NULL) != 0) {
		die("cannot mount tmpfs at %s", libgl_dir);
	};

	for (size_t i = 0; i < source_dir_len; i++) {
		// Populate libgl_dir with symlinks to libraries from hostfs
		sc_hybris_populate_libgl_with_hostfs_symlinks(libgl_dir, source_dir[i],
						       glob_list,
						       glob_list_len);
	}
	// Remount $tgt_dir (i.e. .../lib/gl) read only
	debug("remounting tmpfs as read-only %s", libgl_dir);
	if (mount(NULL, buf, NULL, MS_REMOUNT | MS_BIND | MS_RDONLY, NULL) != 0) {
		die("cannot remount %s as read-only", buf);
	}
}

static void sc_hybris_mount_main(const char *rootfs_dir)
{
	const char *main_libs[] = {
		NATIVE_LIBDIR "/" HOST_ARCH_TRIPLET,
	};
	const size_t main_libs_len =
	    sizeof main_libs / sizeof *main_libs;

	sc_hybris_mkdir_and_mount_and_glob_files(rootfs_dir, main_libs,
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

	sc_hybris_mkdir_and_mount_and_glob_files(rootfs_dir, vulkan_sources,
					  vulkan_sources_len, SC_VULKAN_DIR,
					  hybris_vulkan_globs, hybris_vulkan_globs_len);
}

static void sc_hybris_mount_egl(const char *rootfs_dir)
{
	const char *egl_vendor_sources[] = { SC_EGL_VENDOR_SOURCE_DIR };
	const size_t egl_vendor_sources_len =
	    sizeof egl_vendor_sources / sizeof *egl_vendor_sources;

	sc_hybris_mkdir_and_mount_and_glob_files(rootfs_dir, egl_vendor_sources,
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
