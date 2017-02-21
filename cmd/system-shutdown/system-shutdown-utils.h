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

#ifndef SYSTEM_SHUTDOWN_UTILS_H
#define SYSTEM_SHUTDOWN_UTILS_H

#include <stdbool.h>

// tries to umount all (well, most) things. Returns whether in the last pass it
// no longer found writable.
bool umount_all();

__attribute__ ((noreturn))
void die(const char *msg);
__attribute__ ((format(printf, 1, 2)))
void kmsg(const char *fmt, ...);

#endif
