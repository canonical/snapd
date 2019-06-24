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
    char text[] =
        "key=value\n"
        "other-key=other-value\n"
        "dup-key=value-one\n"
        "dup-key=value-two\n";
    FILE *stream = fmemopen(text, strlen(text), "r");
    g_assert_nonnull(stream);

    /* Keys that are not found get NULL values. */
    char *value = (void *)0xfefefefe;
    sc_infofile_query(stream, "missing-key", &value, NULL);
    g_assert_null(value);

    /* Keys that are found get strdup-duplicated values. */
    value = NULL;
    sc_infofile_query(stream, "key", &value, NULL);
    g_assert_nonnull(value);
    g_assert_cmpstr(value, ==, "value");
    free(value);

    /* Multiple keys can be extracted on one go. */
    char *other_value;
    sc_infofile_query(stream, "key", &value, "other-key", &other_value, NULL);
    g_assert_nonnull(value);
    g_assert_nonnull(other_value);
    g_assert_cmpstr(value, ==, "value");
    g_assert_cmpstr(other_value, ==, "other-value");
    free(value);
    free(other_value);

    /* Order in which keys are extracted does not matter. */
    sc_infofile_query(stream, "other-key", &other_value, "key", &value, NULL);
    g_assert_nonnull(value);
    g_assert_nonnull(other_value);
    g_assert_cmpstr(value, ==, "value");
    g_assert_cmpstr(other_value, ==, "other-value");
    free(value);
    free(other_value);

    /* When duplicate keys are present the first value is extracted. */
    char *dup_value;
    sc_infofile_query(stream, "dup-key", &dup_value, NULL);
    g_assert_nonnull(dup_value);
    g_assert_cmpstr(dup_value, ==, "value-one");
    free(dup_value);

    fclose(stream);
}

static void __attribute__((constructor)) init(void) { g_test_add_func("/infofile/query", test_infofile_query); }
