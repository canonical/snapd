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

#define SC_NVIDIA_DRIVER_VERSION_FILE "/sys/module/nvidia/version"

#define SC_LIBGL_DIR   SC_EXTRA_LIB_DIR "/gl"
#define SC_LIBGL32_DIR SC_EXTRA_LIB_DIR "/gl32"
#define SC_VULKAN_DIR  SC_EXTRA_LIB_DIR "/vulkan"
#define SC_GLVND_DIR  SC_EXTRA_LIB_DIR "/glvnd"

#define SC_VULKAN_SOURCE_DIR "/usr/share/vulkan"
#define SC_GLVND_VENDOR_SOURCE_DIR "/usr/share/glvnd"
#define SC_EGL_VENDOR_SOURCE_DIR "/usr/share/egl"

#define SC_NATIVE_LIBVA_DIR NATIVE_LIBDIR "/" HOST_ARCH_TRIPLET "/dri"

// Location for NVIDIA vulkan files (including _wayland)
static const char *vulkan_globs[] = {
	"icd.d/*nvidia*.json",
};

static const size_t vulkan_globs_len =
    sizeof vulkan_globs / sizeof *vulkan_globs;

// Location for NVIDIA DRI files.
static const char *libvaglobs[] = {
	"nvidia_drv_video.so*",
};

static const size_t libvaglobs_len =
    sizeof libvaglobs / sizeof *libvaglobs;

// Location of EGL vendor files
static const char *glvnd_vendor_globs[] = {
	"egl_vendor.d/*nvidia*.json",
};

static const size_t glvnd_vendor_globs_len =
    sizeof glvnd_vendor_globs / sizeof *glvnd_vendor_globs;

static const char *egl_vendor_globs[] = {
	"egl_external_platform.d/*nvidia*.json",
};

static const size_t egl_vendor_globs_len =
    sizeof egl_vendor_globs / sizeof *egl_vendor_globs;

static char *overlay_dir;

#if defined(NVIDIA_BIARCH) || defined(NVIDIA_MULTIARCH)

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
	"libEGL_nvidia.so*",
	"libGLESv1_CM_nvidia.so*",
	"libGLESv2_nvidia.so*",
	"libGLX_nvidia.so*",
	"libXvMCNVIDIA.so*",
	"libXvMCNVIDIA_dynamic.so*",
	"libnvidia-cfg.so*",
	"libnvidia-compiler.so*",
	"libnvidia-eglcore.so*",
	"libnvidia-egl*",
	"libnvidia-encode.so*",
	"libnvidia-fatbinaryloader.so*",
	"libnvidia-fbc.so*",
	"libnvidia-glcore.so*",
	"libnvidia-glsi.so*",
	"libnvidia-glvkspirv.so*",
	"libnvidia-gpucomp.so*",
	"libnvidia-ifr.so*",
	"libnvidia-ml.so*",
	"libnvidia-opencl.so*",
	"libnvidia-opticalflow.so*",
	"libnvidia-ptxjitcompiler.so*",
	"libnvidia-rtcore.so*",
	"libnvidia-tls.so*",
	"libnvoptix.so*",
	"tls/libnvidia-tls.so*",
	"vdpau/libvdpau_nvidia.so*",

	// additional libraries for Tegra
	// https://docs.nvidia.com/jetson/l4t/index.html#page/Tegra%20Linux%20Driver%20Package%20Development%20Guide/manifest_tx2_tx2i.html
	"libnvdc.so*",
	"libnvos.so*",
	"libnvrm_gpu.so*",
	"libnvimp.so*",
	"libnvrm.so*",
	"libnvrm_graphics.so*",
	// CUDA
	// https://docs.nvidia.com/cuda/#cuda-api-references
	"libcuda.so*",
	"libcudart.so*",
	"libnvcuvid.so*",
	"libcufft.so*",
	"libcublas.so*",
	"libcublasLt.so*",
	"libcusolver.so*",
	"libcuparse.so*",
	"libcurand.so*",
	"libnppc.so*",
	"libnppig.so*",
	"libnppial.so*",
	"libnppicc.so*",
	"libnppidei.so*",
	"libnppist.so*",
	"libnppcif.so*",
	"libnppim.so*",
	"libnppitc.so*",
	"libnvrtc*",
	"libnvrtc-builtins*",
	"libnvToolsExt.so*",
	// libraries for CUDA DNN
	// https://docs.nvidia.com/deeplearning/cudnn/api/index.html
	// https://docs.nvidia.com/deeplearning/cudnn/install-guide/index.html
	"libcudnn.so*",
	"libcudnn_adv_infer*",
	"libcudnn_adv_train*",
	"libcudnn_cnn_infer*",
	"libcudnn_cnn_train*",
	"libcudnn_ops_infer*",
	"libcudnn_ops_train*",
};

