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

#include "mount-entry-change.h"

#include <stdlib.h>
#include <string.h>

#include "../libsnap-confine-private/utils.h"

const char *sc_mount_action_to_str(enum sc_mount_action action)
{
	switch (action) {
	case SC_ACTION_NONE:
		return "none";
	case SC_ACTION_MOUNT:
		return "mount";
	case SC_ACTION_UNMOUNT:
		return "unmount";
	default:
		return "???";
	}
}

/**
 * Look through the haystack and find the first needle.
 **/
static struct sc_mount_entry *sc_mount_entry_find(struct sc_mount_entry
						  *haystack, const struct sc_mount_entry
						  *needle)
{
	for (; haystack != NULL; haystack = haystack->next) {
		if (sc_compare_mount_entry(needle, haystack) == 0) {
			return haystack;
		}
	}
	return NULL;
}

static struct sc_mount_change *sc_mount_change_alloc()
{
	struct sc_mount_change *change = calloc(1, sizeof *change);
	if (change == NULL) {
		die("cannot allocate sc_mount_change object");
	}
	return change;
}

static void sc_mount_change_free_chain(struct sc_mount_change *change)
{
	struct sc_mount_change *c = change, *n;
	while (c != NULL) {
		n = c->next;
		free(c);
		c = n;
	}
}

struct sc_mount_change *sc_compute_required_mount_changes(struct sc_mount_entry_list
							  *desired, struct
							  sc_mount_entry_list
							  *current)
{
	if (desired == NULL || current == NULL) {
		die("cannot compute required mount changes, NULL pointer");
	}
	// Helper function to append to the list of changes.
	struct sc_mount_change *first_change = NULL;
	struct sc_mount_change *last_change = NULL;

	void append_change(const struct sc_mount_entry *entry,
			   enum sc_mount_action action) {
		struct sc_mount_change *change = sc_mount_change_alloc();
		change->action = action;
		change->entry = entry;
		if (first_change == NULL) {
			first_change = change;
		}
		if (last_change != NULL) {
			last_change->next = change;
		}
		last_change = change;
	}

	struct sc_mount_entry *entry;

	// Reset reuse flags in both lists as we use them to track reused entries.
	for (entry = current->first; entry != NULL; entry = entry->next) {
		entry->reuse = 0;
	}
	for (entry = desired->first; entry != NULL; entry = entry->next) {
		entry->reuse = 0;
	}

	// Do a pass over the current list to see if they are present in the
	// desired list. Such entries are flagged for reuse so that they are not
	// touched by either loops below.
	//
	// NOTE: This will linearly search the desired list. If this is going to
	// get expensive it should be changed to a more efficient operation. For
	// the sizes of mount profiles we are working with (typically close to one)
	// this is sufficient though.
	const char *prefix = NULL;
	for (entry = current->first; entry != NULL; entry = entry->next) {
		// We work based on the assumption that the current list is sorted by
		// mount directory (mnt_dir). This is also documented in the header
		// file.
		if (prefix != NULL
		    && strncmp(prefix, entry->entry.mnt_dir,
			       strlen(prefix)) == 0) {
			// This entry is a child of an earlier entry that we did not reuse
			// (it starts with the same path). If the parent is changed we
			// cannot allow the children to be reused.
			continue;
		}
		struct sc_mount_entry *found =
		    sc_mount_entry_find(desired->first, entry);
		if (found == NULL) {
			// Remember the prefix so that children are unmounted too;
			prefix = entry->entry.mnt_dir;
		} else {
			// NOTE: we flag for reuse in both the current and desired lists as
			// we iterate over both lists below.
			entry->reuse = 1;
			found->reuse = 1;
		}
	}

	// Do a pass over the current list and unmount entries not flagged for reuse.
	for (entry = current->last; entry != NULL; entry = entry->prev) {
		if (!entry->reuse) {
			append_change(entry, SC_ACTION_UNMOUNT);
		}
	}

	// Do a pass over the desired list and mount the entries not flagged for reuse.
	for (entry = desired->first; entry != NULL; entry = entry->next) {
		if (!entry->reuse) {
			append_change(entry, SC_ACTION_MOUNT);
		}
	}

	return first_change;
}
