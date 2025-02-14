/*
 * Copyright (C) 2025 Canonical Ltd
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

#ifndef SNAP_NAMESPACE_SUPPORT_PRIVATE_H
#define SNAP_NAMESPACE_SUPPORT_PRIVATE_H

#include <sys/types.h>

#include "ns-support.h"

void sc_set_ns_dir(const char *dir);

const char *sc_get_default_ns_dir(void);

struct sc_mount_ns {
    // Name of the namespace group ($SNAP_NAME).
    char *name;
    // Descriptor to the namespace group control directory.  This descriptor is
    // opened with O_PATH|O_DIRECTORY so it's only used for openat() calls.
    int dir_fd;
    // Pair of descriptors for a pair for a pipe file descriptors (read end,
    // write end) that snap-confine uses to send messages to the helper
    // process and back.
    int pipe_helper[2];
    int pipe_master[2];
    // Identifier of the child process that is used during the one-time (per
    // group) initialization and capture process.
    pid_t child;
};

struct sc_mount_ns *sc_mount_ns_new(void);

#endif
