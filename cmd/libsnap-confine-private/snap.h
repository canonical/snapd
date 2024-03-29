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
	/** The instance key of the snap is not valid. */
	SC_SNAP_INVALID_INSTANCE_KEY = 2,
	/** The instance of the snap is not valid. */
	SC_SNAP_INVALID_INSTANCE_NAME = 3,
	/** System configuration is not supported. */
	SC_SNAP_MOUNT_DIR_UNSUPPORTED = 4,
	/** The component name of the snap is not valid. */
	SC_SNAP_INVALID_COMPONENT = 5,
};

/* SNAP_NAME_LEN is the maximum length of a snap name, enforced by snapd and the
 * store. */
#define SNAP_NAME_LEN 40
/* SNAP_INSTANCE_KEY_LEN is the maximum length of instance key, enforced locally
 * by snapd. */
#define SNAP_INSTANCE_KEY_LEN 10
/* SNAP_INSTANCE_LEN is the maximum length of snap instance name, composed of
 * the snap name, separator '_' and the instance key, enforced locally by
 * snapd. */
#define SNAP_INSTANCE_LEN (SNAP_NAME_LEN + 1 + SNAP_INSTANCE_KEY_LEN)
/* SNAP_SECURITY_TAG_MAX_LEN is the maximum length of a security tag string
 * (not buffer). This is an upper limit. In practice the security tag name is
 * bound by SNAP_NAME_LEN, SNAP_INSTANCE_KEY_LEN, maximum length of an
 * application name as well as a constant overhead of "snap", the optional
 * "hook" and the "." characters connecting the components. */
#define SNAP_SECURITY_TAG_MAX_LEN 256

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
 * Validate the given instance key.
 *
 * Valid instance key cannot be NULL and must match a regular expression
 * describing the strict naming requirements. Please refer to snapd source code
 * for details.
 *
 * The error protocol is observed so if the caller doesn't provide an outgoing
 * error pointer the function will die on any error.
 **/
void sc_instance_key_validate(const char *instance_key,
			      struct sc_error **errorp);

/**
 * Validate the given snap component.
 *
 * Valid snap component must be composed of a valid snap name and a valid
 * component name, separated by a plus sign. The component name must conform to
 * the same rules as a snap name.
 *
 * If snap_instance is not NULL, then the snap name in the snap component will
 * be compared to the snap name in the snap instance. If they don't match, an
 * error will be raised.
 *
 * The error protocol is observed so if the caller doesn't provide an outgoing
 * error pointer the function will die on any error.
 **/
void sc_snap_component_validate(const char *snap_component,
				const char *snap_instance, sc_error ** errorp);

/**
 * Validate the given snap instance name.
 *
 * Valid instance name must be composed of a valid snap name and a valid
 * instance key.
 *
 * The error protocol is observed so if the caller doesn't provide an outgoing
 * error pointer the function will die on any error.
 **/
void sc_instance_name_validate(const char *instance_name,
			       struct sc_error **errorp);

/**
 * Validate security tag against strict naming requirements, snap name,
 * and an optional component name.
 *
 * Note that component_name should be NULL if the security tag should
 * not contain a component name. If a component name is found in the tag
 * and component_name is NULL, an error will be raised. Conversely, if
 * a component name is expected but not found in the tag, an error will
 * be raised.
 *
 *  The executable name is of form:
 *   snap.<name>(.<appname>|(+<componentname>)?.hook.<hookname>)
 *  - <name> must start with lowercase letter, then may contain
 *   lowercase alphanumerics and '-'; it must match snap_name
 *  - <appname> may contain alphanumerics and '-'
 *  - <componentname must start with a lowercase letter, then may
 *   contain lowercase letters and '-'
 *  - <hookname must start with a lowercase letter, then may
 *   contain lowercase letters and '-'
 **/
bool sc_security_tag_validate(const char *security_tag, const char *snap_name,
			      const char *component_name);

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

/**
 * Extract snap name and component name out of a snap component.
 *
 * For example:
 *   snap+component => "snap" & "component"
 *
 **/
void sc_snap_split_snap_component(const char *snap_component, char *snap_name,
				  size_t snap_name_size, char *component_name,
				  size_t component_name_size);

#endif
