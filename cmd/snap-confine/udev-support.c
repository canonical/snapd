/*
 * Copyright (C) 2015-2019 Canonical Ltd
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

#include <ctype.h>
#include <errno.h>
#include <fcntl.h>
#include <string.h>
#include <sys/stat.h>
#include <sys/sysmacros.h>
#include <sys/types.h>
#include <unistd.h>

#include <libudev.h>

#include "../libsnap-confine-private/cleanup-funcs.h"
#include "../libsnap-confine-private/snap.h"
#include "../libsnap-confine-private/string-utils.h"
#include "../libsnap-confine-private/cgroup-support.h"
#include "../libsnap-confine-private/utils.h"
#include "udev-support.h"

__attribute__((format(printf, 2, 3)))
static void sc_dprintf(int fd, const char *format, ...);

static void sc_dprintf(int fd, const char *format, ...)
{
	va_list ap;
	va_start(ap, format);
	if (vdprintf(fd, format, ap) == -1) {
		die("cannot write to fd %d", fd);
	}
	va_end(ap);
}

/* Allow access to common devices. */
static void sc_udev_allow_common(int devices_allow_fd)
{
	/* The devices we add here have static number allocation.
	 * https://www.kernel.org/doc/html/v4.11/admin-guide/devices.html */
	sc_dprintf(devices_allow_fd, "c 1:3 rwm");	// /dev/null
	sc_dprintf(devices_allow_fd, "c 1:5 rwm");	// /dev/zero
	sc_dprintf(devices_allow_fd, "c 1:7 rwm");	// /dev/full
	sc_dprintf(devices_allow_fd, "c 1:8 rwm");	// /dev/random
	sc_dprintf(devices_allow_fd, "c 1:9 rwm");	// /dev/urandom
	sc_dprintf(devices_allow_fd, "c 5:0 rwm");	// /dev/tty
	sc_dprintf(devices_allow_fd, "c 5:1 rwm");	// /dev/console
	sc_dprintf(devices_allow_fd, "c 5:2 rwm");	// /dev/ptmx
}

/** Allow access to current and future PTY slaves.
 *
 * We unconditionally add them since we use a devpts newinstance. Unix98 PTY
 * slaves major are 136-143.
 *
 * See also:
 * https://www.kernel.org/doc/Documentation/admin-guide/devices.txt
 **/
static void sc_udev_allow_pty_slaves(int devices_allow_fd)
{
	for (unsigned pty_major = 136; pty_major <= 143; pty_major++) {
		sc_dprintf(devices_allow_fd, "c %u:* rwm", pty_major);
	}
}

/** Allow access to Nvidia devices.
 *
 * Nvidia modules are proprietary and therefore aren't in sysfs and can't be
 * udev tagged. For now, just add existing nvidia devices to the cgroup
 * unconditionally (AppArmor will still mediate the access).  We'll want to
 * rethink this if snapd needs to mediate access to other proprietary devices.
 *
 * Device major and minor numbers are described in (though nvidia-uvm currently
 * isn't listed):
 *
 * https://www.kernel.org/doc/Documentation/admin-guide/devices.txt
 **/
