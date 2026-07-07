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

#include <fcntl.h>
#include <glib.h>
#include <glib/gstdio.h>

static void sc_set_managed_ca_certs_dir(const char *dir) { sc_managed_ca_certs_dir = dir; }

static char *create_regular_file_under(const char *dir, const char *relpath) {
    char *path = g_build_filename(dir, relpath, NULL);
    char *parent = g_path_get_dirname(path);
    g_assert_cmpint(g_mkdir_with_parents(parent, 0755), ==, 0);
    g_free(parent);

    int fd = open(path, O_WRONLY | O_CREAT | O_TRUNC | O_CLOEXEC, 0644);
    g_assert_cmpint(fd, >=, 0);
    g_assert_cmpint(close(fd), ==, 0);
    return path;
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

static void test_should_bind_mount_dir__directories(void) {
    char *tmpdir = g_dir_make_tmp("snap-confine-mount-test-XXXXXX", NULL);
    g_assert_nonnull(tmpdir);

    char *src = create_directory_under(tmpdir, "src/managed-certs");
    char *dst = create_directory_under(tmpdir, "dst/certs");
    char *src_dir = g_build_filename(tmpdir, "src", NULL);
    char *dst_dir = g_build_filename(tmpdir, "dst", NULL);

    g_assert_true(sc_should_bind_mount_dir(src, dst));

    g_assert_cmpint(g_rmdir(src), ==, 0);
    g_assert_cmpint(g_rmdir(dst), ==, 0);
    g_assert_cmpint(g_rmdir(src_dir), ==, 0);
    g_assert_cmpint(g_rmdir(dst_dir), ==, 0);
    g_assert_cmpint(g_rmdir(tmpdir), ==, 0);

    g_free(src);
    g_free(dst);
    g_free(src_dir);
    g_free(dst_dir);
    g_free(tmpdir);
}

static void test_should_bind_mount_dir__missing_source(void) {
    char *tmpdir = g_dir_make_tmp("snap-confine-mount-test-XXXXXX", NULL);
    g_assert_nonnull(tmpdir);

    char *src = g_build_filename(tmpdir, "src", "managed-certs", NULL);
    char *dst = create_directory_under(tmpdir, "dst/certs");
    char *dst_dir = g_build_filename(tmpdir, "dst", NULL);

    g_assert_false(sc_should_bind_mount_dir(src, dst));

    g_assert_cmpint(g_rmdir(dst), ==, 0);
    g_assert_cmpint(g_rmdir(dst_dir), ==, 0);
    g_assert_cmpint(g_rmdir(tmpdir), ==, 0);

    g_free(src);
    g_free(dst);
    g_free(dst_dir);
    g_free(tmpdir);
}

static void test_should_bind_mount_dir__missing_destination(void) {
    char *tmpdir = g_dir_make_tmp("snap-confine-mount-test-XXXXXX", NULL);
    g_assert_nonnull(tmpdir);

    char *src = create_directory_under(tmpdir, "src/managed-certs");
    char *dst = g_build_filename(tmpdir, "dst", "certs", NULL);
    char *src_dir = g_build_filename(tmpdir, "src", NULL);

    g_assert_false(sc_should_bind_mount_dir(src, dst));

    g_assert_cmpint(g_rmdir(src), ==, 0);
    g_assert_cmpint(g_rmdir(src_dir), ==, 0);
    g_assert_cmpint(g_rmdir(tmpdir), ==, 0);

    g_free(src);
    g_free(dst);
    g_free(src_dir);
    g_free(tmpdir);
}

static void test_should_bind_mount_dir__source_is_file(void) {
    char *tmpdir = g_dir_make_tmp("snap-confine-mount-test-XXXXXX", NULL);
    g_assert_nonnull(tmpdir);

    char *src = create_regular_file_under(tmpdir, "src/managed-ca.crt");
    char *dst = create_directory_under(tmpdir, "dst/certs");
    char *src_dir = g_build_filename(tmpdir, "src", NULL);
    char *dst_dir = g_build_filename(tmpdir, "dst", NULL);

    g_assert_false(sc_should_bind_mount_dir(src, dst));

    g_assert_cmpint(g_remove(src), ==, 0);
    g_assert_cmpint(g_rmdir(dst), ==, 0);
    g_assert_cmpint(g_rmdir(src_dir), ==, 0);
    g_assert_cmpint(g_rmdir(dst_dir), ==, 0);
    g_assert_cmpint(g_rmdir(tmpdir), ==, 0);

    g_free(src);
    g_free(dst);
    g_free(src_dir);
    g_free(dst_dir);
    g_free(tmpdir);
}

static void test_resolve_managed_ca_certs_dir__symlink_to_generation(void) {
    char *tmpdir = g_dir_make_tmp("snap-confine-mount-test-XXXXXX", NULL);
    g_assert_nonnull(tmpdir);

    char *published = create_directory_under(tmpdir, "published/gen-1");
    char *merged = create_symlink_under(tmpdir, "merged", "published/gen-1");

    g_test_queue_destroy((GDestroyNotify)sc_set_managed_ca_certs_dir, (gpointer)SC_MANAGED_CA_CERTS_DIR);
    sc_set_managed_ca_certs_dir(merged);

    char *resolved = sc_resolve_managed_ca_certs_dir();
    g_assert_cmpstr(resolved, ==, published);

    g_free(resolved);
    g_assert_cmpint(g_remove(merged), ==, 0);
    g_assert_cmpint(g_rmdir(published), ==, 0);

    char *published_parent = g_build_filename(tmpdir, "published", NULL);
    g_assert_cmpint(g_rmdir(published_parent), ==, 0);
    g_assert_cmpint(g_rmdir(tmpdir), ==, 0);

    g_free(published_parent);
    g_free(published);
    g_free(merged);
    g_free(tmpdir);
}

static void test_resolve_managed_ca_certs_dir__legacy_directory(void) {
    char *tmpdir = g_dir_make_tmp("snap-confine-mount-test-XXXXXX", NULL);
    g_assert_nonnull(tmpdir);

    char *merged = create_directory_under(tmpdir, "merged");

    g_test_queue_destroy((GDestroyNotify)sc_set_managed_ca_certs_dir, (gpointer)SC_MANAGED_CA_CERTS_DIR);
    sc_set_managed_ca_certs_dir(merged);

    g_assert_null(sc_resolve_managed_ca_certs_dir());

    g_assert_cmpint(g_rmdir(merged), ==, 0);
    g_assert_cmpint(g_rmdir(tmpdir), ==, 0);

    g_free(merged);
    g_free(tmpdir);
}

static void __attribute__((constructor)) init(void) {
    g_test_add_func("/mount/get_nextpath/typical", test_get_nextpath__typical);
    g_test_add_func("/mount/get_nextpath/weird", test_get_nextpath__weird);
    g_test_add_func("/mount/is_subdir", test_is_subdir);
    g_test_add_func("/mount/should_bind_mount_dir/directories", test_should_bind_mount_dir__directories);
    g_test_add_func("/mount/should_bind_mount_dir/missing_source", test_should_bind_mount_dir__missing_source);
    g_test_add_func("/mount/should_bind_mount_dir/missing_destination", test_should_bind_mount_dir__missing_destination);
    g_test_add_func("/mount/should_bind_mount_dir/source_is_file", test_should_bind_mount_dir__source_is_file);
    g_test_add_func("/mount/resolve_managed_ca_certs_dir/symlink_to_generation",
                    test_resolve_managed_ca_certs_dir__symlink_to_generation);
    g_test_add_func("/mount/resolve_managed_ca_certs_dir/legacy_directory",
                    test_resolve_managed_ca_certs_dir__legacy_directory);
}
