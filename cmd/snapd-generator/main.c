/*
 * Copyright (C) 2018-2024 Canonical Ltd
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

#include <stddef.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <dirent.h>
#include <errno.h>
#include <unistd.h>

#include "config.h"

#include "../libsnap-confine-private/cleanup-funcs.h"
#include "../libsnap-confine-private/infofile.h"
#include "../libsnap-confine-private/mountinfo.h"
#include "../libsnap-confine-private/string-utils.h"

static sc_mountinfo_entry *find_dir_mountinfo(sc_mountinfo *mounts, const char *mnt_dir)
{
	sc_mountinfo_entry *cur, *root = NULL;
	for (cur = sc_first_mountinfo_entry(mounts); cur != NULL;
	     cur = sc_next_mountinfo_entry(cur)) {
		// Look for the mount info entry. We take the last one, which
		// would be the last mount on top of mnt_dir.
		if (sc_streq(mnt_dir, cur->mount_dir)) {
			root = cur;
		}
	}
	return root;
}

// Create a mount unit in normal_dir that is performed at early stages for
// "what" in directory "where".
// WARNING we need to escape special characters in "where" to create the unit
// name. We should do the same as systemd-escape(1), but for simplicity we just
// replace slashes with dashes, which is fine for the moment as this is called
// currently for mountpoints /usr/lib/{modules,firmware} only.
static int create_early_mount(const char *normal_dir,
			      const char *what,
			      const char *where)
{
	// Replace directory separators with dashes to build the unit name.
	char *unit_name SC_CLEANUP(sc_cleanup_string) = NULL;
	// (... + 1) to remove the initial '/'
	unit_name = sc_strdup(where + 1);
	for (char *p = unit_name; (p = strchr(p, '/')) != NULL; *p = '-');

	// Construct the file name for a new systemd mount unit.
	char fname[PATH_MAX + 1] = { 0 };
	sc_must_snprintf(fname, sizeof fname,
			 "%s/%s.mount",	 normal_dir, unit_name);

	// Open the mount unit and write the contents.
	FILE *f SC_CLEANUP(sc_cleanup_file) = NULL;
	f = fopen(fname, "w");
	if (!f) {
		fprintf(stderr, "cannot write to %s: %m\n", fname);
		return 1;
	}
	fprintf(f, "[Unit]\n");
	fprintf(f, "Description=Early mount of kernel drivers tree\n");
	fprintf(f, "DefaultDependencies=no\n");
	fprintf(f, "After=systemd-remount-fs.service\n");
	fprintf(f, "Before=sysinit.target\n");
	fprintf(f, "Before=systemd-udevd.service systemd-modules-load.service\n");
	fprintf(f, "Before=umount.target\n");
	fprintf(f, "Conflicts=umount.target\n");
	fprintf(f, "\n");
	fprintf(f, "[Mount]\n");
	fprintf(f, "What=%s\n", what);
	fprintf(f, "Where=%s\n", where);
	fprintf(f, "Options=bind,shared\n");

	// Wanted by sysinit.target.wants - create folders if needed and symlink

	char wants_d[PATH_MAX + 1] = { 0 };
	sc_must_snprintf(wants_d, sizeof wants_d,
			 "%s/sysinit.target.wants", normal_dir);
	if (mkdir(wants_d, 0755) != 0 && errno != EEXIST) {
		fprintf(stderr, "cannot create %s directory: %m\n", wants_d);
		return 1;
	}

	char target[PATH_MAX + 1] = { 0 };
	char lnpath[PATH_MAX + 1] = { 0 };
	sc_must_snprintf(target, sizeof target, "../%s.mount", unit_name);
	sc_must_snprintf(lnpath, sizeof lnpath,
			 "%s/%s.mount", wants_d, unit_name);
	if (symlink(target, lnpath) != 0) {
		fprintf(stderr, "cannot create symlink %s: %m\n", lnpath);
		return 1;
	}

	return 0;
}

#define MAJOR_LOOP_DEV 7
#define SNAPD_DRIVERS_TREE_DIR "/var/lib/snapd/kernel"
#define FIRMWARE_DIR "firmware"
#define MODULES_DIR "modules"
#define FIRMWARE_MNTPOINT "/usr/lib/" FIRMWARE_DIR
#define MODULES_MNTPOINT "/usr/lib/" MODULES_DIR

static int ensure_kernel_drivers_mounts(const char *normal_dir)
{
	const char* const kern_mnt_dir = "/run/mnt/kernel";
	// Find mount information
	sc_mountinfo *mounts SC_CLEANUP(sc_cleanup_mountinfo) = NULL;
	mounts = sc_parse_mountinfo("/proc/1/mountinfo");
	if (!mounts) {
		fprintf(stderr, "cannot open or parse /proc/1/mountinfo\n");
		return 1;
	}

	// Create mount units only if not already present (which would be the
	// case for an old initramfs) - otherwise systemd-fstab-generator
	// complains, and older initramfs won't come in a kernel snap with
	// support for components anyway.
	const char* const kern_mntpts[] = { FIRMWARE_MNTPOINT, MODULES_MNTPOINT };
	for (size_t i = 0; i < sizeof kern_mntpts/sizeof (char *); ++i) {
		sc_mountinfo_entry *minfo = find_dir_mountinfo(mounts, kern_mntpts[i]);
		// If the mounts already exist (old initramfs), do not create them -
		// note that we additionally check for SNAPD_DRIVERS_TREE_DIR in the
		// mount source to make sure the units created here are still
		// generated on "systemctl daemon-reload".
		if (minfo && strstr(minfo->root, SNAPD_DRIVERS_TREE_DIR) == NULL) {
			return 0;
		}
	}

	// Find active kernel name and revision by looking at what was
	// mounted in /run/mnt/kernel by snap-bootstrap.

	sc_mountinfo_entry *kern_minfo = find_dir_mountinfo(mounts, kern_mnt_dir);
	if (!kern_minfo) {
		// This is not Ubuntu Core / hybrid, do nothing and do not fail
		return 0;
	}

	// Mount source should be a snap
	if (!sc_streq(kern_minfo->fs_type, "squashfs")) {
		fprintf(stderr, "unexpected fs type (%s) for %s\n",
			kern_minfo->fs_type, kern_mnt_dir);
		return 1;
	}

	// We expect a loop device as source
	if (kern_minfo->dev_major != MAJOR_LOOP_DEV) {
		fprintf(stderr, "mount source %s for %s is not a loop device\n",
			kern_minfo->mount_source, kern_mnt_dir);
		return 1;
	}

	// Find out backing file
	char fname[PATH_MAX + 1] = { 0 };
	sc_must_snprintf(fname, sizeof fname,
			 "/sys/dev/block/%u:%u/loop/backing_file",
			 kern_minfo->dev_major, kern_minfo->dev_minor);
	FILE *f SC_CLEANUP(sc_cleanup_file) = NULL;
	f = fopen(fname, "r");
	if (!f) {
		fprintf(stderr, "cannot open %s: %m\n", fname);
		return 1;
	}
	char snap_path[PATH_MAX + 1] = { 0 };
	if (fgets(snap_path, sizeof snap_path, f) == NULL) {
		fprintf(stderr, "while reading %s: %m\n", fname);
		return 1;
	}

	// Now parse the snap path
	size_t i;
	for (i = strlen(snap_path); i > 0 && snap_path[--i] != '/';);
	char *snap_fname = snap_path + i + 1;

	// snap_fname is expected to contain "<name>_<rev>.snap\n" - fgets includes
	// that new line at the end, but anyway we ignore what comes after the dot.
	char *saveptr = NULL;
	char *snap_name = strtok_r(snap_fname, "_", &saveptr);
	if (snap_name == NULL) {
		fprintf(stderr, "snap name not found in loop backing file\n");
		return 1;
	}
	char *snap_rev = strtok_r(NULL, ".", &saveptr);
	if (snap_rev == NULL) {
		fprintf(stderr, "snap revision not found in loop backing file\n");
		return 1;
	}

	int res;
	char what[PATH_MAX + 1] = { 0 };
	sc_must_snprintf(what, sizeof what,
			 SNAPD_DRIVERS_TREE_DIR "/%s/%s/lib/" MODULES_DIR,
			 snap_name, snap_rev);
	res = create_early_mount(normal_dir, what, MODULES_MNTPOINT);
	if (res != 0) {
		return res;
	}
	sc_must_snprintf(what, sizeof what,
			 SNAPD_DRIVERS_TREE_DIR "/%s/%s/lib/" FIRMWARE_DIR,
			 snap_name, snap_rev);
	return create_early_mount(normal_dir, what, FIRMWARE_MNTPOINT);
}

static int ensure_root_fs_shared(const char *normal_dir)
{
	// Load /proc/1/mountinfo so that we can inspect the root filesystem.
	sc_mountinfo *mounts SC_CLEANUP(sc_cleanup_mountinfo) = NULL;
	mounts = sc_parse_mountinfo("/proc/1/mountinfo");
	if (!mounts) {
		fprintf(stderr, "cannot open or parse /proc/1/mountinfo\n");
		return 1;
	}
	sc_mountinfo_entry *root = find_dir_mountinfo(mounts, "/");
	if (!root) {
		fprintf(stderr,
			"cannot find mountinfo entry of the root filesystem\n");
		return 1;
	}
	// Check if the root file-system is mounted with shared option.
	if (strstr(root->optional_fields, "shared:") != NULL) {
		// The workaround is not needed, everything is good as-is.
		return 0;
	}
	// Construct the file name for a new systemd mount unit.
	char fname[PATH_MAX + 1] = { 0 };
	sc_must_snprintf(fname, sizeof fname,
			 "%s/" SNAP_MOUNT_DIR_SYSTEMD_UNIT ".mount",
			 normal_dir);

	// Open the mount unit and write the contents.
	FILE *f SC_CLEANUP(sc_cleanup_file) = NULL;
	f = fopen(fname, "wt");
	if (!f) {
		fprintf(stderr, "cannot open %s: %m\n", fname);
		return 1;
	}
	fprintf(f,
		"# Ensure that snap mount directory is mounted \"shared\" "
		"so snaps can be refreshed correctly (LP: #1668759).\n");
	fprintf(f, "[Unit]\n");
	fprintf(f,
		"Description=Ensure that the snap directory "
		"shares mount events.\n");
	fprintf(f, "[Mount]\n");
	fprintf(f, "What=" SNAP_MOUNT_DIR "\n");
	fprintf(f, "Where=" SNAP_MOUNT_DIR "\n");
	fprintf(f, "Type=none\n");
	fprintf(f, "Options=bind,shared\n");

	/* We do not need to create symlinks from any target since
	 * this generated mount will automically be added to implicit
	 * dependencies of sub mount units through
	 * `RequiresMountsFor`.
	 */

	return 0;
}

