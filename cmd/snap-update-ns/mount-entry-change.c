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

#include "../libsnap-confine-private/utils.h"

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

struct sc_mount_change *sc_compute_required_mount_changes(struct sc_mount_entry
							  *desired, struct sc_mount_entry
							  *current)
{
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

	// Reset flags in both lists as we use them to track reused entries.
	for (entry = current; entry != NULL; entry = entry->next) {
		entry->flag = 0;
	}
	for (entry = desired; entry != NULL; entry = entry->next) {
		entry->flag = 0;
	}

	// Do a pass over the current list to see if they are present in the
	// desired list. Such entries are flagged so that they are not toched by
	// either loops below.
	//
	// NOTE: This will linearly search the desired list. If this is going to
	// get expensive it should be changed to a more efficient operation. For
	// the sizes of mount profiles we are working with (typically close to one)
	// this is sufficient though.
	for (entry = current; entry != NULL; entry = entry->next) {
		struct sc_mount_entry *found =
		    sc_mount_entry_find(desired, entry);
		if (found != NULL) {
			// NOTE: we flag both in the current and desired lists as we
			// iterate over both lists below.
			entry->flag = 1;
			found->flag = 1;
		}
	}

	// Do a pass over the current list and unmount unflagged entries.
	for (entry = current; entry != NULL; entry = entry->next) {
		if (entry->flag == 0) {
			append_change(entry, SC_ACTION_UNMOUNT);
		}
	}

	// Do a pass over the desired list and mount the unflagged entries.
	for (entry = desired; entry != NULL; entry = entry->next) {
		if (entry->flag == 0) {
			append_change(entry, SC_ACTION_MOUNT);
		}
	}

	return first_change;
}
