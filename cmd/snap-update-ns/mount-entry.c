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

#ifdef HAVE_CONFIG_H
#include "config.h"
#endif

#include "snap-update-ns/mount-entry.h"

#include <errno.h>
#include <mntent.h>
#include <stdio.h>
#include <string.h>

#include "../libsnap-confine-private/utils.h"
#include "../libsnap-confine-private/string-utils.h"
#include "../libsnap-confine-private/cleanup-funcs.h"

/**
 * Compare two mount entries (through indirect pointers).
 **/
static int
sc_indirect_compare_mount_entry(const struct sc_mount_entry **a,
				const struct sc_mount_entry **b)
{
	return sc_compare_mount_entry(*a, *b);
}

/**
 * Copy struct mntent into a freshly-allocated struct sc_mount_entry.
 *
 * The next pointer is initialized to NULL, it should be managed by the caller.
 * If anything goes wrong the routine die()s.
 **/
static struct sc_mount_entry *sc_clone_mount_entry_from_mntent(const struct
							       mntent *entry)
{
	struct sc_mount_entry *result;
	result = calloc(1, sizeof *result);
	if (result == NULL) {
		die("cannot allocate memory");
	}
	result->entry.mnt_fsname = strdup(entry->mnt_fsname ? : "");
	if (result->entry.mnt_fsname == NULL) {
		die("cannot copy string");
	}
	result->entry.mnt_dir = strdup(entry->mnt_dir ? : "");
	if (result->entry.mnt_dir == NULL) {
		die("cannot copy string");
	}
	result->entry.mnt_type = strdup(entry->mnt_type ? : "");
	if (result->entry.mnt_type == NULL) {
		die("cannot copy string");
	}
	result->entry.mnt_opts = strdup(entry->mnt_opts ? : "");
	if (result->entry.mnt_opts == NULL) {
		die("cannot copy string");
	}
	result->entry.mnt_freq = entry->mnt_freq;
	result->entry.mnt_passno = entry->mnt_passno;
	return result;
}

static struct sc_mount_entry *sc_get_next_and_free_mount_entry(struct
							       sc_mount_entry
							       *entry)
{
	struct sc_mount_entry *next = entry->next;
	free(entry->entry.mnt_fsname);
	free(entry->entry.mnt_dir);
	free(entry->entry.mnt_type);
	free(entry->entry.mnt_opts);
	memset(entry, 0, sizeof *entry);
	free(entry);
	return next;
}

void sc_free_mount_entry_list(struct sc_mount_entry *entry)
{
	while (entry != NULL) {
		entry = sc_get_next_and_free_mount_entry(entry);
	}
}

void sc_cleanup_mount_entry_list(struct sc_mount_entry **entryp)
{
	if (entryp != NULL) {
		sc_free_mount_entry_list(*entryp);
		*entryp = NULL;
	}
}

int sc_compare_mount_entry(const struct sc_mount_entry *a,
			   const struct sc_mount_entry *b)
{
	int result;
	if (a == NULL || b == NULL) {
		die("cannot compare NULL mount entry");
	}
	result = strcmp(a->entry.mnt_fsname, b->entry.mnt_fsname);
	if (result != 0) {
		return result;
	}
	result = strcmp(a->entry.mnt_dir, b->entry.mnt_dir);
	if (result != 0) {
		return result;
	}
	result = strcmp(a->entry.mnt_type, b->entry.mnt_type);
	if (result != 0) {
		return result;
	}
	result = strcmp(a->entry.mnt_opts, b->entry.mnt_opts);
	if (result != 0) {
		return result;
	}
	result = a->entry.mnt_freq - b->entry.mnt_freq;
	if (result != 0) {
		return result;
	}
	result = a->entry.mnt_passno - b->entry.mnt_passno;
	return result;
}

struct sc_mount_entry *sc_load_mount_profile(const char *pathname)
{
	FILE *f __attribute__ ((cleanup(sc_cleanup_endmntent))) = NULL;

	f = setmntent(pathname, "rt");
	if (f == NULL) {
		// NOTE: it is fine if the profile doesn't exist.
		// It is equivalent to having no entries.
		if (errno != ENOENT) {
			die("cannot open mount profile %s for reading",
			    pathname);
		}
		return NULL;
	}
	// Loop over the entries in the file and copy them to a singly-linked list.
	struct sc_mount_entry *entry, *first = NULL, *prev = NULL;
	struct mntent *mntent_entry;
	while (((mntent_entry = getmntent(f)) != NULL)) {
		entry = sc_clone_mount_entry_from_mntent(mntent_entry);
		if (prev != NULL) {
			prev->next = entry;
		}
		if (first == NULL) {
			first = entry;
		}
		prev = entry;
	}
	return first;
}

void sc_save_mount_profile(const struct sc_mount_entry *first,
			   const char *pathname)
{
	FILE *f __attribute__ ((cleanup(sc_cleanup_endmntent))) = NULL;

	f = setmntent(pathname, "wt");
	if (f == NULL) {
		die("cannot open mount profile %s for writing", pathname);
	}

	const struct sc_mount_entry *entry;
	for (entry = first; entry != NULL; entry = entry->next) {
		if (addmntent(f, &entry->entry) != 0) {
			die("cannot add mount entry to %s", pathname);
		}
	}
}
