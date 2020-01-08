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

#define _GNU_SOURCE

#include <errno.h>
#include <fcntl.h>
#include <stdarg.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <unistd.h>

#include "cleanup-funcs.h"
#include "panic.h"
#include "utils.h"

void die(const char *msg, ...)
{
	va_list ap;
	va_start(ap, msg);
	sc_panicv(msg, ap);
	va_end(ap);
}

struct sc_bool_name {
	const char *text;
	bool value;
};

static const struct sc_bool_name sc_bool_names[] = {
	{"yes", true},
	{"no", false},
	{"1", true},
	{"0", false},
	{"", false},
};

/**
 * Convert string to a boolean value, with a default.
 *
 * The return value is 0 in case of success or -1 when the string cannot be
 * converted correctly. In such case errno is set to indicate the problem and
 * the value is not written back to the caller-supplied pointer.
 *
 * If the text cannot be recognized, the default value is used.
 **/
static int parse_bool(const char *text, bool *value, bool default_value)
{
	if (value == NULL) {
		errno = EFAULT;
		return -1;
	}
	if (text == NULL) {
		*value = default_value;
		return 0;
	}
	for (size_t i = 0; i < sizeof sc_bool_names / sizeof *sc_bool_names;
	     ++i) {
		if (strcmp(text, sc_bool_names[i].text) == 0) {
			*value = sc_bool_names[i].value;
			return 0;
		}
	}
	errno = EINVAL;
	return -1;
}

/**
 * Get an environment variable and convert it to a boolean.
 *
 * Supported values are those of parse_bool(), namely "yes", "no" as well as "1"
 * and "0". All other values are treated as false and a diagnostic message is
 * printed to stderr. If the environment variable is unset, set value to the
 * default_value as if the environment variable was set to default_value.
 **/
static bool getenv_bool(const char *name, bool default_value)
{
	const char *str_value = getenv(name);
	bool value = default_value;
	if (parse_bool(str_value, &value, default_value) < 0) {
		if (errno == EINVAL) {
			fprintf(stderr,
				"WARNING: unrecognized value of environment variable %s (expected yes/no or 1/0)\n",
				name);
			return false;
		} else {
			die("cannot convert value of environment variable %s to a boolean", name);
		}
	}
	return value;
}

bool sc_is_debug_enabled(void)
{
	return getenv_bool("SNAP_CONFINE_DEBUG", false)
	    || getenv_bool("SNAPD_DEBUG", false);
}

bool sc_is_reexec_enabled(void)
{
	return getenv_bool("SNAP_REEXEC", true);
}

void debug(const char *msg, ...)
{
	if (sc_is_debug_enabled()) {
		va_list va;
		va_start(va, msg);
		fprintf(stderr, "DEBUG: ");
		vfprintf(stderr, msg, va);
		fprintf(stderr, "\n");
		va_end(va);
	}
}

void write_string_to_file(const char *filepath, const char *buf)
{
	debug("write_string_to_file %s %s", filepath, buf);
	FILE *f = fopen(filepath, "w");
	if (f == NULL)
		die("fopen %s failed", filepath);
	if (fwrite(buf, strlen(buf), 1, f) != 1)
		die("fwrite failed");
	if (fflush(f) != 0)
		die("fflush failed");
	if (fclose(f) != 0)
		die("fclose failed");
}

static bool sc_is_custom_ownership(sc_ownership ownership)
{
	return ownership.uid != (uid_t) (-1) && ownership.gid != (gid_t) (-1);
}

sc_identity sc_set_effective_identity(sc_identity identity)
{
	debug("set_effective_identity uid:%d, gid:%d", identity.uid,
	      identity.gid);
	sc_identity old = {.uid = (uid_t) (-1),.gid = (gid_t) (-1) };

	if (identity.gid != (gid_t) (-1)) {
		old.gid = getegid();
		if (setegid(identity.gid) < 0) {
			die("cannot set effective group to %d", identity.gid);
		}
	}
	if (identity.uid != (uid_t) (-1)) {
		old.uid = geteuid();
		if (setegid(identity.uid) < 0) {
			die("cannot set effective user to %d", identity.uid);
		}
	}
	return old;
}

