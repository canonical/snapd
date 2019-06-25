/*
 * Copyright (C) 2019 Canonical Ltd
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

#include "infofile.h"
#include "infofile.c"

#include <glib.h>

static void test_infofile_query(void) {
    int rc;
    sc_error *err;

    char text[] =
        "key=value\n"
        "other-key=other-value\n"
        "dup-key=value-one\n"
        "dup-key=value-two\n";
    FILE *stream = fmemopen(text, sizeof text - 1, "r");
    g_assert_nonnull(stream);

    /* Keys that are not found get NULL values. */
    char *value = (void *)0xfefefefe;
    rc = sc_infofile_query(stream, &err, "missing-key", &value, NULL);
    g_assert_cmpint(rc, ==, 0);
    g_assert_null(err);
    g_assert_null(value);

    /* Keys that are found get strdup-duplicated values. */
    value = NULL;
    rc = sc_infofile_query(stream, &err, "key", &value, NULL);
    g_assert_cmpint(rc, ==, 0);
    g_assert_null(err);
    g_assert_nonnull(value);
    g_assert_cmpstr(value, ==, "value");
    free(value);

    /* Multiple keys can be extracted on one go. */
    char *other_value;
    rc = sc_infofile_query(stream, &err, "key", &value, "other-key", &other_value, NULL);
    g_assert_cmpint(rc, ==, 0);
    g_assert_null(err);
    g_assert_nonnull(value);
    g_assert_nonnull(other_value);
    g_assert_cmpstr(value, ==, "value");
    g_assert_cmpstr(other_value, ==, "other-value");
    free(value);
    free(other_value);

    /* Order in which keys are extracted does not matter. */
    rc = sc_infofile_query(stream, &err, "other-key", &other_value, "key", &value, NULL);
    g_assert_cmpint(rc, ==, 0);
    g_assert_null(err);
    g_assert_nonnull(value);
    g_assert_nonnull(other_value);
    g_assert_cmpstr(value, ==, "value");
    g_assert_cmpstr(other_value, ==, "other-value");
    free(value);
    free(other_value);

    /* When duplicate keys are present the first value is extracted. */
    char *dup_value;
    rc = sc_infofile_query(stream, &err, "dup-key", &dup_value, NULL);
    g_assert_cmpint(rc, ==, 0);
    g_assert_null(err);
    g_assert_nonnull(dup_value);
    g_assert_cmpstr(dup_value, ==, "value-one");
    free(dup_value);

    fclose(stream);

    char *malformed_value;

    char malformed1[] = "key\n";
    stream = fmemopen(malformed1, sizeof malformed1 - 1, "r");
    g_assert_nonnull(stream);
    rc = sc_infofile_query(stream, &err, "key", &malformed_value, NULL);
    g_assert_cmpint(rc, ==, -1);
    g_assert_nonnull(err);
    g_assert_null(malformed_value);
    sc_error_free(err);
    fclose(stream);

    char malformed2[] = "key=value\0garbage\n";
    stream = fmemopen(malformed2, sizeof malformed2 - 1, "r");
    g_assert_nonnull(stream);
    rc = sc_infofile_query(stream, &err, "key", &malformed_value, NULL);
    g_assert_cmpint(rc, ==, -1);
    g_assert_nonnull(err);
    g_assert_null(malformed_value);
    sc_error_free(err);
    fclose(stream);

    char malformed3[] = "key=";
    stream = fmemopen(malformed3, sizeof malformed3 - 1, "r");
    g_assert_nonnull(stream);
    rc = sc_infofile_query(stream, &err, "key", &malformed_value, NULL);
    g_assert_cmpint(rc, ==, 0);
    g_assert_null(err);
    g_assert_cmpstr(malformed_value, ==, "");
    sc_error_free(err);
    fclose(stream);
    free(malformed_value);
}

static void __attribute__((constructor)) init(void) { g_test_add_func("/infofile/query", test_infofile_query); }
