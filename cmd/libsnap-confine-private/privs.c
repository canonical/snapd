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

#include <stdbool.h>
#include <sys/types.h>
#include <unistd.h>

#include "utils.h"

static uid_t real_uid;
static gid_t real_gid;

void sc_privs_init()
{
	real_uid = getuid();
	real_gid = getgid();
}

void sc_privs_lower_permanently()
{
	bool lowered = false;

	if (getegid() == 0) {
		// Note that we do not call setgroups() here because it is ok that the
		// user keeps the groups he already belongs to.
		if (setgid(real_gid) != 0) {
			die("cannot set group identifier to %d", real_gid);
		}
		if (real_gid != 0 && (getuid() == 0 || geteuid() == 0)) {
			die("cannot permanently lower permissions (gid still elevated)");
		}
		lowered = true;
	}

	if (geteuid() == 0) {
		if (setuid(real_uid) != 0) {
			die("cannot set user identifier to %d", real_uid);
		}
		if (real_uid != 0 && (getgid() == 0 || getegid() == 0)) {
			die("cannot permanently lower permissions (uid still elevated)");
		}
		lowered = true;
	}

	if (lowered) {
		debug("elevated permissions have been permanently lowered");
	}
}

void sc_privs_lower_temporarily()
{
	bool lowered = false;

	if (geteuid() == 0) {
		if (setegid(real_gid) != 0) {
			die("cannot set effective group identifier to %d",
			    real_gid);
		}
		if (real_gid != 0 && geteuid() == 0) {
			die("cannot temporarily lower permissions (gid still elevated)");
		}
		lowered = true;
	}

	if (getegid() == 0) {
		if (seteuid(real_uid) != 0) {
			die("cannot set effective user identifier to %d",
			    real_uid);
		}
		if (real_uid != 0 && getegid() == 0) {
			die("cannot temporarily lower permissions (uid still elevated)");
		}
		lowered = true;
	}

	if (lowered) {
		debug("elevated permission have been temporarily lowered");
	}
}

void sc_privs_raise()
{
	bool raised = false;

	if (real_gid != 0) {
		if (setegid(0) != 0) {
			die("cannot set effective group identifier to %d", 0);
		}
		raised = true;
	}

	if (real_uid != 0) {
		if (seteuid(0) != 0) {
			die("cannot set effective user identifier to %d", 0);
		}
		raised = true;
	}

	if (raised) {
		debug("permissions have been elevated");
	}
}
