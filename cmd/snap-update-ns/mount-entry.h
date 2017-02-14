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
 * A fstab-like mount entry.
 **/
struct sc_mount_entry {
	struct mntent entry;
	struct sc_mount_entry *next;
};

/**
 * Parse a given fstab-like file into a list of sc_mount_entry objects.
 *
 * If the given file does not exist then the result is a NULL (empty) list.
 * If anything goes wrong the routine die()s.
 **/
struct sc_mount_entry *sc_load_mount_profile(const char *pathname);

/**
 * Save a list of sc_mount_entry objects to a fstab-like file.
 *
 * If anything goes wrong the routine die()s.
 **/
void sc_save_mount_profile(const struct sc_mount_entry *first,
			   const char *pathname);

/**
 * Compare two mount entries.
 *
 * Returns 0 if both entries are equal, a number less than zero if the first
 * entry sorts before the second entry or a number greater than zero if the
 * second entry sorts before the second entry.
 **/
int
sc_compare_mount_entry(const struct sc_mount_entry *a,
		       const struct sc_mount_entry *b);

/**
 * Free a dynamically allocated list of strct sc_mount_entry objects.
 *
 * This function is designed to be used with
 * __attribute__((cleanup(sc_cleanup_mount_entry_list))).
 **/
void sc_cleanup_mount_entry_list(struct sc_mount_entry **entryp);

/**
 * Free a dynamically allocated list of strct sc_mount_entry objects.
 **/
void sc_free_mount_entry_list(struct sc_mount_entry *entry);

#endif
