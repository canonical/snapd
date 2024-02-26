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

#include <glib.h>
#include <unistd.h>

#include "infofile.c"

static void test_infofile_get_key(void) {
    int rc;
    sc_error *err;

    char text[] =
        "key=value\n"
        "other-key=other-value\n"
        "dup-key=value-one\n"
        "dup-key=value-two\n";
    FILE *stream = fmemopen(text, sizeof text - 1, "r");
    g_assert_nonnull(stream);

    char *value;

    /* Caller must provide the stream to scan. */
    rc = sc_infofile_get_key(NULL, "key", &value, &err);
    g_assert_cmpint(rc, ==, -1);
    g_assert_nonnull(err);
    g_assert_cmpstr(sc_error_domain(err), ==, SC_LIBSNAP_DOMAIN);
    g_assert_cmpint(sc_error_code(err), ==, SC_API_MISUSE);
    g_assert_cmpstr(sc_error_msg(err), ==, "stream cannot be NULL");
    sc_error_free(err);

    /* Caller must provide the key to look for. */
    rc = sc_infofile_get_key(stream, NULL, &value, &err);
    g_assert_cmpint(rc, ==, -1);
    g_assert_nonnull(err);
    g_assert_cmpstr(sc_error_domain(err), ==, SC_LIBSNAP_DOMAIN);
    g_assert_cmpint(sc_error_code(err), ==, SC_API_MISUSE);
    g_assert_cmpstr(sc_error_msg(err), ==, "key cannot be NULL");
    sc_error_free(err);

    /* Caller must provide storage for the value. */
    rc = sc_infofile_get_key(stream, "key", NULL, &err);
    g_assert_cmpint(rc, ==, -1);
    g_assert_nonnull(err);
    g_assert_cmpstr(sc_error_domain(err), ==, SC_LIBSNAP_DOMAIN);
    g_assert_cmpint(sc_error_code(err), ==, SC_API_MISUSE);
    g_assert_cmpstr(sc_error_msg(err), ==, "value cannot be NULL");
    sc_error_free(err);

    /* Keys that are not found get NULL values. */
    value = (void *)0xfefefefe;
    rewind(stream);
    rc = sc_infofile_get_key(stream, "missing-key", &value, &err);
    g_assert_cmpint(rc, ==, 0);
    g_assert_null(err);
    g_assert_null(value);

    /* Keys that are found get strdup-duplicated values. */
    value = NULL;
    rewind(stream);
    rc = sc_infofile_get_key(stream, "key", &value, &err);
    g_assert_cmpint(rc, ==, 0);
    g_assert_null(err);
    g_assert_nonnull(value);
    g_assert_cmpstr(value, ==, "value");
    free(value);

    /* When duplicate keys are present the first value is extracted. */
    char *dup_value;
    rewind(stream);
    rc = sc_infofile_get_key(stream, "dup-key", &dup_value, &err);
    g_assert_cmpint(rc, ==, 0);
    g_assert_null(err);
    g_assert_nonnull(dup_value);
    g_assert_cmpstr(dup_value, ==, "value-one");
    free(dup_value);

    fclose(stream);

    /* Key without a value. */
    char *tricky_value;
    char tricky1[] = "key\n";
    stream = fmemopen(tricky1, sizeof tricky1 - 1, "r");
    g_assert_nonnull(stream);
    rc = sc_infofile_get_key(stream, "key", &tricky_value, &err);
    g_assert_cmpint(rc, ==, -1);
    g_assert_nonnull(err);
    g_assert_cmpstr(sc_error_domain(err), ==, SC_LIBSNAP_DOMAIN);
    g_assert_cmpint(sc_error_code(err), ==, 0);
    g_assert_cmpstr(sc_error_msg(err), ==, "line 1 is not a key=value assignment");
    g_assert_null(tricky_value);
    sc_error_free(err);
    fclose(stream);

    /* Key-value pair with embedded NUL byte. */
    char tricky2[] = "key=value\0garbage\n";
    stream = fmemopen(tricky2, sizeof tricky2 - 1, "r");
    g_assert_nonnull(stream);
    rc = sc_infofile_get_key(stream, "key", &tricky_value, &err);
    g_assert_cmpint(rc, ==, -1);
    g_assert_nonnull(err);
    g_assert_cmpstr(sc_error_domain(err), ==, SC_LIBSNAP_DOMAIN);
    g_assert_cmpint(sc_error_code(err), ==, 0);
    g_assert_cmpstr(sc_error_msg(err), ==, "line 1 contains NUL byte");
    g_assert_null(tricky_value);
    sc_error_free(err);
    fclose(stream);

    /* Key with empty value but without trailing newline. */
    char tricky3[] = "key=";
    stream = fmemopen(tricky3, sizeof tricky3 - 1, "r");
    g_assert_nonnull(stream);
    rc = sc_infofile_get_key(stream, "key", &tricky_value, &err);
    g_assert_cmpint(rc, ==, -1);
    g_assert_nonnull(err);
    g_assert_cmpstr(sc_error_domain(err), ==, SC_LIBSNAP_DOMAIN);
    g_assert_cmpint(sc_error_code(err), ==, 0);
    g_assert_cmpstr(sc_error_msg(err), ==, "line 1 does not end with a newline");
    g_assert_null(tricky_value);
    sc_error_free(err);
    fclose(stream);

    /* Key with empty value with a trailing newline (which is also valid). */
    char tricky4[] = "key=\n";
    stream = fmemopen(tricky4, sizeof tricky4 - 1, "r");
    g_assert_nonnull(stream);
    rc = sc_infofile_get_key(stream, "key", &tricky_value, &err);
    g_assert_cmpint(rc, ==, 0);
    g_assert_null(err);
    g_assert_cmpstr(tricky_value, ==, "");
    sc_error_free(err);
    fclose(stream);
    free(tricky_value);

    /* The equals character alone (key is empty) */
    char tricky5[] = "=\n";
    stream = fmemopen(tricky5, sizeof tricky5 - 1, "r");
    g_assert_nonnull(stream);
    rc = sc_infofile_get_key(stream, "key", &tricky_value, &err);
    g_assert_cmpint(rc, ==, -1);
    g_assert_nonnull(err);
    g_assert_cmpstr(sc_error_domain(err), ==, SC_LIBSNAP_DOMAIN);
    g_assert_cmpint(sc_error_code(err), ==, 0);
    g_assert_cmpstr(sc_error_msg(err), ==, "line 1 contains empty key");
    g_assert_null(tricky_value);
    sc_error_free(err);
    fclose(stream);

    /* Unexpected section */
    char tricky6[] = "[section]\n";
    stream = fmemopen(tricky6, sizeof tricky6 - 1, "r");
    g_assert_nonnull(stream);
    rc = sc_infofile_get_key(stream, "key", &tricky_value, &err);
    g_assert_cmpint(rc, ==, -1);
    g_assert_nonnull(err);
    g_assert_cmpstr(sc_error_domain(err), ==, SC_LIBSNAP_DOMAIN);
    g_assert_cmpint(sc_error_code(err), ==, 0);
    g_assert_cmpstr(sc_error_msg(err), ==, "line 1 contains unexpected section");
    g_assert_null(tricky_value);
    sc_error_free(err);
    fclose(stream);
}

