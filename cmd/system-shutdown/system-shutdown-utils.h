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
#include <stddef.h>  // size_t

// tries to umount all (well, most) things. Returns whether in the last pass it
// no longer found writable.
bool umount_all(void);

__attribute__((format(printf, 1, 2))) void kmsg(const char *fmt, ...);

// Reads a possible argument for reboot syscall in /run/systemd/reboot-param,
// which is the place where systemd stores it.
int sc_read_reboot_arg(char *arg, size_t max_size);

#endif
