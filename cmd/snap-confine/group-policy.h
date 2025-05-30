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

#ifndef SC_SNAP_CONFINE_GROUP_POLICY_H
#define SC_SNAP_CONFINE_GROUP_POLICY_H

#include <unistd.h>

#include "../libsnap-confine-private/error.h"

/**
 * Error domain for errors related to group policies.
 **/
#define SC_GROUP_DOMAIN "groups"

enum {
    /**
     * Error indicating that the user has no privileges to run snaps as per
     * local polcy.
     **/
    SC_NO_GROUP_PRIVS = 1,
};

/**
 * Assert optional local policy of regular users needing to be a being a member
 * of a specific group in order to run snaps.
 *
 * This involves peeking into the host filesystem to fstatat() the host's
 * snap-confine binary.
 */
bool sc_assert_host_local_group_policy(int root_fd, sc_error **errorp);

#endif /* SC_SNAP_CONFINE_GROUP_POLICY_H */
