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

#ifndef SNAPD_CMD_SNAP_UPDATE_NS_H
#define SNAPD_CMD_SNAP_UPDATE_NS_H

#define _GNU_SOURCE

#include <unistd.h>

extern int bootstrap_errno;
extern const char* bootstrap_msg;

void bootstrap(void);
ssize_t read_cmdline(char* buf, size_t buf_size);
const char* find_snap_name(char* buf, size_t buf_size, size_t num_read);
int partially_validate_snap_name(const char* snap_name);

#endif
