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

#include <errno.h>
#include <fcntl.h>
#include <string.h>
#include <sys/types.h>
#include <unistd.h>
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
#define SC_AA_UNCONFINED_STR "unconfined"

void sc_init_apparmor_support(struct sc_apparmor *apparmor)
{
#ifdef HAVE_APPARMOR
	// Use aa_is_enabled() to see if apparmor is available in the kernel and
	// enabled at boot time. If it isn't log a diagnostic message and assume
	// we're not confined.
	if (aa_is_enabled() != true) {
		switch (errno) {
		case ENOSYS:
			debug
			    ("apparmor extensions to the system are not available");
			break;
		case ECANCELED:
			debug
			    ("apparmor is available on the system but has been disabled at boot");
			break;
		case ENOENT:
			debug
			    ("apparmor is available but the interface but the interface is not available");
			break;
		case EPERM:
			// NOTE: fall-through
		case EACCES:
			debug
			    ("insufficient permissions to determine if apparmor is enabled");
			break;
		default:
			debug("apparmor is not enabled: %s", strerror(errno));
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
	// The label has a special value "unconfined" that is applied to all
	// processes without a dedicated profile. If that label is used then the
	// current process is not confined. All other labels imply confinement.
	if (label != NULL && strcmp(label, SC_AA_UNCONFINED_STR) == 0) {
		apparmor->is_confined = false;
	} else {
		apparmor->is_confined = true;
	}
	// There are several possible results for the confinement type (mode) that
	// are checked for below.
	if (mode != NULL && strcmp(mode, SC_AA_COMPLAIN_STR) == 0) {
		apparmor->mode = SC_AA_COMPLAIN;
	} else if (mode != NULL && strcmp(mode, SC_AA_ENFORCE_STR) == 0) {
		apparmor->mode = SC_AA_ENFORCE;
	} else if (mode != NULL && strcmp(mode, SC_AA_MIXED_STR) == 0) {
		apparmor->mode = SC_AA_MIXED;
	} else {
		apparmor->mode = SC_AA_INVALID;
	}

	// Check that apparmor is actually usable. In some
	// configurations of lxd, apparmor looks available when in
	// reality it isn't. Eg, this can happen when a container runs
	// unprivileged (eg, root in the container is non-root
	// outside) and also unconfined (where lxd doesn't set up an
	// apparmor policy namespace). We can therefore simply check
	// if /sys/kernel/security/apparmor/profiles is readable (like
	// aa-status does), and if it isn't, we know we can't manipulate
	// policy.
	int fd = open("/sys/kernel/security/apparmor/profiles", O_RDONLY);
	if (fd < 0) {
		if (errno == EACCES) {
			apparmor->mode = SC_AA_NOT_APPLICABLE;
		} else {
			die("cannot open /sys/kernel/security/apparmor/profiles");
		}
	}
	close(fd);
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
		if (secure_getenv("SNAPPY_LAUNCHER_INSIDE_TESTS") == NULL) {
			die("cannot change profile for the next exec call");
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
