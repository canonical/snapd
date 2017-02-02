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
#include <fcntl.h>
#include <limits.h>
#include <mntent.h>
#include <stdio.h>
#include <string.h>
#include <sys/mount.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <unistd.h>

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
	result->mnt_fsname = strdup(entry->mnt_fsname);
	if (result->mnt_fsname == NULL) {
		die("cannot copy string");
	}
	result->mnt_dir = strdup(entry->mnt_dir);
	if (result->mnt_dir == NULL) {
		die("cannot copy string");
	}
	result->mnt_type = strdup(entry->mnt_type);
	if (result->mnt_type == NULL) {
		die("cannot copy string");
	}
	result->mnt_opts = strdup(entry->mnt_opts);
	if (result->mnt_opts == NULL) {
		die("cannot copy string");
	}
	result->mnt_freq = entry->mnt_freq;
	result->mnt_passno = entry->mnt_passno;
	return result;
}

static struct sc_mount_entry *sc_get_next_and_free_mount_entry(struct
							       sc_mount_entry
							       *entry)
{
	struct sc_mount_entry *next = entry->next;
	free(entry->mnt_fsname);
	free(entry->mnt_dir);
	free(entry->mnt_type);
	free(entry->mnt_opts);
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
	sc_free_mount_entry_list(*entryp);
	*entryp = NULL;
}

int sc_compare_mount_entry(const struct sc_mount_entry *a,
			   const struct sc_mount_entry *b)
{
	int result;
	if (a == NULL || b == NULL) {
		die("cannot compare NULL mount entry");
	}
	result = strcmp(a->mnt_fsname, b->mnt_fsname);
	if (result != 0) {
		return result;
	}
	result = strcmp(a->mnt_dir, b->mnt_dir);
	if (result != 0) {
		return result;
	}
	result = strcmp(a->mnt_type, b->mnt_type);
	if (result != 0) {
		return result;
	}
	result = strcmp(a->mnt_opts, b->mnt_opts);
	if (result != 0) {
		return result;
	}
	result = a->mnt_freq - b->mnt_freq;
	if (result != 0) {
		return result;
	}
	result = a->mnt_passno - b->mnt_passno;
	return result;
}

const char *sc_mount_entry_to_string(const struct sc_mount_entry *entry)
{
	static char buf[PATH_MAX * 2 + 1000];
	sc_must_snprintf(buf, sizeof buf,
			 "%s %s %s %s %d %d",
			 entry->mnt_fsname,
			 entry->mnt_dir,
			 entry->mnt_type,
			 entry->mnt_opts, entry->mnt_freq, entry->mnt_passno);
	return buf;
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
		struct mntent mntent_entry;
		mntent_entry.mnt_fsname = entry->mnt_fsname;
		mntent_entry.mnt_dir = entry->mnt_dir;
		mntent_entry.mnt_type = entry->mnt_type;
		mntent_entry.mnt_opts = entry->mnt_opts;
		mntent_entry.mnt_freq = entry->mnt_freq;
		mntent_entry.mnt_passno = entry->mnt_passno;

		if (addmntent(f, &mntent_entry) != 0) {
			die("cannot add mount entry to %s", pathname);
		}
	}
}

void sc_sort_mount_entries(struct sc_mount_entry **first)
{
	if (*first == NULL) {
		// NULL list is an empty list
		return;
	}
	// Count the items
	size_t count;
	struct sc_mount_entry *entry;
	for (count = 0, entry = *first; entry != NULL;
	     ++count, entry = entry->next) ;

	// Allocate an array of pointers
	struct sc_mount_entry **entryp_array = NULL;
	entryp_array = calloc(count, sizeof *entryp_array);
	if (entryp_array == NULL) {
		die("cannot allocate memory");
	}
	// Populate the array
	entry = *first;
	for (size_t i = 0; i < count; ++i) {
		entryp_array[i] = entry;
		entry = entry->next;
	}

	// Sort the array according to lexical sorting of all the elements.
	qsort(entryp_array, count, sizeof(void *),
	      (int (*)(const void *, const void *))
	      sc_indirect_compare_mount_entry);

	// Rewrite all the next pointers of each element.
	for (size_t i = 0; i < count - 1; ++i) {
		entryp_array[i]->next = entryp_array[i + 1];
	}
	entryp_array[count - 1]->next = NULL;

	// Rewrite the pointer to the head of the list.
	*first = entryp_array[0];
	free(entryp_array);
}

