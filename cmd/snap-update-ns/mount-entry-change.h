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

#ifndef SNAP_CONFINE_MOUNT_ENTRY_CHANGE_H
#define SNAP_CONFINE_MOUNT_ENTRY_CHANGE_H

#include "mount-entry.h"

/**
 * Mount action describes activity affecting a mount entry.
 **/
enum sc_mount_action {
	SC_ACTION_NONE,		/*< Nothing to do. */
	SC_ACTION_MOUNT,	/*< A mount operation should be attempted. */
	SC_ACTION_UNMOUNT,	/*< A umount operation should be attempted. */
	// TODO: support SC_ACTION_REMOUNT when needed.
};

/**
 * Return the name of a mount action.
 *
 * This returns the string "none", "mount", "unmount" or "???", depending on
 * the input.
 **/
const char *sc_mount_action_to_str(enum sc_mount_action action);

/**
 * Description of a change to the given mount entry.
 *
 * The structure pairs an action with an entry to act on.
 * Structures can be chained with simple single-linked list.
 **/
struct sc_mount_change {
	enum sc_mount_action action;
	const struct sc_mount_entry *entry;
	struct sc_mount_change *next;
};

/**
 * Compare two sorted list of mount entries and compute actionable deltas.
 *
 * The function traverses two lists of mount entries (desired and current).
 * Each element that is in the current entry that is not in the desired entry
 * results in an umount change. Each element in the desired entry that is not
 * in the current entry results in a mount change.
 *
 * The result is computed internally and returned to the caller as
 * newly-allocated chain of sc_mount_change structures. Note that it is
 * possible for the function to return NULL when no changes are required.
 *
 * The caller must ensure that each element of the chain is freed.
 **/
struct sc_mount_change *sc_compute_required_mount_changes(struct sc_mount_entry
							  **desiredp, struct sc_mount_entry
							  **currentp);

#endif
