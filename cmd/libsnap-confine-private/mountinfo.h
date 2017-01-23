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

#ifndef SC_MOUNTINFO_H
#define SC_MOUNTINFO_H

/**
 * Structure describing entire /proc/self/mountinfo file
 **/
struct mountinfo;

/**
 * Structure describing a single entry in /proc/self/mountinfo
 **/
struct mountinfo_entry;

/**
 * Parse a file in according to mountinfo syntax.
 *
 * The argument can be used to parse an arbitrary file.  NULL can be used to
 * implicitly parse /proc/self/mountinfo, that is the mount information
 * associated with the current process.
 **/
struct mountinfo *parse_mountinfo(const char *fname);

/**
 * Free a mountinfo structure.
 *
 * This function is designed to be used with __attribute__((cleanup)) so it
 * takes a pointer to the freed object (which is also a pointer).
 **/
void cleanup_mountinfo(struct mountinfo **ptr) __attribute__ ((nonnull(1)));

/**
 * Get the first mountinfo entry.
 *
 * The returned value may be NULL if the parsed file contained no entries. The
 * returned value is bound to the lifecycle of the whole mountinfo structure
 * and should not be freed explicitly.
 **/
struct mountinfo_entry *first_mountinfo_entry(struct mountinfo *info)
    __attribute__ ((nonnull(1)));

/**
 * Get the next mountinfo entry.
 *
 * The returned value is a pointer to the next mountinfo entry or NULL if this
 * was the last entry. The returned value is bound to the lifecycle of the
 * whole mountinfo structure and should not be freed explicitly.
 **/
struct mountinfo_entry *next_mountinfo_entry(struct mountinfo_entry
					     *entry)
    __attribute__ ((nonnull(1)));

/**
 * Get the mount identifier of a given mount entry.
 **/
int mountinfo_entry_mount_id(struct mountinfo_entry *entry)
    __attribute__ ((nonnull(1)));

/**
 * Get the parent mount identifier of a given mount entry.
 **/
int mountinfo_entry_parent_id(struct mountinfo_entry *entry)
    __attribute__ ((nonnull(1)));

unsigned mountinfo_entry_dev_major(struct mountinfo_entry *entry)
    __attribute__ ((nonnull(1)));

unsigned mountinfo_entry_dev_minor(struct mountinfo_entry *entry)
    __attribute__ ((nonnull(1)));

/**
 * Get the root directory of a given mount entry.
 **/
const char *mountinfo_entry_root(struct mountinfo_entry *entry)
    __attribute__ ((nonnull(1)));

/**
 * Get the mount point of a given mount entry.
 **/
const char *mountinfo_entry_mount_dir(struct mountinfo_entry *entry)
    __attribute__ ((nonnull(1)));

/**
 * Get the mount options of a given mount entry.
 **/
const char *mountinfo_entry_mount_opts(struct mountinfo_entry *entry)
    __attribute__ ((nonnull(1)));

/**
 * Get optional tagged data associated of a given mount entry.
 *
 * The return value is a string (possibly empty but never NULL) in the format
 * tag[:value]. Known tags are:
 *
 * "shared:X":
 * 		mount is shared in peer group X
 * "master:X":
 * 		mount is slave to peer group X
 * "propagate_from:X"
 * 		mount is slave and receives propagation from peer group X (*)
 * "unbindable":
 * 		mount is unbindable
 *
 * (*) X is the closest dominant peer group under the process's root.
 * If X is the immediate master of the mount, or if there's no dominant peer
 * group under the same root, then only the "master:X" field is present and not
 * the "propagate_from:X" field.
 **/
const char *mountinfo_entry_optional_fields(struct mountinfo_entry *entry)
    __attribute__ ((nonnull(1)));

/**
 * Get the file system type of a given mount entry.
 **/
const char *mountinfo_entry_fs_type(struct mountinfo_entry *entry)
    __attribute__ ((nonnull(1)));

/**
 * Get the source of a given mount entry.
 **/
const char *mountinfo_entry_mount_source(struct mountinfo_entry *entry)
    __attribute__ ((nonnull(1)));

/**
 * Get the super block options of a given mount entry.
 **/
const char *mountinfo_entry_super_opts(struct mountinfo_entry *entry)
    __attribute__ ((nonnull(1)));

#endif