static void sc_udev_allow_nvidia(int devices_allow_fd)
{
	struct stat sbuf;

	/* Allow access to /dev/nvidia0 through /dev/nvidia254 */
	for (unsigned nv_minor = 0; nv_minor < 255; nv_minor++) {
		char nv_path[15] = { 0 };	// /dev/nvidiaXXX
		sc_must_snprintf(nv_path, sizeof(nv_path), "/dev/nvidia%u",
				 nv_minor);

		/* Stop trying to find devices after one is not found. In this manner,
		 * we'll add /dev/nvidia0 and /dev/nvidia1 but stop trying to find
		 * nvidia3 - nvidia254 if nvidia2 is not found. */
		if (stat(nv_path, &sbuf) < 0) {
			break;
		}
		sc_dprintf(devices_allow_fd, "c %u:%u rwm", major(sbuf.st_rdev),
			   minor(sbuf.st_rdev));
	}

	if (stat("/dev/nvidiactl", &sbuf) == 0) {
		sc_dprintf(devices_allow_fd, "c %u:%u rwm", major(sbuf.st_rdev),
			   minor(sbuf.st_rdev));
	}
	if (stat("/dev/nvidia-uvm", &sbuf) == 0) {
		sc_dprintf(devices_allow_fd, "c %u:%u rwm", major(sbuf.st_rdev),
			   minor(sbuf.st_rdev));
	}
	if (stat("/dev/nvidia-modeset", &sbuf) == 0) {
		sc_dprintf(devices_allow_fd, "c %u:%u rwm", major(sbuf.st_rdev),
			   minor(sbuf.st_rdev));
	}
}

/**
 * Allow access to /dev/uhid.
 *
 * Currently /dev/uhid isn't represented in sysfs, so add it to the device
 * cgroup if it exists and let AppArmor handle the mediation.
 **/
static void sc_udev_allow_uhid(int devices_allow_fd)
{
	struct stat sbuf;

	if (stat("/dev/uhid", &sbuf) == 0) {
		sc_dprintf(devices_allow_fd, "c %u:%u rwm", major(sbuf.st_rdev),
			   minor(sbuf.st_rdev));
	}
}

/**
 * Allow access to assigned devices.
 *
 * The snapd udev security backend uses udev rules to tag matching devices with
 * tags corresponding to snap applications. Here we interrogate udev and allow
 * access to all assigned devices.
 **/
static void sc_udev_allow_assigned(int devices_allow_fd, struct udev *udev,
				   struct udev_list_entry *assigned)
{
	for (struct udev_list_entry * entry = assigned; entry != NULL;
	     entry = udev_list_entry_get_next(entry)) {
		const char *path = udev_list_entry_get_name(entry);
		if (path == NULL) {
			die("udev_list_entry_get_name failed");
		}
		struct udev_device *device =
		    udev_device_new_from_syspath(udev, path);
		if (device == NULL) {
			die("cannot find device from syspath %s", path);
		}
		dev_t devnum = udev_device_get_devnum(device);
		char type_c = strstr(path, "/block/") != NULL ? 'b' : 'c';
		sc_dprintf(devices_allow_fd, "%c %u:%u rwm", type_c,
			   major(devnum), minor(devnum));

		udev_device_unref(device);
	}
}

static void sc_udev_setup_acls(int devices_allow_fd, int devices_deny_fd,
			       struct udev *udev,
			       struct udev_list_entry *assigned)
{
	/* Deny device access by default.
	 *
	 * Write 'a' to devices.deny to remove all existing devices that were added
	 * in previous launcher invocations, then add the static and assigned
	 * devices. This ensures that at application launch the cgroup only has
	 * what is currently assigned. */
	sc_dprintf(devices_deny_fd, "a");

	/* Allow access to various devices. */
	sc_udev_allow_common(devices_allow_fd);
	sc_udev_allow_pty_slaves(devices_allow_fd);
	sc_udev_allow_nvidia(devices_allow_fd);
	sc_udev_allow_uhid(devices_allow_fd);
	sc_udev_allow_assigned(devices_allow_fd, udev, assigned);
}

static char *sc_udev_mangle_security_tag(const char *security_tag)
{
	char *udev_tag = sc_strdup(security_tag);
	for (char *c = strchr(udev_tag, '.'); c != NULL; c = strchr(c, '.')) {
		*c = '_';
	}
	return udev_tag;
}

static void sc_cleanup_udev(struct udev **udev)
{
	if (udev != NULL && *udev != NULL) {
		udev_unref(*udev);
		*udev = NULL;
	}
}

