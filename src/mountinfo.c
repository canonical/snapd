/*
 * Copyright (C) 2016 Canonical Ltd
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
 */

#include "mountinfo.h"

#include <errno.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

struct mountinfo {
	struct mountinfo_entry *first;
};

struct mountinfo_entry {
	int mount_id;
	int parent_id;
	unsigned dev_major, dev_minor;
	char *root;
	char *mount_dir;
	char *mount_opts;
	char *optional_fields;
	char *fs_type;
	char *mount_source;
	char *super_opts;

	struct mountinfo_entry *next;
	// Buffer holding all of the text data above.
	//
	// The buffer must be the last element of the structure. It is allocated
	// along with the structure itself and does not need to be freed
	// separately.
	char line_buf[0];
};

/**
 * Parse a single mountinfo entry (line).
 *
 * The format, described by Linux kernel documentation, is as follows:
 *
 * 36 35 98:0 /mnt1 /mnt2 rw,noatime master:1 - ext3 /dev/root rw,errors=continue
 * (1)(2)(3)   (4)   (5)      (6)      (7)   (8) (9)   (10)         (11)
 *
 * (1) mount ID:  unique identifier of the mount (may be reused after umount)
 * (2) parent ID:  ID of parent (or of self for the top of the mount tree)
 * (3) major:minor:  value of st_dev for files on filesystem
 * (4) root:  root of the mount within the filesystem
 * (5) mount point:  mount point relative to the process's root
 * (6) mount options:  per mount options
 * (7) optional fields:  zero or more fields of the form "tag[:value]"
 * (8) separator:  marks the end of the optional fields
 * (9) filesystem type:  name of filesystem of the form "type[.subtype]"
 * (10) mount source:  filesystem specific information or "none"
 * (11) super options:  per super block options
 **/
static struct mountinfo_entry *parse_mountinfo_entry(const char *line)
    __attribute__ ((nonnull(1)));

/**
 * Free a mountinfo structure and all its entries.
 **/
static void free_mountinfo(struct mountinfo *info)
    __attribute__ ((nonnull(1)));

/**
 * Free a mountinfo entry.
 **/
static void free_mountinfo_entry(struct mountinfo_entry *entry)
    __attribute__ ((nonnull(1)));

static void cleanup_fclose(FILE ** ptr);
static void cleanup_free(char **ptr);

struct mountinfo_entry *first_mountinfo_entry(struct mountinfo *info)
{
	return info->first;
}

struct mountinfo_entry *next_mountinfo_entry(struct mountinfo_entry
					     *entry)
{
	return entry->next;
}

int mountinfo_entry_mount_id(struct mountinfo_entry *entry)
{
	return entry->mount_id;
}

int mountinfo_entry_parent_id(struct mountinfo_entry *entry)
{
	return entry->parent_id;
}

unsigned mountinfo_entry_dev_major(struct mountinfo_entry *entry)
{
	return entry->dev_major;
}

unsigned mountinfo_entry_dev_minor(struct mountinfo_entry *entry)
{
	return entry->dev_minor;
}

const char *mountinfo_entry_root(struct mountinfo_entry *entry)
{
	return entry->root;
}

const char *mountinfo_entry_mount_dir(struct mountinfo_entry *entry)
{
	return entry->mount_dir;
}

const char *mountinfo_entry_mount_opts(struct mountinfo_entry *entry)
{
	return entry->mount_opts;
}

const char *mountinfo_entry_optional_fields(struct mountinfo_entry *entry)
{
	return entry->optional_fields;
}

const char *mountinfo_entry_fs_type(struct mountinfo_entry *entry)
{
	return entry->fs_type;
}

const char *mountinfo_entry_mount_source(struct mountinfo_entry *entry)
{
	return entry->mount_source;
}

const char *mountinfo_entry_super_opts(struct mountinfo_entry *entry)
{
	return entry->super_opts;
}

