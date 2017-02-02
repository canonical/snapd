/*
 * Copyright (C) 2016-2017 Canonical Ltd
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

#include "string-utils.h"
#include "string-utils.c"

#include <glib.h>

static void test_sc_streq()
{
	g_assert_false(sc_streq(NULL, NULL));
	g_assert_false(sc_streq(NULL, "text"));
	g_assert_false(sc_streq("text", NULL));
	g_assert_false(sc_streq("foo", "bar"));
	g_assert_false(sc_streq("foo", "barbar"));
	g_assert_false(sc_streq("foofoo", "bar"));
	g_assert_true(sc_streq("text", "text"));
	g_assert_true(sc_streq("", ""));
}

static void test_sc_endswith()
{
	// NULL doesn't end with anything, nothing ends with NULL
	g_assert_false(sc_endswith("", NULL));
	g_assert_false(sc_endswith(NULL, ""));
	g_assert_false(sc_endswith(NULL, NULL));
	// Empty string ends with an empty string
	g_assert_true(sc_endswith("", ""));
	// Ends-with (matches)
	g_assert_true(sc_endswith("foobar", "bar"));
	g_assert_true(sc_endswith("foobar", "ar"));
	g_assert_true(sc_endswith("foobar", "r"));
	g_assert_true(sc_endswith("foobar", ""));
	g_assert_true(sc_endswith("bar", "bar"));
	// Ends-with (non-matches)
	g_assert_false(sc_endswith("foobar", "quux"));
	g_assert_false(sc_endswith("", "bar"));
	g_assert_false(sc_endswith("b", "bar"));
	g_assert_false(sc_endswith("ba", "bar"));
}

static void test_sc_must_snprintf()
{
	char buf[5];
	sc_must_snprintf(buf, sizeof buf, "1234");
	g_assert_cmpstr(buf, ==, "1234");
}

static void test_sc_must_snprintf__fail()
{
	if (g_test_subprocess()) {
		char buf[5];
		sc_must_snprintf(buf, sizeof buf, "12345");
		g_test_message("expected sc_must_snprintf not to return");
		g_test_fail();
		return;
	}
	g_test_trap_subprocess(NULL, 0, 0);
	g_test_trap_assert_failed();
	g_test_trap_assert_stderr("cannot format string: 1234\n");
}

// Check that appending to a buffer works OK.
static void test_sc_string_append()
{
	union {
		char bigbuf[6];
		struct {
			signed char canary1;
			char buf[4];
			signed char canary2;
		};
	} data = {
		.buf = {
	'f', '\0', 0xFF, 0xFF},.canary1 = ~0,.canary2 = ~0,};

	// Sanity check, ensure that the layout of structures is as spelled above.
	// (first canary1, then buf and finally canary2.
	g_assert_cmpint(((char *)&data.buf[0]) - ((char *)&data.canary1), ==,
			1);
	g_assert_cmpint(((char *)&data.buf[4]) - ((char *)&data.canary2), ==,
			0);

	sc_string_append(data.buf, sizeof data.buf, "oo");

	// Check that we didn't corrupt either canary.
	g_assert_cmpint(data.canary1, ==, ~0);
	g_assert_cmpint(data.canary2, ==, ~0);

	// Check that we got the result that was expected.
	g_assert_cmpstr(data.buf, ==, "foo");
}

// Check that appending an empty string to a full buffer is valid.
static void test_sc_string_append__empty_to_full()
{
	union {
		char bigbuf[6];
		struct {
			signed char canary1;
			char buf[4];
			signed char canary2;
		};
	} data = {
		.buf = {
	'f', 'o', 'o', '\0'},.canary1 = ~0,.canary2 = ~0,};

	// Sanity check, ensure that the layout of structures is as spelled above.
	// (first canary1, then buf and finally canary2.
	g_assert_cmpint(((char *)&data.buf[0]) - ((char *)&data.canary1), ==,
			1);
	g_assert_cmpint(((char *)&data.buf[4]) - ((char *)&data.canary2), ==,
			0);

	sc_string_append(data.buf, sizeof data.buf, "");

	// Check that we didn't corrupt either canary.
	g_assert_cmpint(data.canary1, ==, ~0);
	g_assert_cmpint(data.canary2, ==, ~0);

	// Check that we got the result that was expected.
	g_assert_cmpstr(data.buf, ==, "foo");
}

// Check that the overflow detection works.
static void test_sc_string_append__overflow()
{
	if (g_test_subprocess()) {
		char buf[4] = { 0, };

		// Try to append a string that's one character too long.
		sc_string_append(buf, sizeof buf, "1234");

		g_test_message("expected sc_string_append not to return");
		g_test_fail();
		return;
	}
	g_test_trap_subprocess(NULL, 0, 0);
	g_test_trap_assert_failed();
	g_test_trap_assert_stderr
	    ("cannot append string: buffer overflow of 1 byte(s)\n");
}

// Check that the uninitialized buffer detection works.
static void test_sc_string_append__uninitialized_buf()
{
	if (g_test_subprocess()) {
		char buf[4] = { 0xFF, 0xFF, 0xFF, 0xFF };
		volatile char canary = 0;

		// Try to append a string to a buffer which is not a valic C-string.
		sc_string_append(buf, sizeof buf, "");

		g_test_message("expected sc_string_append not to return");
		g_test_fail();
		return;
	}
	g_test_trap_subprocess(NULL, 0, 0);
	g_test_trap_assert_failed();
	g_test_trap_assert_stderr
	    ("cannot append string: uninitialized buffer detected\n");
}

// Check that `buf' cannot be NULL.
static void test_sc_string_append__NULL_buf()
{
	if (g_test_subprocess()) {
		char buf[4];

		sc_string_append(NULL, sizeof buf, "foo");

		g_test_message("expected sc_string_append not to return");
		g_test_fail();
		return;
	}
	g_test_trap_subprocess(NULL, 0, 0);
	g_test_trap_assert_failed();
	g_test_trap_assert_stderr("cannot append string: buffer is NULL\n");
}

// Check that `src' cannot be NULL.
static void test_sc_string_append__NULL_str()
{
	if (g_test_subprocess()) {
		char buf[4];

		sc_string_append(buf, sizeof buf, NULL);

		g_test_message("expected sc_string_append not to return");
		g_test_fail();
		return;
	}
	g_test_trap_subprocess(NULL, 0, 0);
	g_test_trap_assert_failed();
	g_test_trap_assert_stderr("cannot append string: string is NULL\n");
}

static void __attribute__ ((constructor)) init()
{
	g_test_add_func("/string-utils/sc_streq", test_sc_streq);
	g_test_add_func("/string-utils/sc_endswith", test_sc_endswith);
	g_test_add_func("/string-utils/sc_must_snprintf",
			test_sc_must_snprintf);
	g_test_add_func("/string-utils/sc_must_snprintf/fail",
			test_sc_must_snprintf__fail);
	g_test_add_func("/string-utils/sc_string_append/normal",
			test_sc_string_append);
	g_test_add_func("/string-utils/sc_string_append/empty_to_full",
			test_sc_string_append__empty_to_full);
	g_test_add_func("/string-utils/sc_string_append/overflow",
			test_sc_string_append__overflow);
	g_test_add_func("/string-utils/sc_string_append/uninitialized_buf",
			test_sc_string_append__uninitialized_buf);
	g_test_add_func("/string-utils/test_sc_string_append/NULL_buf",
			test_sc_string_append__NULL_buf);
	g_test_add_func("/string-utils/test_sc_string_append/NULL_str",
			test_sc_string_append__NULL_str);
}
