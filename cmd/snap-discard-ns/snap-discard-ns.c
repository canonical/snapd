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

#include "../libsnap-wrap-private/utils.h"
#include "../snap-wrap/ns-support.h"

int main(int argc, char **argv)
{
	if (argc != 2)
		die("Usage: %s snap-name", argv[0]);
	const char *snap_name = argv[1];
	struct sc_ns_group *group =
	    sc_open_ns_group(snap_name, SC_NS_FAIL_GRACEFULLY);
	if (group != NULL) {
		sc_lock_ns_mutex(group);
		sc_discard_preserved_ns_group(group);
		sc_unlock_ns_mutex(group);
		sc_close_ns_group(group);
	}
	return 0;
}
