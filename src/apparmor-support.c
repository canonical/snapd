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

#include <string.h>
#ifdef HAVE_APPARMOR
#include <sys/apparmor.h>
#endif				// ifdef HAVE_APPARMOR

#include "cleanup-funcs.h"
#include "utils.h"

// NOTE: Those constants map exactly what apparmor is returning and cannot be
// changed without breaking apparmor functionality.
#define SC_AA_ENFORCE_STR "enforce"
#define SC_AA_COMPLAIN_STR "complain"

void sc_init_apparmor_support(struct sc_apparmor *apparmor)
{
#ifdef HAVE_APPARMOR
	char *label __attribute__ ((cleanup(sc_cleanup_string))) = NULL;
	char *mode = NULL;	// mode cannot be free'd
	if (aa_getcon(&label, &mode) < 0) {
		die("cannot query current apparmor profile");
	}
	// Look at label, if it is non empty then we are confined. 
	if (label != NULL && strcmp(label, "") != 0) {
		apparmor->is_confined = true;
	} else {
		apparmor->is_confined = false;
	}
	// Look at mode, it must be one of the well known strings.
	if (mode != NULL && strcmp(mode, SC_AA_COMPLAIN_STR) == 0) {
		apparmor->mode = SC_AA_COMPLAIN;
	} else if (mode != NULL && strcmp(mode, SC_AA_ENFORCE_STR) == 0) {
		apparmor->mode = SC_AA_ENFORCE;
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
	if (apparmor->is_confined) {
		debug("changing apparmor hat to %s", subprofile);
		if (aa_change_hat(subprofile, magic_token) < 0) {
			die("cannot change apparmor hat");
		}
	}
#endif				// ifdef HAVE_APPARMOR
}