static bool file_exists(const char *path)
{
	struct stat buf;
	// Not using lstat to automatically resolve symbolic links,
	// including handling, as an error, dangling symbolic links.
	return stat(path, &buf) == 0 && (buf.st_mode & S_IFMT) == S_IFREG;
}

// PATH may not be set (the case on 16.04), in which case this is the fallback
// for looking up squashfuse / snapfuse executable.
// Based on what systemd uses when compiled for systems with "unmerged /usr"
// (see man systemd.exec).
static const char *const path_fallback =
    "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin";

static bool executable_exists(const char *name)
{
	char *path = getenv("PATH");
	char *path_copy SC_CLEANUP(sc_cleanup_string) = NULL;
	if (path == NULL) {
		path_copy = sc_strdup(path_fallback);
	} else {
		path_copy = sc_strdup(path);
	}

	char *ptr = NULL;
	char *token = strtok_r(path_copy, ":", &ptr);
	char fname[PATH_MAX + 1] = { 0 };
	while (token) {
		sc_must_snprintf(fname, sizeof fname, "%s/%s", token, name);
		if (access(fname, X_OK) == 0) {
			return true;
		}
		token = strtok_r(NULL, ":", &ptr);
	}
	return false;
}

static bool is_snap_try_snap_unit(const char *units_dir,
				  const char *mount_unit_name)
{
	char fname[PATH_MAX + 1] = { 0 };
	sc_must_snprintf(fname, sizeof fname, "%s/%s", units_dir,
			 mount_unit_name);
	FILE *f SC_CLEANUP(sc_cleanup_file) = NULL;
	f = fopen(fname, "r");
	if (!f) {
		// not really expected
		fprintf(stderr, "cannot open mount unit %s: %m\n", fname);
		return false;
	}

	char *what SC_CLEANUP(sc_cleanup_string) = NULL;
	sc_error *err = NULL;
	if (sc_infofile_get_ini_section_key(f, "Mount", "What", &what, &err) <
	    0) {
		fprintf(stderr, "cannot read mount unit %s: %s\n", fname,
			sc_error_msg(err));
		sc_cleanup_error(&err);
		return false;
	}

	struct stat st;
	// if What points to a directory, then it's a snap try unit.
	return stat(what, &st) == 0 && (st.st_mode & S_IFMT) == S_IFDIR;
}

