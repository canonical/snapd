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

#ifndef SC_SNAP_CONFINE_PRIVS_H
#define SC_SNAP_CONFINE_PRIVS_H

/* This header is included from various places so we need to check if they have
 * already defined _GNU_SOURCE. We're defining it because the corresponding C
 * source file includes <unistd.h> to get getresuid(2) but apparently merely
 * including <sys/types.h> here, without defining _GNU_SOURCE messes up
 * internal glibc headers enough that future definition of _GNU_SOURCE followed
 * by the inclusion of <unistd.h> doesn't define the required function. */
#ifndef _GNU_SOURCE
#define _GNU_SOURCE
#endif
#include <unistd.h>

#include <sys/types.h>

/**
 * sc_main_change_to_real_gid switches effective group ID to real ID.
 *
 * This reduces the surface area of snap-confine that runs with the root group
 * ID. This feature was done after snap-confine became set-group-id executable.
 **/
void sc_main_change_to_real_gid(gid_t effective_gid, gid_t real_gid);

/**
 * sc_main_temporarily_drop_to_user switches effective group and user IDs.
 *
 * The switch "lowers" permissions to that of the user calling snap-confine.
 * The permissions can be re-raised to perform privileged operations.
 **/
void sc_main_temporarily_drop_to_user(uid_t real_uid, gid_t real_gid);

/**
 * sc_main_temporarily_raise_to_root_gid raises effective group ID to root.
 *
 * This function needs to exist because bulk of snap-confine executes as the
 * real group ID.  Once sc_main_change_to_real_gid is removed this function and
 * the undo counterpart sc_main_temporarily_drop_from_root_gid can be removed.
 **/
void sc_main_temporarily_raise_to_root_gid(gid_t saved_gid);

/**
 * sc_main_temporarily_drop_from_root_gid drops effective group ID to real ID.
 *
 * The only purpose of this function is to undo changes made by
 * sc_main_temporarily_raise_to_root_gid.
 **/
void sc_main_temporarily_drop_from_root_gid(gid_t real_gid);

/**
 * sc_udev_raise_to_root_uid sets real user ID to root.
 *
 * This function is used prior to executing snap-device-helper from a forked
 * process. It only exists to ensure that the helper shell script runs as the
 * real root user. This is required to manipulate cgroups.
 **/
void sc_udev_raise_to_root_uid(void);

/**
 * sc_seccomp_temporarily_raise_to_root_uid raises effective user ID to root.
 *
 * This function needs to exist because sc_apply_seccomp_filter, which is
 * called by sc_apply_seccomp_profile_for_security_tag and by
 * sc_apply_global_seccomp_profile are both executed after
 * sc_main_temporarily_drop_to_user.
 *
 *  Once the call sequence is adjusted so that that part of snap-confine
 *  executes as root the pair of functions (along with
 *  sc_seccomp_temporarily_raise_to_root_uid) can be discarded.
 **/
void sc_seccomp_temporarily_raise_to_root_uid(uid_t saved_uid, uid_t effective_uid);

/**
 * sc_main_temporarily_drop_from_root_uid drops effective user ID to real ID.
 *
 * The only purpose of this function is to undo changes made by
 * sc_main_temporarily_raise_to_root_uid.
 **/
void sc_seccomp_temporarily_drop_from_root_uid(void);

/**
 * sc_main_permanently_drop_to_user switches to given user and group.
 *
 * The switch is permanent because we set effective, real and saved the ID.
 * After this call snap-confine can no longer perform privileged operations.
 **/
void sc_main_permanently_drop_to_user(uid_t real_uid, gid_t real_gid);

#endif
