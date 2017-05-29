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

#include "cookie-support.h"

#include "../libsnap-confine-private/cleanup-funcs.h"
#include "../libsnap-confine-private/string-utils.h"
#include "../libsnap-confine-private/utils.h"

#include "config.h"

#include <fcntl.h>
#include <string.h>
#include <sys/types.h>
#include <sys/stat.h>
#include <unistd.h>

#define SC_COOKIE_DIR "/var/lib/snapd/cookie"

/**
 * Effective value of CONTEXT_DIR
 **/
static const char *sc_cookie_dir = SC_COOKIE_DIR;

char *sc_cookie_get_from_snapd(const char *snap_name, struct sc_error **errorp)
{
	char context_path[PATH_MAX];
	struct sc_error *err = NULL;

	sc_must_snprintf(context_path, sizeof(context_path), "%s/snap.%s",
			 sc_cookie_dir, snap_name);
	int fd __attribute__ ((cleanup(sc_cleanup_close))) = -1;
	fd = open(context_path, O_RDONLY | O_NOFOLLOW | O_CLOEXEC);
	if (fd < 0) {
		err =
		    sc_error_init(SC_ERRNO_DOMAIN, 0,
				  "cannot open cookie file %s, SNAP_COOKIE will not be set",
				  context_path);
    sc_error_forward(errorp, err);
    return NULL;
	}

	char context_val[255];
	if (context_val == NULL) {
		die("failed to allocate memory for snap context");
	}
  int n = read(fd, context_val, sizeof(context_val) - 1);
  if (n < 0) {
		err =
		    sc_error_init(SC_ERRNO_DOMAIN, 0,
				  "failed to read cookie file %s",
				  context_path);
    sc_error_forward(errorp, err);
    return NULL;
	}
  context_val[n] = 0;
	return strdup(context_val);
}
