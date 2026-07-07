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

// Set alternate managed CA certs directory.
static void sc_set_managed_ca_certs_dir(const char *dir) { sc_managed_ca_certs_dir = dir; }

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

// Use a temporary directory for the managed CA certs path.
static const char *sc_test_use_fake_managed_ca_certs_dir(void) {
    char *managed_dir = g_dir_make_tmp(NULL, NULL);
    char *managed_link = g_build_filename(managed_dir, "merged", NULL);
    g_assert_nonnull(managed_dir);
    g_test_queue_free(managed_dir);
    g_test_queue_free(managed_link);
    g_test_queue_destroy((GDestroyNotify)rm_rf_tmp, managed_dir);
    g_test_queue_destroy((GDestroyNotify)sc_set_managed_ca_certs_dir, (gpointer)SC_MANAGED_CA_CERTS_DIR);
    sc_set_managed_ca_certs_dir(managed_link);
    return managed_dir;
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

static char *create_fake_managed_generation(const char *managed_dir, const char *generation) {
    char *published_dir = g_build_filename(managed_dir, "published", generation, NULL);
    g_assert_cmpint(g_mkdir_with_parents(published_dir, 0755), ==, 0);

    char *merged = g_build_filename(managed_dir, "merged", NULL);
    char *target = g_build_filename("published", generation, NULL);
    g_assert_cmpint(symlink(target, merged), ==, 0);
    g_free(target);
    g_free(merged);
    return published_dir;
}

// When the info file does not exist and there is no current generation, the
// namespace can be reused.
static void test_managed_ca_cert_db_changed__no_info_file(void) {
    const char *ns_dir = sc_test_use_fake_ns_dir();
    const char *managed_dir = sc_test_use_fake_managed_ca_certs_dir();
    (void)ns_dir;
    (void)managed_dir;

    sc_invocation inv = {.snap_instance = "test-snap"};
    g_assert_false(managed_ca_cert_db_changed(&inv));
}

// When the info file exists but no generation is recorded while the host now
// exposes one, the namespace must be recreated.
static void test_managed_ca_cert_db_changed__no_generation_key(void) {
    const char *ns_dir = sc_test_use_fake_ns_dir();
    const char *managed_dir = sc_test_use_fake_managed_ca_certs_dir();

    char *published_dir = create_fake_managed_generation(managed_dir, "gen-1");
    g_test_queue_free(published_dir);

    write_file(ns_dir, "snap.test-snap.info", "base-snap-name=core24\n");

    sc_invocation inv = {.snap_instance = "test-snap"};
    g_assert_true(managed_ca_cert_db_changed(&inv));
}

// When the recorded generation matches the current generation, the function
// should return false.
static void test_managed_ca_cert_db_changed__generation_unchanged(void) {
    const char *ns_dir = sc_test_use_fake_ns_dir();
    const char *managed_dir = sc_test_use_fake_managed_ca_certs_dir();

    char *published_dir = create_fake_managed_generation(managed_dir, "gen-1");
    g_test_queue_free(published_dir);

    char info_content[512] = {0};
    snprintf(info_content, sizeof info_content,
             "base-snap-name=core24\nmanaged-ca-certs-generation=gen-1\n");
    write_file(ns_dir, "snap.test-snap.info", info_content);

    sc_invocation inv = {.snap_instance = "test-snap"};
    g_assert_false(managed_ca_cert_db_changed(&inv));
}

// When the info file carries the old mtime-based key and the host now exposes
// a generation-backed trust store, the namespace must be recreated.
static void test_managed_ca_cert_db_changed__legacy_bundle_key(void) {
    const char *ns_dir = sc_test_use_fake_ns_dir();
    const char *managed_dir = sc_test_use_fake_managed_ca_certs_dir();

    char *published_dir = create_fake_managed_generation(managed_dir, "gen-1");
    g_test_queue_free(published_dir);

    write_file(ns_dir, "snap.test-snap.info",
               "base-snap-name=core24\nmanaged-ca-cert-db-mtime=0.000000000\n");

    sc_invocation inv = {.snap_instance = "test-snap"};
    g_assert_true(managed_ca_cert_db_changed(&inv));
}

// When the host generation differs from what was recorded, the function
// should return true.
static void test_managed_ca_cert_db_changed__generation_changed(void) {
    const char *ns_dir = sc_test_use_fake_ns_dir();
    const char *managed_dir = sc_test_use_fake_managed_ca_certs_dir();

    char *published_dir = create_fake_managed_generation(managed_dir, "gen-2");
    g_test_queue_free(published_dir);

    write_file(ns_dir, "snap.test-snap.info",
               "base-snap-name=core24\nmanaged-ca-certs-generation=gen-1\n");

    sc_invocation inv = {.snap_instance = "test-snap"};
    g_assert_true(managed_ca_cert_db_changed(&inv));
}

// When a generation was recorded but the current managed CA cert path is not a
// symlink anymore, the namespace must be recreated.
static void test_managed_ca_cert_db_changed__legacy_directory_layout(void) {
    const char *ns_dir = sc_test_use_fake_ns_dir();
    const char *managed_dir = sc_test_use_fake_managed_ca_certs_dir();

    char *merged = g_build_filename(managed_dir, "merged", NULL);
    g_assert_cmpint(g_mkdir_with_parents(merged, 0755), ==, 0);
    g_test_queue_free(merged);

    write_file(ns_dir, "snap.test-snap.info",
               "base-snap-name=core24\nmanaged-ca-certs-generation=gen-1\n");

    sc_invocation inv = {.snap_instance = "test-snap"};
    g_assert_true(managed_ca_cert_db_changed(&inv));
}

static void __attribute__((constructor)) init(void) {
    g_test_add_func("/ns/sc_alloc_mount_ns", test_sc_alloc_mount_ns);
    g_test_add_func("/ns/sc_open_mount_ns", test_sc_open_mount_ns);
    g_test_add_func("/ns/nsfs_fs_id", test_nsfs_fs_id);
    g_test_add_func("/ns/managed_ca_cert_db_changed/no_info_file",
                     test_managed_ca_cert_db_changed__no_info_file);
    g_test_add_func("/ns/managed_ca_cert_db_changed/no_generation_key",
                     test_managed_ca_cert_db_changed__no_generation_key);
    g_test_add_func("/ns/managed_ca_cert_db_changed/generation_unchanged",
                     test_managed_ca_cert_db_changed__generation_unchanged);
    g_test_add_func("/ns/managed_ca_cert_db_changed/legacy_bundle_key",
                     test_managed_ca_cert_db_changed__legacy_bundle_key);
    g_test_add_func("/ns/managed_ca_cert_db_changed/generation_changed",
                     test_managed_ca_cert_db_changed__generation_changed);
    g_test_add_func("/ns/managed_ca_cert_db_changed/legacy_directory_layout",
                     test_managed_ca_cert_db_changed__legacy_directory_layout);
}