static void test_infofile_get_ini_key(void) {
    int rc;
    sc_error *err;

    char text[] =
        "[section1]\n"
        "key=value\n"
        "[section2]\n"
        "key2=value-two\n"
        "other-key2=other-value-two\n"
        "key=value-one-two\n";
    FILE *stream = fmemopen(text, sizeof text - 1, "r");
    g_assert_nonnull(stream);

    char *value;

    /* Key in matching in the first section */
    value = NULL;
    rewind(stream);
    rc = sc_infofile_get_ini_section_key(stream, "section1", "key", &value, &err);
    g_assert_cmpint(rc, ==, 0);
    g_assert_null(err);
    g_assert_nonnull(value);
    g_assert_cmpstr(value, ==, "value");
    free(value);

    /* Key matching in the second section */
    value = NULL;
    rewind(stream);
    rc = sc_infofile_get_ini_section_key(stream, "section2", "key2", &value, &err);
    g_assert_cmpint(rc, ==, 0);
    g_assert_null(err);
    g_assert_nonnull(value);
    g_assert_cmpstr(value, ==, "value-two");
    free(value);

    /* Key matching in the second section (identical to the key from 1st
     * section) */
    value = NULL;
    rewind(stream);
    rc = sc_infofile_get_ini_section_key(stream, "section2", "key", &value, &err);
    g_assert_cmpint(rc, ==, 0);
    g_assert_null(err);
    g_assert_nonnull(value);
    g_assert_cmpstr(value, ==, "value-one-two");
    free(value);

    /* No matching section */
    value = NULL;
    rewind(stream);
    rc = sc_infofile_get_ini_section_key(stream, "section-x", "key", &value, &err);
    g_assert_cmpint(rc, ==, 0);
    g_assert_null(err);
    g_assert_null(value);

    /* Invalid empty section name */
    value = NULL;
    rewind(stream);
    rc = sc_infofile_get_ini_section_key(stream, "", "key", &value, &err);
    g_assert_cmpint(rc, ==, -1);
    g_assert_nonnull(err);
    g_assert_cmpstr(sc_error_domain(err), ==, SC_LIBSNAP_DOMAIN);
    g_assert_cmpint(sc_error_code(err), ==, SC_API_MISUSE);
    g_assert_cmpstr(sc_error_msg(err), ==, "section name cannot be empty");
    g_assert_null(value);
    sc_error_free(err);

    /* Malformed section */
    value = NULL;
    char malformed[] = "[section\n";
    stream = fmemopen(malformed, sizeof malformed - 1, "r");
    g_assert_nonnull(stream);
    rc = sc_infofile_get_ini_section_key(stream, "section", "key", &value, &err);
    g_assert_cmpint(rc, ==, -1);
    g_assert_nonnull(err);
    g_assert_cmpstr(sc_error_domain(err), ==, SC_LIBSNAP_DOMAIN);
    g_assert_cmpint(sc_error_code(err), ==, 0);
    g_assert_cmpstr(sc_error_msg(err), ==, "line 1 is not a valid ini section");
    g_assert_null(value);
    sc_error_free(err);
    fclose(stream);
}

static void __attribute__((constructor)) init(void) {
    g_test_add_func("/infofile/get_key", test_infofile_get_key);
    g_test_add_func("/infofile/get_ini_key", test_infofile_get_ini_key);
}
