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
 *
 */

#ifndef SNAP_CONFINE_MOUNT_OPT_H
#define SNAP_CONFINE_MOUNT_OPT_H

#include <stdbool.h>
#include <stddef.h>

/**
 * Convert flags for mount(2) system call to a string representation.
 **/
const char *sc_mount_opt2str(char *buf, size_t buf_size, unsigned long flags);

/**
 * Compute an equivalent mount(8) command from mount(2) arguments.
 *
 * This function serves as a human-readable representation of the mount system
 * call. The return value is a string that looks like a shell mount command.
 *
 * Note that the returned command is may not be a valid mount command. No
 * sanity checking is performed on the mount flags, source or destination
 * arguments.
 *
 * The returned value is always buf, it is provided as a convenience.
 **/
const char *sc_mount_cmd(char *buf, size_t buf_size, const char *source, const char *target, const char *fs_type,
                         unsigned long mountflags, const void *data);

/**
 * Compute an equivalent umount(8) command from umount2(2) arguments.
 *
 * This function serves as a human-readable representation of the unmount
 * system call. The return value is a string that looks like a shell unmount
 * command.
 *
 * Note that some flags are not surfaced at umount command line level. For
 * those flags a fake option is synthesized.
 *
 * Note that the returned command is may not be a valid umount command. No
 * sanity checking is performed on the mount flags, source or destination
 * arguments.
 *
 * The returned value is always buf, it is provided as a convenience.
 **/
const char *sc_umount_cmd(char *buf, size_t buf_size, const char *target, int flags);

/**
 * A thin wrapper around mount(2) with logging and error checks.
 **/
void sc_do_mount(const char *source, const char *target, const char *fs_type, unsigned long mountflags,
                 const void *data);

/**
 * A thin wrapper around mount(2) with logging and error checks.
 *
 * This variant is allowed to silently fail when mount fails with ENOENT.
 * That is, it can be used to perform mount operations and if either the source
 * or the destination is not present, carry on as if nothing had happened.
 *
 * The return value indicates if the operation was successful or not.
 **/
bool sc_do_optional_mount(const char *source, const char *target, const char *fs_type, unsigned long mountflags,
                          const void *data);

/**
 * A thin wrapper around umount(2) with logging and error checks.
 **/
void sc_do_umount(const char *target, int flags);

#endif  // SNAP_CONFINE_MOUNT_OPT_H
