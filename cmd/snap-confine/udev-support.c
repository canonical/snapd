/*
 * Copyright (C) 2015-2016 Canonical Ltd
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
#include <limits.h>
#include <linux/kdev_t.h>
#include <sched.h>
#include <string.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <sys/wait.h>
#include <unistd.h>

#include "../libsnap-confine-private/snap.h"
#include "../libsnap-confine-private/string-utils.h"
#include "../libsnap-confine-private/utils.h"
#include "udev-support.h"

static void
_run_snappy_app_dev_add_majmin(struct snappy_udev *udev_s,
			       const char *path, unsigned major, unsigned minor)
{
	int status = 0;
	pid_t pid = fork();
	if (pid < 0) {
		die("cannot fork support process for device cgroup assignment");
	}
	if (pid == 0) {
		uid_t real_uid, effective_uid, saved_uid;
		if (getresuid(&real_uid, &effective_uid, &saved_uid) != 0)
			die("cannot get real, effective and saved user IDs");
		// can't update the cgroup unless the real_uid is 0, euid as
		// 0 is not enough
		if (real_uid != 0 && effective_uid == 0)
			if (setuid(0) != 0)
				die("cannot set user ID to zero");
		char buf[64] = { 0 };
		// pass snappy-add-dev an empty environment so the
		// user-controlled environment can't be used to subvert
		// snappy-add-dev
		char *env[] = { NULL };
		if (minor == UINT_MAX) {
			sc_must_snprintf(buf, sizeof(buf), "%u:*", major);
		} else {
			sc_must_snprintf(buf, sizeof(buf), "%u:%u", major,
					 minor);
		}
		debug("running snap-device-helper add %s %s %s",
		      udev_s->tagname, path, buf);
		// This code runs inside the core snap. We have two paths
		// for the udev helper.
		//
		// First try new "snap-device-helper" path first but
		// when running against an older core snap fallback to
		// the old name.
		if (access("/usr/lib/snapd/snap-device-helper", X_OK) == 0)
			execle("/usr/lib/snapd/snap-device-helper",
			       "/usr/lib/snapd/snap-device-helper", "add",
			       udev_s->tagname, path, buf, NULL, env);
		else
			execle("/lib/udev/snappy-app-dev",
			       "/lib/udev/snappy-app-dev", "add",
			       udev_s->tagname, path, buf, NULL, env);
		die("execl failed");
	}
	if (waitpid(pid, &status, 0) < 0)
		die("waitpid failed");
	if (WIFEXITED(status) && WEXITSTATUS(status) != 0)
		die("child exited with status %i", WEXITSTATUS(status));
	else if (WIFSIGNALED(status))
		die("child died with signal %i", WTERMSIG(status));
}

void run_snappy_app_dev_add(struct snappy_udev *udev_s, const char *path)
{
	if (udev_s == NULL)
		die("snappy_udev is NULL");
	if (udev_s->udev == NULL)
		die("snappy_udev->udev is NULL");
	if (udev_s->tagname_len == 0
	    || udev_s->tagname_len >= MAX_BUF
	    || strnlen(udev_s->tagname, MAX_BUF) != udev_s->tagname_len
	    || udev_s->tagname[udev_s->tagname_len] != '\0')
		die("snappy_udev->tagname has invalid length");

	debug("%s: %s %s", __func__, path, udev_s->tagname);

	struct udev_device *d =
	    udev_device_new_from_syspath(udev_s->udev, path);
	if (d == NULL)
		die("cannot find device from syspath %s", path);
	dev_t devnum = udev_device_get_devnum(d);
	udev_device_unref(d);

	unsigned major = MAJOR(devnum);
	unsigned minor = MINOR(devnum);
	_run_snappy_app_dev_add_majmin(udev_s, path, major, minor);
}

/*
 * snappy_udev_init() - setup the snappy_udev structure. Return 0 if devices
 * are assigned, else return -1. Callers should use snappy_udev_cleanup() to
 * cleanup.
 */