int sc_nonfatal_mkpath(const char *const path, mode_t mode,
		       sc_ownership ownership)
{
	debug("sc_nonfatal_mkpath %s %#04o ownership %d/%d",
	      path, mode, ownership.uid, ownership.gid);

	int retval = -1;

	// If asked to create an empty path, return immediately.
	if (strlen(path) == 0) {
		retval = 0;
		goto out;
	}
	// We're going to use strtok_r, which needs to modify the path, so we'll
	// make a copy of it.
	char *path_copy SC_CLEANUP(sc_cleanup_string) = NULL;
	path_copy = strdup(path);
	if (path_copy == NULL) {
		goto out;
	}
	// Open flags to use while we walk the user data path:
	// - Don't follow symlinks
	// - Don't allow child access to file descriptor
	// - Only open a directory (fail otherwise)
	const int open_flags = O_NOFOLLOW | O_CLOEXEC | O_DIRECTORY;

	// We're going to create each path segment via openat/mkdirat calls instead
	// of mkdir calls, to avoid following symlinks and placing the user data
	// directory somewhere we never intended for it to go. The first step is to
	// get an initial file descriptor.
	int fd SC_CLEANUP(sc_cleanup_close) = AT_FDCWD;
	if (path_copy[0] == '/') {
		fd = open("/", open_flags);
		if (fd < 0) {
			goto out;
		}
	}
	// strtok_r needs a pointer to keep track of where it is in the string.
	char *path_walker = NULL;

	// Initialize tokenizer and obtain first path segment.
	char *path_segment = strtok_r(path_copy, "/", &path_walker);
	while (path_segment) {
		// Try to create the directory.  It's okay if it already existed, but
		// return with error on any other error. Reset errno before attempting
		// this as it may stay stale (errno is not reset if mkdirat(2) returns
		// successfully).
		errno = 0;
		if (mkdirat(fd, path_segment, 0700) < 0 && errno != EEXIST) {
			goto out;
		}
		// Open the parent directory we just made (and close the previous one
		// (but not the special value AT_FDCWD) so we can continue down the
		// path.
		int previous_fd = fd;
		fd = openat(fd, path_segment, open_flags);
		if (previous_fd != AT_FDCWD && close(previous_fd) != 0) {
			goto out;
		}
		if (fd < 0) {
			goto out;
		}
		if (sc_is_custom_ownership(ownership)) {
			if (fchown(fd, ownership.uid, ownership.gid) < 0) {
				die("cannot chown %s to %d:%d", path_segment,
				    ownership.uid, ownership.gid);
			}
		}
		struct stat file_info;
		if (fstat(fd, &file_info) < 0) {
			die("cannot fstat %s", path_segment);
		}
		if ((file_info.st_mode & 07777) != mode) {
			if (fchmod(fd, mode) < 0) {
				die("cannot chmod %s to %#4o", path_segment,
				    mode);
			}
		}
		// Obtain the next path segment.
		path_segment = strtok_r(NULL, "/", &path_walker);
	}
	retval = 0;

 out:
	return retval;
}

void sc_mkdir(const char *dir, mode_t mode, sc_ownership ownership)
{
	debug("sc_mkdir %s %#04o ownership %d/%d", dir, mode,
	      ownership.uid, ownership.gid);

	/* Create the directory with permissions 0700, chown then chmod to final to
	 * avoid races and capability denials. */
	if (mkdir(dir, 0700) < 0) {
		/* Allow the directory to exist without shenanigans */
		if (errno != EEXIST) {
			die("cannot create directory %s", dir);
		}
	}
	// TODO: remove O_RDONLY
	int dir_fd = open(dir, O_RDONLY | O_DIRECTORY | O_CLOEXEC | O_NOFOLLOW);
	if (dir_fd < 0) {
		die("cannot open directory %s", dir);
	}
	if (sc_is_custom_ownership(ownership)) {
		if (fchown(dir_fd, ownership.uid, ownership.gid) < 0) {
			die("cannot chown %s to %d:%d", dir, ownership.uid,
			    ownership.gid);
		}
	}
	struct stat file_info;
	if (fstat(dir_fd, &file_info) < 0) {
		die("cannot fstat %s", dir);
	}
	if ((file_info.st_mode & 07777) != mode) {
		if (fchmod(dir_fd, mode) < 0) {
			die("cannot chmod %s to %#4o", dir, mode);
		}
	}
}

void sc_mksubdir(const char *parent, const char *subdir, mode_t mode,
		 sc_ownership ownership)
{
	debug("sc_mksubdir %s/%s %#04o ownership %d/%d", parent,
	      subdir, mode, ownership.uid, ownership.gid);

	int parent_fd =
	    open(parent, O_PATH | O_DIRECTORY | O_CLOEXEC | O_NOFOLLOW);
	if (parent_fd < 0) {
		die("cannot open path of directory %s", parent);
	}
	/* Create the directory with permissions 0700, chown then chmod to final to
	 * avoid races and capability denials. */
	if (mkdirat(parent_fd, subdir, 0700) < 0) {
		/* Allow the directory to exist without shenanigans */
		if (errno != EEXIST) {
			die("cannot create directory %s/%s", parent, subdir);
		}
	}
	// TODO: remove O_RDONLY
	int subdir_fd = openat(parent_fd, subdir,
			       O_RDONLY | O_DIRECTORY | O_CLOEXEC | O_NOFOLLOW);
	if (subdir_fd < 0) {
		die("cannot open directory %s/%s", parent, subdir);
	}
	if (sc_is_custom_ownership(ownership)) {
		if (fchown(subdir_fd, ownership.uid, ownership.gid) < 0) {
			die("cannot chown %s/%s to %d:%d", parent, subdir,
			    ownership.uid, ownership.gid);
		}
	}
	struct stat file_info;
	if (fstat(subdir_fd, &file_info) < 0) {
		die("cannot fstat %s/%s", parent, subdir);
	}
	if ((file_info.st_mode & 07777) != mode) {
		if (fchmod(subdir_fd, mode) < 0) {
			die("cannot chmod %s/%s to %#4o", parent, subdir, mode);
		}
	}
}
