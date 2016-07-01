/*
 * Copyright (C) 2015 Canonical Ltd
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
#include "mount-support-nvidia.h"

#include <fcntl.h>
#include <glob.h>
#include <stdlib.h>
#include <string.h>
#include <sys/mount.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <unistd.h>

#include "utils.h"
#include "cleanup-funcs.h"

#ifdef NVIDIA_ARCH

// List of globs that describe nvidia userspace libraries.
// This list was compiled from the following packages.
//
// https://www.archlinux.org/packages/extra/x86_64/nvidia-304xx-libgl/files/
// https://www.archlinux.org/packages/extra/x86_64/nvidia-304xx-utils/files/
// https://www.archlinux.org/packages/extra/x86_64/nvidia-340xx-libgl/files/
// https://www.archlinux.org/packages/extra/x86_64/nvidia-340xx-utils/files/
// https://www.archlinux.org/packages/extra/x86_64/nvidia-libgl/files/
// https://www.archlinux.org/packages/extra/x86_64/nvidia-utils/files/
//
// FIXME: this doesn't yet work with libGLX and libglvnd redirector
// FIXME: this still doesn't work with the 361 driver
static const char *nvidia_globs[] = {
	"/usr/lib/libEGL.so*",
	"/usr/lib/libEGL_nvidia.so*",
	"/usr/lib/libGL.so*",
	"/usr/lib/libOpenGL.so*",
	"/usr/lib/libGLESv1_CM.so*",
	"/usr/lib/libGLESv1_CM_nvidia.so*",
	"/usr/lib/libGLESv2.so*",
	"/usr/lib/libGLESv2_nvidia.so*",
	"/usr/lib/libGLX_indirect.so*",
	"/usr/lib/libGLX_nvidia.so*",
	"/usr/lib/libGLX.so*",
	"/usr/lib/libGLdispatch.so*",
	"/usr/lib/libGLU.so*",
	"/usr/lib/libXvMCNVIDIA.so*",
	"/usr/lib/libXvMCNVIDIA_dynamic.so*",
	"/usr/lib/libcuda.so*",
	"/usr/lib/libnvcuvid.so*",
	"/usr/lib/libnvidia-cfg.so*",
	"/usr/lib/libnvidia-compiler.so*",
	"/usr/lib/libnvidia-eglcore.so*",
	"/usr/lib/libnvidia-encode.so*",
	"/usr/lib/libnvidia-fatbinaryloader.so*",
	"/usr/lib/libnvidia-fbc.so*",
	"/usr/lib/libnvidia-glcore.so*",
	"/usr/lib/libnvidia-glsi.so*",
	"/usr/lib/libnvidia-ifr.so*",
	"/usr/lib/libnvidia-ml.so*",
	"/usr/lib/libnvidia-ptxjitcompiler.so*",
	"/usr/lib/libnvidia-tls.so*",
};

static const size_t nvidia_globs_len =
    sizeof nvidia_globs / sizeof *nvidia_globs;

// Populate libgl_dir with a symlink farm to files matching glob_list.
//
// The symbolic links are made in one of two ways. If the library found is a
// file a regular symlink "$libname" -> "/path/to/hostfs/$libname" is created.
// If the library is a symbolic link then relative links are kept as-is but
// absolute links are translated to have "/path/to/hostfs" up front so that
// they work after the pivot_root elsewhere.
static void sc_populate_libgl_with_hostfs_symlinks(const char *libgl_dir,
						   const char *glob_list[],
						   size_t glob_list_len)
{
	glob_t glob_res __attribute__ ((__cleanup__(globfree))) = {
	.gl_pathv = NULL};
	// Find all the entries matching the list of globs
	for (size_t i = 0; i < glob_list_len; ++i) {
		const char *glob_pattern = glob_list[i];
		int err =
		    glob(glob_pattern, i ? GLOB_APPEND : 0, NULL, &glob_res);
		// Not all of the files have to be there (they differ depending on the
		// driver version used). Ignore all errors that are not GLOB_NOMATCH.
		if (err != 0 && err != GLOB_NOMATCH) {
			die("cannot search using glob pattern %s: %d",
			    glob_pattern, err);
		}
	}
	// Symlink each file found
	for (size_t i = 0; i < glob_res.gl_pathc; ++i) {
		char symlink_name[512];
		char symlink_target[512];
		const char *pathname = glob_res.gl_pathv[i];
		char *pathname_copy
		    __attribute__ ((cleanup(sc_cleanup_string))) =
		    strdup(pathname);
		char *filename = basename(pathname_copy);
		struct stat stat_buf;
		int err = lstat(pathname, &stat_buf);
		if (err != 0) {
			die("cannot stat file %s", pathname);
		}
		switch (stat_buf.st_mode & S_IFMT) {
		case S_IFLNK:;
			// Read the target of the symbolic link
			char hostfs_symlink_target[512];
			ssize_t num_read;
			hostfs_symlink_target[0] = 0;
			num_read =
			    readlink(pathname, hostfs_symlink_target,
				     sizeof hostfs_symlink_target);
			if (num_read == -1) {
				die("cannot read symbolic link %s", pathname);
			}
			hostfs_symlink_target[num_read] = 0;
			if (hostfs_symlink_target[0] == '/') {
				must_snprintf(symlink_target,
					      sizeof symlink_target,
					      "/var/lib/snapd/hostfs%s",
					      hostfs_symlink_target);
			} else {
				// Keep relative symlinks as-is, so that they point to -> libfoo.so.0.123
				must_snprintf(symlink_target,
					      sizeof symlink_target, "%s",
					      hostfs_symlink_target);
			}
			break;
		case S_IFREG:
			must_snprintf(symlink_target,
				      sizeof symlink_target,
				      "/var/lib/snapd/hostfs%s", pathname);
			break;
		default:
			debug("ignoring unsupported entry: %s", pathname);
			continue;
		}
		must_snprintf(symlink_name, sizeof symlink_name,
			      "%s/%s", libgl_dir, filename);
		debug("creating symbolic link %s -> %s", symlink_name,
		      symlink_target);
		if (symlink(symlink_target, symlink_name) != 0) {
			die("cannot create symbolic link %s -> %s",
			    symlink_name, symlink_target);
		}
	}
}

static void sc_mount_nvidia_driver_arch(const char *rootfs_dir)
{
	// Bind mount a tmpfs on $rootfs_dir/var/lib/snapd/lib/gl
	char buf[512];
	must_snprintf(buf, sizeof(buf), "%s%s", rootfs_dir,
		      "/var/lib/snapd/lib/gl");
	const char *libgl_dir = buf;
	debug("mounting tmpfs at %s", libgl_dir);
	if (mount("none", libgl_dir, "tmpfs", MS_NODEV | MS_NOEXEC, NULL) != 0) {
		die("cannot mount tmpfs at %s", libgl_dir);
	};
	// Populate libgl_dir with symlinks to libraries from hostfs
	sc_populate_libgl_with_hostfs_symlinks(libgl_dir, nvidia_globs,
					       nvidia_globs_len);
	// Remount .../lib/gl read only
	debug("remounting tmpfs as read-only %s", libgl_dir);
	if (mount(NULL, buf, NULL, MS_REMOUNT | MS_RDONLY, NULL) != 0) {
		die("cannot remount %s as read-only", buf);
	}
}

#endif				// ifdef NVIDIA_ARCH

#ifdef NVIDIA_UBUNTU
static void sc_mount_nvidia_driver_ubuntu(const char *rootfs_dir)
{
	// The driver can be in one of a few locations. On some distributions
	// it is /usr/lib/nvidia-{xxx} (where xxx is the version number)
	// on other distributions it is just /usr/lib/nvidia.
	// Before this is all made easy by snapd and the mount security backend
	// we just look in all the possible places.
	const char *glob_pattern = "/usr/lib/nvidia-[1-9][0-9][0-9]";
	glob_t glob_res __attribute__ ((__cleanup__(globfree))) = {
	.gl_pathv = NULL};
	int err = glob(glob_pattern, GLOB_ONLYDIR | GLOB_MARK, NULL, &glob_res);
	debug("glob(%s, ...) returned %d", glob_pattern, err);
	switch (glob_res.gl_pathc) {
	case 0:
		debug("cannot find any nvidia drivers");
		break;
	case 1:;
		// Bind mount the binary nvidia driver into /var/lib/snapd/lib/gl.
		const char *src = glob_res.gl_pathv[0];
		char buf[512];
		must_snprintf(buf, sizeof(buf), "%s%s", rootfs_dir,
			      "/var/lib/snapd/lib/gl");
		const char *dst = buf;

		debug("bind mounting nvidia driver %s -> %s", src, dst);
		if (mount(src, dst, NULL, MS_BIND, NULL) != 0) {
			die("cannot bind mount nvidia driver %s -> %s",
			    src, dst);
		}
		break;
	default:
		die("multiple nvidia drivers detected, this is not supported");
		break;
	}
}
#endif				// ifdef NVIDIA_UBUNTU

void sc_mount_nvidia_driver(const char *rootfs_dir)
{
#ifdef NVIDIA_UBUNTU
	sc_mount_nvidia_driver_ubuntu(rootfs_dir);
#endif				// ifdef NVIDIA_UBUNTU
#ifdef NVIDIA_ARCH
	sc_mount_nvidia_driver_arch(rootfs_dir);
#endif				// ifdef NVIDIA_ARCH
}
