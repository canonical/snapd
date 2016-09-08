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

#include "ns-support.h"
#include "ns-support.c"

#include "cleanup-funcs.h"

#include <errno.h>
#include <glib.h>
#include <glib/gstdio.h>

// Set alternate namespace directory
static void sc_set_ns_dir(const char *dir)
{
	sc_ns_dir = dir;
}

// Use temporary directory for namespace groups.
//
// The directory is automatically reset to the real value at the end of the
// test.
static const char *sc_test_use_fake_ns_dir()
{
	char *ns_dir = NULL;
	ns_dir = g_dir_make_tmp(NULL, NULL);
	g_assert_nonnull(ns_dir);
	g_test_queue_destroy((GDestroyNotify) sc_set_ns_dir, SC_NS_DIR);
	g_test_queue_free(ns_dir);
	sc_set_ns_dir(ns_dir);
	// TODO: queue something that rm -rf's the directory tree
	return ns_dir;
}

// Check that allocating a namespace group sets up internal data structures to
// safe values.
static void test_sc_alloc_ns_group()
{
	struct sc_ns_group *group = NULL;
	group = sc_alloc_ns_group();
	g_test_queue_free(group);
	g_assert_nonnull(group);
	g_assert_cmpint(group->dir_fd, ==, -1);
	g_assert_cmpint(group->lock_fd, ==, -1);
	g_assert_cmpint(group->event_fd, ==, -1);
	g_assert_cmpint(group->child, ==, 0);
	g_assert_cmpint(group->should_populate, ==, false);
	g_assert_null(group->name);
}

// Initialize a namespace group.
//
// The group is automatically destroyed at the end of the test.
static struct sc_ns_group *sc_test_open_ns_group(const char *group_name)
{
	// Initialize a namespace group
	struct sc_ns_group *group = NULL;
	if (group_name == NULL) {
		group_name = "test-group";
	}
	group = sc_open_ns_group(group_name);
	g_test_queue_destroy((GDestroyNotify) sc_close_ns_group, group);
	// Check if the returned group data looks okay
	g_assert_nonnull(group);
	g_assert_cmpint(group->dir_fd, !=, -1);
	g_assert_cmpint(group->lock_fd, !=, -1);
	g_assert_cmpint(group->event_fd, ==, -1);
	g_assert_cmpint(group->child, ==, 0);
	g_assert_cmpint(group->should_populate, ==, false);
	g_assert_cmpstr(group->name, ==, group_name);
	return group;
}

// Check that initializing a namespace group creates the appropriate
// filesystem structure and obtains open file descriptors for the lock.
static void test_sc_open_ns_group()
{
	const char *ns_dir = sc_test_use_fake_ns_dir();
	struct sc_ns_group *group = sc_test_open_ns_group(NULL);
	// Check that the group directory exists
	g_assert_true(g_file_test
		      (ns_dir, G_FILE_TEST_EXISTS | G_FILE_TEST_IS_DIR));
	// Check that the lock file exists
	char *lock_file __attribute__ ((cleanup(sc_cleanup_string))) = NULL;
	lock_file =
	    g_strdup_printf("%s/%s%s", ns_dir, group->name, SC_NS_LOCK_FILE);
	g_assert_true(g_file_test
		      (lock_file, G_FILE_TEST_EXISTS | G_FILE_TEST_IS_REGULAR));
}

// Check that locking a namespace actually flock's the mutex with LOCK_EX
static void test_sc_lock_unlock_ns_mutex()
{
	const char *ns_dir = sc_test_use_fake_ns_dir();
	struct sc_ns_group *group = sc_test_open_ns_group(NULL);
	// Lock the namespace group mutex
	sc_lock_ns_mutex(group);
	// Construct the name of the lock file
	char *lock_file __attribute__ ((cleanup(sc_cleanup_string))) = NULL;
	lock_file =
	    g_strdup_printf("%s/%s%s", ns_dir, group->name, SC_NS_LOCK_FILE);
	// Open the lock file again to obtain a separate file descriptor.
	// According to flock(2) locks are associated with an open file table entry
	// so this descriptor will be separate and can compete for the same lock.
	int lock_fd __attribute__ ((cleanup(sc_cleanup_close))) = -1;
	lock_fd = open(lock_file, O_RDWR | O_CLOEXEC | O_NOFOLLOW);
	g_assert_cmpint(lock_fd, !=, -1);
	// The non-blocking lock operation should fail with EWOULDBLOCK as the lock
	// file is locked by sc_nlock_ns_mutex() already.
	int err = flock(lock_fd, LOCK_EX | LOCK_NB);
	int saved_errno = errno;
	g_assert_cmpint(err, ==, -1);
	g_assert_cmpint(saved_errno, ==, EWOULDBLOCK);
	// Unlock the namespace group mutex
	sc_unlock_ns_mutex(group);
	// Re-attempt the locking operation. This time it should succeed.
	err = flock(lock_fd, LOCK_EX | LOCK_NB);
	g_assert_cmpint(err, ==, 0);
}

static void __attribute__ ((constructor)) init()
{
	g_test_add_func("/ns/sc_alloc_ns_group", test_sc_alloc_ns_group);
	g_test_add_func("/ns/sc_init_ns_group", test_sc_open_ns_group);
	g_test_add_func("/ns/sc_lock_unlock_ns_mutex",
			test_sc_lock_unlock_ns_mutex);
}