int snappy_udev_init(const char *security_tag, struct snappy_udev *udev_s)
{
	debug("%s", __func__);
	int rc = 0;

	udev_s->tagname[0] = '\0';
	udev_s->tagname_len = 0;
	// TAG+="snap_<security tag>" (udev doesn't like '.' in the tag name)
	udev_s->tagname_len = sc_must_snprintf(udev_s->tagname, MAX_BUF,
					       "%s", security_tag);
	for (size_t i = 0; i < udev_s->tagname_len; i++)
		if (udev_s->tagname[i] == '.')
			udev_s->tagname[i] = '_';

	udev_s->udev = udev_new();
	if (udev_s->udev == NULL)
		die("udev_new failed");

	udev_s->devices = udev_enumerate_new(udev_s->udev);
	if (udev_s->devices == NULL)
		die("udev_enumerate_new failed");

	if (udev_enumerate_add_match_tag(udev_s->devices, udev_s->tagname) != 0)
		die("udev_enumerate_add_match_tag");

	if (udev_enumerate_scan_devices(udev_s->devices) != 0)
		die("udev_enumerate_scan failed");

	udev_s->assigned = udev_enumerate_get_list_entry(udev_s->devices);
	if (udev_s->assigned == NULL)
		rc = -1;

	return rc;
}

void snappy_udev_cleanup(struct snappy_udev *udev_s)
{
	// udev_s->assigned does not need to be unreferenced since it is a
	// pointer into udev_s->devices
	if (udev_s->devices != NULL)
		udev_enumerate_unref(udev_s->devices);
	if (udev_s->udev != NULL)
		udev_unref(udev_s->udev);
}

