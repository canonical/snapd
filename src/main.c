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
#ifndef _GNU_SOURCE
#define _GNU_SOURCE
#endif

#include <unistd.h>
#include <stdio.h>
#include <stdlib.h>
#include <limits.h>
#include <linux/sched.h>
#include <sys/mount.h>
#include <sys/apparmor.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <sys/wait.h>
#include <errno.h>
#include <sched.h>
#include <string.h>
#include <linux/kdev_t.h>
#include <stdlib.h>
#include <regex.h>
#include <grp.h>
#include <fcntl.h>
#include <glob.h>

#include <ctype.h>

#include "libudev.h"

#include "utils.h"
#include "seccomp.h"

#define MAX_BUF 1000

struct snappy_udev {
	struct udev *udev;
	struct udev_enumerate *devices;
	struct udev_list_entry *assigned;
	char tagname[MAX_BUF];
	size_t tagname_len;
};

bool verify_appname(const char *appname)
{
	// these chars are allowed in a appname
	const char *whitelist_re = "^[a-z0-9][a-z0-9+._-]+$";
	regex_t re;
	if (regcomp(&re, whitelist_re, REG_EXTENDED | REG_NOSUB) != 0)
		die("can not compile regex %s", whitelist_re);

	int status = regexec(&re, appname, 0, NULL, 0);
	regfree(&re);

	return (status == 0);
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

	debug("run_snappy_app_dev_add: %s %s", path, udev_s->tagname);

	struct udev_device *d =
	    udev_device_new_from_syspath(udev_s->udev, path);
	if (d == NULL)
		die("can not find %s", path);
	dev_t devnum = udev_device_get_devnum(d);
	udev_device_unref(d);

	int status = 0;
	pid_t pid = fork();
	if (pid < 0) {
		die("could not fork");
	}
	if (pid == 0) {
		uid_t real_uid, effective_uid, saved_uid;
		if (getresuid(&real_uid, &effective_uid, &saved_uid) != 0)
			die("could not find user IDs");
		// can't update the cgroup unless the real_uid is 0, euid as
		// 0 is not enough
		if (real_uid != 0 && effective_uid == 0)
			if (setuid(0) != 0)
				die("setuid failed");
		char buf[64];
		unsigned major = MAJOR(devnum);
		unsigned minor = MINOR(devnum);
		must_snprintf(buf, sizeof(buf), "%u:%u", major, minor);
		execl("/lib/udev/snappy-app-dev", "/lib/udev/snappy-app-dev",
		      "add", udev_s->tagname, path, buf, NULL);
		die("execl failed");
	}
	if (waitpid(pid, &status, 0) < 0)
		die("waitpid failed");
	if (WIFEXITED(status) && WEXITSTATUS(status) != 0)
		die("child exited with status %i", WEXITSTATUS(status));
	else if (WIFSIGNALED(status))
		die("child died with signal %i", WTERMSIG(status));
}

/*
 * snappy_udev_init() - setup the snappy_udev structure. Return 0 if devices
 * are assigned, else return -1. Callers should use snappy_udev_cleanup() to
 * cleanup.
 */
