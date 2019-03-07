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

#ifndef SC_INVOCATION_H
#define SC_INVOCATION_H

#include <stdbool.h>

/**
 * sc_invocation contains information about how snap-confine was invoked.
 *
 * All of the pointer fields have the life-cycle bound to the main process.
 **/
typedef struct sc_invocation {
	/* Things declared by the system. */
	const char *base_snap_name;
	const char *executable;
	const char *security_tag;
	const char *snap_instance;
	bool classic_confinement;
	/* Things derived at runtime. */
	bool is_normal_mode;
} sc_invocation;

#endif
