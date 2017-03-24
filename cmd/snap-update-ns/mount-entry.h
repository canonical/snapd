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

#ifndef SNAP_CONFINE_MOUNT_ENTRY_H
#define SNAP_CONFINE_MOUNT_ENTRY_H

#ifdef HAVE_CONFIG_H
#include "config.h"
#endif

#include <mntent.h>

/**
 * A list of mount entries.
 **/
struct sc_mount_entry_list {
	struct sc_mount_entry *first, *last;
};

/**
 * A fstab-like mount entry.
 **/
struct sc_mount_entry {
	struct mntent entry;
	struct sc_mount_entry *prev, *next;
	unsigned int reuse;	// internal flag, not compared
};

/**
 * Parse a given fstab-like file into a list of sc_mount_entry objects.
 *
 * If the given file does not exist then the result is an empty list.
 * If anything goes wrong the routine die()s.
 *
 * The caller must free the list with sc_mount_entry_list.
 **/
struct sc_mount_entry_list *sc_load_mount_profile(const char *pathname);

/**
 * Save a list of sc_mount_entry objects to a fstab-like file.
 *
 * If anything goes wrong the routine die()s.
 **/
void sc_save_mount_profile(const struct sc_mount_entry_list *list,
			   const char *pathname);

/**
 * Compare two mount entries.
 *
 * Returns 0 if both entries are equal, a number less than zero if the first
 * entry sorts before the second entry or a number greater than zero if the
 * second entry sorts before the second entry.
 *
 * The order of comparison is: mnt_{dir,fsname,type,opts,freq,passno}.
 **/
int
sc_compare_mount_entry(const struct sc_mount_entry *a,
		       const struct sc_mount_entry *b);

/**
 * Sort the linked list of mount entries.
 *
 * The list is sorted and all the next/prev pointers are updated to point to
 * the lexically subsequent/preceding element.
 *
 * This function sorts in the ascending order, as specified by
 * sc_compare_mount_entry.
 **/
void sc_sort_mount_entry_list(struct sc_mount_entry_list *list);

/**
 * Free a dynamically allocated list of mount entry objects.
 *
 * This function is designed to be used with
 * __attribute__((cleanup(sc_cleanup_mount_entry_list))).
 **/
void sc_cleanup_mount_entry_list(struct sc_mount_entry_list **listp);

/**
 * Free a dynamically allocated list of mount entry objects.
 **/
void sc_free_mount_entry_list(struct sc_mount_entry_list *list);

#endif
