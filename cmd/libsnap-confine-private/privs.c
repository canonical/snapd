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
		die("cannot obtain value of capability flag (cap_get_flag)");
	}
	// Free the representation of the capability state of the current process.
	if (cap_free(caps) < 0) {
		die("cannot free capability flag (cap_free)");
	}
	// Check if the effective bit of the capability is set.
	return cap_flags_value == CAP_SET;
}

void sc_privs_drop()
{
	// Get the real, effective and saved user identifiers
	uid_t ruid, euid, suid;
	if (getresuid(&ruid, &euid, &suid) < 0) {
		die("cannot get real, effective and saved user identifiers");
	}
	// Ditto for group identifiers 
	gid_t rgid, egid, sgid;
	if (getresgid(&rgid, &egid, &sgid) < 0) {
		die("cannot get real, effective and saved group identifiers");
	}
	if (euid == 0) {
		// Drop extra group membership if we can.
		if (sc_has_capability("cap_setgid")) {
			gid_t gid_list[1] = { rgid };
			if (setgroups(1, gid_list) < 0) {
				die("cannot set supplementary group identifiers");
			}
		}
		// Switch to real group ID
		if (setgid(rgid) < 0) {
			die("cannot set group identifier to %d", rgid);
		}
		// Switch to real user ID
		if (setuid(ruid) < 0) {
			die("cannot set user identifier to %d", ruid);
		}
		// Verify everything
		//
		// With the above, this should never happen but be paranoid to help
		// future-proof code changes. Specifically, if our real gid was not
		// root, but one of uid/euid still are root, die(). Same for if our
		// real uid was not root, but one of gid/egid are root, die().
		if (rgid != 0 && (getuid() == 0 || geteuid() == 0)) {
			die("cannot permanently drop permissions (uid still elevated)");
		}
		if (ruid != 0 && (getgid() == 0 || getegid() == 0)) {
			die("cannot permanently drop permissions (gid still elevated)");
		}
		// XXX Should we verify supplementary groups?
		debug("elevated permissions have been permanently dropped");
	}
}