void setup_devices_cgroup(const char *security_tag, struct snappy_udev *udev_s)
{
	debug("%s", __func__);
	// Devices that must always be present
	const char *static_devices[] = {
		"/sys/class/mem/null",
		"/sys/class/mem/full",
		"/sys/class/mem/zero",
		"/sys/class/mem/random",
		"/sys/class/mem/urandom",
		"/sys/class/tty/tty",
		"/sys/class/tty/console",
		"/sys/class/tty/ptmx",
		NULL,
	};

	if (udev_s == NULL)
		die("snappy_udev is NULL");
	if (udev_s->udev == NULL)
		die("snappy_udev->udev is NULL");
	if (udev_s->devices == NULL)
		die("snappy_udev->devices is NULL");
	if (udev_s->assigned == NULL)
		die("snappy_udev->assigned is NULL");
	if (udev_s->tagname_len == 0
	    || udev_s->tagname_len >= MAX_BUF
	    || strnlen(udev_s->tagname, MAX_BUF) != udev_s->tagname_len
	    || udev_s->tagname[udev_s->tagname_len] != '\0')
		die("snappy_udev->tagname has invalid length");

	// create devices cgroup controller
	char cgroup_dir[PATH_MAX] = { 0 };

	sc_must_snprintf(cgroup_dir, sizeof(cgroup_dir),
			 "/sys/fs/cgroup/devices/%s/", security_tag);

	if (mkdir(cgroup_dir, 0755) < 0 && errno != EEXIST)
		die("cannot create cgroup hierarchy %s", cgroup_dir);

	// move ourselves into it
	char cgroup_file[PATH_MAX] = { 0 };
	sc_must_snprintf(cgroup_file, sizeof(cgroup_file), "%s%s", cgroup_dir,
			 "tasks");

	char buf[128] = { 0 };
	sc_must_snprintf(buf, sizeof(buf), "%i", getpid());
	write_string_to_file(cgroup_file, buf);

	// deny by default. Write 'a' to devices.deny to remove all existing
	// devices that were added in previous launcher invocations, then add
	// the static and assigned devices. This ensures that at application
	// launch the cgroup only has what is currently assigned.
	sc_must_snprintf(cgroup_file, sizeof(cgroup_file), "%s%s", cgroup_dir,
			 "devices.deny");
	write_string_to_file(cgroup_file, "a");

	// add the common devices
	for (int i = 0; static_devices[i] != NULL; i++)
		run_snappy_app_dev_add(udev_s, static_devices[i]);

	// add glob for current and future PTY slaves. We unconditionally add
	// them since we use a devpts newinstance. Unix98 PTY slaves major
	// are 136-143.
	// https://github.com/torvalds/linux/blob/master/Documentation/admin-guide/devices.txt
	for (unsigned pty_major = 136; pty_major <= 143; pty_major++) {
		// '/dev/pts/slaves' is only used for debugging and by
		// /usr/lib/snapd/snap-device-helper to determine if it is a block
		// device, so just use something to indicate what the
		// addition is for
		_run_snappy_app_dev_add_majmin(udev_s, "/dev/pts/slaves",
					       pty_major, UINT_MAX);
	}

	// nvidia modules are proprietary and therefore aren't in sysfs and
	// can't be udev tagged. For now, just add existing nvidia devices to
	// the cgroup unconditionally (AppArmor will still mediate the access).
	// We'll want to rethink this if snapd needs to mediate access to other
	// proprietary devices.
	//
	// Device major and minor numbers are described in (though nvidia-uvm
	// currently isn't listed):
	// https://github.com/torvalds/linux/blob/master/Documentation/admin-guide/devices.txt
	char nv_path[15] = { 0 };	// /dev/nvidiaXXX
	const char *nvctl_path = "/dev/nvidiactl";
	const char *nvuvm_path = "/dev/nvidia-uvm";
	const char *nvidia_modeset_path = "/dev/nvidia-modeset";

	struct stat sbuf;

	// /dev/nvidia0 through /dev/nvidia254
	for (unsigned nv_minor = 0; nv_minor < 255; nv_minor++) {
		sc_must_snprintf(nv_path, sizeof(nv_path), "/dev/nvidia%u",
				 nv_minor);

		// Stop trying to find devices after one is not found. In this
		// manner, we'll add /dev/nvidia0 and /dev/nvidia1 but stop
		// trying to find nvidia3 - nvidia254 if nvidia2 is not found.
		if (stat(nv_path, &sbuf) != 0) {
			break;
		}
		_run_snappy_app_dev_add_majmin(udev_s, nv_path,
					       MAJOR(sbuf.st_rdev),
					       MINOR(sbuf.st_rdev));
	}

	// /dev/nvidiactl
	if (stat(nvctl_path, &sbuf) == 0) {
		_run_snappy_app_dev_add_majmin(udev_s, nvctl_path,
					       MAJOR(sbuf.st_rdev),
					       MINOR(sbuf.st_rdev));
	}
	// /dev/nvidia-uvm
	if (stat(nvuvm_path, &sbuf) == 0) {
		_run_snappy_app_dev_add_majmin(udev_s, nvuvm_path,
					       MAJOR(sbuf.st_rdev),
					       MINOR(sbuf.st_rdev));
	}
	// /dev/nvidia-modeset
	if (stat(nvidia_modeset_path, &sbuf) == 0) {
		_run_snappy_app_dev_add_majmin(udev_s, nvidia_modeset_path,
					       MAJOR(sbuf.st_rdev),
					       MINOR(sbuf.st_rdev));
	}
	// /dev/uhid isn't represented in sysfs, so add it to the device cgroup
	// if it exists and let AppArmor handle the mediation
	if (stat("/dev/uhid", &sbuf) == 0) {
		_run_snappy_app_dev_add_majmin(udev_s, "/dev/uhid",
					       MAJOR(sbuf.st_rdev),
					       MINOR(sbuf.st_rdev));
	}
	// add the assigned devices
	while (udev_s->assigned != NULL) {
		const char *path = udev_list_entry_get_name(udev_s->assigned);
		if (path == NULL)
			die("udev_list_entry_get_name failed");
		run_snappy_app_dev_add(udev_s, path);
		udev_s->assigned = udev_list_entry_get_next(udev_s->assigned);
	}
}
