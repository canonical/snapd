/*
 * Copyright (C) 2019 Canonical Ltd
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

#ifndef SC_SNAP_CONFINE_INVOCATION_H
#define SC_SNAP_CONFINE_INVOCATION_H

#include <stdbool.h>

#include "snap-confine-args.h"

/**
 * sc_invocation contains information about how snap-confine was invoked.
 *
 * All of the pointer fields have the life-cycle bound to the main process.
 **/
typedef struct sc_invocation {
    /* Things declared by the system. */
    char *snap_instance;
    char *base_snap_name;
    char *security_tag;
    char *executable;
    bool classic_confinement;
    /* Things derived at runtime. */
    bool is_normal_mode;
} sc_invocation;

/**
 * sc_init_invocation initializes the invocation object.
 *
 * Invocation is constructed based on command line arguments as well as
 * environment value (SNAP_INSTANCE_NAME). All input is untrusted and is
 * validated internally.
 **/
void sc_init_invocation(sc_invocation *inv, const struct sc_args *args, const char *snap_instance);

/**
 * sc_fini_invocation releases memory used by its constituents.
 **/
void sc_fini_invocation(sc_invocation *inv);

/**
 * sc_cleanup_invocation is a cleanup function for sc_invocation.
 *
 * Cleanup functions are automaticallty called by the compiler whenever a
 * variable gets out of scope, like C++ destructors would.
 *
 * This function is designed to be used with SC_CLEANUP(sc_cleanup_invocation).
 **/
void sc_cleanup_invocation(sc_invocation *inv);

#endif