static const size_t nvidia_globs_len =
    sizeof nvidia_globs / sizeof *nvidia_globs;

static const char *glvnd_globs[] = {
	"libEGL.so*",
	"libGL.so*",
	"libOpenGL.so*",
	"libGLESv1_CM.so*",
	"libGLESv2.so*",
	"libGLX_indirect.so*",
	"libGLX.so*",
	"libGLdispatch.so*",
	"libGLU.so*",
};

static const size_t glvnd_globs_len =
    sizeof glvnd_globs / sizeof *glvnd_globs;

#endif				// defined(NVIDIA_BIARCH) || defined(NVIDIA_MULTIARCH)

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
static void sc_populate_libgl_with_hostfs_symlinks(const char *libgl_dir,
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
			if (!strncmp("/etc/alternatives/", hostfs_symlink_target, 18)) {
				char hostfs_alt_symlink_target[512] = { 0 };
				hostfs_alt_symlink_target[0] = 0;
				num_read =
				    readlink(hostfs_symlink_target, hostfs_alt_symlink_target,
					    sizeof hostfs_alt_symlink_target - 1);
				if (num_read == -1) {
					die("cannot read symbolic link %s", pathname);
				}
				hostfs_alt_symlink_target[num_read] = 0;
				sc_must_snprintf(symlink_target,
						 sizeof symlink_target,
						 "/var/lib/snapd/hostfs%s",
						 hostfs_alt_symlink_target);
			} else if (hostfs_symlink_target[0] == '/') {
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

static void sc_overlay_init(const char *rootfs_dir)
{
	static char overlay_template[512];

	sc_must_snprintf(overlay_template, sizeof(overlay_template), "%s/tmp/snap.nvidia_overlay_XXXXXX", rootfs_dir);
	if (mkdtemp(overlay_template) == NULL) {
		die("cannot create temporary directory %s for the nvidia overlay file system", overlay_template);
	}

	overlay_dir = overlay_template;

	if (mount("none", overlay_dir, "tmpfs", MS_NODEV | MS_NOEXEC, NULL) != 0) {
		die("cannot mount tmpfs at %s", overlay_dir);
	}
}

static void sc_overlay_final(void)
{
	// Remount $tgt_dir (i.e. .../lib/gl) read only
	debug("remounting overlay tmpfs as read-only %s", overlay_dir);
	if (mount(NULL, overlay_dir, NULL, MS_REMOUNT | MS_BIND | MS_RDONLY, NULL) != 0) {
		die("cannot remount %s as read-only", overlay_dir);
	}
}

static int sc_overlay_mount(const char *target)
{
	char copy[512] = { 0 };

	strncpy(copy, target, sizeof(copy) - 1);
	const char *base_dir = basename(copy);
	char overlay_upper[512] = { 0 };
	char overlay_work[512] = { 0 };
	char overlay_options[512] = { 0 };

	sc_must_snprintf(overlay_upper, sizeof(overlay_upper), "%s/upper_%s", overlay_dir, base_dir);
	sc_must_snprintf(overlay_work, sizeof(overlay_work), "%s/work_%s", overlay_dir, base_dir);
	int res = mkdir(overlay_upper, 0755);
	if (res != 0 && errno != EEXIST) {
		die("cannot create overlay fs target %s", overlay_upper);
	}
	res = mkdir(overlay_work, 0755);
	if (res != 0 && errno != EEXIST) {
		die("cannot create overlay fs target %s", overlay_work);
	}

	res = mkdir(target, 0755);
	if (res != 0 && errno != EEXIST) {
		die("cannot create target %s", target);
	}

	sc_must_snprintf(overlay_options, sizeof(overlay_options), "lowerdir=%s,upperdir=%s,workdir=%s", target, overlay_upper, overlay_work);

	if ((res = mount("overlay", target, "overlay", 0, overlay_options)) != 0) {
		die("Unable to create overlay mount, target: %s, upper: %s, work: %s, options: %s, res: %d, errno: %d",
				target, overlay_upper, overlay_work, overlay_options, res, errno);
	}

	return res;
}

static void sc_mkdir_and_mount_and_glob_files(const char *rootfs_dir,
					      const char *source1,
					      const char *source2,
					      const char *tgt_dir,
					      const char *glob_list[],
					      size_t glob_list_len)
{
	// Bind mount a tmpfs on $rootfs_dir/$tgt_dir (i.e. /var/lib/snapd/lib/gl)
	char buf[511] = { 0 };
	sc_must_snprintf(buf, sizeof(buf), "%s%s", rootfs_dir, tgt_dir);
	const char *libgl_dir = buf;

	sc_identity old = sc_set_effective_identity(sc_root_group_identity());
	(void)sc_set_effective_identity(old);

	if (sc_overlay_mount(libgl_dir) != 0) {
		die("cannot mount overlay at %s", libgl_dir);
	}

	if (chown(libgl_dir, 0, 0) < 0) {
		// Adjust the ownership only if we created the directory.
		die("cannot change ownership of %s", libgl_dir);
	}

	// Populate libgl_dir with symlinks to libraries from hostfs
	if (source1 != NULL) {
		sc_populate_libgl_with_hostfs_symlinks(libgl_dir, source1,
						       glob_list,
						       glob_list_len);
	}
	if (source2 != NULL) {
		sc_populate_libgl_with_hostfs_symlinks(libgl_dir, source2,
						       glob_list,
						       glob_list_len);
	}
}

#ifdef NVIDIA_BIARCH

// Expose host NVIDIA drivers to the snap on biarch systems.
//
// Order is absolutely imperative here. We'll attempt to find the
// primary files for the architecture in the main directory, and end
// up copying any files across. However it is possible we're using a
// GLVND enabled host, in which case we copied libGL* to the farm.
// The next step in the list is to look within the private nvidia
// directory, exposed using ld.so.conf tricks within the host OS.
// In some distros (i.e. Solus) only the private libGL/libEGL files
// may be found here, and they'll clobber the existing GLVND files from
// the previous run.
// In other distros (like Fedora) all NVIDIA libraries are contained
// within the private directory, so we clobber the GLVND files and we
// also grab all the private NVIDIA libraries.
//
// In non GLVND cases we just copy across the exposed libGLs and NVIDIA
// libraries from wherever we find, and clobbering is also harmless.
static void sc_mount_nvidia_driver_biarch(const char *rootfs_dir, const char **globs, size_t globs_len)
{

	// Primary arch
	sc_mkdir_and_mount_and_glob_files(rootfs_dir,
					  NATIVE_LIBDIR, NATIVE_LIBDIR "/nvidia*",
					  SC_LIBGL_DIR, globs,
					  globs_len);

#if UINTPTR_MAX == 0xffffffffffffffff
	// Alternative 32-bit support
	sc_mkdir_and_mount_and_glob_files(rootfs_dir,
					  LIB32_DIR, LIB32_DIR "/nvidia*",
					  SC_LIBGL32_DIR,
					  globs, globs_len);
#endif
}

#endif				// ifdef NVIDIA_BIARCH

#ifdef NVIDIA_MULTIARCH

typedef struct {
	int major;
	// Driver version format is MAJOR.MINOR[.MICRO] but we only care about the
	// major version and the full version string. The micro component has been
	// seen with relevant leading zeros (e.g. "440.48.02").
	char raw[128]; // The size was picked as "big enough" for version strings.
} sc_nv_version;

static void sc_probe_nvidia_driver(sc_nv_version * version)
{
	memset(version, 0, sizeof *version);

	FILE *file SC_CLEANUP(sc_cleanup_file) = NULL;
	debug("opening file describing nvidia driver version");
	file = fopen(SC_NVIDIA_DRIVER_VERSION_FILE, "rt");
	if (file == NULL) {
		if (errno == ENOENT) {
			debug("nvidia driver version file doesn't exist");
			return;
		}
		die("cannot open file describing nvidia driver version");
	}
	int nread = fread(version->raw, 1, sizeof version->raw - 1, file);
	if (nread < 0) {
		die("cannot read nvidia driver version string");
	}
	if (nread == sizeof version->raw - 1 && !feof(file)) {
		die("cannot fit entire nvidia driver version string");
	}
	version->raw[nread] = '\0';
	if (nread > 0 && version->raw[nread - 1] == '\n') {
		version->raw[nread - 1] = '\0';
	}
	if (sscanf(version->raw, "%d.", &version->major) != 1) {
		die("cannot parse major version from nvidia driver version string");
	}
}

static void sc_mkdir_and_mount_and_bind(const char *rootfs_dir,
					const char *src_dir,
					const char *tgt_dir)
{
	sc_nv_version version;

	// Probe sysfs to get the version of the driver that is currently inserted.
	sc_probe_nvidia_driver(&version);

	// If there's driver in the kernel then don't mount userspace.
	if (version.major == 0) {
		return;
	}
	// Construct the paths for the driver userspace libraries
	// and for the gl directory.
	char src[PATH_MAX] = { 0 };
	char dst[PATH_MAX] = { 0 };
	sc_must_snprintf(src, sizeof src, "%s-%d", src_dir, version.major);
	sc_must_snprintf(dst, sizeof dst, "%s%s", rootfs_dir, tgt_dir);

	// If there is no userspace driver available then don't try to mount it.
	// This can happen for any number of reasons but one interesting one is
	// that that snapd runs in a lxd container on a host that uses nvidia. In
	// that case the container may not have the userspace library installed but
	// the kernel will still have the module around.
	if (access(src, F_OK) != 0) {
		return;
	}
	sc_identity old = sc_set_effective_identity(sc_root_group_identity());
	int res = mkdir(dst, 0755);
	if (res != 0 && errno != EEXIST) {
		die("cannot create directory %s", dst);
	}
	if (res == 0 && (chown(dst, 0, 0) < 0)) {
		// Adjust the ownership only if we created the directory.
		die("cannot change ownership of %s", dst);
	}
	(void)sc_set_effective_identity(old);
	// Bind mount the binary nvidia driver into $tgt_dir (i.e. /var/lib/snapd/lib/gl).
	debug("bind mounting nvidia driver %s -> %s", src, dst);
	if (mount(src, dst, NULL, MS_BIND, NULL) != 0) {
		die("cannot bind mount nvidia driver %s -> %s", src, dst);
	}
}

static int sc_mount_nvidia_is_driver_in_dir(const char *dir)
{
	char driver_path[512] = { 0 };

	sc_nv_version version;

	// Probe sysfs to get the version of the driver that is currently inserted.
	sc_probe_nvidia_driver(&version);

	// If there's no driver then we should not bother ourselves with finding the
	// matching library
	if (version.major == 0) {
		return 0;
	}

	// Probe if a well known library is found in directory dir. We must use the
	// raw version because it may contain more than just major.minor. In
	// practice the micro version may have leading zeros that are relevant.
	sc_must_snprintf(driver_path, sizeof driver_path,
			 "%s/libnvidia-glcore.so.%s", dir, version.raw);

	debug("looking for nvidia canary file %s", driver_path);
	if (access(driver_path, F_OK) == 0) {
		debug("nvidia library detected at path %s", driver_path);
		return 1;
	}
	return 0;
}

static void sc_mount_nvidia_driver_multiarch(const char *rootfs_dir, const char **globs, size_t globs_len)
{
	const char *native_libdir = NATIVE_LIBDIR "/" HOST_ARCH_TRIPLET;
	const char *lib32_libdir = NATIVE_LIBDIR "/" HOST_ARCH32_TRIPLET;

	if ((strlen(HOST_ARCH_TRIPLET) > 0) &&
	    (sc_mount_nvidia_is_driver_in_dir(native_libdir) == 1)) {

		// Primary arch
		sc_mkdir_and_mount_and_glob_files(rootfs_dir,
						  native_libdir,
						  NULL,
						  SC_LIBGL_DIR, globs,
						  globs_len);

		// Alternative 32-bit support
		if ((strlen(HOST_ARCH32_TRIPLET) > 0) &&
		    (sc_mount_nvidia_is_driver_in_dir(lib32_libdir) == 1)) {

			sc_mkdir_and_mount_and_glob_files(rootfs_dir,
							  lib32_libdir, NULL,
							  SC_LIBGL32_DIR,
							  globs,
							  globs_len);
		}
	} else {
		// Attempt mount of both the native and 32-bit variants of the driver if they exist
		sc_mkdir_and_mount_and_bind(rootfs_dir, "/usr/lib/nvidia",
					    SC_LIBGL_DIR);
		// Alternative 32-bit support
		sc_mkdir_and_mount_and_bind(rootfs_dir, "/usr/lib32/nvidia",
					    SC_LIBGL32_DIR);
	}
}

#endif				// ifdef NVIDIA_MULTIARCH

static void sc_mount_vulkan(const char *rootfs_dir)
{
	sc_mkdir_and_mount_and_glob_files(rootfs_dir, SC_VULKAN_SOURCE_DIR, NULL, SC_VULKAN_DIR, vulkan_globs, vulkan_globs_len);
}

static void sc_mount_glvnd(const char *rootfs_dir)
{
	sc_mkdir_and_mount_and_glob_files(rootfs_dir, SC_GLVND_VENDOR_SOURCE_DIR, NULL, SC_GLVND_DIR, glvnd_vendor_globs, glvnd_vendor_globs_len);
}

static void sc_mount_egl(const char *rootfs_dir)
{
	sc_mkdir_and_mount_and_glob_files(rootfs_dir, SC_EGL_VENDOR_SOURCE_DIR, NULL, SC_GLVND_DIR, egl_vendor_globs, egl_vendor_globs_len);
	setenv("__EGL_VENDOR_LIBRARY_DIRS", SC_GLVND_DIR "/egl_vendor.d", true);
	setenv("__EGL_EXTERNAL_PLATFORM_CONFIG_DIRS", SC_GLVND_DIR "/egl_external_platform.d", true);
}

// This is all a dirty hack involving mangling the snap's filesystem.
// Unfortunately, desktop-launcher from snapcraft unconditionally sets
// LIBVA_DRIVERS_PATH, which means that we have no choice except to put any
// libva files in that directory.
static void sc_mount_libva(const char *rootfs_dir)
{
	char dest_dir[500] = { 0 };

	sc_must_snprintf(dest_dir, sizeof dest_dir, "%s/usr/lib/%s/dri", getenv("SNAP"), HOST_ARCH_TRIPLET);

	sc_mkdir_and_mount_and_glob_files(rootfs_dir, SC_NATIVE_LIBVA_DIR, NULL, dest_dir, libvaglobs, libvaglobs_len);
}

void sc_mount_nvidia_driver(const char *rootfs_dir, const char *base_snap_name)
{
	/* If NVIDIA module isn't loaded, don't attempt to mount the drivers */
	if (access(SC_NVIDIA_DRIVER_VERSION_FILE, F_OK) != 0) {
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

	sc_overlay_init(rootfs_dir);

#if defined(NVIDIA_BIARCH) || defined(NVIDIA_MULTIARCH)
	/* We include the globs for the glvnd libraries for old snaps
	 * based on core, Ubuntu 16.04 did not include glvnd itself.
	 *
	 * While there is no guarantee that the host system's glvnd
	 * libGL will be compatible (as it is built with the host
	 * system's glibc), the Mesa libGL included with the snap will
	 * definitely not be compatible (as it expects to find the Mesa
	 * implementation of the GLX extension)..
	 */
	const char **globs = nvidia_globs;
	size_t globs_len = nvidia_globs_len;
	const char **full_globs SC_CLEANUP(sc_cleanup_shallow_strv) = NULL;
	if (sc_streq(base_snap_name, "core")) {
		full_globs = malloc(sizeof nvidia_globs + sizeof glvnd_globs);
		if (full_globs == NULL) {
			die("cannot allocate globs array");
		}
		memcpy(full_globs, nvidia_globs, sizeof nvidia_globs);
		memcpy(&full_globs[nvidia_globs_len], glvnd_globs, sizeof glvnd_globs);
		globs = full_globs;
		globs_len = nvidia_globs_len + glvnd_globs_len;
	}
#endif

#ifdef NVIDIA_MULTIARCH
	sc_mount_nvidia_driver_multiarch(rootfs_dir, globs, globs_len);
#endif				// ifdef NVIDIA_MULTIARCH
#ifdef NVIDIA_BIARCH
	sc_mount_nvidia_driver_biarch(rootfs_dir, globs, globs_len);
#endif				// ifdef NVIDIA_BIARCH

	// Common for both driver mechanisms
	sc_mount_vulkan(rootfs_dir);
	sc_mount_glvnd(rootfs_dir);
	sc_mount_egl(rootfs_dir);
	sc_mount_libva(rootfs_dir);

	sc_overlay_final();
}
