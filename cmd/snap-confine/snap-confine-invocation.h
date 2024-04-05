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
    char *snap_instance; /* snap instance name (<snap>_<key>) */
    char *snap_name;     /* snap name (without instance key) */
    char *orig_base_snap_name;
    char *security_tag;
    char *executable;
    bool classic_confinement;
    /* Things derived at runtime. */
    char *base_snap_name;
    char *rootfs_dir;
    char **homedirs;
    int num_homedirs;
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
 * sc_cleanup_invocation is a cleanup function for sc_invocation.
 *
 * Cleanup functions are automatically called by the compiler whenever a
 * variable gets out of scope, like C++ destructors would.
 *
 * This function is designed to be used with SC_CLEANUP(sc_cleanup_invocation).
 **/
void sc_cleanup_invocation(sc_invocation *inv);

/**
 * sc_check_rootfs_dir checks the rootfs_dir and applies potential fall-backs.
 *
 * Checks that the rootfs_dir for the given base_snap exists and may apply
 * the fallback logic below. Will die() if no base_snap can be found.
 *
 * When performing ubuntu-core to core migration, the  snap "core" may not be
 * mounted yet. In that mode when snapd instructs us to use "core" as the base
 * snap name snap-confine may choose to transparently fallback to "ubuntu-core"
 * it that is available instead.
 *
 * This check must be performed in the regular mount namespace (that is, that
 * of the init process) because it relies on the value of compile-time-choice
 * of SNAP_MOUNT_DIR.
 **/
void sc_check_rootfs_dir(sc_invocation *inv);

/**
 * sc_invocation_init_homedirs() reads the homedirs configuration
 * file of snapd and fills the "homedirs" string vector in the
 * sc_invocation structure.
 */
void sc_invocation_init_homedirs(sc_invocation *inv);

#endif
