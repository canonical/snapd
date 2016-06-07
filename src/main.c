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

#include <unistd.h>
#include <stdio.h>
#include <stdlib.h>
#include <limits.h>
#include <sys/mount.h>
#ifdef STRICT_CONFINEMENT
#include <sys/apparmor.h>
#endif				// ifdef STRICT_CONFINEMENT
#include <errno.h>
#include <sched.h>
#include <string.h>
#include <fcntl.h>

#include "utils.h"
#include "snap.h"
#include "classic.h"
#include "mount-support.h"
#ifdef STRICT_CONFINEMENT
#include "seccomp-support.h"
#include "udev-support.h"
#endif				// ifdef STRICT_CONFINEMENT

void mkpath(const char *const path)
{
	// If asked to create an empty path, return immediately.
	if (strlen(path) == 0) {
		return;
	}
	// We're going to use strtok_r, which needs to modify the path, so
	// we'll make a copy of it.
	char *path_copy = strdup(path);
	if (path_copy == NULL) {
		die("failed to create user data directory");
	}
	// Open flags to use while we walk the user data path:
	// - Don't follow symlinks
	// - Don't allow child access to file descriptor
	// - Only open a directory (fail otherwise)
	int open_flags = O_NOFOLLOW | O_CLOEXEC | O_DIRECTORY;

	// We're going to create each path segment via openat/mkdirat calls
	// instead of mkdir calls, to avoid following symlinks and placing the
	// user data directory somewhere we never intended for it to go. The
	// first step is to get an initial file descriptor.
	int fd = AT_FDCWD;
	if (path_copy[0] == '/') {
		fd = open("/", open_flags);
		if (fd < 0) {
			free(path_copy);
			die("failed to create user data directory");
		}
	}
	// strtok_r needs a pointer to keep track of where it is in the string.
	char *path_walker;

	// Initialize tokenizer and obtain first path segment.
	char *path_segment = strtok_r(path_copy, "/", &path_walker);
	while (path_segment) {
		// Try to create the directory. It's okay if it already
		// existed, but any other error is fatal.
		if (mkdirat(fd, path_segment, 0755) < 0 && errno != EEXIST) {
			close(fd);	// we die regardless of return code
			free(path_copy);
			die("failed to create user data directory");
		}
		// Open the parent directory we just made (and close the
		// previous one) so we can continue down the path.
		int previous_fd = fd;
		fd = openat(fd, path_segment, open_flags);
		if (close(previous_fd) != 0) {
			free(path_copy);
			die("could not close path segment");
		}
		if (fd < 0) {
			free(path_copy);
			die("failed to create user data directory");
		}
		// Obtain the next path segment.
		path_segment = strtok_r(NULL, "/", &path_walker);
	}

	// Close the descriptor for the final directory in the path.
	if (close(fd) != 0) {
		free(path_copy);
		die("could not close final directory");
	}

	free(path_copy);
}

void setup_user_data()
{
	const char *user_data = getenv("SNAP_USER_DATA");

	if (user_data == NULL)
		return;
	// Only support absolute paths.
	if (user_data[0] != '/') {
		die("user data directory must be an absolute path");
	}

	mkpath(user_data);
}

int main(int argc, char **argv)
{
	const int NR_ARGS = 2;
	if (argc < NR_ARGS + 1)
		die("Usage: %s <security-tag> <binary>", argv[0]);

	const char *appname = argv[1];
#ifdef STRICT_CONFINEMENT
	const char *aa_profile = argv[1];
#endif				// ifdef STRICT_CONFINEMENT
	const char *binary = argv[2];
	uid_t real_uid = getuid();
	gid_t real_gid = getgid();

	if (!verify_appname(appname))
		die("appname %s not allowed", appname);

	// this code always needs to run as root for the cgroup/udev setup,
	// however for the tests we allow it to run as non-root
	if (geteuid() != 0
	    && secure_getenv("UBUNTU_CORE_LAUNCHER_NO_ROOT") == NULL) {
		die("need to run as root or suid");
	}

	if (geteuid() == 0) {

		// ensure we run in our own slave mount namespace, this will
		// create a new mount namespace and make it a slave of "/"
		//
		// Note that this means that no mount actions inside our
		// namespace are propagated to the main "/". We need this
		// both for the private /tmp we create and for the bind
		// mounts we do on a classic distribution system
		//
		// This also means you can't run an automount daemon unter
		// this launcher
		setup_slave_mount_namespace();

		// do the mounting if run on a non-native snappy system
		if (is_running_on_classic_distribution()) {
			setup_snappy_os_mounts();
		}
#ifdef STRICT_CONFINEMENT
		// set up private mounts
		setup_private_mount(appname);

		// set up private /dev/pts
		setup_private_pts();

		// this needs to happen as root
		struct snappy_udev udev_s;
		if (snappy_udev_init(appname, &udev_s) == 0)
			setup_devices_cgroup(appname, &udev_s);
		snappy_udev_cleanup(&udev_s);
#endif				// ifdef STRICT_CONFINEMENT

		// the rest does not so temporarily drop privs back to calling
		// user (we'll permanently drop after loading seccomp)
		if (setegid(real_gid) != 0)
			die("setegid failed");
		if (seteuid(real_uid) != 0)
			die("seteuid failed");

		if (real_gid != 0 && geteuid() == 0)
			die("dropping privs did not work");
		if (real_uid != 0 && getegid() == 0)
			die("dropping privs did not work");
	}
	// Ensure that the user data path exists.
	setup_user_data();

	// https://wiki.ubuntu.com/SecurityTeam/Specifications/SnappyConfinement

#ifdef STRICT_CONFINEMENT
	int rc = 0;
	// set apparmor rules
	rc = aa_change_onexec(aa_profile);
	if (rc != 0) {
		if (secure_getenv("SNAPPY_LAUNCHER_INSIDE_TESTS") == NULL)
			die("aa_change_onexec failed with %i", rc);
	}
	// set seccomp (note: seccomp_load_filters die()s on all failures)
	seccomp_load_filters(aa_profile);
#endif				// ifdef STRICT_CONFINEMENT

	// Permanently drop if not root
	if (geteuid() == 0) {
		// Note that we do not call setgroups() here because its ok
		// that the user keeps the groups he already belongs to
		if (setgid(real_gid) != 0)
			die("setgid failed");
		if (setuid(real_uid) != 0)
			die("setuid failed");

		if (real_gid != 0 && (getuid() == 0 || geteuid() == 0))
			die("permanently dropping privs did not work");
		if (real_uid != 0 && (getgid() == 0 || getegid() == 0))
			die("permanently dropping privs did not work");
	}
	// and exec the new binary
	execv(binary, (char *const *)&argv[NR_ARGS]);
	perror("execv failed");
	return 1;
}
