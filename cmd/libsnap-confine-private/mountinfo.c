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

#include "cleanup-funcs.h"

struct sc_mountinfo {
	struct sc_mountinfo_entry *first;
};

struct sc_mountinfo_entry {
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

	struct sc_mountinfo_entry *next;
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
static struct sc_mountinfo_entry *sc_parse_mountinfo_entry(const char *line)
    __attribute__ ((nonnull(1)));

/**
 * Free a sc_mountinfo structure and all its entries.
 **/
static void sc_free_mountinfo(struct sc_mountinfo *info)
    __attribute__ ((nonnull(1)));

/**
 * Free a sc_mountinfo entry.
 **/
static void sc_free_mountinfo_entry(struct sc_mountinfo_entry *entry)
    __attribute__ ((nonnull(1)));

struct sc_mountinfo_entry *sc_first_mountinfo_entry(struct sc_mountinfo *info)
{
	return info->first;
}

struct sc_mountinfo_entry *sc_next_mountinfo_entry(struct sc_mountinfo_entry
						   *entry)
{
	return entry->next;
}

int sc_mountinfo_entry_mount_id(struct sc_mountinfo_entry *entry)
{
	return entry->mount_id;
}

int sc_mountinfo_entry_parent_id(struct sc_mountinfo_entry *entry)
{
	return entry->parent_id;
}

unsigned sc_mountinfo_entry_dev_major(struct sc_mountinfo_entry *entry)
{
	return entry->dev_major;
}

unsigned sc_mountinfo_entry_dev_minor(struct sc_mountinfo_entry *entry)
{
	return entry->dev_minor;
}

const char *sc_mountinfo_entry_root(struct sc_mountinfo_entry *entry)
{
	return entry->root;
}

const char *sc_mountinfo_entry_mount_dir(struct sc_mountinfo_entry *entry)
{
	return entry->mount_dir;
}

const char *sc_mountinfo_entry_mount_opts(struct sc_mountinfo_entry *entry)
{
	return entry->mount_opts;
}

const char *sc_mountinfo_entry_optional_fields(struct sc_mountinfo_entry *entry)
{
	return entry->optional_fields;
}

const char *sc_mountinfo_entry_fs_type(struct sc_mountinfo_entry *entry)
{
	return entry->fs_type;
}

const char *sc_mountinfo_entry_mount_source(struct sc_mountinfo_entry *entry)
{
	return entry->mount_source;
}

const char *sc_mountinfo_entry_super_opts(struct sc_mountinfo_entry *entry)
{
	return entry->super_opts;
}

struct sc_mountinfo *sc_parse_mountinfo(const char *fname)
{
	struct sc_mountinfo *info = calloc(1, sizeof *info);
	if (info == NULL) {
		return NULL;
	}
	if (fname == NULL) {
		fname = "/proc/self/mountinfo";
	}
	FILE *f __attribute__ ((cleanup(sc_cleanup_file))) = NULL;
	f = fopen(fname, "rt");
	if (f == NULL) {
		free(info);
		return NULL;
	}
	char *line __attribute__ ((cleanup(sc_cleanup_string))) = NULL;
	size_t line_size = 0;
	struct sc_mountinfo_entry *entry, *last = NULL;
	for (;;) {
		errno = 0;
		if (getline(&line, &line_size, f) == -1) {
			if (errno != 0) {
				sc_free_mountinfo(info);
				return NULL;
			}
			break;
		};
		entry = sc_parse_mountinfo_entry(line);
		if (entry == NULL) {
			sc_free_mountinfo(info);
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

static struct sc_mountinfo_entry *sc_parse_mountinfo_entry(const char *line)
{
	// NOTE: the sc_mountinfo structure is allocated along with enough extra
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
	//
	// At the end of the parsing process, the input buffer (line) and the
	// output buffer (entry->line_buf) are the same except for where spaces
	// were converted into NUL bytes (string terminators) and except for the
	// leading part of the buffer that contains mount_id, parent_id, dev_major
	// and dev_minor integer fields that are parsed separately.
	//
	// If MOUNTINFO_DEBUG is defined then extra debugging is printed to stderr
	// and this allows for visual analysis of what is going on.
	struct sc_mountinfo_entry *entry =
	    calloc(1, sizeof *entry + strlen(line) + 1);
	if (entry == NULL) {
		return NULL;
	}
#ifdef MOUNTINFO_DEBUG
	// Poison the buffer with '\1' bytes that are printed as '#' characters
	// by show_buffers() below. This is "unaltered" memory.
	memset(entry->line_buf, 1, strlen(line));
#endif				// MOUNTINFO_DEBUG
	int nscanned;
	int offset_delta, offset = 0;
	nscanned = sscanf(line, "%d %d %u:%u %n",
			  &entry->mount_id, &entry->parent_id,
			  &entry->dev_major, &entry->dev_minor, &offset_delta);
	if (nscanned != 4)
		goto fail;
	offset += offset_delta;

	void show_buffers() {
#ifdef MOUNTINFO_DEBUG
		fprintf(stderr, "Input buffer (first), with offset arrow\n");
		fprintf(stderr, "Output buffer (second)\n");

		fputc(' ', stderr);
		for (int i = 0; i < offset - 1; ++i)
			fputc('-', stderr);
		fputc('v', stderr);
		fputc('\n', stderr);

		fprintf(stderr, ">%s<\n", line);

		fputc('>', stderr);
		for (int i = 0; i < strlen(line); ++i) {
			int c = entry->line_buf[i];
			fputc(c == 0 ? '@' : c == 1 ? '#' : c, stderr);
		}
		fputc('<', stderr);
		fputc('\n', stderr);

		fputc('>', stderr);
		for (int i = 0; i < strlen(line); ++i)
			fputc('=', stderr);
		fputc('<', stderr);
		fputc('\n', stderr);
#endif				// MOUNTINFO_DEBUG
	}

	show_buffers();

	char *parse_next_string_field() {
		char *field = &entry->line_buf[0] + offset;
		int nscanned =
		    sscanf(line + offset, "%s %n", field, &offset_delta);
		if (nscanned != 1)
			return NULL;
		offset += offset_delta;
		show_buffers();
		return field;
	}
	if ((entry->root = parse_next_string_field()) == NULL)
		goto fail;
	if ((entry->mount_dir = parse_next_string_field()) == NULL)
		goto fail;
	if ((entry->mount_opts = parse_next_string_field()) == NULL)
		goto fail;
	entry->optional_fields = &entry->line_buf[0] + offset;
	// NOTE: This ensures that optional_fields is never NULL. If this changes,
	// must adjust all callers of parse_mountinfo_entry() accordingly.
	char *to = entry->optional_fields;
	for (int field_num = 0;; ++field_num) {
		char *opt_field = parse_next_string_field();
		if (opt_field == NULL)
			goto fail;
		if (strcmp(opt_field, "-") == 0) {
			opt_field[0] = 0;
			break;
		}
		if (field_num > 0) {
			opt_field[-1] = ' ';
		}
	}
	if ((entry->fs_type = parse_next_string_field()) == NULL)
		goto fail;
	if ((entry->mount_source = parse_next_string_field()) == NULL)
		goto fail;
	if ((entry->super_opts = parse_next_string_field()) == NULL)
		goto fail;
	show_buffers();
	return entry;
 fail:
	free(entry);
	return NULL;
}

void sc_cleanup_mountinfo(struct sc_mountinfo **ptr)
{
	if (*ptr != NULL) {
		sc_free_mountinfo(*ptr);
		*ptr = NULL;
	}
}

static void sc_free_mountinfo(struct sc_mountinfo *info)
{
	struct sc_mountinfo_entry *entry, *next;
	for (entry = info->first; entry != NULL; entry = next) {
		next = entry->next;
		sc_free_mountinfo_entry(entry);
	}
	free(info);
}

static void sc_free_mountinfo_entry(struct sc_mountinfo_entry *entry)
{
	free(entry);
}
