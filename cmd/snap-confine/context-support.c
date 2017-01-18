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

#include "cleanup-funcs.h"
#include "config.h"
#include "context-support.h"
#include "utils.h"

#include <string.h>
#include <unistd.h>
#include <errno.h>
#include <sys/types.h>
#include <sys/stat.h>
#include <fcntl.h>

#define CONTEXTS_DIR "/var/lib/snapd/contexts"

char *sc_nonfatal_context_get_from_snapd(const char *snap_name,
					 struct sc_error **errorp)
{
	char context_path[PATH_MAX];
	char *context_val = NULL;
	struct sc_error *err = NULL;

	if (snap_name == NULL) {
		die("SNAP_NAME is not set");
	}

	int fd __attribute__ ((cleanup(sc_cleanup_close))) = -1;
	must_snprintf(context_path, sizeof(context_path), "%s/snap.%s",
		      CONTEXTS_DIR, snap_name);
	fd = open(context_path, O_RDONLY | O_NOFOLLOW | O_CLOEXEC);
	if (fd < 0) {
		err =
		    sc_error_init(SC_CONTEXT_DOMAIN, 0,
				  "cannot open context file %s, SNAP_CONTEXT will not be set: %s",
				  context_path, strerror(errno));
		goto out;
	}
	// context is a 32 bytes, base64-encoding makes it 44.
	context_val = calloc(1, 45);
	if (context_val == NULL) {
		die("failed to allocate memory for snap context");
	}
	if (read(fd, context_val, 44) < 0) {
		free(context_val);
		context_val = NULL;
		err =
		    sc_error_init(SC_CONTEXT_DOMAIN, 0,
				  "failed to read context file %s: %s",
				  context_path, strerror(errno));
		goto out;
	}

 out:
	sc_error_forward(errorp, err);
	return context_val;
}

void sc_context_set_environment(const char *context)
{
	if (context != NULL) {
		// Don't overwrite an existing value as it may be already set if running a hook.
		setenv("SNAP_CONTEXT", context, 0);
	}
}
