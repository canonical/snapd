/*
 * Copyright (C) 2025 Canonical Ltd
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

#define _GNU_SOURCE
#include "group-policy.c"
#include "group-policy.h"

#include <glib.h>

#include "../libsnap-confine-private/test-utils.h"  // For rm_rf_tmp

// For compatibility with g_test_queue_destroy.
static void close_noerr(gpointer fd) { (void)close(GPOINTER_TO_INT(fd)); }

enum {
    CANONICAL_PATH = 1,
    ALTERNATIVE_PATH = 2,
};

static gchar *mock_snap_confine(gchar *root_dir, int which) {
    GError *err = NULL;

    g_assert_true(which == CANONICAL_PATH || which == ALTERNATIVE_PATH);

    const char *tools_dir = SC_CANONICAL_HOST_TOOLS_DIR;
    if (which == ALTERNATIVE_PATH) {
        tools_dir = SC_ALTERNATE_HOST_TOOLS_DIR;
    }

    char *sc_path = g_build_filename(root_dir, "/proc/1/root", tools_dir, "snap-confine", NULL);
    g_test_queue_free(sc_path);

    g_debug("snap-confine mocked at %s", sc_path);

    char *sc_dir = g_path_get_dirname(sc_path);
    int ret = g_mkdir_with_parents(sc_dir, 0755);
    g_assert_cmpint(ret, ==, 0);
    g_free(sc_dir);

    gboolean bret = g_file_set_contents(sc_path, NULL, 0, &err);
    g_assert_cmpint(bret, ==, TRUE);
    g_assert_no_error(err);

    return sc_path;
}

static char *mock_root_dir(void) {
    char *root_dir = g_dir_make_tmp(NULL, NULL);
    g_assert_nonnull(root_dir);
    g_test_queue_free(root_dir);
    g_test_queue_destroy((GDestroyNotify)rm_rf_tmp, root_dir);
    g_debug("root dir mocked at %s", root_dir);
    return root_dir;
}

static void test_group_policy_no_sc(void) {
    char *root_dir = mock_root_dir();

    int root_fd = open(root_dir, O_PATH | O_DIRECTORY | O_NOFOLLOW | O_CLOEXEC);
    g_assert_cmpint(root_fd, !=, -1);
    g_test_queue_destroy((GDestroyNotify)close_noerr, GINT_TO_POINTER(root_fd));

    struct sc_error *err SC_CLEANUP(sc_cleanup_error) = NULL;
    bool ret = _sc_assert_host_local_group_policy(root_fd, geteuid() + 1, NULL, 0, &err);
    g_assert_false(ret);
    g_assert_nonnull(err);

    g_assert_cmpstr(sc_error_domain(err), ==, SC_ERRNO_DOMAIN);
    g_assert_cmpint(sc_error_code(err), ==, ENOENT);
    g_assert_cmpstr(sc_error_msg(err), ==, "cannot locate snap-confine in host root filesystem");
}

static void test_group_policy_happy_canonical(void) {
    char *root_dir = mock_root_dir();

    int root_fd = open(root_dir, O_PATH | O_DIRECTORY | O_NOFOLLOW | O_CLOEXEC);
    g_assert_cmpint(root_fd, !=, -1);
    g_test_queue_destroy((GDestroyNotify)close_noerr, GINT_TO_POINTER(root_fd));

    mock_snap_confine(root_dir, CANONICAL_PATH);

    struct sc_error *err SC_CLEANUP(sc_cleanup_error) = NULL;
    bool ret = sc_assert_host_local_group_policy(root_fd, &err);
    g_assert_true(ret);
    g_assert_null(err);
}

static void test_group_policy_happy_alternative(void) {
    char *root_dir = mock_root_dir();

    int root_fd = open(root_dir, O_PATH | O_DIRECTORY | O_NOFOLLOW | O_CLOEXEC);
    g_assert_cmpint(root_fd, !=, -1);
    g_test_queue_destroy((GDestroyNotify)close_noerr, GINT_TO_POINTER(root_fd));

    mock_snap_confine(root_dir, ALTERNATIVE_PATH);

    struct sc_error *err SC_CLEANUP(sc_cleanup_error) = NULL;
    bool ret = sc_assert_host_local_group_policy(root_fd, &err);
    g_assert_null(err);
    g_assert_true(ret);
}

static void test_group_policy_nomatch_user(void) {
    char *root_dir = mock_root_dir();

    int root_fd = open(root_dir, O_PATH | O_DIRECTORY | O_NOFOLLOW | O_CLOEXEC);
    g_assert_cmpint(root_fd, !=, -1);
    g_test_queue_destroy((GDestroyNotify)close_noerr, GINT_TO_POINTER(root_fd));

    char *sc_path = mock_snap_confine(root_dir, CANONICAL_PATH);
    if (geteuid() == 0) {
        /* running as root, we need to change the ownership of mocked
         * snap-confine to indicate the presence of a local policy, the owning
         * GID must be non-0 and different than the one used in the check */
        int ret = chown(sc_path, -1, getgid() + 2);
        g_assert_cmpint(ret, ==, 0);
    }

    struct sc_error *err SC_CLEANUP(sc_cleanup_error) = NULL;
    bool ret = _sc_assert_host_local_group_policy(root_fd, getgid() + 1, NULL, 0, &err);
    g_assert_nonnull(err);
    g_assert_false(ret);
    g_assert_cmpstr(sc_error_domain(err), ==, SC_GROUP_DOMAIN);
    g_assert_cmpint(sc_error_code(err), ==, SC_NO_GROUP_PRIVS);
    g_assert_cmpstr(
        sc_error_msg(err), ==,
        "user is not a member of group owning snap-confine; check your distribution's policy for running snaps");
}

static void test_group_policy_root(void) {
    if (geteuid() != 0) {
        g_test_skip("test can only be run by real root user");
        return;
    }

    char *root_dir = mock_root_dir();

    int root_fd = open(root_dir, O_PATH | O_DIRECTORY | O_NOFOLLOW | O_CLOEXEC);
    g_assert_cmpint(root_fd, !=, -1);
    g_test_queue_destroy((GDestroyNotify)close_noerr, GINT_TO_POINTER(root_fd));

    /* no need to mock snap-confine */
    struct sc_error *err SC_CLEANUP(sc_cleanup_error) = NULL;
    bool ret = sc_assert_host_local_group_policy(root_fd, &err);
    g_assert_null(err);
    g_assert_true(ret);
}

static void __attribute__((constructor)) init(void) {
    g_test_add_func("/group/no_sc", test_group_policy_no_sc);
    g_test_add_func("/group/happy_canonical", test_group_policy_happy_canonical);
    g_test_add_func("/group/happy_alternative", test_group_policy_happy_alternative);
    g_test_add_func("/group/no_match_user", test_group_policy_nomatch_user);
    g_test_add_func("/group/root", test_group_policy_root);
}
