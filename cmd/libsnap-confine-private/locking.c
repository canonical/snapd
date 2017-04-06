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

#ifdef HAVE_CONFIG_H
#include "config.h"
#endif

#include "locking.h"

#include <fcntl.h>
#include <stdarg.h>
#include <sys/file.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <unistd.h>

#include "../libsnap-confine-private/cleanup-funcs.h"
#include "../libsnap-confine-private/string-utils.h"
#include "../libsnap-confine-private/utils.h"

#define SC_LOCK_DIR "/run/snapd/lock"

static const char *sc_lock_dir = SC_LOCK_DIR;

void sc_call_while_locked(const char *scope, ...)
{
	// Create (if required) and open the lock directory.
	debug("creating lock directory %s (if missing)", sc_lock_dir);
	if (sc_nonfatal_mkpath(sc_lock_dir, 0755) < 0) {
		die("cannot create lock directory %s", sc_lock_dir);
	}
	debug("opening lock directory %s", sc_lock_dir);
	int dir_fd __attribute__ ((cleanup(sc_cleanup_close))) = -1;
	dir_fd =
	    open(sc_lock_dir, O_DIRECTORY | O_PATH | O_CLOEXEC | O_NOFOLLOW);
	if (dir_fd < 0) {
		die("cannot open lock directory");
	}
	// Construct the name of the lock file.
	char lock_fname[PATH_MAX];
	sc_must_snprintf(lock_fname, sizeof lock_fname, "%s/%s.lock",
			 sc_lock_dir, scope ? : "");

	// Open the lock file and acquire an exclusive lock.
	debug("opening lock file: %s", lock_fname);
	int lock_fd __attribute__ ((cleanup(sc_cleanup_close))) = -1;
	lock_fd = openat(dir_fd, lock_fname,
			 O_CREAT | O_RDWR | O_CLOEXEC | O_NOFOLLOW, 0600);
	if (lock_fd < 0) {
		die("cannot open lock file: %s", lock_fname);
	}
	debug("acquiring exclusive lock (scope %s)", scope ? : "(global)");
	if (flock(lock_fd, LOCK_EX) < 0) {
		die("cannot acquire exclusive lock (scope %s)",
		    scope ? : "(global)");
	}
	// Run all callbacks while holding the lock. 
	sc_locked_fn fn;
	va_list ap;
	va_start(ap, scope);
	while ((fn = va_arg(ap, sc_locked_fn)) != NULL) {
		fn(scope);
	}
	va_end(ap);

	// Release the lock and finish.
	debug("releasing lock (scope: %s)", scope ? : "(global)");
	if (flock(lock_fd, LOCK_UN) < 0) {
		die("cannot release lock (scope: %s)", scope ? : "(global)");
	}
}