void
sc_compute_required_mount_changes(struct sc_mount_entry * *desiredp,
				  struct sc_mount_entry * *currentp,
				  struct sc_mount_change *change)
{
	struct sc_mount_entry *d, *c;
	if (desiredp == NULL) {
		die("cannot compute required mount changes, desiredp is NULL");
	}
	if (currentp == NULL) {
		die("cannot compute required mount changes, currentp is NULL");
	}
	if (change == NULL) {
		die("cannot compute required mount changes, change is NULL");
	}
	d = *desiredp;
	c = *currentp;
 again:
	if (c == NULL && d == NULL) {
		// Both profiles exhausted. There is nothing to do left.
		change->action = SC_ACTION_NONE;
		change->entry = NULL;
		*currentp = NULL;
		*desiredp = NULL;
	} else if (c == NULL && d != NULL) {
		// Current profile exhausted but desired profile is not.
		// Generate a MOUNT action based on desired profile and advance it.
		change->action = SC_ACTION_MOUNT;
		change->entry = d;
		*currentp = NULL;
		*desiredp = d->next;
	} else if (c != NULL && d == NULL) {
		// Current profile is not exhausted but the desired profile is.
		// Generate an UNMOUNT action based on the current profile and advance it.
		change->action = SC_ACTION_UNMOUNT;
		change->entry = c;
		*currentp = c->next;
		*desiredp = NULL;
	} else if (c != NULL && d != NULL) {
		// Both profiles have entries to consider.
		if (sc_compare_mount_entry(c, d) == 0) {
			// Identical entries are just skipped and the algorithm continues.
			c = c->next;
			d = d->next;
			goto again;
		} else {
			// Non-identail entries mean that we need to unmount the current
			// entry and mount the desired entry.
			//
			// Let's process all the unmounts first. This way we can "clear the
			// stage" (so to speak). Either the tip of the current profile and
			// tip of the desired profile become identical (we're in sync) or
			// we're eventually going to exhaust the current profile and the
			// code above will start to process items in the desired profile
			// (which will cause all the mount calls to happen).
			change->action = SC_ACTION_UNMOUNT;
			change->entry = c;
			*currentp = c->next;
			*desiredp = d;
		}
	}
}

struct sc_mount_flag {
	const char *name;
	unsigned long value;
};

static const struct sc_mount_flag known_flags[] = {
	{.name = "ro",.value = MS_RDONLY},
	{.name = "nosuid",.value = MS_NOSUID},
	{.name = "nodev",.value = MS_NODEV},
	{.name = "noexec",.value = MS_NOEXEC},
	{.name = "sync",.value = MS_SYNCHRONOUS},
	{.name = "remount",.value = MS_REMOUNT},
	{.name = "mand",.value = MS_MANDLOCK},
	{.name = "dirsync",.value = MS_DIRSYNC},
	{.name = "noatime",.value = MS_NOATIME},
	{.name = "nodiratime",.value = MS_NODIRATIME},
	{.name = "bind",.value = MS_BIND},
	{.name = "rbind",.value = MS_BIND | MS_REC},
	{.name = "move",.value = MS_MOVE},
	{.name = "silent",.value = MS_SILENT},
	{.name = "acl",.value = MS_POSIXACL},
	{.name = "private",.value = MS_PRIVATE},
	{.name = "rprivate",.value = MS_PRIVATE | MS_REC},
	{.name = "slave",.value = MS_SLAVE},
	{.name = "rslave",.value = MS_SLAVE | MS_REC},
	{.name = "shared",.value = MS_SHARED},
	{.name = "rshared",.value = MS_SHARED | MS_REC},
	{.name = "unbindable",.value = MS_UNBINDABLE},
	{.name = "runbindable",.value = MS_UNBINDABLE | MS_REC},
	{.name = "relatime",.value = MS_RELATIME},
	// NOTE: we don't support MS_KERNMOUNT and MS_I_VERSION.
	{.name = "strictatime",.value = MS_STRICTATIME},
	// NOTE: we don't support MS_LAZYTIME, MS_NOSEC, MS_BORN, MS_ACTIVE or
	// MS_NOUSER until there's a need for that.
};

unsigned long sc_mount_str2opt(const char *opts)
{
	// In glibc this code just looks at the mnt_opt field so rather than
	// replicating all of hasmntopt here we're just faking a mntent structure
	// and calling the real thing.
	const struct mntent mnt = {.mnt_opts = (char *)opts };
	unsigned long flags = 0;
	for (size_t i = 0; i < sizeof known_flags / sizeof *known_flags; ++i) {
		if (hasmntopt(&mnt, known_flags[i].name) != NULL) {
			flags |= known_flags[i].value;
		}
	}
	return flags;
}

void sc_mount_mount_entry(const struct sc_mount_entry *entry)
{
	unsigned long flags = sc_mount_str2opt(entry->mnt_opts);
	// TODO: switch to sc_do_mount() when it lands
	if (mount(entry->mnt_fsname, entry->mnt_dir, entry->mnt_type,
		  flags, NULL) < 0) {
		die("cannot mount %s", entry->mnt_dir);
	}
}

void sc_unmount_mount_entry(const struct sc_mount_entry *entry)
{
	// TODO: switch to sc_do_umount() when it lands
	if (umount2(entry->mnt_dir, UMOUNT_NOFOLLOW) < 0) {
		die("cannot unmount %s", entry->mnt_dir);
	}
}

void sc_act_on_mount_change(const struct sc_mount_change *change)
{
	switch (change->action) {
	case SC_ACTION_NONE:
		break;
	case SC_ACTION_MOUNT:
		sc_mount_mount_entry(change->entry);
		break;
	case SC_ACTION_UNMOUNT:
		sc_unmount_mount_entry(change->entry);
		break;
	}
}
