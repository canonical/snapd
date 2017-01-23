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

/**
 * A fstab-like mount entry.
 **/
struct sc_mount_entry {
	char *mnt_fsname;	/* name of mounted filesystem */
	char *mnt_dir;		/* filesystem path prefix */
	char *mnt_type;		/* mount type (see mntent.h) */
	char *mnt_opts;		/* mount options (see mntent.h) */
	int mnt_freq;		/* dump frequency in days */
	int mnt_passno;		/* pass number on parallel fsck */

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
 * Convert a mount entry to string.
 *
 * NOTE: this does not handle octal escapes that should be generated for
 * reliable parsing of entries that contain spaces. This is only useful for
 * debugging and diagnostic messages.
 *
 * The result uses a statically-allocated buffer that is reused on each call.
 **/
const char *sc_mount_entry_to_string(const struct sc_mount_entry *entry);

/**
 * Sort the linked list of mount entries.
 *
 * The initial argument is a pointer to the first element (which can be NULL).
 * The list is sorted and all the next pointers are updated to point to the
 * lexically subsequent element.
 **/
void sc_sort_mount_entries(struct sc_mount_entry **first);

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

/**
 * Action that should be taken on a mount entry.
 **/
enum sc_mount_action {
	SC_ACTION_NONE,		/*< Nothing to do. */
	SC_ACTION_MOUNT,	/*< A mount operation should be attempted. */
	SC_ACTION_UNMOUNT,	/*< A umount operation should be attempted. */
	// TODO: support SC_ACTION_REMOUNT when needed.
};

/**
 * Change that brings the mount profile closer to the desired state.
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
 * The result is written back to all the there pointers passed to the
 * functions. Both desired and current are advanced as the algorithm traverses
 * the list. The change is always written. The caller should stop when desired
 * and current both become NULL. At that time the resulting change will become
 * SC_ACTION_NONE.
 **/
void
sc_compute_required_mount_changes(struct sc_mount_entry **desiredp,
				  struct sc_mount_entry **currentp,
				  struct sc_mount_change *change);

/**
 * Convert flags for mount(2) system call to a string representation. 
 *
 * The function uses an internal static buffer that is overwritten on each
 * request.
 **/
unsigned long sc_mount_str2opt(const char *flags);

/**
 * Perform a mount operation as described by the given entry.
 **/
void sc_mount_mount_entry(const struct sc_mount_entry *entry);

/**
 * Perform an unmount operation that affects the given entry.
 **/
void sc_unmount_mount_entry(const struct sc_mount_entry *entry);

/**
 * Take the action described by the given mount change.
 *
 * This function either mounts or unmounts the appropriate
 * location. In the future it may also support in-place remounts.
 **/
void sc_act_on_mount_change(const struct sc_mount_change *change);

#endif
