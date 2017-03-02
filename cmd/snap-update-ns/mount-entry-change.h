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
 * Description of a change to the given mount entry.
 *
 * The structure pairs an action with an entry to act on.
 **/
struct sc_mount_change {
	enum sc_mount_action action;
	const struct sc_mount_entry *entry;
};

/**
 * Compare two sorted list of mount entries and compute actionable deltas.
 *
 * The function traverses two lists of mount entries (desired and current).
 * Each element that is in the current entry that is not in the desired entry
 * results in an umount change. Each element in the desired entry that is not
 * in the current entry results in a mount change.
 *
 * Both lists *must* be sorted by the caller prior to using this function.
 *
 * The result is written back to all the pointers passed to the functions. Both
 * desired and current are advanced as the algorithm traverses the list. The
 * change is always written. The caller should stop when desired and current
 * both become NULL. At that time the resulting change will become
 * SC_ACTION_NONE.
 **/
void
sc_compute_required_mount_changes(struct sc_mount_entry **desiredp,
				  struct sc_mount_entry **currentp,
				  struct sc_mount_change *change);

#endif
