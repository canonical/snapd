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
 * Type of functions called by sc_call_while_locked.
 **/
typedef void (*sc_locked_fn) (const char *scope);

/**
 * Call a list of functions while holding a scoped lock.
 *
 * The scope may be the name of a snap or NULL (global lock).  Each subsequent
 * argument is of type sc_locked_fn and gets called with the scope argument.
 *
 * The function guarantees that a filesystem lock is reliably acquired and
 * released on return or immediately upon process death.
 *
 * The actual lock is placed in "/run/snapd/ns" and is either called
 * "/run/snapd/ns/.lock" if scope is NULL or
 * "/run/snapd/ns/$scope.lock" otherwise.
 **/
__attribute__ ((sentinel))
void sc_call_while_locked(const char *scope, ...);

#endif				// SNAP_CONFINE_LOCKING_H
