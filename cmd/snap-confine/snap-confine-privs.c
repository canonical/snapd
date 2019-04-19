/*
 * Copyright (C) 2019 Canonical Ltd
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

#include "snap-confine-privs.h"

#include <unistd.h>

#include "../libsnap-confine-private/utils.h"

void sc_main_change_to_real_gid(gid_t effective_gid, gid_t real_gid)
{
	if (effective_gid == 0 && real_gid != 0) {
		if (setegid(real_gid) != 0) {
			die("cannot set effective group id to %d", real_gid);
		}
	}
}

void sc_main_temporarily_drop_to_user(uid_t real_uid, gid_t real_gid)
{
		if (setegid(real_gid) != 0)
			die("setegid failed");
		if (seteuid(real_uid) != 0)
			die("seteuid failed");

		if (real_gid != 0 && geteuid() == 0)
			die("dropping privs did not work");
		if (real_uid != 0 && getegid() == 0)
			die("dropping privs did not work");
}

void sc_main_temporarily_raise_to_root_gid(gid_t saved_gid)
{
	if (getegid() != 0 && saved_gid == 0) {
		// Temporarily raise egid so we can chown the freezer cgroup under LXD.
		if (setegid(0) != 0) {
			die("cannot set effective group id to root");
		}
	}
}

void sc_main_temporarily_drop_from_root_gid(gid_t real_gid)
{
	if (geteuid() == 0 && real_gid != 0) {
		if (setegid(real_gid) != 0) {
			die("cannot set effective group id to %d", real_gid);
		}
	}
}

void sc_udev_raise_to_root_uid(void)
{
		uid_t real_uid, effective_uid, saved_uid;
		if (getresuid(&real_uid, &effective_uid, &saved_uid) != 0)
			die("cannot get real, effective and saved user IDs");
		// can't update the cgroup unless the real_uid is 0, euid as
		// 0 is not enough
		if (real_uid != 0 && effective_uid == 0)
			if (setuid(0) != 0)
				die("cannot set user ID to zero");
}

void sc_seccomp_temporarily_raise_to_root_uid(uid_t saved_uid, uid_t effective_uid)
{
	if (effective_uid != 0 && saved_uid == 0) {
		if (seteuid(0) != 0) {
			die("seteuid failed");
		}
		if (geteuid() != 0) {
			die("raising privs before seccomp_load did not work");
		}
	}
}

void sc_seccomp_temporarily_drop_from_root_uid(void)
{
	if (geteuid() == 0) {
		unsigned real_uid = getuid();
		if (seteuid(real_uid) != 0) {
			die("seteuid failed");
		}
		if (real_uid != 0 && geteuid() == 0) {
			die("dropping privs after seccomp_load did not work");
		}
	}
}

void sc_main_permanently_drop_to_user(uid_t real_uid, gid_t real_gid)
{
	if (geteuid() == 0) {
		// Note that we do not call setgroups() here because its ok
		// that the user keeps the groups he already belongs to
		if (setgid(real_gid) != 0)
			die("setgid failed");
		if (setuid(real_uid) != 0)
			die("setuid failed");

		if (real_gid != 0 && (getuid() == 0 || geteuid() == 0))
			die("permanently dropping privs did not work");
		if (real_uid != 0 && (getgid() == 0 || getegid() == 0))
			die("permanently dropping privs did not work");
	}

}