int snappy_udev_init(const char *appname, struct snappy_udev *udev_s)
{
	debug("snappy_udev_init");
	int rc = 0;

	// extra paranoia
	if (!verify_appname(appname))
		die("appname %s not allowed", appname);

	udev_s->tagname[0] = '\0';
	udev_s->tagname_len = 0;
	// TAG+="snap_<appname>" (udev doesn't like '.' in the tag name)
	udev_s->tagname_len = must_snprintf(udev_s->tagname, MAX_BUF,
					    "snap_%s", appname);
	for (int i = 0; i < udev_s->tagname_len; i++)
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

void setup_devices_cgroup(const char *appname, struct snappy_udev *udev_s)
{
	debug("setup_devices_cgroup");
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

	// extra paranoia
	if (!verify_appname(appname))
		die("appname %s not allowed", appname);
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
	char cgroup_dir[PATH_MAX];

	must_snprintf(cgroup_dir, sizeof(cgroup_dir),
		      "/sys/fs/cgroup/devices/snap.%s/", appname);

	if (mkdir(cgroup_dir, 0755) < 0 && errno != EEXIST)
		die("mkdir failed");

	// move ourselves into it
	char cgroup_file[PATH_MAX];
	must_snprintf(cgroup_file, sizeof(cgroup_file), "%s%s", cgroup_dir,
		      "tasks");

	char buf[128];
	must_snprintf(buf, sizeof(buf), "%i", getpid());
	write_string_to_file(cgroup_file, buf);

	// deny by default. Write 'a' to devices.deny to remove all existing
	// devices that were added in previous launcher invocations, then add
	// the static and assigned devices. This ensures that at application
	// launch the cgroup only has what is currently assigned.
	must_snprintf(cgroup_file, sizeof(cgroup_file), "%s%s", cgroup_dir,
		      "devices.deny");
	write_string_to_file(cgroup_file, "a");

	// add the common devices
	for (int i = 0; static_devices[i] != NULL; i++)
		run_snappy_app_dev_add(udev_s, static_devices[i]);

	// add the assigned devices
	while (udev_s->assigned != NULL) {
		const char *path = udev_list_entry_get_name(udev_s->assigned);
		if (path == NULL)
			die("udev_list_entry_get_name failed");
		run_snappy_app_dev_add(udev_s, path);
		udev_s->assigned = udev_list_entry_get_next(udev_s->assigned);
	}
}

bool is_running_on_classic_ubuntu()
{
	return (access("/var/lib/dpkg/status", F_OK) == 0);
}

void setup_private_mount(const char *appname)
{
	uid_t uid = getuid();
	gid_t gid = getgid();
	char tmpdir[MAX_BUF] = { 0 };

	// Create a 0700 base directory, this is the base dir that is
	// protected from other users.
	//
	// Under that basedir, we put a 1777 /tmp dir that is then bind
	// mounted for the applications to use
	must_snprintf(tmpdir, sizeof(tmpdir), "/tmp/snap.%d_%s_XXXXXX", uid,
		      appname);
	if (mkdtemp(tmpdir) == NULL) {
		die("unable to create tmpdir");
	}
	// now we create a 1777 /tmp inside our private dir
	mode_t old_mask = umask(0);
	char *d = strdup(tmpdir);
	if (!d) {
		die("Out of memory");
	}
	must_snprintf(tmpdir, sizeof(tmpdir), "%s/tmp", d);
	free(d);

	if (mkdir(tmpdir, 01777) != 0) {
		die("unable to create /tmp inside private dir");
	}
	umask(old_mask);

	// MS_BIND is there from linux 2.4
	if (mount(tmpdir, "/tmp", NULL, MS_BIND, NULL) != 0) {
		die("unable to bind private /tmp");
	}
	// MS_PRIVATE needs linux > 2.6.11
	if (mount("none", "/tmp", NULL, MS_PRIVATE, NULL) != 0) {
		die("unable to make /tmp/ private");
	}
	// do the chown after the bind mount to avoid potential shenanigans
	if (chown("/tmp/", uid, gid) < 0) {
		die("unable to chown tmpdir");
	}
	// ensure we set the various TMPDIRs to our newly created tmpdir
	const char *tmpd[] = { "TMPDIR", "TEMPDIR", NULL };
	int i;
	for (i = 0; tmpd[i] != NULL; i++) {
		if (setenv(tmpd[i], "/tmp", 1) != 0) {
			die("unable to set '%s'", tmpd[i]);
		}
	}
}

void setup_private_pts()
{
	// See https://www.kernel.org/doc/Documentation/filesystems/devpts.txt
	//
	// Ubuntu by default uses devpts 'single-instance' mode where
	// /dev/pts/ptmx is mounted with ptmxmode=0000. We don't want to change
	// the startup scripts though, so we follow the instructions in point
	// '4' of 'User-space changes' in the above doc. In other words, after
	// unshare(CLONE_NEWNS), we mount devpts with -o
	// newinstance,ptmxmode=0666 and then bind mount /dev/pts/ptmx onto
	// /dev/ptmx

	struct stat st;

	// Make sure /dev/pts/ptmx exists, otherwise we are in legacy mode
	// which doesn't provide the isolation we require.
	if (stat("/dev/pts/ptmx", &st) != 0) {
		die("/dev/pts/ptmx does not exist");
	}
	// Make sure /dev/ptmx exists so we can bind mount over it
	if (stat("/dev/ptmx", &st) != 0) {
		die("/dev/ptmx does not exist");
	}
	// Since multi-instance, use ptmxmode=0666. The other options are
	// copied from /etc/default/devpts
	if (mount("devpts", "/dev/pts", "devpts", MS_MGC_VAL,
		  "newinstance,ptmxmode=0666,mode=0620,gid=5")) {
		die("unable to mount a new instance of '/dev/pts'");
	}

	if (mount("/dev/pts/ptmx", "/dev/ptmx", "none", MS_BIND, 0)) {
		die("unable to mount '/dev/pts/ptmx'->'/dev/ptmx'");
	}
}

void setup_snappy_os_mounts()
{
	debug("setup_snappy_os_mounts()\n");

	// FIXME: hardcoded "ubuntu-core.*"
	glob_t glob_res;
	if (glob("/snap/ubuntu-core*/current/", 0, NULL, &glob_res) != 0) {
		die("can not find a snappy os");
	}
	if ((glob_res.gl_pathc = !1)) {
		die("expected 1 os snap, found %i", (int)glob_res.gl_pathc);
	}
	char *mountpoint = glob_res.gl_pathv[0];

	// we mount some whitelisted directories
	//
	// Note that we do not mount "/etc/" from snappy. We could do that,
	// but if we do we need to ensure that data like /etc/{hostname,hosts,
	// passwd,groups} is in sync between the two systems (probably via
	// selected bind mounts of those files).
	const char *mounts[] =
	    { "/bin", "/sbin", "/lib", "/lib32", "/libx32", "/lib64", "/usr" };
	for (int i = 0; i < sizeof(mounts) / sizeof(char *); i++) {
		// we mount the OS snap /bin over the real /bin in this NS
		const char *dst = mounts[i];

		// some system do not have e.g. /lib64
		struct stat sbuf;
		if (stat(dst, &sbuf) != 0) {
			if (errno == ENOENT)
				continue;
			else
				die("could not stat mount point");
		}

		char buf[512];
		must_snprintf(buf, sizeof(buf), "%s%s", mountpoint, dst);
		const char *src = buf;

		debug("mounting %s -> %s\n", src, dst);
		if (mount(src, dst, NULL, MS_BIND, NULL) != 0) {
			die("unable to bind %s to %s", src, dst);
		}
	}

	globfree(&glob_res);
}

void setup_slave_mount_namespace()
{
	// unshare() and CLONE_NEWNS require linux >= 2.6.16 and glibc >= 2.14
	// if using an older glibc, you'd need -D_BSD_SOURCE or -D_SVID_SORUCE.
	if (unshare(CLONE_NEWNS) < 0) {
		die("unable to set up mount namespace");
	}
	// make our "/" a rslave of the real "/". this means that
	// mounts from the host "/" get propagated to our namespace
	// (i.e. we see new media mounts)
	if (mount("none", "/", NULL, MS_REC | MS_SLAVE, NULL) != 0) {
		die("can not make make / rslave");
	}
}

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
	const int NR_ARGS = 3;
	if (argc < NR_ARGS + 1)
		die("Usage: %s <appname> <apparmor> <binary>", argv[0]);

	const char *appname = argv[1];
	const char *aa_profile = argv[2];
	const char *binary = argv[3];
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
		// mounts we do on a classic ubuntu system
		//
		// This also means you can't run an automount daemon unter
		// this launcher
		setup_slave_mount_namespace();

		// do the mounting if run on a non-native snappy system
		if (is_running_on_classic_ubuntu()) {
			setup_snappy_os_mounts();
		}
		// set up private mounts
		setup_private_mount(appname);

		// set up private /dev/pts
		setup_private_pts();

		// this needs to happen as root
		struct snappy_udev udev_s;
		if (snappy_udev_init(appname, &udev_s) == 0)
			setup_devices_cgroup(appname, &udev_s);
		snappy_udev_cleanup(&udev_s);

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

	int rc = 0;
	// set apparmor rules
	rc = aa_change_onexec(aa_profile);
	if (rc != 0) {
		if (secure_getenv("SNAPPY_LAUNCHER_INSIDE_TESTS") == NULL)
			die("aa_change_onexec failed with %i", rc);
	}
	// set seccomp (note: seccomp_load_filters die()s on all failures)
	seccomp_load_filters(aa_profile);

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
