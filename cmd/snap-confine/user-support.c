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
#include <stdio.h>
#include <string.h>
#include <sys/stat.h>

#include "utils.h"

void setup_user_data()
{
	const char *user_data = getenv("SNAP_USER_DATA");

	if (user_data == NULL)
		return;

	// Only support absolute paths.
	if (user_data[0] != '/') {
		die("user data directory must be an absolute path");
	}

	debug("creating user data directory: %s", user_data);
	if (sc_nonfatal_mkpath(user_data, 0755) < 0) {
		die("cannot create user data directory: %s", user_data);
	};
}

void setup_user_xdg_runtime_dir()
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

void setup_snap_context_var()
{
	const char *snap_name = getenv("SNAP_NAME");
	char context_path[1000];
	// context is a 32 bytes, base64-encoding makes it 44.
	char context_val[45];

	if (snap_name == NULL) {
		die("SNAP_NAME is not set");
	}
	// this should never happen.
	if (strlen(CONTEXTS_DIR) + strlen(snap_name) + 2 > 1000) {
		die("Cannot set SNAP_CONTEXT, file path too long");
	}

	strcpy(context_path, CONTEXTS_DIR "/");
	strcat(context_path, snap_name);

	FILE *f = fopen(context_path, "rt");
	if (f == NULL) {
		error
		    ("Cannot open context file %s, SNAP_CONTEXT will not be set",
		     context_path);
		return;
	}
	if (fgets(context_val, 45, f) == NULL) {
		error("Failed to read context file %s", context_path);
	} else {
		setenv("SNAP_CONTEXT", context_val, 1);
	}
	fclose(f);
}
