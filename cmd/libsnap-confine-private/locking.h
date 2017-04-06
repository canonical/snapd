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
#ifndef SNAP_CONFINE_LOCKING_H
#define SNAP_CONFINE_LOCKING_H

/**
 * Obtain a flock-based, exclusive lock.
 *
 * The scope may be the name of a snap or NULL (global lock).  Each subsequent
 * argument is of type sc_locked_fn and gets called with the scope argument.
 * The function guarantees that a filesystem lock is reliably acquired and
 * released on call to sc_unlock() immediately upon process death.
 *
 * The actual lock is placed in "/run/snapd/ns" and is either called
 * "/run/snapd/ns/.lock" if scope is NULL or
 * "/run/snapd/ns/$scope.lock" otherwise.
 *
 * If the lock cannot be acquired for three seconds (via
 * sc_enable_sanity_timeout) then the function fails and the process dies.
 *
 * The return value needs to be passed to sc_unlock(), there is no need to
 * check for errors as the function will die() on any problem.
 **/
int sc_lock(const char *scope);

/**
 * Release a flock-based lock.
 *
 * This function simply unlocks the lock and closes the file descriptor.
 **/
void sc_unlock(const char *scope, int lock_fd);

/**
 * Obtain a flock-based, exclusive, globally scoped, lock.
 *
 * This function is exactly like sc_lock(NULL), that is the acquired lock is
 * not specific to any snap but global.
 **/
int sc_lock_global();

/**
 * Release a flock-based, globally scoped, lock
 *
 * This function is exactly like sc_unlock(NULL, lock_fd).
 **/
void sc_unlock_global(int lock_fd);

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
void sc_enable_sanity_timeout();

/**
 * Disable sanity-check timeout and abort the process if it expired.
 *
 * This call has to be paired with sc_enable_sanity_timeout(), see the function
 * description for more details.
 **/
void sc_disable_sanity_timeout();

#endif				// SNAP_CONFINE_LOCKING_H
