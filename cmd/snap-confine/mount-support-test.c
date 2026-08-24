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

// sc_manifest_line_relpath() and its callers are only compiled when
// NVIDIA_MULTIARCH is defined (see the #ifdef NVIDIA_MULTIARCH block around
// sc_mount_exported_paths() in mount-support-nvidia.c, which is
// deliberately not hoisted out of it - the shipped snapd snap always
// builds with --enable-nvidia-multiarch), so these tests must be
// conditional on the same macro or they fail to link/compile under
// --enable-nvidia-biarch and the plain (neither) configure variant.
#ifdef NVIDIA_MULTIARCH
static void test_manifest_line_relpath__valid(void) {
    // The common case: one unit, one subdir, one file.
    g_assert_cmpstr(sc_manifest_line_relpath("15_snap_provider_egl-driver-libs/egl_vendor.d/foo.json"), ==,
                    "egl_vendor.d/foo.json");
    // Only the leading "<unit>/" component is stripped, however many further
    // path components relpath itself has.
    g_assert_cmpstr(sc_manifest_line_relpath("unit/a/b/c"), ==, "a/b/c");
    // The minimal valid shape: "<unit>/<file>".
    g_assert_cmpstr(sc_manifest_line_relpath("unit/file"), ==, "file");
}

static void test_manifest_line_relpath__invalid(void) {
    // Empty line.
    g_assert_null(sc_manifest_line_relpath(""));
    // Absolute path.
    g_assert_null(sc_manifest_line_relpath("/unit/subdir/file"));
    // No "/" separator at all, so there is no unit component to strip.
    g_assert_null(sc_manifest_line_relpath("nounit"));
    // Separator is the very last character, leaving an empty relpath.
    g_assert_null(sc_manifest_line_relpath("unit/"));
    // ".." anywhere in the line is rejected, whether in the unit or the
    // relpath component.
    g_assert_null(sc_manifest_line_relpath("unit/../etc/passwd"));
    g_assert_null(sc_manifest_line_relpath("../unit/subdir/file"));
    // The check is a plain substring match, not a path-component match, so
    // it also (over-broadly, but safely - see the comment on
    // sc_manifest_line_relpath) rejects a filename that merely contains
    // ".." without it being a path traversal, such as two dots in a row
    // inside an otherwise normal name.
    g_assert_null(sc_manifest_line_relpath("unit/foo..bar.json"));
}

static void test_manifest_line_relpath__too_long(void) {
    // One byte over SC_EXPORT_MANIFEST_LINE_MAX must be rejected, the same
    // way any other malformed entry is - rather than being handed to the
    // sc_must_snprintf calls in sc_mount_exported_config_files(), which
    // would die() on truncation instead of degrading gracefully (see the
    // comment on SC_EXPORT_MANIFEST_LINE_MAX).
    char too_long[SC_EXPORT_MANIFEST_LINE_MAX + 2] = {0};
    memset(too_long, 'a', sizeof too_long - 1);
    // Give it the shape of an otherwise-valid entry: "unit/<filler>".
    too_long[4] = '/';
    g_assert_null(sc_manifest_line_relpath(too_long));

    // The boundary itself must still be accepted.
    char at_limit[SC_EXPORT_MANIFEST_LINE_MAX + 1] = {0};
    memset(at_limit, 'a', sizeof at_limit - 1);
    at_limit[4] = '/';
    g_assert_nonnull(sc_manifest_line_relpath(at_limit));
}
#endif  // ifdef NVIDIA_MULTIARCH

static void __attribute__((constructor)) init(void) {
    g_test_add_func("/mount/get_nextpath/typical", test_get_nextpath__typical);
    g_test_add_func("/mount/get_nextpath/weird", test_get_nextpath__weird);
    g_test_add_func("/mount/is_subdir", test_is_subdir);
#ifdef NVIDIA_MULTIARCH
    g_test_add_func("/mount/manifest_line_relpath/valid", test_manifest_line_relpath__valid);
    g_test_add_func("/mount/manifest_line_relpath/invalid", test_manifest_line_relpath__invalid);
    g_test_add_func("/mount/manifest_line_relpath/too_long", test_manifest_line_relpath__too_long);
#endif  // ifdef NVIDIA_MULTIARCH
}
