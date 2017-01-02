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

/**
 * Convert flags for mount(2) system call to a string representation. 
 *
 * The function uses an internal static buffer that is overwritten on each
 * request.
 **/
const char *sc_mount_opt2str(unsigned long flags);

/**
 * Compute an equivalent mount(8) command from mount(2) arguments.
 *
 * This function serves as a human-readable representation of the mount system
 * call. The return value is a string that looks like a shell mount command.
 *
 * The return value is dynamically allocated and must be released by the
 * caller.
 **/
char *sc_mount_cmd(const char *source, const char *target,
		   const char *filesystemtype, unsigned long mountflags,
		   const void *data);

#endif				// SNAP_CONFINE_MOUNT_OPT_H
