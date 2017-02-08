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

#include <stddef.h>

/**
 * Convert flags for mount(2) system call to a string representation. 
 *
 * The function uses an internal static buffer that is overwritten on each
 * request.
 **/
const char *sc_mount_opt2str(char *buf, size_t buf_size, unsigned long flags);

#endif				// SNAP_CONFINE_MOUNT_OPT_H
