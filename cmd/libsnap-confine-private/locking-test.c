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

#include "locking.h"
#include "locking.c"

#include "../libsnap-confine-private/cleanup-funcs.h"
#include "../libsnap-confine-private/test-utils.h"

#include <errno.h>

#include <glib.h>
#include <glib/gstdio.h>

// Set alternate locking directory
static void sc_set_lock_dir(const char *dir) { sc_lock_dir = dir; }

// A variant of unsetenv that is compatible with GDestroyNotify
static void my_unsetenv(const char *k) { unsetenv(k); }

// Use temporary directory for locking.
//
// The directory is automatically reset to the real value at the end of the
// test.
static const char *sc_test_use_fake_lock_dir(void) {
    char *lock_dir = NULL;
    if (g_test_subprocess()) {
        // Check if the environment variable is set. If so then someone is
        // already managing the temporary directory and we should not create a
        // new one.
        lock_dir = getenv("SNAP_CONFINE_LOCK_DIR");
        g_assert_nonnull(lock_dir);
    } else {
        lock_dir = g_dir_make_tmp(NULL, NULL);
        g_assert_nonnull(lock_dir);
        g_test_queue_free(lock_dir);
        g_assert_cmpint(setenv("SNAP_CONFINE_LOCK_DIR", lock_dir, 0), ==, 0);
        g_test_queue_destroy((GDestroyNotify)my_unsetenv, "SNAP_CONFINE_LOCK_DIR");
        g_test_queue_destroy((GDestroyNotify)rm_rf_tmp, lock_dir);
    }
    g_test_queue_destroy((GDestroyNotify)sc_set_lock_dir, SC_LOCK_DIR);
    sc_set_lock_dir(lock_dir);
    return lock_dir;
}

// Check that locking a namespace actually flock's the mutex with LOCK_EX
static void test_sc_lock_unlock(void) {
    if (geteuid() != 0) {
        g_test_skip("this test only runs as root");
        return;
    }

    const char *lock_dir = sc_test_use_fake_lock_dir();
    int fd = sc_lock_generic("foo", 123);
    // Construct the name of the lock file
    char *lock_file SC_CLEANUP(sc_cleanup_string) = NULL;
    lock_file = g_strdup_printf("%s/foo.123.lock", lock_dir);
    // Open the lock file again to obtain a separate file descriptor.
    // According to flock(2) locks are associated with an open file table entry
    // so this descriptor will be separate and can compete for the same lock.
    int lock_fd SC_CLEANUP(sc_cleanup_close) = -1;
    lock_fd = open(lock_file, O_RDWR | O_CLOEXEC | O_NOFOLLOW);
    g_assert_cmpint(lock_fd, !=, -1);
    // The non-blocking lock operation should fail with EWOULDBLOCK as the lock
    // file is locked by sc_nlock_ns_mutex() already.
    int err = flock(lock_fd, LOCK_EX | LOCK_NB);
    int saved_errno = errno;
    g_assert_cmpint(err, ==, -1);
    g_assert_cmpint(saved_errno, ==, EWOULDBLOCK);
    // Unlock the lock.
    sc_unlock(fd);
    // Re-attempt the locking operation. This time it should succeed.
    err = flock(lock_fd, LOCK_EX | LOCK_NB);
    g_assert_cmpint(err, ==, 0);
}

// Check that holding a lock is properly detected.
static void test_sc_verify_snap_lock__locked(void) {
    if (geteuid() != 0) {
        g_test_skip("this test only runs as root");
        return;
    }

    (void)sc_test_use_fake_lock_dir();
    int fd = sc_lock_snap("foo");
    sc_verify_snap_lock("foo");
    sc_unlock(fd);
}

// Check that holding a lock is properly detected.
static void test_sc_verify_snap_lock__unlocked(void) {
    if (geteuid() != 0) {
        g_test_skip("this test only runs as root");
        return;
    }

    (void)sc_test_use_fake_lock_dir();
    if (g_test_subprocess()) {
        sc_verify_snap_lock("foo");
        return;
    }
    g_test_trap_subprocess(NULL, 0, 0);
    g_test_trap_assert_failed();
    g_test_trap_assert_stderr("unexpectedly managed to acquire exclusive lock over snap foo\n");
}

static void test_sc_enable_sanity_timeout(void) {
    if (geteuid() != 0) {
        g_test_skip("this test only runs as root");
        return;
    }

    if (g_test_subprocess()) {
        sc_enable_sanity_timeout();
        debug("waiting...");
        usleep(7 * G_USEC_PER_SEC);
        debug("woke up");
        sc_disable_sanity_timeout();
        return;
    }
    g_test_trap_subprocess(NULL, 1 * G_USEC_PER_SEC, G_TEST_SUBPROCESS_INHERIT_STDERR);
    g_test_trap_assert_failed();
    g_test_trap_assert_stderr("sanity timeout expired: Interrupted system call\n");
}

static void __attribute__((constructor)) init(void) {
    g_test_add_func("/locking/sc_lock_unlock", test_sc_lock_unlock);
    g_test_add_func("/locking/sc_enable_sanity_timeout", test_sc_enable_sanity_timeout);
    g_test_add_func("/locking/sc_verify_snap_lock__locked", test_sc_verify_snap_lock__locked);
    g_test_add_func("/locking/sc_verify_snap_lock__unlocked", test_sc_verify_snap_lock__unlocked);
}
