/*
 * Copyright (C) 2017-2018 Canonical Ltd
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

#include <errno.h>
#include <fcntl.h>
#include <signal.h>
#include <stdarg.h>
#include <sys/file.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <unistd.h>

#include "../libsnap-confine-private/cleanup-funcs.h"
#include "../libsnap-confine-private/string-utils.h"
#include "../libsnap-confine-private/utils.h"

// SANITY_TIMEOUT is the timeout in seconds that is used when
// "sc_enable_sanity_timeout()" is called
static const int SANITY_TIMEOUT = 30;

/**
 * Flag indicating that a sanity timeout has expired.
 **/
static volatile sig_atomic_t sanity_timeout_expired = 0;

/**
 * Signal handler for SIGALRM that sets sanity_timeout_expired flag to 1.
 **/
static void sc_SIGALRM_handler(int signum)
{
	sanity_timeout_expired = 1;
}

void sc_enable_sanity_timeout(void)
{
	sanity_timeout_expired = 0;
	struct sigaction act = {.sa_handler = sc_SIGALRM_handler };
	if (sigemptyset(&act.sa_mask) < 0) {
		die("cannot initialize POSIX signal set");
	}
	// NOTE: we are using sigaction so that we can explicitly control signal
	// flags and *not* pass the SA_RESTART flag. The intent is so that any
	// system call we may be sleeping on to gets interrupted.
	act.sa_flags = 0;
	if (sigaction(SIGALRM, &act, NULL) < 0) {
		die("cannot install signal handler for SIGALRM");
	}
	alarm(SANITY_TIMEOUT);
	debug("sanity timeout initialized and set for %i seconds\n",
	      SANITY_TIMEOUT);
}

void sc_disable_sanity_timeout(void)
{
	if (sanity_timeout_expired) {
		die("sanity timeout expired");
	}
	alarm(0);
	struct sigaction act = {.sa_handler = SIG_DFL };
	if (sigemptyset(&act.sa_mask) < 0) {
		die("cannot initialize POSIX signal set");
	}
	if (sigaction(SIGALRM, &act, NULL) < 0) {
		die("cannot uninstall signal handler for SIGALRM");
	}
	debug("sanity timeout reset and disabled");
}

#define SC_LOCK_DIR "/run/snapd/lock"

static const char *sc_lock_dir = SC_LOCK_DIR;

static int get_lock_directory(void)
{
	// Create (if required) and open the lock directory.
	debug("creating lock directory %s (if missing)", sc_lock_dir);
	if (sc_nonfatal_mkpath(sc_lock_dir, 0755, 0, 0) < 0) {
		die("cannot create lock directory %s", sc_lock_dir);
	}
	debug("opening lock directory %s", sc_lock_dir);
	int dir_fd =
	    open(sc_lock_dir, O_DIRECTORY | O_PATH | O_CLOEXEC | O_NOFOLLOW);
	if (dir_fd < 0) {
		die("cannot open lock directory");
	}
	return dir_fd;
}

static void get_lock_name(char *lock_fname, size_t size, const char *scope,
			  uid_t uid)
{
	if (uid == 0) {
		// The root user doesn't have a per-user mount namespace.
		// Doing so would be confusing for services which use $SNAP_DATA
		// as home, and not in $SNAP_USER_DATA.
		sc_must_snprintf(lock_fname, size, "%s.lock", scope ? : "");
	} else {
		sc_must_snprintf(lock_fname, size, "%s.%d.lock",
				 scope ? : "", uid);
	}
}

static int open_lock(const char *scope, uid_t uid)
{
	int dir_fd SC_CLEANUP(sc_cleanup_close) = -1;
	char lock_fname[PATH_MAX] = { 0 };
	int lock_fd;

	dir_fd = get_lock_directory();
	get_lock_name(lock_fname, sizeof lock_fname, scope, uid);

	// Open the lock file and acquire an exclusive lock.
	debug("opening lock file: %s/%s", sc_lock_dir, lock_fname);
	lock_fd = openat(dir_fd, lock_fname,
			 O_CREAT | O_RDWR | O_CLOEXEC | O_NOFOLLOW, 0600);
	if (lock_fd < 0) {
		die("cannot open lock file: %s/%s", sc_lock_dir, lock_fname);
	}
	if (fchown(lock_fd, 0, 0) < 0) {
		die("cannot chown lock file: %s/%s", sc_lock_dir, lock_fname);
	}
	return lock_fd;
}

static int sc_lock_generic(const char *scope, uid_t uid)
{
	int lock_fd = open_lock(scope, uid);
	sc_enable_sanity_timeout();
	debug("acquiring exclusive lock (scope %s, uid %d)",
	      scope ? : "(global)", uid);
	if (flock(lock_fd, LOCK_EX) < 0) {
		sc_disable_sanity_timeout();
		close(lock_fd);
		die("cannot acquire exclusive lock (scope %s, uid %d)",
		    scope ? : "(global)", uid);
	} else {
		sc_disable_sanity_timeout();
	}
	return lock_fd;
}

int sc_lock_global(void)
{
	return sc_lock_generic(NULL, 0);
}

int sc_lock_snap(const char *snap_name)
{
	return sc_lock_generic(snap_name, 0);
}

void sc_verify_snap_lock(const char *snap_name)
{
	int lock_fd, retval;

	lock_fd = open_lock(snap_name, 0);
	debug("trying to verify whether exclusive lock over snap %s is held",
	      snap_name);
	retval = flock(lock_fd, LOCK_EX | LOCK_NB);
	if (retval == 0) {
		/* We managed to grab the lock, the lock was not held! */
		flock(lock_fd, LOCK_UN);
		close(lock_fd);
		errno = 0;
		die("unexpectedly managed to acquire exclusive lock over snap %s", snap_name);
	}
	if (retval < 0 && errno != EWOULDBLOCK) {
		die("cannot verify exclusive lock over snap %s", snap_name);
	}
	/* We tried but failed to grab the lock because the file is already locked.
	 * Good, this is what we expected. */
}

int sc_lock_snap_user(const char *snap_name, uid_t uid)
{
	return sc_lock_generic(snap_name, uid);
}

void sc_unlock(int lock_fd)
{
	// Release the lock and finish.
	debug("releasing lock %d", lock_fd);
	if (flock(lock_fd, LOCK_UN) < 0) {
		die("cannot release lock %d", lock_fd);
	}
	close(lock_fd);
}
