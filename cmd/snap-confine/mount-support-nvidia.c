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
#define SC_EGL_VENDOR_SOURCE_DIR "/usr/share/glvnd"

// Location for NVIDIA vulkan files (including _wayland)
static const char *vulkan_globs[] = {
	"icd.d/*nvidia*.json",
};

static const size_t vulkan_globs_len =
    sizeof vulkan_globs / sizeof *vulkan_globs;

// Location of EGL vendor files
static const char *egl_vendor_globs[] = {
	"egl_vendor.d/*nvidia*.json",
};

static const size_t egl_vendor_globs_len =
    sizeof egl_vendor_globs / sizeof *egl_vendor_globs;

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
	"gbm/nvidia-drm_gbm.so*",
	"libnvidia-allocator.so*",
	"libnvidia-api.so*",
	"libnvidia-cbl.so*",
	"libnvidia-cfg.so*",
	"libnvidia-compiler-next.so*",
	"libnvidia-egl-gbm.so*",
	"libnvidia-ngx.so*",
	"libnvidia-nscq.so*",
	"libnvidia-nvvm.so*",
	"libnvidia-pkcs11-openssl3.so*",
	"libnvidia-pkcs11.so*",
	"libnvidia-vulkan-producer.so*",
	"libnvidia-vksc-core.so.*",

	"libEGL_nvidia.so*",
	"libGLESv1_CM_nvidia.so*",
	"libGLESv2_nvidia.so*",
	"libGLX_nvidia.so*",
	"libXvMCNVIDIA.so*",
	"libXvMCNVIDIA_dynamic.so*",
	"libnvidia-cfg.so*",
	"libnvidia-compiler.so*",
	"libnvidia-eglcore.so*",
	"libnvidia-egl-wayland*",
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

static const size_t glvnd_globs_len = sizeof glvnd_globs / sizeof *glvnd_globs;

#endif				// defined(NVIDIA_BIARCH) || defined(NVIDIA_MULTIARCH)

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
static void sc_mount_nvidia_driver_biarch(const char *rootfs_dir,
					  const char **globs, size_t globs_len)
{

	static const char *native_sources[] = {
		NATIVE_LIBDIR,
		NATIVE_LIBDIR "/nvidia*",
	};
	const size_t native_sources_len =
	    sizeof native_sources / sizeof *native_sources;

#if UINTPTR_MAX == 0xffffffffffffffff
	// Alternative 32-bit support
	static const char *lib32_sources[] = {
		LIB32_DIR,
		LIB32_DIR "/nvidia*",
	};
	const size_t lib32_sources_len =
	    sizeof lib32_sources / sizeof *lib32_sources;
#endif

	// Primary arch
	sc_mkdir_and_mount_and_glob_files(rootfs_dir,
					  native_sources, native_sources_len,
					  SC_LIBGL_DIR, globs, globs_len);

#if UINTPTR_MAX == 0xffffffffffffffff
	// Alternative 32-bit support
	sc_mkdir_and_mount_and_glob_files(rootfs_dir, lib32_sources,
					  lib32_sources_len, SC_LIBGL32_DIR,
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
	char raw[128];		// The size was picked as "big enough" for version strings.
} sc_nv_version;

static void sc_probe_nvidia_driver(sc_nv_version *version)
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

static void sc_mount_nvidia_driver_multiarch(const char *rootfs_dir,
					     const char **globs,
					     size_t globs_len)
{
	const char *native_libdir = NATIVE_LIBDIR "/" HOST_ARCH_TRIPLET;
	const char *lib32_libdir = NATIVE_LIBDIR "/" HOST_ARCH32_TRIPLET;

	if ((strlen(HOST_ARCH_TRIPLET) > 0) &&
	    (sc_mount_nvidia_is_driver_in_dir(native_libdir) == 1)) {

		// sc_mkdir_and_mount_and_glob_files() takes an array of strings, so
		// initialize native_sources accordingly, but calculate the array length
		// dynamically to make adjustments to native_sources easier.
		const char *native_sources[] = { native_libdir };
		const size_t native_sources_len =
		    sizeof native_sources / sizeof *native_sources;
		// Primary arch
		sc_mkdir_and_mount_and_glob_files(rootfs_dir,
						  native_sources,
						  native_sources_len,
						  SC_LIBGL_DIR, globs,
						  globs_len);

		// Alternative 32-bit support
		if ((strlen(HOST_ARCH32_TRIPLET) > 0) &&
		    (sc_mount_nvidia_is_driver_in_dir(lib32_libdir) == 1)) {

			// sc_mkdir_and_mount_and_glob_files() takes an array of strings, so
			// initialize lib32_sources accordingly, but calculate the array length
			// dynamically to make adjustments to lib32_sources easier.
			const char *lib32_sources[] = { lib32_libdir };
			const size_t lib32_sources_len =
			    sizeof lib32_sources / sizeof *lib32_sources;
			sc_mkdir_and_mount_and_glob_files(rootfs_dir,
							  lib32_sources,
							  lib32_sources_len,
							  SC_LIBGL32_DIR,
							  globs, globs_len);
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
	const char *vulkan_sources[] = {
		SC_VULKAN_SOURCE_DIR,
	};
	const size_t vulkan_sources_len =
	    sizeof vulkan_sources / sizeof *vulkan_sources;

	sc_mkdir_and_mount_and_glob_files(rootfs_dir, vulkan_sources,
					  vulkan_sources_len, SC_VULKAN_DIR,
					  vulkan_globs, vulkan_globs_len);
}

static void sc_mount_egl(const char *rootfs_dir)
{
	const char *egl_vendor_sources[] = { SC_EGL_VENDOR_SOURCE_DIR };
	const size_t egl_vendor_sources_len =
	    sizeof egl_vendor_sources / sizeof *egl_vendor_sources;

	sc_mkdir_and_mount_and_glob_files(rootfs_dir, egl_vendor_sources,
					  egl_vendor_sources_len, SC_GLVND_DIR,
					  egl_vendor_globs,
					  egl_vendor_globs_len);
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
		memcpy(&full_globs[nvidia_globs_len], glvnd_globs,
		       sizeof glvnd_globs);
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
	sc_mount_egl(rootfs_dir);
}
