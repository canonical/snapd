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
 * This function should be called before sc_initialize_mount_ns().
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
void sc_initialize_mount_ns(void);

/**
 * Data required to manage namespaces amongst a group of processes.
 */
struct sc_mount_ns;

/**
 * Open a namespace group.
 *
 * This will open and keep file descriptors for /run/snapd/ns/.
 *
 * The following methods should be called only while holding a lock protecting
 * that specific snap namespace:
 * - sc_create_or_join_mount_ns()
 * - sc_should_populate_mount_ns()
 * - sc_preserve_populated_mount_ns()
 * - sc_discard_preserved_mount_ns()
 */
struct sc_mount_ns *sc_open_mount_ns(const char *group_name);

/**
 * Close namespace group.
 *
 * This will close all of the open file descriptors and release allocated memory.
 */
void sc_close_mount_ns(struct sc_mount_ns *group);

/**
 * Join a preserved mount namespace if one exists.
 *
 * Technically the function opens /run/snapd/ns/${group_name}.mnt and tries to
 * use setns() with the obtained file descriptor. If the call succeeds then the
 * function returns and subsequent call to sc_should_populate_mount_ns() will
 * return false.
 *
 * If the preserved mount namespace does not exist or exists but is stale and
 * was discarded and returns ESRCH. If the mount namespace was joined the
 * function returns zero.
 **/
int sc_join_preserved_ns(struct sc_mount_ns *group, struct sc_apparmor
			 *apparmor, const char *base_snap_name,
			 const char *snap_name);

/**
 * Check if the namespace needs to be populated.
 *
 * If the return value is true then at this stage the namespace is already
 * unshared. The caller should perform any mount operations that are desired
 * and then proceed to call sc_preserve_populated_mount_ns().
 **/
bool sc_should_populate_mount_ns(struct sc_mount_ns *group);

/**
 * Fork off a helper process for mount namespace capture.
 *
 * This function forks the helper process. It needs to be paired with
 * sc_wait_for_helper which instructs the helper to shut down and waits for
 * that to happen.
 *
 * For rationale for forking and using a helper process please see
 * https://lists.linuxfoundation.org/pipermail/containers/2013-August/033386.html
 **/
void sc_fork_helper(struct sc_mount_ns *group, struct sc_apparmor *apparmor);

/**
 * Preserve prepared namespace group.
 *
 * This function signals the child support process for namespace capture to
 * perform the capture.
 *
 * Technically this function writes to pipe that causes the child process to
 * wake up and bind mount /proc/$ppid/ns/mnt to
 * /run/snapd/ns/${group_name}.mnt.
 *
 * The helper process will wait for subsequent commands. Please call
 * sc_wait_for_helper() to terminate it.
 **/
void sc_preserve_populated_mount_ns(struct sc_mount_ns *group);

/**
 * Ask the helper process to terminate and wait for it to finish.
 *
 * This function asks the helper process to exit by writing an appropriate
 * command to the pipe used for the inter process communication between the
 * main snap-confine process and the helper and then waits for the process to
 * terminate cleanly.
 **/
void sc_wait_for_helper(struct sc_mount_ns *group);

/**
 * Discard the preserved namespace group.
 *
 * This function unmounts the bind-mounted files representing the kernel mount
 * namespace.
 **/
void sc_discard_preserved_mount_ns(struct sc_mount_ns *group);

#endif
