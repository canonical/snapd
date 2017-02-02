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

#ifndef SNAP_CONFINE_MOUNTINFO_H
#define SNAP_CONFINE_MOUNTINFO_H

/**
 * Structure describing entire /proc/self/sc_mountinfo file
 **/
struct sc_mountinfo;

/**
 * Structure describing a single entry in /proc/self/sc_mountinfo
 **/
struct sc_mountinfo_entry;

/**
 * Parse a file in according to sc_mountinfo syntax.
 *
 * The argument can be used to parse an arbitrary file.  NULL can be used to
 * implicitly parse /proc/self/sc_mountinfo, that is the mount information
 * associated with the current process.
 **/
struct sc_mountinfo *sc_parse_mountinfo(const char *fname);

/**
 * Free a sc_mountinfo structure.
 *
 * This function is designed to be used with __attribute__((cleanup)) so it
 * takes a pointer to the freed object (which is also a pointer).
 **/
void sc_cleanup_mountinfo(struct sc_mountinfo **ptr)
    __attribute__ ((nonnull(1)));

/**
 * Get the first sc_mountinfo entry.
 *
 * The returned value may be NULL if the parsed file contained no entries. The
 * returned value is bound to the lifecycle of the whole sc_mountinfo structure
 * and should not be freed explicitly.
 **/
struct sc_mountinfo_entry *sc_first_mountinfo_entry(struct sc_mountinfo *info)
    __attribute__ ((nonnull(1)));

/**
 * Get the next sc_mountinfo entry.
 *
 * The returned value is a pointer to the next sc_mountinfo entry or NULL if this
 * was the last entry. The returned value is bound to the lifecycle of the
 * whole sc_mountinfo structure and should not be freed explicitly.
 **/
struct sc_mountinfo_entry *sc_next_mountinfo_entry(struct sc_mountinfo_entry
						   *entry)
    __attribute__ ((nonnull(1)));

/**
 * Get the mount identifier of a given mount entry.
 **/
int sc_mountinfo_entry_mount_id(struct sc_mountinfo_entry *entry)
    __attribute__ ((nonnull(1)));

/**
 * Get the parent mount identifier of a given mount entry.
 **/
int sc_mountinfo_entry_parent_id(struct sc_mountinfo_entry *entry)
    __attribute__ ((nonnull(1)));

unsigned sc_mountinfo_entry_dev_major(struct sc_mountinfo_entry *entry)
    __attribute__ ((nonnull(1)));

unsigned sc_mountinfo_entry_dev_minor(struct sc_mountinfo_entry *entry)
    __attribute__ ((nonnull(1)));

/**
 * Get the root directory of a given mount entry.
 **/
const char *sc_mountinfo_entry_root(struct sc_mountinfo_entry *entry)
    __attribute__ ((nonnull(1)));

/**
 * Get the mount point of a given mount entry.
 **/
const char *sc_mountinfo_entry_mount_dir(struct sc_mountinfo_entry *entry)
    __attribute__ ((nonnull(1)));

/**
 * Get the mount options of a given mount entry.
 **/
const char *sc_mountinfo_entry_mount_opts(struct sc_mountinfo_entry *entry)
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
const char *sc_mountinfo_entry_optional_fields(struct sc_mountinfo_entry *entry)
    __attribute__ ((nonnull(1)));

/**
 * Get the file system type of a given mount entry.
 **/
const char *sc_mountinfo_entry_fs_type(struct sc_mountinfo_entry *entry)
    __attribute__ ((nonnull(1)));

/**
 * Get the source of a given mount entry.
 **/
const char *sc_mountinfo_entry_mount_source(struct sc_mountinfo_entry *entry)
    __attribute__ ((nonnull(1)));

/**
 * Get the super block options of a given mount entry.
 **/
const char *sc_mountinfo_entry_super_opts(struct sc_mountinfo_entry *entry)
    __attribute__ ((nonnull(1)));

#endif
