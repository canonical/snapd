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
#include <errno.h>
#include <fcntl.h>
#include <regex.h>
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
bool getenv_bool(const char *name, bool default_value)
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

sc_identity sc_set_effective_identity(sc_identity identity)
{
	debug("set_effective_identity uid:%d (change: %s), gid:%d (change: %s)",
	      identity.uid, identity.change_uid ? "yes" : "no",
	      identity.gid, identity.change_gid ? "yes" : "no");
	/* We are being careful not to return a value instructing us to change GID
	 * or UID by accident. */
	sc_identity old = {
		.change_gid = 0,
		.change_uid = 0,
	};

	if (identity.change_gid) {
		old.gid = getegid();
		old.change_gid = 1;
		if (setegid(identity.gid) < 0) {
			die("cannot set effective group to %d", identity.gid);
		}
		if (getegid() != identity.gid) {
			die("effective group change from %d to %d has failed",
			    old.gid, identity.gid);
		}
	}
	if (identity.change_uid) {
		old.uid = geteuid();
		old.change_uid = 1;
		if (seteuid(identity.uid) < 0) {
			die("cannot set effective user to %d", identity.uid);
		}
		if (geteuid() != identity.uid) {
			die("effective user change from %d to %d has failed",
			    old.uid, identity.uid);
		}
	}
	return old;
}

int sc_nonfatal_mkpath(const char *const path, mode_t mode,
                       uid_t uid, uid_t gid)
{
	// If asked to create an empty path, return immediately.
	if (strlen(path) == 0) {
		return 0;
	}
	// We're going to use strtok_r, which needs to modify the path, so we'll
	// make a copy of it.
	char *path_copy SC_CLEANUP(sc_cleanup_string) = NULL;
	path_copy = strdup(path);
	if (path_copy == NULL) {
		return -1;
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
			return -1;
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
		if (mkdirat(fd, path_segment, 0000) < 0) {
			if (errno != EEXIST) {
				return -1;
			}
		} else {
			// new directory: set the right permissions and mode
			if (fchownat(fd, path_segment, uid, gid, AT_SYMLINK_NOFOLLOW) < 0 ||
			    fchmodat(fd, path_segment, mode, 0) < 0) {
				return -1;
			}
		}
		// Open the parent directory we just made (and close the previous one
		// (but not the special value AT_FDCWD) so we can continue down the
		// path.
		int previous_fd = fd;
		fd = openat(fd, path_segment, open_flags);
		if (previous_fd != AT_FDCWD && close(previous_fd) != 0) {
			return -1;
		}
		if (fd < 0) {
			return -1;
		}
		// Obtain the next path segment.
		path_segment = strtok_r(NULL, "/", &path_walker);
	}
	return 0;
}

bool sc_is_expected_path(const char *path)
{
	const char *expected_path_re =
	    "^(/snap/(snapd|core)/x?[0-9]+/usr/lib|/usr/lib(exec)?)/snapd/snap-confine$";
	regex_t re;
	if (regcomp(&re, expected_path_re, REG_EXTENDED | REG_NOSUB) != 0)
		die("can not compile regex %s", expected_path_re);
	int status = regexec(&re, path, 0, NULL, 0);
	regfree(&re);
	return status == 0;
}

bool sc_wait_for_file(const char *path, size_t timeout_sec)
{
	for (size_t i = 0; i < timeout_sec; ++i) {
		if (access(path, F_OK) == 0) {
			return true;
		}
		sleep(1);
	}
	return false;
}
