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
#include "user-support.h"

#include <errno.h>
#include <stdlib.h>
#include <sys/stat.h>
#include <limits.h>
#include <sys/types.h>
#include <sys/stat.h>
#include <fcntl.h>

#include "../libsnap-confine-private/cleanup-funcs.h"
#include "../libsnap-confine-private/feature.h"
#include "../libsnap-confine-private/string-utils.h"
#include "../libsnap-confine-private/utils.h"

static void sc_migrate_to_dot_snapdata(const char *real_home)
{
	struct stat fi;
	int hfd SC_CLEANUP(sc_cleanup_close) = -1;
	hfd = open(real_home, O_PATH | O_DIRECTORY | O_NOFOLLOW);
	if (hfd < 0) {
		die("cannot open path %s", real_home);
	}
	/* If ~/.snapdata exists then there is nothing to do. */
	if (fstatat(hfd, ".snapdata", &fi, AT_SYMLINK_NOFOLLOW) < 0) {
		if (errno != ENOENT) {
			die("cannot stat ~/.snapdata");
		}
	} else {
		return;
	}

	/* If ~/snap doesn't exist then there is nothing to do. */
	if (fstatat(hfd, "snap", &fi, AT_SYMLINK_NOFOLLOW) < 0) {
		if (errno == ENOENT) {
			return;
		}
		die("cannot stat ~/snap");
	}
	/* If ~/snap is a symlink then don't perform the rename! */
	if ((fi.st_mode & S_IFMT) == S_IFLNK) {
		return;
	}

	/* Rename ~/snap to ~/.snapdata making sure not to clobber ~/.snapdata. */
	if (renameat2(hfd, "snap", hfd, ".snapdata", RENAME_NOREPLACE) < 0) {
		if (errno == EEXIST) {
			return;
		}
		die("cannot migrate ~/snap to ~/.snapdata");
	}
}

static void sc_migrate_to_snap(const char *real_home)
{
	struct stat fi;
	int hfd SC_CLEANUP(sc_cleanup_close) = -1;
	hfd = open(real_home, O_PATH | O_DIRECTORY | O_NOFOLLOW);
	if (hfd < 0) {
		die("cannot open path %s", real_home);
	}
	/* If ~/snap exists then there is nothing to do. */
	if (fstatat(hfd, "snap", &fi, AT_SYMLINK_NOFOLLOW) < 0) {
		if (errno != ENOENT) {
			die("cannot stat ~/snap");
		}
	} else {
		return;
	}

	/* If ~/.snapdata doesn't exist then there is nothing to do. */
	if (fstatat(hfd, ".snapdata", &fi, AT_SYMLINK_NOFOLLOW) < 0) {
		if (errno == ENOENT) {
			return;
		}
		die("cannot stat ~/.snapdata");
	}
	/* If ~/.snapdata is a symlink then don't perform the rename! */
	if ((fi.st_mode & S_IFMT) == S_IFLNK) {
		return;
	}

	/* Rename ~/.snapdata to ~/snap making sure not to clobber ~/snap. */
	if (renameat2(hfd, ".snapdata", hfd, "snap", RENAME_NOREPLACE) < 0) {
		if (errno == EEXIST) {
			return;
		}
		die("cannot migrate ~/.snapdata to ~/snap");
	}
}

void setup_user_data(const char *snap_instance)
{
	const char *user_data = getenv("SNAP_USER_DATA");
	const char *real_home = getenv("SNAP_REAL_HOME");

	if (user_data == NULL) {
		return;
	}
	if (real_home == NULL) {
		die("cannot remap snap folder, SNAP_REAL_HOME is not set");
	}

	if (sc_feature_enabled(SC_FEATURE_HIDDEN_SNAP_FOLDER)) {
		char buf[PATH_MAX + 1];
		const char *revision = getenv("SNAP_REVISION");
		if (revision == NULL) {
			die("cannot remap snap folder, SNAP_REVISION is not set");
		}
		sc_must_snprintf(buf, sizeof buf, "%s/.snapdata/%s/%s",
				 real_home, snap_instance, revision);
		setenv("SNAP_USER_DATA", buf, 1);
		setenv("HOME", buf, 1);
		sc_must_snprintf(buf, sizeof buf, "%s/.snapdata/%s/common",
				 real_home, snap_instance);
		setenv("SNAP_USER_COMMON", buf, 1);
		user_data = getenv("SNAP_USER_DATA");

		sc_migrate_to_dot_snapdata(real_home);
	} else {
		sc_migrate_to_snap(real_home);
	}
	// Only support absolute paths.
	if (user_data[0] != '/') {
		die("user data directory must be an absolute path");
	}

	debug("creating user data directory: %s", user_data);
	if (sc_nonfatal_mkpath(user_data, 0755) < 0) {
		if (errno == EROFS && !sc_startswith(user_data, "/home/")) {
			// clear errno or it will be displayed in die()
			errno = 0;
			die("Sorry, home directories outside of /home are not currently supported. \nSee https://forum.snapcraft.io/t/11209 for details.");
		}
		die("cannot create user data directory: %s", user_data);
	};
}

void setup_user_xdg_runtime_dir(void)
{
	const char *xdg_runtime_dir = getenv("XDG_RUNTIME_DIR");

	if (xdg_runtime_dir == NULL)
		return;
	// Only support absolute paths.
	if (xdg_runtime_dir[0] != '/') {
		die("XDG_RUNTIME_DIR must be an absolute path");
	}

	errno = 0;
	debug("creating user XDG_RUNTIME_DIR directory: %s", xdg_runtime_dir);
	if (sc_nonfatal_mkpath(xdg_runtime_dir, 0755) < 0) {
		die("cannot create user XDG_RUNTIME_DIR directory: %s",
		    xdg_runtime_dir);
	}
	// if successfully created the directory (ie, not EEXIST), then chmod it.
	if (errno == 0 && chmod(xdg_runtime_dir, 0700) != 0) {
		die("cannot change permissions of user XDG_RUNTIME_DIR directory to 0700");
	}
}
