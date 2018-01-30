/*
 * Copyright (C) 2016 Canonical Ltd
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

#ifndef SNAP_NAMESPACE_SUPPORT
#define SNAP_NAMESPACE_SUPPORT

#include <stdbool.h>

#include "apparmor-support.h"

/**
 * Re-associate the current process with the mount namespace of pid 1.
 *
 * This function inspects the mount namespace of the current process and that
 * of pid 1. In case they differ the current process is re-associated with the
 * mount namespace of pid 1.
 *
 * This function should be called before sc_initialize_ns_groups().
 **/
void sc_reassociate_with_pid1_mount_ns(void);

/**
 * Initialize namespace sharing.
 *
 * This function must be called once in each process that wishes to create or
 * join a namespace group.
 *
 * It is responsible for bind mounting the control directory over itself and
 * making it private (unsharing it with all the other peers) so that it can be
 * used for storing preserved namespaces as bind-mounted files from the nsfs
 * filesystem (namespace filesystem).
 *
 * This function should be called with a global lock (see sc_lock_global) held
 * to ensure that no other instance of snap-confine attempts to do this
 * concurrently.
 *
 * This function inspects /proc/self/mountinfo to determine if the directory
 * where namespaces are kept (/run/snapd/ns) is correctly prepared as described
 * above.
 *
 * For more details see namespaces(7).
 **/
void sc_initialize_ns_groups(void);

/**
 * Data required to manage namespaces amongst a group of processes.
 */
struct sc_ns_group;

enum {
	SC_NS_FAIL_GRACEFULLY = 1
};

/**
 * Open a namespace group.
 *
 * This will open and keep file descriptors for /run/snapd/ns/.
 *
 * If the flags argument is SC_NS_FAIL_GRACEFULLY then the function returns
 * NULL if the /run/snapd/ns directory doesn't exist. In all other cases it
 * calls die() and exits the process.
 *
 * The following methods should be called only while holding a lock protecting
 * that specific snap namespace:
 * - sc_create_or_join_ns_group()
 * - sc_should_populate_ns_group()
 * - sc_preserve_populated_ns_group()
 * - sc_discard_preserved_ns_group()
 */
struct sc_ns_group *sc_open_ns_group(const char *group_name,
				     const unsigned flags);

/**
 * Close namespace group.
 *
 * This will close all of the open file descriptors and release allocated memory.
 */
void sc_close_ns_group(struct sc_ns_group *group);

/**
 * Join the mount namespace associated with this group if one exists.
 *
 * Technically the function opens /run/snapd/ns/${group_name}.mnt and tries to
 * use setns() with the obtained file descriptor. If the call succeeds then the
 * function returns and subsequent call to sc_should_populate_ns_group() will
 * return false.
 *
 * If the call fails then an eventfd is constructed and a support process is
 * forked. The child process waits until data is written to the eventfd (this
 * can be done by calling sc_preserve_populated_ns_group()). In the meantime
 * the parent process unshares the mount namespace and sets a flag so that
 * sc_should_populate_ns_group() returns true.
 *
 * @returns 0 on success and EAGAIN if the namespace was stale and needs
 * to be re-made.
 **/
int sc_create_or_join_ns_group(struct sc_ns_group *group,
			       struct sc_apparmor *apparmor,
			       const char *base_snap_name,
			       const char *snap_name);

/**
 * Check if the namespace needs to be populated.
 *
 * If the return value is true then at this stage the namespace is already
 * unshared. The caller should perform any mount operations that are desired
 * and then proceed to call sc_preserve_populated_ns_group().
 **/
bool sc_should_populate_ns_group(struct sc_ns_group *group);

/**
 * Preserve prepared namespace group.
 *
 * This function signals the child support process for namespace capture to
 * perform the capture and shut down. It must be called after the call to
 * sc_create_or_join_ns_group() and only when sc_should_populate_ns_group()
 * returns true.
 *
 * Technically this function writes to an eventfd that causes the child process
 * to wake up, bind mount /proc/$ppid/ns/mnt to /run/snapd/ns/${group_name}.mnt
 * and then exit. The parent process (the caller) then collects the child
 * process and returns.
 **/
void sc_preserve_populated_ns_group(struct sc_ns_group *group);

/**
 * Discard the preserved namespace group.
 *
 * This function unmounts the bind-mounted files representing the kernel mount
 * namespace.
 **/
void sc_discard_preserved_ns_group(struct sc_ns_group *group);

#endif
