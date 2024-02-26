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

#include "cleanup-funcs.h"
#include "cleanup-funcs.c"

#include <glib.h>
#include <glib/gstdio.h>

#include <fcntl.h>
#include <string.h>
#include <sys/stat.h>
#include <sys/timerfd.h>
#include <sys/types.h>

static int called = 0;

static void cleanup_fn(int *ptr) { called = 1; }

// Test that cleanup functions are applied as expected
static void test_cleanup_sanity(void) {
    {
        int test SC_CLEANUP(cleanup_fn);
        test = 0;
        test++;
    }
    g_assert_cmpint(called, ==, 1);
}

static void test_cleanup_string(void) {
    /* It is safe to use with a NULL pointer to a string. */
    sc_cleanup_string(NULL);

    /* It is safe to use with a NULL string. */
    char *str = NULL;
    sc_cleanup_string(&str);

    /* It is safe to use with a non-NULL string. */
    str = malloc(1);
    g_assert_nonnull(str);
    sc_cleanup_string(&str);
    g_assert_null(str);
}

static void test_cleanup_file(void) {
    /* It is safe to use with a NULL pointer to a FILE. */
    sc_cleanup_file(NULL);

    /* It is safe to use with a NULL FILE. */
    FILE *f = NULL;
    sc_cleanup_file(&f);

    /* It is safe to use with a non-NULL FILE. */
    f = fmemopen(NULL, 10, "rt");
    g_assert_nonnull(f);
    sc_cleanup_file(&f);
    g_assert_null(f);
}

static void test_cleanup_endmntent(void) {
    /* It is safe to use with a NULL pointer to a FILE. */
    sc_cleanup_endmntent(NULL);

    /* It is safe to use with a NULL FILE. */
    FILE *f = NULL;
    sc_cleanup_endmntent(&f);

    /* It is safe to use with a non-NULL FILE. */
    GError *err = NULL;
    char *mock_fstab = NULL;
    gint mock_fstab_fd = g_file_open_tmp("s-c-test-fstab-mock.XXXXXX", &mock_fstab, &err);
    g_assert_no_error(err);
    g_assert_cmpint(mock_fstab_fd, >=, 0);
    g_assert_true(g_close(mock_fstab_fd, NULL));
    /* XXX: not strictly needed as the test only calls setmntent */
    const char *mock_fstab_data = "/dev/foo / ext4 defaults 0 1";
    g_assert_true(g_file_set_contents(mock_fstab, mock_fstab_data, -1, NULL));

    f = setmntent(mock_fstab, "rt");
    g_assert_nonnull(f);
    sc_cleanup_endmntent(&f);
    g_assert_null(f);

    g_remove(mock_fstab);

    g_free(mock_fstab);
}

static void test_cleanup_closedir(void) {
    /* It is safe to use with a NULL pointer to a DIR. */
    sc_cleanup_closedir(NULL);

    /* It is safe to use with a NULL DIR. */
    DIR *d = NULL;
    sc_cleanup_closedir(&d);

    /* It is safe to use with a non-NULL DIR. */
    d = opendir(".");
    g_assert_nonnull(d);
    sc_cleanup_closedir(&d);
    g_assert_null(d);
}

static void test_cleanup_close(void) {
    /* It is safe to use with a NULL pointer to an int. */
    sc_cleanup_close(NULL);

    /* It is safe to use with a -1 file descriptor. */
    int fd = -1;
    sc_cleanup_close(&fd);

    /* It is safe to use with a non-invalid file descriptor. */
    /* Timerfd is a simple to use and widely available object that can be
     * created and closed without interacting with the filesystem. */
    fd = timerfd_create(CLOCK_MONOTONIC, TFD_CLOEXEC);
    g_assert_cmpint(fd, !=, -1);
    sc_cleanup_close(&fd);
    g_assert_cmpint(fd, ==, -1);
}

static void test_cleanup_deep_strv(void) {
    /* It is safe to use with a NULL pointer */
    sc_cleanup_deep_strv(NULL);

    char **argses = NULL;
    /* It is OK if the pointer value is NULL */
    sc_cleanup_deep_strv(&argses);
    g_assert_null(argses);

    /* It is safe to call with an empty array */
    argses = calloc(10, sizeof(char *));
    g_assert_nonnull(argses);
    sc_cleanup_deep_strv(&argses);

    /* And of course the typical case works as well */
    argses = calloc(10, sizeof(char *));
    g_assert_nonnull(argses);
    for (int i = 0; i < 9; i++) {
        argses[i] = strdup("hello");
    }
    sc_cleanup_deep_strv(&argses);
    g_assert_null(argses);
}

static void test_cleanup_shallow_strv(void) {
    /* It is safe to use with a NULL pointer */
    sc_cleanup_shallow_strv(NULL);

    const char **argses = NULL;
    /* It is ok of the pointer value is NULL */
    sc_cleanup_shallow_strv(&argses);
    g_assert_null(argses);

    argses = calloc(10, sizeof(char *));
    g_assert_nonnull(argses);
    /* Fill with bogus pointers so attempts to free them would segfault */
    for (int i = 0; i < 10; i++) {
        argses[i] = (char *)0x100 + i;
    }
    sc_cleanup_shallow_strv(&argses);
    g_assert_null(argses);
    /* If we are alive at this point, most likely only the array was free'd */
}

static void __attribute__((constructor)) init(void) {
    g_test_add_func("/cleanup/sanity", test_cleanup_sanity);
    g_test_add_func("/cleanup/string", test_cleanup_string);
    g_test_add_func("/cleanup/file", test_cleanup_file);
    g_test_add_func("/cleanup/endmntent", test_cleanup_endmntent);
    g_test_add_func("/cleanup/closedir", test_cleanup_closedir);
    g_test_add_func("/cleanup/close", test_cleanup_close);
    g_test_add_func("/cleanup/deep_strv", test_cleanup_deep_strv);
    g_test_add_func("/cleanup/shallow_strv", test_cleanup_shallow_strv);
}
