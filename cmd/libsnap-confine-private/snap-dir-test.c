/*
 * Copyright (C) 2024 Canonical Ltd
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

#include "snap-dir.h"
#include "snap-dir.c"

#include <glib.h>
#include <stdint.h>

#include "test-utils.h"  // For rm_rf_tmp

// For compatibility with g_test_queue_destroy.
static void close_noerr(uintptr_t fd) { (void)close((int)fd); }

static void test_sc_probe_snap_mount_dir__absent(void) {
    char *root_dir = g_dir_make_tmp(NULL, NULL);
    g_assert_nonnull(root_dir);
    g_test_queue_free(root_dir);
    g_test_queue_destroy((GDestroyNotify)rm_rf_tmp, root_dir);

    int root_fd = open(root_dir, O_PATH | O_DIRECTORY | O_NOFOLLOW | O_CLOEXEC);
    g_assert_cmpint(root_fd, !=, -1);
    g_test_queue_destroy((GDestroyNotify)close_noerr, (void *)(uintptr_t)root_fd);

    sc_probe_snap_mount_dir_from_pid_1_mount_ns(root_fd, NULL);
    const char *snap_mount_dir = sc_snap_mount_dir(NULL);
    g_assert_nonnull(snap_mount_dir);
    g_assert_cmpstr(snap_mount_dir, ==, "/var/lib/snapd/snap");
}

static void test_sc_probe_snap_mount_dir__canonical(void) {
    char *root_dir = g_dir_make_tmp(NULL, NULL);
    g_assert_nonnull(root_dir);
    g_test_queue_free(root_dir);
    g_test_queue_destroy((GDestroyNotify)rm_rf_tmp, root_dir);

    char *proc_snap_dir = g_build_filename(root_dir, "/proc/1/root/snap", NULL);
    g_test_queue_free(proc_snap_dir);
    int ret = g_mkdir_with_parents(proc_snap_dir, 0755);
    g_assert_cmpint(ret, ==, 0);

    int root_fd = open(root_dir, O_PATH | O_DIRECTORY | O_NOFOLLOW | O_CLOEXEC);
    g_assert_cmpint(root_fd, !=, -1);
    g_test_queue_destroy((GDestroyNotify)close_noerr, (void *)(uintptr_t)root_fd);

    sc_probe_snap_mount_dir_from_pid_1_mount_ns(root_fd, NULL);
    const char *snap_mount_dir = sc_snap_mount_dir(NULL);
    g_assert_nonnull(snap_mount_dir);
    g_assert_cmpstr(snap_mount_dir, ==, "/snap");
}

static void test_sc_probe_snap_mount_dir__alternate_absolute(void) {
    char *root_dir = g_dir_make_tmp(NULL, NULL);
    g_assert_nonnull(root_dir);
    g_test_queue_free(root_dir);
    g_test_queue_destroy((GDestroyNotify)rm_rf_tmp, root_dir);

    char *proc_root_dir = g_build_filename(root_dir, "/proc/1/root/", NULL);
    g_test_queue_free(proc_root_dir);
    int ret = g_mkdir_with_parents(proc_root_dir, 0755);
    g_assert_cmpint(ret, ==, 0);

    char *proc_snap_symlink = g_build_filename(proc_root_dir, "snap", NULL);
    g_test_queue_free(proc_snap_symlink);
    ret = symlink("/var/lib/snapd/snap", proc_snap_symlink);
    g_assert_cmpint(ret, ==, 0);

    int root_fd = open(root_dir, O_PATH | O_DIRECTORY | O_NOFOLLOW | O_CLOEXEC);
    g_assert_cmpint(root_fd, !=, -1);
    g_test_queue_destroy((GDestroyNotify)close_noerr, (void *)(uintptr_t)root_fd);

    sc_probe_snap_mount_dir_from_pid_1_mount_ns(root_fd, NULL);
    const char *snap_mount_dir = sc_snap_mount_dir(NULL);
    g_assert_nonnull(snap_mount_dir);
    g_assert_cmpstr(snap_mount_dir, ==, "/var/lib/snapd/snap");
}

static void test_sc_probe_snap_mount_dir__alternate_relative(void) {
    char *root_dir = g_dir_make_tmp(NULL, NULL);
    g_assert_nonnull(root_dir);
    g_test_queue_free(root_dir);
    g_test_queue_destroy((GDestroyNotify)rm_rf_tmp, root_dir);

    char *proc_root_dir = g_build_filename(root_dir, "/proc/1/root/", NULL);
    g_test_queue_free(proc_root_dir);
    int ret = g_mkdir_with_parents(proc_root_dir, 0755);
    g_assert_cmpint(ret, ==, 0);

    char *proc_snap_symlink = g_build_filename(proc_root_dir, "snap", NULL);
    g_test_queue_free(proc_snap_symlink);
    ret = symlink("var/lib/snapd/snap", proc_snap_symlink);
    g_assert_cmpint(ret, ==, 0);

    int root_fd = open(root_dir, O_PATH | O_DIRECTORY | O_NOFOLLOW | O_CLOEXEC);
    g_assert_cmpint(root_fd, !=, -1);
    g_test_queue_destroy((GDestroyNotify)close_noerr, (void *)(uintptr_t)root_fd);

    sc_probe_snap_mount_dir_from_pid_1_mount_ns(root_fd, NULL);
    const char *snap_mount_dir = sc_snap_mount_dir(NULL);
    g_assert_nonnull(snap_mount_dir);
    g_assert_cmpstr(snap_mount_dir, ==, "/var/lib/snapd/snap");
}

static void test_sc_probe_snap_mount_dir__bad_symlink_target(void) {
    char *root_dir = g_dir_make_tmp(NULL, NULL);
    g_assert_nonnull(root_dir);
    g_test_queue_free(root_dir);
    g_test_queue_destroy((GDestroyNotify)rm_rf_tmp, root_dir);

    char *proc_root_dir = g_build_filename(root_dir, "/proc/1/root/", NULL);
    g_test_queue_free(proc_root_dir);
    int ret = g_mkdir_with_parents(proc_root_dir, 0755);
    g_assert_cmpint(ret, ==, 0);

    char *proc_snap_symlink = g_build_filename(proc_root_dir, "snap", NULL);
    g_test_queue_free(proc_snap_symlink);
    ret = symlink("/potato", proc_snap_symlink);
    g_assert_cmpint(ret, ==, 0);

    int root_fd = open(root_dir, O_PATH | O_DIRECTORY | O_NOFOLLOW | O_CLOEXEC);
    g_assert_cmpint(root_fd, !=, -1);
    g_test_queue_destroy((GDestroyNotify)close_noerr, (void *)(uintptr_t)root_fd);

    struct sc_error *err = NULL;
    (void)sc_probe_snap_mount_dir_from_pid_1_mount_ns(root_fd, &err);
    g_test_queue_destroy((GDestroyNotify)sc_error_free, err);
    g_assert_cmpstr(sc_error_domain(err), ==, SC_SNAP_DOMAIN);
    g_assert_cmpint(sc_error_code(err), ==, SC_SNAP_MOUNT_DIR_UNSUPPORTED);
    g_assert_cmpstr(sc_error_msg(err), ==, "/snap must be a symbolic link to /var/lib/snapd/snap");
}

static void test_sc_snap_mount_dir__not_probed(void) {
    _snap_mount_dir = NULL;

    struct sc_error *err = NULL;
    const char *snap_mount_dir = sc_snap_mount_dir(&err);
    g_test_queue_destroy((GDestroyNotify)sc_error_free, err);
    g_assert_null(snap_mount_dir);
    g_assert_cmpstr(sc_error_domain(err), ==, SC_LIBSNAP_DOMAIN);
    g_assert_cmpint(sc_error_code(err), ==, SC_API_MISUSE);
    g_assert_cmpstr(sc_error_msg(err), ==, "sc_probe_snap_mount_dir_from_pid_1_mount_ns was not called yet");
}

static void __attribute__((constructor)) init(void) {
    g_test_add_func("/snap-dir/probe-absent", test_sc_probe_snap_mount_dir__absent);
    g_test_add_func("/snap-dir/probe-canonical", test_sc_probe_snap_mount_dir__canonical);
    g_test_add_func("/snap-dir/probe-alternate-absolute", test_sc_probe_snap_mount_dir__alternate_absolute);
    g_test_add_func("/snap-dir/probe-alternate-relative", test_sc_probe_snap_mount_dir__alternate_relative);
    g_test_add_func("/snap-dir/probe-bad-symlink-target", test_sc_probe_snap_mount_dir__bad_symlink_target);
    g_test_add_func("/snap-dir/dir-not-probed", test_sc_snap_mount_dir__not_probed);
}
