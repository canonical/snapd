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
#include <stddef.h>

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

/**
 * Validate security tag against strict naming requirements and snap name.
 *
 *  The executable name is of form:
 *   snap.<name>.(<appname>|hook.<hookname>)
 *  - <name> must start with lowercase letter, then may contain
 *   lowercase alphanumerics and '-'; it must match snap_name
 *  - <appname> may contain alphanumerics and '-'
 *  - <hookname must start with a lowercase letter, then may
 *   contain lowercase letters and '-'
 **/
bool verify_security_tag(const char *security_tag, const char *snap_name);

bool sc_is_hook_security_tag(const char *security_tag);

/**
 * Extract snap name out of an instance name.
 *
 * A snap may be installed multiple times in parallel under distinct instance names.
 * This function extracts the snap name out of a name that possibly contains a snap
 * instance key.
 *
 * For example: snap_instance => snap, just-snap => just-snap
 **/
void sc_snap_drop_instance_key(const char *instance_name, char *snap_name,
			       size_t snap_name_size);

/**
 * Extract snap name and instance key out of an instance name.
 *
 * A snap may be installed multiple times in parallel under distinct instance
 * names. This function extracts the snap name and instance key out of the
 * instance name. One of snap_name, instance_key must be non-NULL.
 *
 * For example:
 *   name_instance => "name" & "instance"
 *   just-name     => "just-name" & ""
 *
 **/
void sc_snap_split_instance_name(const char *instance_name, char *snap_name,
				 size_t snap_name_size, char *instance_key,
				 size_t instance_key_size);

#endif
