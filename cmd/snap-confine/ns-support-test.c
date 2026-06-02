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
#include "../libsnap-confine-private/test-utils.h"

#include <errno.h>
#include <linux/magic.h>  // for NSFS_MAGIC
#include <sys/utsname.h>
#include <sys/vfs.h>

#include <glib.h>
#include <glib/gstdio.h>

// Set alternate namespace directory
static void sc_set_ns_dir(const char *dir) { sc_ns_dir = dir; }

// A variant of unsetenv that is compatible with GDestroyNotify
static void my_unsetenv(const char *k) { unsetenv(k); }

// Use temporary directory for namespace groups.
//
// The directory is automatically reset to the real value at the end of the
// test.
static const char *sc_test_use_fake_ns_dir(void) {
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
        g_assert_cmpint(setenv("SNAP_CONFINE_NS_DIR", ns_dir, 0), ==, 0);
        g_test_queue_destroy((GDestroyNotify)my_unsetenv, "SNAP_CONFINE_NS_DIR");
        g_test_queue_destroy((GDestroyNotify)rm_rf_tmp, ns_dir);
    }
    g_test_queue_destroy((GDestroyNotify)sc_set_ns_dir, SC_NS_DIR);
    sc_set_ns_dir(ns_dir);
    return ns_dir;
}

// Check that allocating a namespace group sets up internal data structures to
// safe values.
static void test_sc_alloc_mount_ns(void) {
    struct sc_mount_ns *group = NULL;
    group = sc_alloc_mount_ns();
    g_test_queue_free(group);
    g_assert_nonnull(group);
    g_assert_cmpint(group->dir_fd, ==, -1);
    g_assert_cmpint(group->pipe_master[0], ==, -1);
    g_assert_cmpint(group->pipe_master[1], ==, -1);
    g_assert_cmpint(group->pipe_helper[0], ==, -1);
    g_assert_cmpint(group->pipe_helper[1], ==, -1);
    g_assert_cmpint(group->child, ==, 0);
    g_assert_null(group->name);
}

// Initialize a namespace group.
//
// The group is automatically destroyed at the end of the test.
static struct sc_mount_ns *sc_test_open_mount_ns(const char *group_name) {
    // Initialize a namespace group
    struct sc_mount_ns *group = NULL;
    if (group_name == NULL) {
        group_name = "test-group";
    }
    group = sc_open_mount_ns(group_name);
    g_test_queue_destroy((GDestroyNotify)sc_close_mount_ns, group);
    // Check if the returned group data looks okay
    g_assert_nonnull(group);
    g_assert_cmpint(group->dir_fd, !=, -1);
    g_assert_cmpint(group->pipe_master[0], ==, -1);
    g_assert_cmpint(group->pipe_master[1], ==, -1);
    g_assert_cmpint(group->pipe_helper[0], ==, -1);
    g_assert_cmpint(group->pipe_helper[1], ==, -1);
    g_assert_cmpint(group->child, ==, 0);
    g_assert_cmpstr(group->name, ==, group_name);
    return group;
}

// Check that initializing a namespace group creates the appropriate
// filesystem structure.
static void test_sc_open_mount_ns(void) {
    const char *ns_dir = sc_test_use_fake_ns_dir();
    sc_test_open_mount_ns(NULL);
    // Check that the group directory exists
    g_assert_true(g_file_test(ns_dir, G_FILE_TEST_EXISTS | G_FILE_TEST_IS_DIR));
}

