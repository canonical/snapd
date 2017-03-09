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
	struct sc_mount_entry *d, *c;
	d = desired;
	c = current;

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

	// Do a pass over the two lists advancing them in the body of the loop.
	while (c != NULL || d != NULL) {
		if (c == NULL && d != NULL) {
			// Current profile exhausted but desired profile is not.
			// Unless the desired profile is flagged (it was reused earlier)
			// we want to mount the desired entry now.
			if (!d->flag) {
				append_change(d, SC_ACTION_MOUNT);
			}
			d = d->next;
		} else if (c != NULL && d == NULL) {
			// Current profile is not exhausted but the desired profile is.
			// Generate an UNMOUNT action based on the current entry and
			// advance it.
			append_change(c, SC_ACTION_UNMOUNT);
			c = c->next;
		} else if (c != NULL && d != NULL) {
			// Both profiles have entries to consider.
			if (sc_compare_mount_entry(c, d) == 0) {
				// Identical entries are just skipped and the algorithm continues.
				c = c->next;
				d = d->next;
			} else {
				// Non-identical entries mean that we need to unmount the current
				// entry and mount the desired entry.
				//
				// Let's process all the unmounts first. This way we can "clear the
				// stage" (so to speak). Either the tip of the current profile and
				// tip of the desired profile become identical (we're in sync) or
				// we're eventually going to exhaust the current profile and the
				// code above will start to process items in the desired profile
				// (which will cause all the mount calls to happen).
				struct sc_mount_entry *found =
				    sc_mount_entry_find(desired, c);
				if (found != NULL) {
					// If the current mount entry is further down the desired
					// profile chain then we don't need to unmount it. We want
					// to flag it though, so that we don't try to mount it when
					// processing the leftovers of the desired list.
					found->flag = 1;
					c = c->next;
				} else {
					// If the current entry is not desired then just unmount it.
					append_change(c, SC_ACTION_UNMOUNT);
					c = c->next;
				}
			}
		}
	}
	// Both profiles exhausted. There is nothing to do left.
	return first_change;
}