static void sc_cleanup_udev_enumerate(struct udev_enumerate **enumerate)
{
	if (enumerate != NULL && *enumerate != NULL) {
		udev_enumerate_unref(*enumerate);
		*enumerate = NULL;
	}
}

typedef struct sc_cgroup_fds {
	int devices_allow_fd;
	int devices_deny_fd;
	int cgroup_procs_fd;
} sc_cgroup_fds;

static sc_cgroup_fds sc_udev_open_cgroup_v1(const char *security_tag)
{
	sc_cgroup_fds fds = { -1, -1, -1 };

	/* Open /sys/fs/cgroup */
	const char *cgroup_path = "/sys/fs/cgroup";
	int SC_CLEANUP(sc_cleanup_close) cgroup_fd = -1;
	cgroup_fd = open(cgroup_path,
			 O_PATH | O_DIRECTORY | O_CLOEXEC | O_NOFOLLOW);
	if (cgroup_fd < 0 && errno == ENOENT) {
		if (errno == ENOENT) {
			/* This system does not support cgroups. */
			return fds;
		}
		die("cannot open %s", cgroup_path);
	}

	/* Open devices relative to /sys/fs/cgroup */
	const char *devices_relpath = "devices";
	int SC_CLEANUP(sc_cleanup_close) devices_fd = -1;
	devices_fd = openat(cgroup_fd, devices_relpath,
			    O_PATH | O_DIRECTORY | O_CLOEXEC | O_NOFOLLOW);
	if (devices_fd < 0) {
		if (errno == ENOENT) {
			/* This system does not support the device cgroup. */
			return fds;
		}
		die("cannot open %s/%s", cgroup_path, devices_relpath);
	}

	/* Open snap.$SNAP_NAME.$APP_NAME relative to /sys/fs/cgroup/devices,
	 * creating the directory if necessary. Note that we always chown the
	 * resulting directory to root:root. */
	const char *security_tag_relpath = security_tag;
	if (mkdirat(devices_fd, security_tag_relpath, 0755) < 0) {
		if (errno != EEXIST) {
			die("cannot create directory %s/%s/%s", cgroup_path,
			    devices_relpath, security_tag_relpath);
		}
	}

	int SC_CLEANUP(sc_cleanup_close) security_tag_fd = -1;
	security_tag_fd = openat(devices_fd, security_tag_relpath,
				 O_RDONLY | O_DIRECTORY | O_CLOEXEC |
				 O_NOFOLLOW);
	if (security_tag_fd < 0) {
		die("cannot open %s/%s/%s", cgroup_path, devices_relpath,
		    security_tag_relpath);
	}
	if (fchown(security_tag_fd, 0, 0) < 0) {
		die("cannot chown %s/%s/%s to root:root", cgroup_path,
		    devices_relpath, security_tag_relpath);
	}

	/* Open devices.allow relative to /sys/fs/cgroup/devices/snap.$SNAP_NAME.$APP_NAME */
	const char *devices_allow_relpath = "devices.allow";
	int devices_allow_fd = -1;
	devices_allow_fd = openat(security_tag_fd, devices_allow_relpath,
				  O_WRONLY | O_CLOEXEC | O_NOFOLLOW);
	if (devices_allow_fd < 0) {
		die("cannot open %s/%s/%s/%s", cgroup_path, devices_relpath,
		    security_tag_relpath, devices_allow_relpath);
	}

	/* Open devices.deny relative to /sys/fs/cgroup/devices/snap.$SNAP_NAME.$APP_NAME */
	const char *devices_deny_relpath = "devices.deny";
	int devices_deny_fd = -1;
	devices_deny_fd = openat(security_tag_fd, devices_deny_relpath,
				 O_WRONLY | O_CLOEXEC | O_NOFOLLOW);
	if (devices_deny_fd < 0) {
		die("cannot open %s/%s/%s/%s", cgroup_path, devices_relpath,
		    security_tag_relpath, devices_deny_relpath);
	}

	/* Open cgroup.procs relative to /sys/fs/cgroup/devices/snap.$SNAP_NAME.$APP_NAME */
	const char *cgroup_procs_relpath = "cgroup.procs";
	int cgroup_procs_fd = -1;
	cgroup_procs_fd = openat(security_tag_fd, cgroup_procs_relpath,
				 O_WRONLY | O_CLOEXEC | O_NOFOLLOW);
	if (cgroup_procs_fd < 0) {
		die("cannot open %s/%s/%s/%s", cgroup_path, devices_relpath,
		    security_tag_relpath, cgroup_procs_relpath);
	}

	/* Everything worked so pack the result and "move" the descriptors over so
	 * that they are not closed by the cleanup functions. */
	fds.devices_allow_fd = devices_allow_fd;
	fds.devices_deny_fd = devices_deny_fd;
	fds.cgroup_procs_fd = cgroup_procs_fd;
	devices_allow_fd = -1;
	devices_deny_fd = -1;
	cgroup_procs_fd = -1;
	return fds;
}

