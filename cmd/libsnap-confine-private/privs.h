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

#ifndef SNAP_CONFINE_PRIVS_H
#define SNAP_CONFINE_PRIVS_H

#include <stdint.h>

/**
 * Permanently drop elevated permissions.
 *
 * If the user has elevated permission as a result of running a setuid root
 * application then such permission are permanently dropped.
 *
 * The set of dropped permissions include:
 *  - user and group identifier
 *  - supplementary group identifiers
 *
 * The function ensures that the elevated permission are dropped or dies if
 * this cannot be achieved. Note that only the elevated permissions are
 * dropped. When the process itself was started by root then this function does
 * nothing at all.
 **/
void sc_privs_drop(void);

/**
 * sc_cap_mask is the type we use to store a mask of capabilities.
 *
 * It works similar to the masks defined in the cap_user_data_t structure used
 * by capset(), except that it is a 64 bit one and therefore can accommodate
 * all currently defined capabilities. At the moment all capabilities used by
 * snap-confine are anyway located in the lower 32 bits, but we try to be open
 * to future changes. */
typedef uint64_t sc_cap_mask;
#define SC_CAP_TO_MASK(cap) ((sc_cap_mask)1 << cap)

typedef struct sc_capabilities {
    sc_cap_mask effective;
    sc_cap_mask permitted;
    sc_cap_mask inheritable;
} sc_capabilities;

/**
 * Set the given capabilities on the current process.
 */
void sc_set_capabilities(const sc_capabilities *capabilities);

#endif
