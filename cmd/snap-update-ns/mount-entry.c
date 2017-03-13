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
 * Compare two mount entries (through indirect pointers).
 **/
static int
sc_indirect_reverse_compare_mount_entry(const struct sc_mount_entry **a,
					const struct sc_mount_entry **b)
{
	return sc_compare_mount_entry(*b, *a);
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

void sc_free_mount_entry_list(struct sc_mount_entry_list *list)
{
	struct sc_mount_entry *entry = list->first;

	while (entry != NULL) {
		entry = sc_get_next_and_free_mount_entry(entry);
	}
	free(list);
}

void sc_cleanup_mount_entry_list(struct sc_mount_entry_list **listp)
{
	if (listp != NULL) {
		sc_free_mount_entry_list(*listp);
		*listp = NULL;
	}
}

int sc_compare_mount_entry(const struct sc_mount_entry *a,
			   const struct sc_mount_entry *b)
{
	int result;
	if (a == NULL || b == NULL) {
		die("cannot compare NULL mount entry");
	}
	// NOTE: sort reorder field so that mnt_dir is before
	// mnt_fsname. This ordering is a little bit more interesting
	// as the directory matters more and allows us to do useful
	// things later.
	result = strcmp(a->entry.mnt_dir, b->entry.mnt_dir);
	if (result != 0) {
		return result;
	}
	result = strcmp(a->entry.mnt_fsname, b->entry.mnt_fsname);
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

struct sc_mount_entry_list *sc_load_mount_profile(const char *pathname)
{
	struct sc_mount_entry_list *list = calloc(1, sizeof *list);
	if (list == NULL) {
		die("cannot allocate sc_mount_entry_list");
	}

	FILE *f __attribute__ ((cleanup(sc_cleanup_endmntent))) = NULL;
	f = setmntent(pathname, "rt");
	if (f == NULL) {
		// NOTE: it is fine if the profile doesn't exist.
		// It is equivalent to having no entries.
		if (errno != ENOENT) {
			die("cannot open mount profile %s for reading",
			    pathname);
		}
		return list;
	}
	// Loop over the entries in the file and copy them to a doubly-linked list.
	struct sc_mount_entry *entry = NULL, *prev_entry = NULL;
	struct mntent *mntent_entry;
	while (((mntent_entry = getmntent(f)) != NULL)) {
		entry = sc_clone_mount_entry_from_mntent(mntent_entry);
		entry->prev = prev_entry;
		if (prev_entry != NULL) {
			prev_entry->next = entry;
		}
		if (list->first == NULL) {
			list->first = entry;
		}
		prev_entry = entry;
	}
	list->last = entry;

	return list;
}

void sc_save_mount_profile(const struct sc_mount_entry_list *list,
			   const char *pathname)
{
	if (list == NULL) {
		die("cannot save mount profile, list is NULL");
	}

	FILE *f __attribute__ ((cleanup(sc_cleanup_endmntent))) = NULL;

	f = setmntent(pathname, "wt");
	if (f == NULL) {
		die("cannot open mount profile %s for writing", pathname);
	}

	const struct sc_mount_entry *entry;
	for (entry = list->first; entry != NULL; entry = entry->next) {
		if (addmntent(f, &entry->entry) != 0) {
			die("cannot add mount entry to %s", pathname);
		}
	}
}

typedef int (*mount_entry_cmp_fn) (const struct sc_mount_entry **,
				   const struct sc_mount_entry **);

static void sc_sort_mount_entry_list_with(struct sc_mount_entry_list *list,
					  mount_entry_cmp_fn cmp_fn)
{
	if (list == NULL) {
		die("cannot sort mount entry list, list is NULL");
	}

	if (list->first == NULL) {
		// NULL list is an empty list
		return;
	}
	// Count the items
	size_t count;
	struct sc_mount_entry *entry;
	for (count = 0, entry = list->first; entry != NULL;
	     ++count, entry = entry->next) ;

	// Allocate an array of pointers
	struct sc_mount_entry **entryp_array = NULL;
	entryp_array = calloc(count, sizeof *entryp_array);
	if (entryp_array == NULL) {
		die("cannot allocate memory");
	}
	// Populate the array
	entry = list->first;
	for (size_t i = 0; i < count; ++i) {
		entryp_array[i] = entry;
		entry = entry->next;
	}

	// Sort the array according to lexical sorting of all the elements.
	qsort(entryp_array, count, sizeof(void *),
	      (int (*)(const void *, const void *))cmp_fn);

	// Rewrite all the next/prev pointers of each element.
	for (size_t i = 0; i < count; ++i) {
		entryp_array[i]->next =
		    i + 1 < count ? entryp_array[i + 1] : NULL;
		entryp_array[i]->prev = i > 0 ? entryp_array[i - 1] : NULL;
	}

	// Rewrite the list head.
	list->first = count > 0 ? entryp_array[0] : NULL;
	list->last = count > 0 ? entryp_array[count - 1] : NULL;

	free(entryp_array);
}

void sc_sort_mount_entry_list(struct sc_mount_entry_list *list)
{
	sc_sort_mount_entry_list_with(list, sc_indirect_compare_mount_entry);
}

void sc_reverse_sort_mount_entry_list(struct sc_mount_entry_list *list)
{
	sc_sort_mount_entry_list_with(list,
				      sc_indirect_reverse_compare_mount_entry);
}
