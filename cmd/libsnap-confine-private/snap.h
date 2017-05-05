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

#ifndef SNAP_CONFINE_SNAP_H
#define SNAP_CONFINE_SNAP_H

#include <stdbool.h>

#include "error.h"

/**
 * Error domain for errors related to the snap module.
 **/
#define SC_SNAP_DOMAIN "snap"

enum {
	/** The name of the snap is not valid. */
	SC_SNAP_INVALID_NAME = 1,
};

/**
 * Validate the given snap name.
 *
 * Valid name cannot be NULL and must match a regular expression describing the
 * strict naming requirements. Please refer to snapd source code for details.
 *
 * The error protocol is observed so if the caller doesn't provide an outgoing
 * error pointer the function will die on any error.
 **/
void sc_snap_name_validate(const char *snap_name, struct sc_error **errorp);

bool verify_security_tag(const char *security_tag);

bool verify_is_hook_security_tag(const char *security_tag);
bool verify_snap_name(const char *name);

#endif
