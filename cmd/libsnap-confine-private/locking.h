/*
 * Copyright (C) 2017-2018 Canonical Ltd
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
#ifndef SNAP_CONFINE_LOCKING_H
#define SNAP_CONFINE_LOCKING_H

// Include config.h which pulls in _GNU_SOURCE which in turn allows sys/types.h
// to define O_PATH. Since locking.h is included from locking.c this is
// required to see O_PATH there.
#ifdef HAVE_CONFIG_H
#include "config.h"
#endif

#include <sys/types.h>

/**
 * Obtain a flock-based, exclusive, globally scoped, lock.
 *
 * The actual lock is placed in "/run/snap/ns/.lock"
 *
 * If the lock cannot be acquired for three seconds (via
 * sc_enable_sanity_timeout) then the function fails and the process dies.
 *
 * The return value needs to be passed to sc_unlock(), there is no need to
 * check for errors as the function will die() on any problem.
 **/
int sc_lock_global(void);

/**
 * Obtain a flock-based, exclusive, snap-scoped, lock.
 *
 * The actual lock is placed in "/run/snapd/ns/$SNAP_NAME.lock"
 * It should be acquired only when holding the global lock.
 *
 * If the lock cannot be acquired for three seconds (via
 * sc_enable_sanity_timeout) then the function fails and the process dies.
 *
 * The return value needs to be passed to sc_unlock(), there is no need to
 * check for errors as the function will die() on any problem.
 **/
int sc_lock_snap(const char *snap_name);

/**
 * Verify that a flock-based, exclusive, snap-scoped, lock is held.
 *
 * If the lock is not held the process dies. The details about the lock
 * are exactly the same as for sc_lock_snap().
 **/
void sc_verify_snap_lock(const char *snap_name);

/**
 * Obtain a flock-based, exclusive, snap-scoped, lock.
 *
 * The actual lock is placed in "/run/snapd/ns/$SNAP_NAME.$UID.lock"
 * It should be acquired only when holding the snap-specific lock.
 *
 * If the lock cannot be acquired for three seconds (via
 * sc_enable_sanity_timeout) then the function fails and the process dies.
 * The return value needs to be passed to sc_unlock(), there is no need to
 * check for errors as the function will die() on any problem.
 **/
int sc_lock_snap_user(const char *snap_name, uid_t uid);

/**
 * Release a flock-based lock.
 *
 * All kinds of locks can be unlocked the same way. This function simply
 * unlocks the lock and closes the file descriptor.
 **/
void sc_unlock(int lock_fd);

/**
 * Enable a sanity-check timeout.
 *
 * The timeout is based on good-old alarm(2) and is intended to break a
 * suspended system call, such as flock, after a few seconds. The built-in
 * timeout is primed for three seconds. After that any sleeping system calls
 * are interrupted and a flag is set.
 *
 * The call should be paired with sc_disable_sanity_check_timeout() that
 * disables the alarm and acts on the flag, aborting the process if the timeout
 * gets exceeded.
 **/
void sc_enable_sanity_timeout(void);

/**
 * Disable sanity-check timeout and abort the process if it expired.
 *
 * This call has to be paired with sc_enable_sanity_timeout(), see the function
 * description for more details.
 **/
void sc_disable_sanity_timeout(void);

#endif  // SNAP_CONFINE_LOCKING_H