static int ensure_fusesquashfs_inside_container(const char *normal_dir)
{
	// check if we are running inside a container, systemd
	// provides this file all the way back to trusty if run in a
	// container
	if (!file_exists("/run/systemd/container")) {
		return 0;
	}

	const char *fstype;
	if (executable_exists("squashfuse")) {
		fstype = "fuse.squashfuse";
	} else if (executable_exists("snapfuse")) {
		fstype = "fuse.snapfuse";
	} else {
		fprintf(stderr,
			"cannot find squashfuse or snapfuse executable\n");
		return 2;
	}

	DIR *units_dir SC_CLEANUP(sc_cleanup_closedir) = NULL;
	units_dir = opendir("/etc/systemd/system");
	if (units_dir == NULL) {
		// nothing to do
		return 0;
	}

	char fname[PATH_MAX + 1] = { 0 };

	struct dirent *ent;
	while ((ent = readdir(units_dir))) {
		// find snap mount units, i.e:
		// snap-somename.mount or var-lib-snapd-snap-somename.mount
		if (!sc_endswith(ent->d_name, ".mount")) {
			continue;
		}
		if (!(sc_startswith(ent->d_name, "snap-")
		      || sc_startswith(ent->d_name, "var-lib-snapd-snap-"))) {
			continue;
		}
		if (is_snap_try_snap_unit("/etc/systemd/system", ent->d_name)) {
			continue;
		}
		sc_must_snprintf(fname, sizeof fname,
				 "%s/%s.d", normal_dir, ent->d_name);
		if (mkdir(fname, 0755) != 0) {
			if (errno != EEXIST) {
				fprintf(stderr,
					"cannot create %s directory: %m\n",
					fname);
				return 2;
			}
		}

		sc_must_snprintf(fname, sizeof fname,
				 "%s/%s.d/container.conf", normal_dir,
				 ent->d_name);

		FILE *f SC_CLEANUP(sc_cleanup_file) = NULL;
		f = fopen(fname, "w");
		if (!f) {
			fprintf(stderr, "cannot open %s: %m\n", fname);
			return 2;
		}
		fprintf(f,
			"[Mount]\nType=%s\nOptions=nodev,ro,x-gdu.hide,x-gvfs-hide,allow_other\nLazyUnmount=yes\n",
			fstype);
	}

	return 0;
}

int main(int argc, char **argv)
{
	if (argc != 4) {
		printf
		    ("usage: snapd-generator normal-dir early-dir late-dir\n");
		return 1;
	}
	const char *normal_dir = argv[1];
	// For reference, but we don't use those variables here.
	// const char *early_dir = argv[2];
	// const char *late_dir = argv[3];

	int status = 0;
	status = ensure_root_fs_shared(normal_dir);
	status |= ensure_fusesquashfs_inside_container(normal_dir);
	status |= ensure_kernel_drivers_mounts(normal_dir);

	return status;
}
