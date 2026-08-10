/*
 * Copyright (C) 2015 Canonical Ltd
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

#include "mount-support.h"
#include "mount-support-nvidia.c"
#include "mount-support-nvidia.h"
#include "mount-support.c"

#include <glib.h>
#include <glib/gstdio.h>

static void sc_set_managed_ca_certs_dir(const char *dir) { sc_managed_ca_certs_dir = dir; }

static void sc_set_managed_ca_generation_dir(const char *dir) { sc_managed_ca_generation_dir = dir; }

static void sc_test_set_managed_ca_dirs(const char *managed_dir, const char *generation_dir) {
    g_test_queue_destroy((GDestroyNotify)sc_set_managed_ca_certs_dir, (gpointer)SC_MANAGED_CA_CERTS_DIR);
    sc_set_managed_ca_certs_dir(managed_dir);

    g_test_queue_destroy((GDestroyNotify)sc_set_managed_ca_generation_dir, (gpointer)SC_MANAGED_CA_GENERATION_DIR);
    sc_set_managed_ca_generation_dir(generation_dir);
}

static char *create_directory_under(const char *dir, const char *relpath) {
    char *path = g_build_filename(dir, relpath, NULL);
    g_assert_cmpint(g_mkdir_with_parents(path, 0755), ==, 0);
    return path;
}

static char *create_symlink_under(const char *dir, const char *relpath, const char *target) {
    char *path = g_build_filename(dir, relpath, NULL);
    char *parent = g_path_get_dirname(path);
    g_assert_cmpint(g_mkdir_with_parents(parent, 0755), ==, 0);
    g_free(parent);
    g_assert_cmpint(symlink(target, path), ==, 0);
    return path;
}

static void replace_slashes_with_NUL(char *path, size_t len) {
    for (size_t i = 0; i < len; i++) {
        if (path[i] == '/') path[i] = '\0';
    }
}

static void test_get_nextpath__typical(void) {
    char path[] = "/some/path";
    size_t offset = 0;
    size_t fulllen = strlen(path);

    // Prepare path for usage with get_nextpath() by replacing
    // all path separators with the NUL byte.
    replace_slashes_with_NUL(path, fulllen);

    // Run get_nextpath a few times to see what happens.
    char *result;
    result = get_nextpath(path, &offset, fulllen);
    g_assert_cmpstr(result, ==, "some");
    result = get_nextpath(path, &offset, fulllen);
    g_assert_cmpstr(result, ==, "path");
    result = get_nextpath(path, &offset, fulllen);
    g_assert_cmpstr(result, ==, NULL);
}

static void test_get_nextpath__weird(void) {
    char path[] = "..///path";
    size_t offset = 0;
    size_t fulllen = strlen(path);

    // Prepare path for usage with get_nextpath() by replacing
    // all path separators with the NUL byte.
    replace_slashes_with_NUL(path, fulllen);

    // Run get_nextpath a few times to see what happens.
    char *result;
    result = get_nextpath(path, &offset, fulllen);
    g_assert_cmpstr(result, ==, "path");
    result = get_nextpath(path, &offset, fulllen);
    g_assert_cmpstr(result, ==, NULL);
}

static void test_is_subdir(void) {
    // Sensible exaples are sensible
    g_assert_true(is_subdir("/dir/subdir", "/dir/"));
    g_assert_true(is_subdir("/dir/subdir", "/dir"));
    g_assert_true(is_subdir("/dir/", "/dir"));
    g_assert_true(is_subdir("/dir", "/dir"));
    // Also without leading slash
    g_assert_true(is_subdir("dir/subdir", "dir/"));
    g_assert_true(is_subdir("dir/subdir", "dir"));
    g_assert_true(is_subdir("dir/", "dir"));
    g_assert_true(is_subdir("dir", "dir"));
    // Some more ideas
    g_assert_true(is_subdir("//", "/"));
    g_assert_true(is_subdir("/", "/"));
    g_assert_true(is_subdir("", ""));
    // but this is not true
    g_assert_false(is_subdir("/", "/dir"));
    g_assert_false(is_subdir("/rid", "/dir"));
    g_assert_false(is_subdir("/different/dir", "/dir"));
    g_assert_false(is_subdir("/", ""));
}

static void test_resolve_managed_ca_certs_dir__symlink_to_generation(void) {
    char *tmpdir = g_dir_make_tmp("snap-confine-mount-test-XXXXXX", NULL);
    g_assert_nonnull(tmpdir);

    char *published = create_directory_under(tmpdir, "published/gen-1");
    char *published_parent = g_build_filename(tmpdir, "published", NULL);
    char *merged = create_symlink_under(tmpdir, "merged", "published/gen-1");

    sc_test_set_managed_ca_dirs(merged, published_parent);

    char *resolved = sc_resolve_managed_ca_certs_dir();
    g_assert_cmpstr(resolved, ==, published);

    g_free(resolved);
    g_assert_cmpint(g_remove(merged), ==, 0);
    g_assert_cmpint(g_rmdir(published), ==, 0);
    g_assert_cmpint(g_rmdir(published_parent), ==, 0);
    g_assert_cmpint(g_rmdir(tmpdir), ==, 0);

    g_free(published_parent);
    g_free(published);
    g_free(merged);
    g_free(tmpdir);
}

static void test_resolve_managed_ca_certs_dir__outside_published(void) {
    char *tmpdir = g_dir_make_tmp("snap-confine-mount-test-XXXXXX", NULL);
    g_assert_nonnull(tmpdir);

    char *outside = create_directory_under(tmpdir, "outside/gen-1");
    char *published_parent = g_build_filename(tmpdir, "published", NULL);
    char *merged = create_symlink_under(tmpdir, "merged", outside);

    sc_test_set_managed_ca_dirs(merged, published_parent);

    g_assert_null(sc_resolve_managed_ca_certs_dir());

    g_assert_cmpint(g_remove(merged), ==, 0);
    g_assert_cmpint(g_rmdir(outside), ==, 0);

    char *outside_parent = g_build_filename(tmpdir, "outside", NULL);
    g_assert_cmpint(g_rmdir(outside_parent), ==, 0);
    g_assert_cmpint(g_rmdir(tmpdir), ==, 0);

    g_free(published_parent);
    g_free(outside_parent);
    g_free(outside);
    g_free(merged);
    g_free(tmpdir);
}

static void test_resolve_managed_ca_certs_dir__nested_path(void) {
    char *tmpdir = g_dir_make_tmp("snap-confine-mount-test-XXXXXX", NULL);
    g_assert_nonnull(tmpdir);

    char *nested = create_directory_under(tmpdir, "published/gen-1/nested");
    char *published_parent = g_build_filename(tmpdir, "published", NULL);
    char *merged = create_symlink_under(tmpdir, "merged", "published/gen-1/nested");

    sc_test_set_managed_ca_dirs(merged, published_parent);

    g_assert_null(sc_resolve_managed_ca_certs_dir());

    g_assert_cmpint(g_remove(merged), ==, 0);
    g_assert_cmpint(g_rmdir(nested), ==, 0);

    char *generation_dir = g_build_filename(tmpdir, "published", "gen-1", NULL);
    g_assert_cmpint(g_rmdir(generation_dir), ==, 0);
    g_assert_cmpint(g_rmdir(published_parent), ==, 0);
    g_assert_cmpint(g_rmdir(tmpdir), ==, 0);

    g_free(published_parent);
    g_free(generation_dir);
    g_free(nested);
    g_free(merged);
    g_free(tmpdir);
}

static void test_resolve_managed_ca_certs_dir__legacy_directory(void) {
    char *tmpdir = g_dir_make_tmp("snap-confine-mount-test-XXXXXX", NULL);
    g_assert_nonnull(tmpdir);

    char *published_parent = g_build_filename(tmpdir, "published", NULL);
    char *merged = create_directory_under(tmpdir, "merged");

    sc_test_set_managed_ca_dirs(merged, published_parent);

    g_assert_null(sc_resolve_managed_ca_certs_dir());

    g_assert_cmpint(g_rmdir(merged), ==, 0);
    g_assert_cmpint(g_rmdir(tmpdir), ==, 0);

    g_free(published_parent);
    g_free(merged);
    g_free(tmpdir);
}

static void test_maybe_bind_mount_managed_ca_certs_dir__destination_symlink(void) {
    if (g_test_subprocess()) {
        char *tmpdir = g_dir_make_tmp("snap-confine-mount-test-XXXXXX", NULL);
        g_assert_nonnull(tmpdir);

        char *published = create_directory_under(tmpdir, "published/gen-1");
        char *published_parent = g_build_filename(tmpdir, "published", NULL);
        char *merged = create_symlink_under(tmpdir, "merged", "published/gen-1");
        char *scratch = create_directory_under(tmpdir, "scratch/etc/ssl");
        char *target = create_symlink_under(tmpdir, "scratch/etc/ssl/certs", "/etc/ssl/certs");

        sc_test_set_managed_ca_dirs(merged, published_parent);

        sc_maybe_bind_mount_managed_ca_certs_dir(g_build_filename(tmpdir, "scratch", NULL));
        g_assert_not_reached();

        g_free(published_parent);
        g_free(target);
        g_free(scratch);
        g_free(merged);
        g_free(published);
        g_free(tmpdir);
        return;
    }

    g_test_trap_subprocess(NULL, 0, 0);
    g_test_trap_assert_failed();
    g_test_trap_assert_stderr("*cannot bind mount managed CA certificates over a symlink*");
}

static void test_maybe_bind_mount_managed_ca_certs_dir__missing_destination(void) {
    char *tmpdir = g_dir_make_tmp("snap-confine-mount-test-XXXXXX", NULL);
    g_assert_nonnull(tmpdir);

    char *published = create_directory_under(tmpdir, "published/gen-1");
    char *published_parent = g_build_filename(tmpdir, "published", NULL);
    char *merged = create_symlink_under(tmpdir, "merged", "published/gen-1");
    char *scratch_ssl = create_directory_under(tmpdir, "scratch/etc/ssl");
    char *scratch = g_build_filename(tmpdir, "scratch", NULL);
    char *target = g_build_filename(scratch, "etc/ssl/certs", NULL);

    sc_test_set_managed_ca_dirs(merged, published_parent);

    sc_maybe_bind_mount_managed_ca_certs_dir(scratch);

    g_assert_cmpint(access(target, F_OK), ==, -1);
    g_assert_cmpint(errno, ==, ENOENT);

    g_assert_cmpint(g_rmdir(scratch_ssl), ==, 0);
    char *scratch_etc = g_build_filename(tmpdir, "scratch/etc", NULL);
    g_assert_cmpint(g_rmdir(scratch_etc), ==, 0);
    char *scratch_root = g_build_filename(tmpdir, "scratch", NULL);
    g_assert_cmpint(g_rmdir(scratch_root), ==, 0);
    g_assert_cmpint(g_remove(merged), ==, 0);
    g_assert_cmpint(g_rmdir(published), ==, 0);

    g_assert_cmpint(g_rmdir(published_parent), ==, 0);
    g_assert_cmpint(g_rmdir(tmpdir), ==, 0);

    g_free(published_parent);
    g_free(scratch_root);
    g_free(scratch_etc);
    g_free(target);
    g_free(scratch);
    g_free(scratch_ssl);
    g_free(merged);
    g_free(published);
    g_free(tmpdir);
}

static void __attribute__((constructor)) init(void) {
    g_test_add_func("/mount/get_nextpath/typical", test_get_nextpath__typical);
    g_test_add_func("/mount/get_nextpath/weird", test_get_nextpath__weird);
    g_test_add_func("/mount/is_subdir", test_is_subdir);
    g_test_add_func("/mount/resolve_managed_ca_certs_dir/symlink_to_generation",
                    test_resolve_managed_ca_certs_dir__symlink_to_generation);
    g_test_add_func("/mount/resolve_managed_ca_certs_dir/outside_published",
                    test_resolve_managed_ca_certs_dir__outside_published);
    g_test_add_func("/mount/resolve_managed_ca_certs_dir/nested_path", test_resolve_managed_ca_certs_dir__nested_path);
    g_test_add_func("/mount/resolve_managed_ca_certs_dir/legacy_directory",
                    test_resolve_managed_ca_certs_dir__legacy_directory);
    g_test_add_func("/mount/maybe_bind_mount_managed_ca_certs_dir/destination_symlink",
                    test_maybe_bind_mount_managed_ca_certs_dir__destination_symlink);
    g_test_add_func("/mount/maybe_bind_mount_managed_ca_certs_dir/missing_destination",
                    test_maybe_bind_mount_managed_ca_certs_dir__missing_destination);
}
