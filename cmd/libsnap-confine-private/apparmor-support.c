/*
 * Copyright (C) 2016 Canonical Ltd
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

#include "apparmor-support.h"
#include "string-utils.h"
#include "utils.h"

#include <string.h>
#include <errno.h>
#ifdef HAVE_APPARMOR
#include <sys/apparmor.h>
#endif				// ifdef HAVE_APPARMOR

#include "../libsnap-confine-private/cleanup-funcs.h"
#include "../libsnap-confine-private/utils.h"

// NOTE: Those constants map exactly what apparmor is returning and cannot be
// changed without breaking apparmor functionality.
#define SC_AA_ENFORCE_STR "enforce"
#define SC_AA_COMPLAIN_STR "complain"
#define SC_AA_MIXED_STR "mixed"
#define SC_AA_KILL_STR "kill"
#define SC_AA_UNCONFINED_STR "unconfined"

void sc_init_apparmor_support(struct sc_apparmor *apparmor)
{
#ifdef HAVE_APPARMOR
	// Use aa_is_enabled() to see if apparmor is available in the kernel and
	// enabled at boot time. If it isn't log a diagnostic message and assume
	// we're not confined.
	if (aa_is_enabled() == 0) {
		switch (errno) {
		case ENOSYS:
			debug
			    ("apparmor extensions to the system are not available");
			break;
		case EBUSY:
			debug
			    ("apparmor is enabled but the interface is private");
			break;
		case ECANCELED:
			debug
			    ("apparmor is available on the system but has been disabled at boot");
			break;
		case EPERM:
			// NOTE: fall-through
		case EACCES:
			// Since snap-confine is setuid root this should never happen so
			// likely someone is trying to manipulate our execution environment
			// - fail hard.
			die("insufficient permissions to determine if apparmor is enabled");
			break;
		case ENOENT:
			die("apparmor is enabled but the interface is not available");
			break;
		case ENOMEM:
			die("insufficient memory to determine if apparmor is available");
			break;
		default:
			// this shouldn't happen under normal usage so it
			// is possible someone is trying to manipulate our
			// execution environment - fail hard
			die("aa_is_enabled() failed unexpectedly (%s)",
			    strerror(errno));
			break;
		}
		apparmor->is_confined = false;
		apparmor->mode = SC_AA_NOT_APPLICABLE;
		return;
	}
	// Use aa_getcon() to check the label of the current process and
	// confinement type. Note that the returned label must be released with
	// free() but the mode is a constant string that must not be freed.
	char *label SC_CLEANUP(sc_cleanup_string) = NULL;
	char *mode = NULL;
	if (aa_getcon(&label, &mode) < 0) {
		die("cannot query current apparmor profile");
	}
	debug("apparmor label on snap-confine is: %s", label);
	debug("apparmor mode is: %s", mode);
	// expect to be confined by a profile with the name of a valid
	// snap-confine binary since if not we may be executed under a
	// profile with more permissions than expected
	bool confined_mode = sc_streq(mode, SC_AA_ENFORCE_STR)
	    || sc_streq(mode, SC_AA_KILL_STR);
	if (label != NULL && confined_mode && sc_is_expected_path(label)) {
		apparmor->is_confined = true;
	} else {
		apparmor->is_confined = false;
	}
	// There are several possible results for the confinement type (mode) that
	// are checked for below.
	if (mode == NULL) {
		apparmor->mode = SC_AA_NOT_APPLICABLE;
	} else if (sc_streq(mode, SC_AA_COMPLAIN_STR)) {
		apparmor->mode = SC_AA_COMPLAIN;
	} else if (sc_streq(mode, SC_AA_ENFORCE_STR)) {
		apparmor->mode = SC_AA_ENFORCE;
	} else if (sc_streq(mode, SC_AA_MIXED_STR)) {
		apparmor->mode = SC_AA_MIXED;
	} else if (sc_streq(mode, SC_AA_KILL_STR)) {
		apparmor->mode = SC_AA_KILL;
	} else {
		apparmor->mode = SC_AA_INVALID;
	}
#else
	apparmor->mode = SC_AA_NOT_APPLICABLE;
	apparmor->is_confined = false;
#endif				// ifdef HAVE_APPARMOR
}

void
sc_maybe_aa_change_onexec(struct sc_apparmor *apparmor, const char *profile)
{
#ifdef HAVE_APPARMOR
	if (apparmor->mode == SC_AA_NOT_APPLICABLE) {
		return;
	}
	debug("requesting changing of apparmor profile on next exec to %s",
	      profile);
	if (aa_change_onexec(profile) < 0) {
		/* Save errno because secure_getenv() can overwrite it */
		int aa_change_onexec_errno = errno;
		if (secure_getenv("SNAPPY_LAUNCHER_INSIDE_TESTS") == NULL) {
			errno = aa_change_onexec_errno;
			if (errno == ENOENT) {
				fprintf(stderr, "missing profile %s.\n"
					"Please make sure that the snapd.apparmor service is enabled and started\n",
					profile);
				exit(1);
			} else {
				die("cannot change profile for the next exec call");
			}
		}
	}
#endif				// ifdef HAVE_APPARMOR
}

void
sc_maybe_aa_change_hat(struct sc_apparmor *apparmor,
		       const char *subprofile, unsigned long magic_token)
{
#ifdef HAVE_APPARMOR
	if (apparmor->mode == SC_AA_NOT_APPLICABLE) {
		return;
	}
	if (apparmor->is_confined) {
		debug("changing apparmor hat to %s", subprofile);
		if (aa_change_hat(subprofile, magic_token) < 0) {
			die("cannot change apparmor hat");
		}
	}
#endif				// ifdef HAVE_APPARMOR
}
