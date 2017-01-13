/*
 * Copyright (C) 2017 Canonical Ltd
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
#include "context-support.h"
#include "utils.h"
#include <stdio.h>

#include <string.h>

#define CONTEXTS_DIR "/var/lib/snapd/contexts"

char *read_snap_context(const char *snap_name)
{
	char context_path[1000];
	char *context_val = NULL;

	if (snap_name == NULL) {
		die("SNAP_NAME is not set");
	}

	must_snprintf(context_path, 1000, "%s/snap.%s", CONTEXTS_DIR,
		      snap_name);

	FILE *f = fopen(context_path, "rt");
	if (f == NULL) {
		error
		    ("Cannot open context file %s, SNAP_CONTEXT will not be set",
		     context_path);
		return NULL;
	}
	// context is a 32 bytes, base64-encoding makes it 44.
	context_val = malloc(45);
	if (context_val == NULL) {
		die("Failed to allocate memory for snap context");
	}
	if (fgets(context_val, 45, f) == NULL) {
		free(context_val);
		error("Failed to read context file %s", context_path);
	}
	fclose(f);
	return context_val;
}

void set_snap_context_env(const char *context)
{
	if (context != NULL) {
		setenv("SNAP_CONTEXT", context, 1);
	}
}
