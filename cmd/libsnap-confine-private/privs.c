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

#include "privs.h"

#define _GNU_SOURCE

#include <unistd.h>

#include <grp.h>
#include <stdbool.h>
#include <sys/capability.h>
#include <sys/types.h>
#include <unistd.h>

#include "utils.h"

static bool sc_has_capability(const char *cap_name)
{
	// Lookup capability with the given name.
	cap_value_t cap;
	if (cap_from_name(cap_name, &cap) < 0) {
		die("cannot resolve capability name %s", cap_name);
	}
	// Get the capability state of the current process.
	cap_t caps;
	if ((caps = cap_get_proc()) == NULL) {
		die("cannot obtain capability state (cap_get_proc)");
	}
	// Read the effective value of the flag we're dealing with
	cap_flag_value_t cap_flags_value;
	if (cap_get_flag(caps, cap, CAP_EFFECTIVE, &cap_flags_value) < 0) {
		cap_free(caps);	// don't bother checking, we die anyway.
		die("cannot obtain value of capability flag (cap_get_flag)");
	}
	// Free the representation of the capability state of the current process.
	if (cap_free(caps) < 0) {
		die("cannot free capability flag (cap_free)");
	}
	// Check if the effective bit of the capability is set.
	return cap_flags_value == CAP_SET;
}

void sc_privs_drop(void)
{
	gid_t gid = getgid();
	uid_t uid = getuid();

	// Drop extra group membership if we can.
	if (sc_has_capability("cap_setgid")) {
		gid_t gid_list[1] = { gid };
		if (setgroups(1, gid_list) < 0) {
			die("cannot set supplementary group identifiers");
		}
	}
	// Switch to real group ID
	if (setgid(getgid()) < 0) {
		die("cannot set group identifier to %d", gid);
	}
	// Switch to real user ID
	if (setuid(getuid()) < 0) {
		die("cannot set user identifier to %d", uid);
	}
}