struct mountinfo *parse_mountinfo(const char *fname)
{
	struct mountinfo *info = calloc(1, sizeof *info);
	if (info == NULL) {
		return NULL;
	}
	if (fname == NULL) {
		fname = "/proc/self/mountinfo";
	}
	FILE *f __attribute__ ((cleanup(cleanup_fclose))) = fopen(fname, "rt");
	if (f == NULL) {
		free(info);
		return NULL;
	}
	char *line __attribute__ ((cleanup(cleanup_free))) = NULL;
	size_t line_size = 0;
	struct mountinfo_entry *entry, *last = NULL;
	for (;;) {
		errno = 0;
		if (getline(&line, &line_size, f) == -1) {
			if (errno != 0) {
				free_mountinfo(info);
				return NULL;
			}
			break;
		};
		entry = parse_mountinfo_entry(line);
		if (entry == NULL) {
			free_mountinfo(info);
			return NULL;
		}
		if (last != NULL) {
			last->next = entry;
		} else {
			info->first = entry;
		}
		last = entry;
	}
	return info;
}

static struct mountinfo_entry *parse_mountinfo_entry(const char *line)
{
	// NOTE: the mountinfo structure is allocated along with enough extra
	// storage to hold the whole line we are parsing. This is used as backing
	// store for all text fields.
	//
	// The idea is that since the line has a given length and we are only after
	// set of substrings we can easily predict the amount of required space
	// (after all, it is just a set of non-overlapping substrings) and append
	// it to the allocated entry structure.
	//
	// The parsing code below, specifically parse_next_string_field(), uses
	// this extra memory to hold data parsed from the original line. In the
	// end, the result is similar to using strtok except that the source and
	// destination buffers are separate.
	struct mountinfo_entry *entry =
	    calloc(1, sizeof *entry + strlen(line) + 1);
	if (entry == NULL) {
		return NULL;
	}
	int nscanned;
	int offset, total_offset = 0;
	nscanned = sscanf(line, "%d %d %u:%u %n",
			  &entry->mount_id, &entry->parent_id,
			  &entry->dev_major, &entry->dev_minor, &offset);
	if (nscanned != 4)
		goto fail;
	total_offset += offset;
	int total_used = 0;
	char *parse_next_string_field() {
		char *field = &entry->line_buf[0] + total_used;
		nscanned = sscanf(line + total_offset, "%s %n", field, &offset);
		if (nscanned != 1)
			return NULL;
		total_offset += offset;
		total_used += offset + 1;
		return field;
	}
	if ((entry->root = parse_next_string_field()) == NULL)
		goto fail;
	if ((entry->mount_dir = parse_next_string_field()) == NULL)
		goto fail;
	if ((entry->mount_opts = parse_next_string_field()) == NULL)
		goto fail;
	entry->optional_fields = &entry->line_buf[0] + total_used++;
	// NOTE: This ensures that optional_fields is never NULL. If this changes,
	// must adjust all callers of parse_mountinfo_entry() accordingly.
	strcpy(entry->optional_fields, "");
	for (;;) {
		char *opt_field = parse_next_string_field();
		if (opt_field == NULL)
			goto fail;
		if (strcmp(opt_field, "-") == 0) {
			break;
		}
		if (*entry->optional_fields) {
			strcat(entry->optional_fields, " ");
		}
		strcat(entry->optional_fields, opt_field);
	}
	if ((entry->fs_type = parse_next_string_field()) == NULL)
		goto fail;
	if ((entry->mount_source = parse_next_string_field()) == NULL)
		goto fail;
	if ((entry->super_opts = parse_next_string_field()) == NULL)
		goto fail;
	return entry;
 fail:
	free(entry);
	return NULL;
}

void cleanup_mountinfo(struct mountinfo **ptr)
{
	if (*ptr != NULL) {
		free_mountinfo(*ptr);
		*ptr = NULL;
	}
}

static void free_mountinfo(struct mountinfo *info)
{
	struct mountinfo_entry *entry, *next;
	for (entry = info->first; entry != NULL; entry = next) {
		next = entry->next;
		free_mountinfo_entry(entry);
	}
	free(info);
}

static void free_mountinfo_entry(struct mountinfo_entry *entry)
{
	free(entry);
}

static void cleanup_fclose(FILE ** ptr)
{
	fclose(*ptr);
}

static void cleanup_free(char **ptr)
{
	free(*ptr);
}