static void sc_cleanup_cgroup_fds(sc_cgroup_fds * fds)
{
	if (fds != NULL) {
		sc_cleanup_close(&fds->devices_allow_fd);
		sc_cleanup_close(&fds->devices_deny_fd);
		sc_cleanup_close(&fds->cgroup_procs_fd);
	}
}

void sc_setup_device_cgroup(const char *security_tag)
{
	if (sc_cgroup_is_v2()) {
		return;
	}

	/* Derive the udev tag from the snap security tag.
	 *
	 * Because udev does not allow for dots in tag names, those are replaced by
	 * underscores in snapd. We just match that behavior. */
	char *udev_tag SC_CLEANUP(sc_cleanup_string) = NULL;
	udev_tag = sc_udev_mangle_security_tag(security_tag);

	/* Use udev APIs to talk to udev-the-daemon to determine the list of
	 * "devices" with that tag assigned. The list may be empty, in which case
	 * there's no udev tagging in effect and we must refrain from constructing
	 * the cgroup as it would interfere with the execution of a program. */
	struct udev SC_CLEANUP(sc_cleanup_udev) * udev = NULL;
	udev = udev_new();
	if (udev == NULL) {
		die("cannot connect to udev");
	}
	struct udev_enumerate SC_CLEANUP(sc_cleanup_udev_enumerate) * devices =
	    NULL;
	devices = udev_enumerate_new(udev);
	if (devices == NULL) {
		die("cannot create udev device enumeration");
	}
	if (udev_enumerate_add_match_tag(devices, udev_tag) < 0) {
		die("cannot add tag match to udev device enumeration");
	}
	if (udev_enumerate_scan_devices(devices) < 0) {
		die("cannot enumerate udev devices");
	}
	/* NOTE: udev_list_entry is bound to life-cycle of the used udev_enumerate */
	struct udev_list_entry *assigned;
	assigned = udev_enumerate_get_list_entry(devices);
	if (assigned == NULL) {
		/* NOTE: Nothing is assigned, don't create or use the device cgroup. */
		debug("no devices tagged with %s, skipping device cgroup setup",
		      udev_tag);
		return;
	}

	sc_cgroup_fds SC_CLEANUP(sc_cleanup_cgroup_fds) fds = { -1, -1, -1 };
	fds = sc_udev_open_cgroup_v1(security_tag);
	if (fds.cgroup_procs_fd < 0) {
		debug("cgroup v1 unavailable, skipping device cgroup setup");
		return;
	}
	/* Setup the device group access control list */
	sc_udev_setup_acls(fds.devices_allow_fd, fds.devices_deny_fd,
			   udev, assigned);

	/* Move ourselves to the device cgroup */
	sc_dprintf(fds.cgroup_procs_fd, "%i", getpid());
	debug("associated snap application process with device cgroup %s",
	      security_tag);
}
