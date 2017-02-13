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

#include "../libsnap-confine-private/cleanup-funcs.h"

#include <errno.h>
#include <linux/magic.h>	// for NSFS_MAGIC
#include <sys/utsname.h>
#include <sys/vfs.h>

#include <glib.h>
#include <glib/gstdio.h>

// Set alternate namespace directory
static void sc_set_ns_dir(const char *dir)
{
	sc_ns_dir = dir;
}

// Shell-out to "rm -rf -- $dir" as long as $dir is in /tmp.
static void rm_rf_tmp(const char *dir)
{
	// Sanity check, don't remove anything that's not in the temporary
	// directory. This is here to prevent unintended data loss.
	if (!g_str_has_prefix(dir, "/tmp/"))
		die("refusing to remove: %s", dir);
	const gchar *working_directory = NULL;
	gchar **argv = NULL;
	gchar **envp = NULL;
	GSpawnFlags flags = G_SPAWN_SEARCH_PATH;
	GSpawnChildSetupFunc child_setup = NULL;
	gpointer user_data = NULL;
	gchar **standard_output = NULL;
	gchar **standard_error = NULL;
	gint exit_status = 0;
	GError *error = NULL;

	argv = calloc(5, sizeof *argv);
	if (argv == NULL)
		die("cannot allocate command argument array");
	argv[0] = g_strdup("rm");
	if (argv[0] == NULL)
		die("cannot allocate memory");
	argv[1] = g_strdup("-rf");
	if (argv[1] == NULL)
		die("cannot allocate memory");
	argv[2] = g_strdup("--");
	if (argv[2] == NULL)
		die("cannot allocate memory");
	argv[3] = g_strdup(dir);
	if (argv[3] == NULL)
		die("cannot allocate memory");
	argv[4] = NULL;
	g_assert_true(g_spawn_sync
		      (working_directory, argv, envp, flags, child_setup,
		       user_data, standard_output, standard_error, &exit_status,
		       &error));
	g_assert_true(g_spawn_check_exit_status(exit_status, NULL));
	if (error != NULL) {
		g_test_message("cannot remove temporary directory: %s\n",
			       error->message);
		g_error_free(error);
	}
	g_free(argv[0]);
	g_free(argv[1]);
	g_free(argv[2]);
	g_free(argv[3]);
	g_free(argv);
}

// Check that rm_rf_tmp doesn't remove things outside of /tmp
static void test_rm_rf_tmp()
{
	if (access("/nonexistent", F_OK) == 0) {
		g_test_message
		    ("/nonexistent exists but this test doesn't want it to");
		g_test_fail();
		return;
	}
	if (g_test_subprocess()) {
		rm_rf_tmp("/nonexistent");
		return;
	}
	g_test_trap_subprocess(NULL, 0, 0);
	g_test_trap_assert_failed();
}

