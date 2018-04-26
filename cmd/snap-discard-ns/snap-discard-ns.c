/*
 * Copyright (C) 2015 Canonical Ltd
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

#include <errno.h>
#include <limits.h>
#include <unistd.h>

#include "../libsnap-confine-private/locking.h"
#include "../libsnap-confine-private/string-utils.h"
#include "../libsnap-confine-private/utils.h"
#include "../snap-confine/ns-support.h"

int main(int argc, char **argv)
{
	if (argc != 2)
		die("Usage: %s snap-name", argv[0]);
	const char *snap_name = argv[1];

	int snap_lock_fd = sc_lock_snap(snap_name);
	debug("initializing mount namespace: %s", snap_name);
	struct sc_ns_group *group =
	    sc_open_ns_group(snap_name, SC_NS_FAIL_GRACEFULLY);
	if (group != NULL) {
		sc_discard_preserved_ns_group(group);
		sc_close_ns_group(group);
	}
	// Unlink the current mount profile, if any.
	char profile_path[PATH_MAX] = { 0 };
	sc_must_snprintf(profile_path, sizeof(profile_path),
			 "/run/snapd/ns/snap.%s.fstab", snap_name);
	if (unlink(profile_path) < 0) {
		// Silently ignore ENOENT as the profile doens't have to be there.
		if (errno != ENOENT) {
			die("cannot remove current mount profile: %s",
			    profile_path);
		}
	}

	sc_unlock(snap_lock_fd);
	return 0;
}
