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

#include <string.h>

#define CONTEXTS_DIR "/var/lib/snapd/contexts"

void setup_snap_context_var()
{
	const char *snap_name = getenv("SNAP_NAME");
	char context_path[1000];
	// context is a 32 bytes, base64-encoding makes it 44.
	char context_val[45];

	if (snap_name == NULL) {
		die("SNAP_NAME is not set");
	}

    must_snprintf(context_path, "%s/snap.%s", CONTEXTS_DIR, snap_name)

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