// Use temporary directory for namespace groups.
//
// The directory is automatically reset to the real value at the end of the
// test.
static const char *sc_test_use_fake_ns_dir()
{
	char *ns_dir = NULL;
	if (g_test_subprocess()) {
		// Check if the environment variable is set. If so then someone is already
		// managing the temporary directory and we should not create a new one.
		ns_dir = getenv("SNAP_CONFINE_NS_DIR");
		g_assert_nonnull(ns_dir);
	} else {
		ns_dir = g_dir_make_tmp(NULL, NULL);
		g_assert_nonnull(ns_dir);
		g_test_queue_free(ns_dir);
		g_assert_cmpint(setenv("SNAP_CONFINE_NS_DIR", ns_dir, 0), ==,
				0);
		g_test_queue_destroy((GDestroyNotify) unsetenv,
				     "SNAP_CONFINE_NS_DIR");
		g_test_queue_destroy((GDestroyNotify) rm_rf_tmp, ns_dir);
	}
	g_test_queue_destroy((GDestroyNotify) sc_set_ns_dir, SC_NS_DIR);
	sc_set_ns_dir(ns_dir);
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
	group = sc_open_ns_group(group_name, 0);
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

static void test_sc_open_ns_group_graceful()
{
	sc_set_ns_dir("/nonexistent");
	g_test_queue_destroy((GDestroyNotify) sc_set_ns_dir, SC_NS_DIR);
	struct sc_ns_group *group =
	    sc_open_ns_group("foo", SC_NS_FAIL_GRACEFULLY);
	g_assert_null(group);
}

static void test_sc_lock_ns_mutex_precondition()
{
	sc_test_use_fake_ns_dir();
	if (g_test_subprocess()) {
		struct sc_ns_group *group = sc_alloc_ns_group();
		g_test_queue_free(group);
		// Try to lock the mutex, this should abort because we never opened the
		// lock file and don't have a valid file descriptor.
		sc_lock_ns_mutex(group);
		return;
	}
	g_test_trap_subprocess(NULL, 0, 0);
	g_test_trap_assert_failed();
}

static void test_sc_unlock_ns_mutex_precondition()
{
	sc_test_use_fake_ns_dir();
	if (g_test_subprocess()) {
		struct sc_ns_group *group = sc_alloc_ns_group();
		g_test_queue_free(group);
		// Try to unlock the mutex, this should abort because we never opened the
		// lock file and don't have a valid file descriptor.
		sc_unlock_ns_mutex(group);
		return;
	}
	g_test_trap_subprocess(NULL, 0, 0);
	g_test_trap_assert_failed();
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

static void unmount_dir(void *dir)
{
	umount(dir);
}

static void test_sc_is_ns_group_dir_private()
{
	if (geteuid() != 0) {
		g_test_skip("this test needs to run as root");
		return;
	}
	const char *ns_dir = sc_test_use_fake_ns_dir();
	g_test_queue_destroy(unmount_dir, (char *)ns_dir);

	if (g_test_subprocess()) {
		// The temporary directory should not be private initially
		g_assert_false(sc_is_ns_group_dir_private());

		/// do what "mount --bind /foo /foo; mount --make-private /foo" does.
		int err;
		err = mount(ns_dir, ns_dir, NULL, MS_BIND, NULL);
		g_assert_cmpint(err, ==, 0);
		err = mount(NULL, ns_dir, NULL, MS_PRIVATE, NULL);
		g_assert_cmpint(err, ==, 0);

		// The temporary directory should now be private
		g_assert_true(sc_is_ns_group_dir_private());
		return;
	}
	g_test_trap_subprocess(NULL, 0, G_TEST_SUBPROCESS_INHERIT_STDERR);
	g_test_trap_assert_passed();
}

static void test_sc_initialize_ns_groups()
{
	if (geteuid() != 0) {
		g_test_skip("this test needs to run as root");
		return;
	}
	// NOTE: this is g_test_subprocess aware!
	const char *ns_dir = sc_test_use_fake_ns_dir();
	g_test_queue_destroy(unmount_dir, (char *)ns_dir);
	if (g_test_subprocess()) {
		// Initialize namespace groups using a fake directory.
		sc_initialize_ns_groups();

		// Check that the fake directory is now a private mount.
		g_assert_true(sc_is_ns_group_dir_private());

		// Check that the lock file did not leak unclosed.

		// Construct the name of the lock file
		char *lock_file __attribute__ ((cleanup(sc_cleanup_string))) =
		    NULL;
		lock_file =
		    g_strdup_printf("%s/%s", sc_ns_dir, SC_NS_LOCK_FILE);
		// Attempt to open and lock the lock file.
		int lock_fd __attribute__ ((cleanup(sc_cleanup_close))) = -1;
		lock_fd = open(lock_file, O_RDWR | O_CLOEXEC | O_NOFOLLOW);
		g_assert_cmpint(lock_fd, !=, -1);
		// The non-blocking lock operation should not fail
		int err = flock(lock_fd, LOCK_EX | LOCK_NB);
		g_assert_cmpint(err, ==, 0);
		return;
	}
	g_test_trap_subprocess(NULL, 0, G_TEST_SUBPROCESS_INHERIT_STDERR);
	g_test_trap_assert_passed();
}

// Sanity check, ensure that the namespace filesystem identifier is what we
// expect, aka NSFS_MAGIC.
static void test_nsfs_fs_id()
{
	struct utsname uts;
	if (uname(&uts) < 0) {
		g_test_message("cannot use uname(2)");
		g_test_fail();
		return;
	}
	int major, minor;
	if (sscanf(uts.release, "%d.%d", &major, &minor) != 2) {
		g_test_message("cannot use sscanf(2) to parse kernel release");
		g_test_fail();
		return;
	}
	if (major < 3 || (major == 3 && minor < 19)) {
		g_test_skip("this test needs kernel 3.19+");
		return;
	}
	struct statfs buf;
	int err = statfs("/proc/self/ns/mnt", &buf);
	g_assert_cmpint(err, ==, 0);
	g_assert_cmpint(buf.f_type, ==, NSFS_MAGIC);
}

static void test_sc_enable_sanity_timeout()
{
	if (g_test_subprocess()) {
		sc_enable_sanity_timeout();
		debug("waiting...");
		usleep(4 * G_USEC_PER_SEC);
		debug("woke up");
		sc_disable_sanity_timeout();
		return;
	}
	g_test_trap_subprocess(NULL, 5 * G_USEC_PER_SEC,
			       G_TEST_SUBPROCESS_INHERIT_STDERR);
	g_test_trap_assert_failed();
}

static void __attribute__ ((constructor)) init()
{
	g_test_add_func("/internal/rm_rf_tmp", test_rm_rf_tmp);
	g_test_add_func("/ns/sc_enable_sanity_timeout",
			test_sc_enable_sanity_timeout);
	g_test_add_func("/ns/sc_alloc_ns_group", test_sc_alloc_ns_group);
	g_test_add_func("/ns/sc_open_ns_group", test_sc_open_ns_group);
	g_test_add_func("/ns/sc_open_ns_group/graceful",
			test_sc_open_ns_group_graceful);
	g_test_add_func("/ns/sc_lock_unlock_ns_mutex",
			test_sc_lock_unlock_ns_mutex);
	g_test_add_func("/ns/sc_lock_ns_mutex/precondition",
			test_sc_lock_ns_mutex_precondition);
	g_test_add_func("/ns/sc_unlock_ns_mutex/precondition",
			test_sc_unlock_ns_mutex_precondition);
	g_test_add_func("/ns/nsfs_fs_id", test_nsfs_fs_id);
	g_test_add_func("/system/ns/sc_is_ns_group_dir_private",
			test_sc_is_ns_group_dir_private);
	g_test_add_func("/system/ns/sc_initialize_ns_groups",
			test_sc_initialize_ns_groups);
}