// Sanity check, ensure that the namespace filesystem identifier is what we
// expect, aka NSFS_MAGIC.
static void test_nsfs_fs_id(void) {
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

// Helper: write content to a file under the given directory.
static void write_file(const char *dir, const char *name, const char *content) {
    char path[PATH_MAX] = {0};
    sc_must_snprintf(path, sizeof path, "%s/%s", dir, name);
    FILE *f = fopen(path, "w");
    g_assert_nonnull(f);
    if (content != NULL) {
        fputs(content, f);
    }
    fclose(f);
}

// When the info file does not exist, the function should return false (no
// change detected).
static void test_managed_ca_cert_db_changed__no_info_file(void) {
    const char *ns_dir = sc_test_use_fake_ns_dir();
    (void)ns_dir;

    sc_invocation inv = {.snap_instance = "test-snap"};
    g_assert_false(managed_ca_cert_db_changed(&inv));
}

// When the info file exists but has no managed CA cert mtime key, the
// function should return false (upgraded from older snap-confine).
static void test_managed_ca_cert_db_changed__no_mtime_key(void) {
    const char *ns_dir = sc_test_use_fake_ns_dir();

    write_file(ns_dir, "snap.test-snap.info", "base-snap-name=core24\n");

    sc_invocation inv = {.snap_instance = "test-snap"};
    g_assert_false(managed_ca_cert_db_changed(&inv));
}

// When the recorded mtime matches the current mtime of the managed CA cert
// directory, the function should return false.
static void test_managed_ca_cert_db_changed__mtime_unchanged(void) {
    const char *ns_dir = sc_test_use_fake_ns_dir();

    // We cannot redirect SC_MANAGED_CA_CERTS_DIR in a unit test, so if the
    // real managed CA cert directory happens to exist, record its actual
    // mtime to get a "not changed" result. Otherwise skip to the changed
    // tests which exercise the same parsing code.
    struct stat ca_stat;
    if (stat(SC_MANAGED_CA_CERTS_DIR, &ca_stat) != 0) {
        g_test_skip("managed CA cert directory not present on this system");
        return;
    }

    char mtime_str[64] = {0};
    snprintf(mtime_str, sizeof mtime_str, "%lld.%09ld",
             (long long)ca_stat.st_mtim.tv_sec, ca_stat.st_mtim.tv_nsec);

    char info_content[512] = {0};
    snprintf(info_content, sizeof info_content,
             "base-snap-name=core24\nmanaged-ca-certs-dir-mtime=%s\n", mtime_str);
    write_file(ns_dir, "snap.test-snap.info", info_content);

    sc_invocation inv = {.snap_instance = "test-snap"};
    g_assert_false(managed_ca_cert_db_changed(&inv));
}

// When the managed CA cert directory has been updated (mtime differs), the
// function should return true.
static void test_managed_ca_cert_db_changed__legacy_bundle_key(void) {
    const char *ns_dir = sc_test_use_fake_ns_dir();

    write_file(ns_dir, "snap.test-snap.info",
               "base-snap-name=core24\nmanaged-ca-cert-db-mtime=0.000000000\n");

    sc_invocation inv = {.snap_instance = "test-snap"};
    g_assert_true(managed_ca_cert_db_changed(&inv));
}

// When the managed CA cert directory has been updated (mtime differs), the
// should return true.
static void test_managed_ca_cert_db_changed__mtime_changed(void) {
    const char *ns_dir = sc_test_use_fake_ns_dir();

    // Write an info file with a clearly stale mtime.
    write_file(ns_dir, "snap.test-snap.info",
               "base-snap-name=core24\nmanaged-ca-certs-dir-mtime=0.000000000\n");

    sc_invocation inv = {.snap_instance = "test-snap"};
    // The real SC_MANAGED_CA_CERTS_DIR either doesn't exist (returns true
    // because mtime was recorded but file is gone) or has a different
    // mtime than 0.000000000 (returns true).
    g_assert_true(managed_ca_cert_db_changed(&inv));
}

// When the managed CA cert directory has a clearly stale recorded mtime, the
// function should return true.
static void test_managed_ca_cert_db_changed__db_removed(void) {
    const char *ns_dir = sc_test_use_fake_ns_dir();

    write_file(ns_dir, "snap.test-snap.info",
               "base-snap-name=core24\nmanaged-ca-certs-dir-mtime=9999999999.000000000\n");

    sc_invocation inv = {.snap_instance = "test-snap"};
    // SC_MANAGED_CA_CERTS_DIR almost certainly doesn't exist at that mtime,
    // so the function detects a change.
    g_assert_true(managed_ca_cert_db_changed(&inv));
}

static void __attribute__((constructor)) init(void) {
    g_test_add_func("/ns/sc_alloc_mount_ns", test_sc_alloc_mount_ns);
    g_test_add_func("/ns/sc_open_mount_ns", test_sc_open_mount_ns);
    g_test_add_func("/ns/nsfs_fs_id", test_nsfs_fs_id);
    g_test_add_func("/ns/managed_ca_cert_db_changed/no_info_file",
                     test_managed_ca_cert_db_changed__no_info_file);
    g_test_add_func("/ns/managed_ca_cert_db_changed/no_mtime_key",
                     test_managed_ca_cert_db_changed__no_mtime_key);
    g_test_add_func("/ns/managed_ca_cert_db_changed/mtime_unchanged",
                     test_managed_ca_cert_db_changed__mtime_unchanged);
    g_test_add_func("/ns/managed_ca_cert_db_changed/legacy_bundle_key",
                     test_managed_ca_cert_db_changed__legacy_bundle_key);
    g_test_add_func("/ns/managed_ca_cert_db_changed/mtime_changed",
                     test_managed_ca_cert_db_changed__mtime_changed);
    g_test_add_func("/ns/managed_ca_cert_db_changed/db_removed",
                     test_managed_ca_cert_db_changed__db_removed);
}
