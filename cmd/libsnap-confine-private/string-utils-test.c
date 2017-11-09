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

static void test_sc_streq(void)
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

static void test_sc_endswith(void)
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

static void test_sc_must_snprintf(void)
{
	char buf[5] = { 0 };
	sc_must_snprintf(buf, sizeof buf, "1234");
	g_assert_cmpstr(buf, ==, "1234");
}

static void test_sc_must_snprintf__fail(void)
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
static void test_sc_string_append(void)
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
static void test_sc_string_append__empty_to_full(void)
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
static void test_sc_string_append__overflow(void)
{
	if (g_test_subprocess()) {
		char buf[4] = { 0 };

		// Try to append a string that's one character too long.
		sc_string_append(buf, sizeof buf, "1234");

		g_test_message("expected sc_string_append not to return");
		g_test_fail();
		return;
	}
	g_test_trap_subprocess(NULL, 0, 0);
	g_test_trap_assert_failed();
	g_test_trap_assert_stderr
	    ("cannot append string: str is too long or unterminated\n");
}

// Check that the uninitialized buffer detection works.
static void test_sc_string_append__uninitialized_buf(void)
{
	if (g_test_subprocess()) {
		char buf[4] = { 0xFF, 0xFF, 0xFF, 0xFF };

		// Try to append a string to a buffer which is not a valic C-string.
		sc_string_append(buf, sizeof buf, "");

		g_test_message("expected sc_string_append not to return");
		g_test_fail();
		return;
	}
	g_test_trap_subprocess(NULL, 0, 0);
	g_test_trap_assert_failed();
	g_test_trap_assert_stderr
	    ("cannot append string: dst is unterminated\n");
}

// Check that `buf' cannot be NULL.
static void test_sc_string_append__NULL_buf(void)
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
static void test_sc_string_append__NULL_str(void)
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

static void test_sc_string_init__normal(void)
{
	char buf[1] = { 0xFF };

	sc_string_init(buf, sizeof buf);
	g_assert_cmpint(buf[0], ==, 0);
}

static void test_sc_string_init__empty_buf(void)
{
	if (g_test_subprocess()) {
		char buf[1] = { 0xFF };

		sc_string_init(buf, 0);

		g_test_message("expected sc_string_init not to return");
		g_test_fail();
		return;
	}
	g_test_trap_subprocess(NULL, 0, 0);
	g_test_trap_assert_failed();
	g_test_trap_assert_stderr
	    ("cannot initialize string, buffer is too small\n");
}

static void test_sc_string_init__NULL_buf(void)
{
	if (g_test_subprocess()) {
		sc_string_init(NULL, 1);

		g_test_message("expected sc_string_init not to return");
		g_test_fail();
		return;
	}
	g_test_trap_subprocess(NULL, 0, 0);
	g_test_trap_assert_failed();
	g_test_trap_assert_stderr("cannot initialize string, buffer is NULL\n");
}

static void test_sc_string_append_char__uninitialized_buf(void)
{
	if (g_test_subprocess()) {
		char buf[2] = { 0xFF, 0xFF };
		sc_string_append_char(buf, sizeof buf, 'a');

		g_test_message("expected sc_string_append_char not to return");
		g_test_fail();
		return;
	}
	g_test_trap_subprocess(NULL, 0, 0);
	g_test_trap_assert_failed();
	g_test_trap_assert_stderr
	    ("cannot append character: dst is unterminated\n");
}

static void test_sc_string_append_char__NULL_buf(void)
{
	if (g_test_subprocess()) {
		sc_string_append_char(NULL, 2, 'a');

		g_test_message("expected sc_string_append_char not to return");
		g_test_fail();
		return;
	}
	g_test_trap_subprocess(NULL, 0, 0);
	g_test_trap_assert_failed();
	g_test_trap_assert_stderr("cannot append character: buffer is NULL\n");
}

static void test_sc_string_append_char__overflow(void)
{
	if (g_test_subprocess()) {
		char buf[1] = { 0 };
		sc_string_append_char(buf, sizeof buf, 'a');

		g_test_message("expected sc_string_append_char not to return");
		g_test_fail();
		return;
	}
	g_test_trap_subprocess(NULL, 0, 0);
	g_test_trap_assert_failed();
	g_test_trap_assert_stderr
	    ("cannot append character: not enough space\n");
}

static void test_sc_string_append_char__invalid_zero(void)
{
	if (g_test_subprocess()) {
		char buf[2] = { 0 };
		sc_string_append_char(buf, sizeof buf, '\0');

		g_test_message("expected sc_string_append_char not to return");
		g_test_fail();
		return;
	}
	g_test_trap_subprocess(NULL, 0, 0);
	g_test_trap_assert_failed();
	g_test_trap_assert_stderr
	    ("cannot append character: cannot append string terminator\n");
}

static void test_sc_string_append_char__normal(void)
{
	char buf[16];
	size_t len;
	sc_string_init(buf, sizeof buf);

	len = sc_string_append_char(buf, sizeof buf, 'h');
	g_assert_cmpstr(buf, ==, "h");
	g_assert_cmpint(len, ==, 1);
	len = sc_string_append_char(buf, sizeof buf, 'e');
	g_assert_cmpstr(buf, ==, "he");
	g_assert_cmpint(len, ==, 2);
	len = sc_string_append_char(buf, sizeof buf, 'l');
	g_assert_cmpstr(buf, ==, "hel");
	g_assert_cmpint(len, ==, 3);
	len = sc_string_append_char(buf, sizeof buf, 'l');
	g_assert_cmpstr(buf, ==, "hell");
	g_assert_cmpint(len, ==, 4);
	len = sc_string_append_char(buf, sizeof buf, 'o');
	g_assert_cmpstr(buf, ==, "hello");
	g_assert_cmpint(len, ==, 5);
}

static void test_sc_string_append_char_pair__uninitialized_buf(void)
{
	if (g_test_subprocess()) {
		char buf[3] = { 0xFF, 0xFF, 0xFF };
		sc_string_append_char_pair(buf, sizeof buf, 'a', 'b');

		g_test_message
		    ("expected sc_string_append_char_pair not to return");
		g_test_fail();
		return;
	}
	g_test_trap_subprocess(NULL, 0, 0);
	g_test_trap_assert_failed();
	g_test_trap_assert_stderr
	    ("cannot append character pair: dst is unterminated\n");
}

static void test_sc_string_append_char_pair__NULL_buf(void)
{
	if (g_test_subprocess()) {
		sc_string_append_char_pair(NULL, 3, 'a', 'b');

		g_test_message
		    ("expected sc_string_append_char_pair not to return");
		g_test_fail();
		return;
	}
	g_test_trap_subprocess(NULL, 0, 0);
	g_test_trap_assert_failed();
	g_test_trap_assert_stderr
	    ("cannot append character pair: buffer is NULL\n");
}

static void test_sc_string_append_char_pair__overflow(void)
{
	if (g_test_subprocess()) {
		char buf[2] = { 0 };
		sc_string_append_char_pair(buf, sizeof buf, 'a', 'b');

		g_test_message
		    ("expected sc_string_append_char_pair not to return");
		g_test_fail();
		return;
	}
	g_test_trap_subprocess(NULL, 0, 0);
	g_test_trap_assert_failed();
	g_test_trap_assert_stderr
	    ("cannot append character pair: not enough space\n");
}

static void test_sc_string_append_char_pair__invalid_zero_c1(void)
{
	if (g_test_subprocess()) {
		char buf[3] = { 0 };
		sc_string_append_char_pair(buf, sizeof buf, '\0', 'a');

		g_test_message
		    ("expected sc_string_append_char_pair not to return");
		g_test_fail();
		return;
	}
	g_test_trap_subprocess(NULL, 0, 0);
	g_test_trap_assert_failed();
	g_test_trap_assert_stderr
	    ("cannot append character pair: cannot append string terminator\n");
}

static void test_sc_string_append_char_pair__invalid_zero_c2(void)
{
	if (g_test_subprocess()) {
		char buf[3] = { 0 };
		sc_string_append_char_pair(buf, sizeof buf, 'a', '\0');

		g_test_message
		    ("expected sc_string_append_char_pair not to return");
		g_test_fail();
		return;
	}
	g_test_trap_subprocess(NULL, 0, 0);
	g_test_trap_assert_failed();
	g_test_trap_assert_stderr
	    ("cannot append character pair: cannot append string terminator\n");
}

static void test_sc_string_append_char_pair__normal(void)
{
	char buf[16];
	size_t len;
	sc_string_init(buf, sizeof buf);

	len = sc_string_append_char_pair(buf, sizeof buf, 'h', 'e');
	g_assert_cmpstr(buf, ==, "he");
	g_assert_cmpint(len, ==, 2);
	len = sc_string_append_char_pair(buf, sizeof buf, 'l', 'l');
	g_assert_cmpstr(buf, ==, "hell");
	g_assert_cmpint(len, ==, 4);
	len = sc_string_append_char_pair(buf, sizeof buf, 'o', '!');
	g_assert_cmpstr(buf, ==, "hello!");
	g_assert_cmpint(len, ==, 6);
}

static void test_sc_string_quote_NULL_str(void)
{
	if (g_test_subprocess()) {
		char buf[16] = { 0 };
		sc_string_quote(buf, sizeof buf, NULL);

		g_test_message("expected sc_string_quote not to return");
		g_test_fail();
		return;
	}
	g_test_trap_subprocess(NULL, 0, 0);
	g_test_trap_assert_failed();
	g_test_trap_assert_stderr("cannot quote string: string is NULL\n");
}

static void test_quoting_of(bool tested[], int c, const char *expected)
{
	char buf[16];

	g_assert_cmpint(c, >=, 0);
	g_assert_cmpint(c, <=, 255);

	// Create an input string with one character.
	char input[2] = { (unsigned char)c, 0 };
	sc_string_quote(buf, sizeof buf, input);

	// Ensure it was quoted as we expected.
	g_assert_cmpstr(buf, ==, expected);

	tested[c] = true;
}

static void test_sc_string_quote(void)
{
#define DQ "\""
	char buf[16];
	bool is_tested[256] = { false };

	// Exhaustive test for quoting of every 8bit input.  This is very verbose
	// but the goal is to have a very obvious and correct test that ensures no
	// edge case is lost.
	//
	// block 1: 0x00 - 0x0f
	test_quoting_of(is_tested, 0x00, DQ "" DQ);
	test_quoting_of(is_tested, 0x01, DQ "\\x01" DQ);
	test_quoting_of(is_tested, 0x02, DQ "\\x02" DQ);
	test_quoting_of(is_tested, 0x03, DQ "\\x03" DQ);
	test_quoting_of(is_tested, 0x04, DQ "\\x04" DQ);
	test_quoting_of(is_tested, 0x05, DQ "\\x05" DQ);
	test_quoting_of(is_tested, 0x06, DQ "\\x06" DQ);
	test_quoting_of(is_tested, 0x07, DQ "\\x07" DQ);
	test_quoting_of(is_tested, 0x08, DQ "\\x08" DQ);
	test_quoting_of(is_tested, 0x09, DQ "\\t" DQ);
	test_quoting_of(is_tested, 0x0a, DQ "\\n" DQ);
	test_quoting_of(is_tested, 0x0b, DQ "\\v" DQ);
	test_quoting_of(is_tested, 0x0c, DQ "\\x0c" DQ);
	test_quoting_of(is_tested, 0x0d, DQ "\\r" DQ);
	test_quoting_of(is_tested, 0x0e, DQ "\\x0e" DQ);
	test_quoting_of(is_tested, 0x0f, DQ "\\x0f" DQ);
	// block 2: 0x10 - 0x1f
	test_quoting_of(is_tested, 0x10, DQ "\\x10" DQ);
	test_quoting_of(is_tested, 0x11, DQ "\\x11" DQ);
	test_quoting_of(is_tested, 0x12, DQ "\\x12" DQ);
	test_quoting_of(is_tested, 0x13, DQ "\\x13" DQ);
	test_quoting_of(is_tested, 0x14, DQ "\\x14" DQ);
	test_quoting_of(is_tested, 0x15, DQ "\\x15" DQ);
	test_quoting_of(is_tested, 0x16, DQ "\\x16" DQ);
	test_quoting_of(is_tested, 0x17, DQ "\\x17" DQ);
	test_quoting_of(is_tested, 0x18, DQ "\\x18" DQ);
	test_quoting_of(is_tested, 0x19, DQ "\\x19" DQ);
	test_quoting_of(is_tested, 0x1a, DQ "\\x1a" DQ);
	test_quoting_of(is_tested, 0x1b, DQ "\\x1b" DQ);
	test_quoting_of(is_tested, 0x1c, DQ "\\x1c" DQ);
	test_quoting_of(is_tested, 0x1d, DQ "\\x1d" DQ);
	test_quoting_of(is_tested, 0x1e, DQ "\\x1e" DQ);
	test_quoting_of(is_tested, 0x1f, DQ "\\x1f" DQ);
	// block 3: 0x20 - 0x2f
	test_quoting_of(is_tested, 0x20, DQ " " DQ);
	test_quoting_of(is_tested, 0x21, DQ "!" DQ);
	test_quoting_of(is_tested, 0x22, DQ "\\\"" DQ);
	test_quoting_of(is_tested, 0x23, DQ "#" DQ);
	test_quoting_of(is_tested, 0x24, DQ "$" DQ);
	test_quoting_of(is_tested, 0x25, DQ "%" DQ);
	test_quoting_of(is_tested, 0x26, DQ "&" DQ);
	test_quoting_of(is_tested, 0x27, DQ "'" DQ);
	test_quoting_of(is_tested, 0x28, DQ "(" DQ);
	test_quoting_of(is_tested, 0x29, DQ ")" DQ);
	test_quoting_of(is_tested, 0x2a, DQ "*" DQ);
	test_quoting_of(is_tested, 0x2b, DQ "+" DQ);
	test_quoting_of(is_tested, 0x2c, DQ "," DQ);
	test_quoting_of(is_tested, 0x2d, DQ "-" DQ);
	test_quoting_of(is_tested, 0x2e, DQ "." DQ);
	test_quoting_of(is_tested, 0x2f, DQ "/" DQ);
	// block 4: 0x30 - 0x3f
	test_quoting_of(is_tested, 0x30, DQ "0" DQ);
	test_quoting_of(is_tested, 0x31, DQ "1" DQ);
	test_quoting_of(is_tested, 0x32, DQ "2" DQ);
	test_quoting_of(is_tested, 0x33, DQ "3" DQ);
	test_quoting_of(is_tested, 0x34, DQ "4" DQ);
	test_quoting_of(is_tested, 0x35, DQ "5" DQ);
	test_quoting_of(is_tested, 0x36, DQ "6" DQ);
	test_quoting_of(is_tested, 0x37, DQ "7" DQ);
	test_quoting_of(is_tested, 0x38, DQ "8" DQ);
	test_quoting_of(is_tested, 0x39, DQ "9" DQ);
	test_quoting_of(is_tested, 0x3a, DQ ":" DQ);
	test_quoting_of(is_tested, 0x3b, DQ ";" DQ);
	test_quoting_of(is_tested, 0x3c, DQ "<" DQ);
	test_quoting_of(is_tested, 0x3d, DQ "=" DQ);
	test_quoting_of(is_tested, 0x3e, DQ ">" DQ);
	test_quoting_of(is_tested, 0x3f, DQ "?" DQ);
	// block 5: 0x40 - 0x4f
	test_quoting_of(is_tested, 0x40, DQ "@" DQ);
	test_quoting_of(is_tested, 0x41, DQ "A" DQ);
	test_quoting_of(is_tested, 0x42, DQ "B" DQ);
	test_quoting_of(is_tested, 0x43, DQ "C" DQ);
	test_quoting_of(is_tested, 0x44, DQ "D" DQ);
	test_quoting_of(is_tested, 0x45, DQ "E" DQ);
	test_quoting_of(is_tested, 0x46, DQ "F" DQ);
	test_quoting_of(is_tested, 0x47, DQ "G" DQ);
	test_quoting_of(is_tested, 0x48, DQ "H" DQ);
	test_quoting_of(is_tested, 0x49, DQ "I" DQ);
	test_quoting_of(is_tested, 0x4a, DQ "J" DQ);
	test_quoting_of(is_tested, 0x4b, DQ "K" DQ);
	test_quoting_of(is_tested, 0x4c, DQ "L" DQ);
	test_quoting_of(is_tested, 0x4d, DQ "M" DQ);
	test_quoting_of(is_tested, 0x4e, DQ "N" DQ);
	test_quoting_of(is_tested, 0x4f, DQ "O" DQ);
	// block 6: 0x50 - 0x5f
	test_quoting_of(is_tested, 0x50, DQ "P" DQ);
	test_quoting_of(is_tested, 0x51, DQ "Q" DQ);
	test_quoting_of(is_tested, 0x52, DQ "R" DQ);
	test_quoting_of(is_tested, 0x53, DQ "S" DQ);
	test_quoting_of(is_tested, 0x54, DQ "T" DQ);
	test_quoting_of(is_tested, 0x55, DQ "U" DQ);
	test_quoting_of(is_tested, 0x56, DQ "V" DQ);
	test_quoting_of(is_tested, 0x57, DQ "W" DQ);
	test_quoting_of(is_tested, 0x58, DQ "X" DQ);
	test_quoting_of(is_tested, 0x59, DQ "Y" DQ);
	test_quoting_of(is_tested, 0x5a, DQ "Z" DQ);
	test_quoting_of(is_tested, 0x5b, DQ "[" DQ);
	test_quoting_of(is_tested, 0x5c, DQ "\\\\" DQ);
	test_quoting_of(is_tested, 0x5d, DQ "]" DQ);
	test_quoting_of(is_tested, 0x5e, DQ "^" DQ);
	test_quoting_of(is_tested, 0x5f, DQ "_" DQ);
	// block 7: 0x60 - 0x6f
	test_quoting_of(is_tested, 0x60, DQ "`" DQ);
	test_quoting_of(is_tested, 0x61, DQ "a" DQ);
	test_quoting_of(is_tested, 0x62, DQ "b" DQ);
	test_quoting_of(is_tested, 0x63, DQ "c" DQ);
	test_quoting_of(is_tested, 0x64, DQ "d" DQ);
	test_quoting_of(is_tested, 0x65, DQ "e" DQ);
	test_quoting_of(is_tested, 0x66, DQ "f" DQ);
	test_quoting_of(is_tested, 0x67, DQ "g" DQ);
	test_quoting_of(is_tested, 0x68, DQ "h" DQ);
	test_quoting_of(is_tested, 0x69, DQ "i" DQ);
	test_quoting_of(is_tested, 0x6a, DQ "j" DQ);
	test_quoting_of(is_tested, 0x6b, DQ "k" DQ);
	test_quoting_of(is_tested, 0x6c, DQ "l" DQ);
	test_quoting_of(is_tested, 0x6d, DQ "m" DQ);
	test_quoting_of(is_tested, 0x6e, DQ "n" DQ);
	test_quoting_of(is_tested, 0x6f, DQ "o" DQ);
	// block 8: 0x70 - 0x7f
	test_quoting_of(is_tested, 0x70, DQ "p" DQ);
	test_quoting_of(is_tested, 0x71, DQ "q" DQ);
	test_quoting_of(is_tested, 0x72, DQ "r" DQ);
	test_quoting_of(is_tested, 0x73, DQ "s" DQ);
	test_quoting_of(is_tested, 0x74, DQ "t" DQ);
	test_quoting_of(is_tested, 0x75, DQ "u" DQ);
	test_quoting_of(is_tested, 0x76, DQ "v" DQ);
	test_quoting_of(is_tested, 0x77, DQ "w" DQ);
	test_quoting_of(is_tested, 0x78, DQ "x" DQ);
	test_quoting_of(is_tested, 0x79, DQ "y" DQ);
	test_quoting_of(is_tested, 0x7a, DQ "z" DQ);
	test_quoting_of(is_tested, 0x7b, DQ "{" DQ);
	test_quoting_of(is_tested, 0x7c, DQ "|" DQ);
	test_quoting_of(is_tested, 0x7d, DQ "}" DQ);
	test_quoting_of(is_tested, 0x7e, DQ "~" DQ);
	test_quoting_of(is_tested, 0x7f, DQ "\\x7f" DQ);
	// block 9 (8-bit): 0x80 - 0x8f
	test_quoting_of(is_tested, 0x80, DQ "\\x80" DQ);
	test_quoting_of(is_tested, 0x81, DQ "\\x81" DQ);
	test_quoting_of(is_tested, 0x82, DQ "\\x82" DQ);
	test_quoting_of(is_tested, 0x83, DQ "\\x83" DQ);
	test_quoting_of(is_tested, 0x84, DQ "\\x84" DQ);
	test_quoting_of(is_tested, 0x85, DQ "\\x85" DQ);
	test_quoting_of(is_tested, 0x86, DQ "\\x86" DQ);
	test_quoting_of(is_tested, 0x87, DQ "\\x87" DQ);
	test_quoting_of(is_tested, 0x88, DQ "\\x88" DQ);
	test_quoting_of(is_tested, 0x89, DQ "\\x89" DQ);
	test_quoting_of(is_tested, 0x8a, DQ "\\x8a" DQ);
	test_quoting_of(is_tested, 0x8b, DQ "\\x8b" DQ);
	test_quoting_of(is_tested, 0x8c, DQ "\\x8c" DQ);
	test_quoting_of(is_tested, 0x8d, DQ "\\x8d" DQ);
	test_quoting_of(is_tested, 0x8e, DQ "\\x8e" DQ);
	test_quoting_of(is_tested, 0x8f, DQ "\\x8f" DQ);
	// block 10 (8-bit): 0x90 - 0x9f
	test_quoting_of(is_tested, 0x90, DQ "\\x90" DQ);
	test_quoting_of(is_tested, 0x91, DQ "\\x91" DQ);
	test_quoting_of(is_tested, 0x92, DQ "\\x92" DQ);
	test_quoting_of(is_tested, 0x93, DQ "\\x93" DQ);
	test_quoting_of(is_tested, 0x94, DQ "\\x94" DQ);
	test_quoting_of(is_tested, 0x95, DQ "\\x95" DQ);
	test_quoting_of(is_tested, 0x96, DQ "\\x96" DQ);
	test_quoting_of(is_tested, 0x97, DQ "\\x97" DQ);
	test_quoting_of(is_tested, 0x98, DQ "\\x98" DQ);
	test_quoting_of(is_tested, 0x99, DQ "\\x99" DQ);
	test_quoting_of(is_tested, 0x9a, DQ "\\x9a" DQ);
	test_quoting_of(is_tested, 0x9b, DQ "\\x9b" DQ);
	test_quoting_of(is_tested, 0x9c, DQ "\\x9c" DQ);
	test_quoting_of(is_tested, 0x9d, DQ "\\x9d" DQ);
	test_quoting_of(is_tested, 0x9e, DQ "\\x9e" DQ);
	test_quoting_of(is_tested, 0x9f, DQ "\\x9f" DQ);
	// block 11 (8-bit): 0xa0 - 0xaf
	test_quoting_of(is_tested, 0xa0, DQ "\\xa0" DQ);
	test_quoting_of(is_tested, 0xa1, DQ "\\xa1" DQ);
	test_quoting_of(is_tested, 0xa2, DQ "\\xa2" DQ);
	test_quoting_of(is_tested, 0xa3, DQ "\\xa3" DQ);
	test_quoting_of(is_tested, 0xa4, DQ "\\xa4" DQ);
	test_quoting_of(is_tested, 0xa5, DQ "\\xa5" DQ);
	test_quoting_of(is_tested, 0xa6, DQ "\\xa6" DQ);
	test_quoting_of(is_tested, 0xa7, DQ "\\xa7" DQ);
	test_quoting_of(is_tested, 0xa8, DQ "\\xa8" DQ);
	test_quoting_of(is_tested, 0xa9, DQ "\\xa9" DQ);
	test_quoting_of(is_tested, 0xaa, DQ "\\xaa" DQ);
	test_quoting_of(is_tested, 0xab, DQ "\\xab" DQ);
	test_quoting_of(is_tested, 0xac, DQ "\\xac" DQ);
	test_quoting_of(is_tested, 0xad, DQ "\\xad" DQ);
	test_quoting_of(is_tested, 0xae, DQ "\\xae" DQ);
	test_quoting_of(is_tested, 0xaf, DQ "\\xaf" DQ);
	// block 12 (8-bit): 0xb0 - 0xbf
	test_quoting_of(is_tested, 0xb0, DQ "\\xb0" DQ);
	test_quoting_of(is_tested, 0xb1, DQ "\\xb1" DQ);
	test_quoting_of(is_tested, 0xb2, DQ "\\xb2" DQ);
	test_quoting_of(is_tested, 0xb3, DQ "\\xb3" DQ);
	test_quoting_of(is_tested, 0xb4, DQ "\\xb4" DQ);
	test_quoting_of(is_tested, 0xb5, DQ "\\xb5" DQ);
	test_quoting_of(is_tested, 0xb6, DQ "\\xb6" DQ);
	test_quoting_of(is_tested, 0xb7, DQ "\\xb7" DQ);
	test_quoting_of(is_tested, 0xb8, DQ "\\xb8" DQ);
	test_quoting_of(is_tested, 0xb9, DQ "\\xb9" DQ);
	test_quoting_of(is_tested, 0xba, DQ "\\xba" DQ);
	test_quoting_of(is_tested, 0xbb, DQ "\\xbb" DQ);
	test_quoting_of(is_tested, 0xbc, DQ "\\xbc" DQ);
	test_quoting_of(is_tested, 0xbd, DQ "\\xbd" DQ);
	test_quoting_of(is_tested, 0xbe, DQ "\\xbe" DQ);
	test_quoting_of(is_tested, 0xbf, DQ "\\xbf" DQ);
	// block 13 (8-bit): 0xc0 - 0xcf
	test_quoting_of(is_tested, 0xc0, DQ "\\xc0" DQ);
	test_quoting_of(is_tested, 0xc1, DQ "\\xc1" DQ);
	test_quoting_of(is_tested, 0xc2, DQ "\\xc2" DQ);
	test_quoting_of(is_tested, 0xc3, DQ "\\xc3" DQ);
	test_quoting_of(is_tested, 0xc4, DQ "\\xc4" DQ);
	test_quoting_of(is_tested, 0xc5, DQ "\\xc5" DQ);
	test_quoting_of(is_tested, 0xc6, DQ "\\xc6" DQ);
	test_quoting_of(is_tested, 0xc7, DQ "\\xc7" DQ);
	test_quoting_of(is_tested, 0xc8, DQ "\\xc8" DQ);
	test_quoting_of(is_tested, 0xc9, DQ "\\xc9" DQ);
	test_quoting_of(is_tested, 0xca, DQ "\\xca" DQ);
	test_quoting_of(is_tested, 0xcb, DQ "\\xcb" DQ);
	test_quoting_of(is_tested, 0xcc, DQ "\\xcc" DQ);
	test_quoting_of(is_tested, 0xcd, DQ "\\xcd" DQ);
	test_quoting_of(is_tested, 0xce, DQ "\\xce" DQ);
	test_quoting_of(is_tested, 0xcf, DQ "\\xcf" DQ);
	// block 14 (8-bit): 0xd0 - 0xdf
	test_quoting_of(is_tested, 0xd0, DQ "\\xd0" DQ);
	test_quoting_of(is_tested, 0xd1, DQ "\\xd1" DQ);
	test_quoting_of(is_tested, 0xd2, DQ "\\xd2" DQ);
	test_quoting_of(is_tested, 0xd3, DQ "\\xd3" DQ);
	test_quoting_of(is_tested, 0xd4, DQ "\\xd4" DQ);
	test_quoting_of(is_tested, 0xd5, DQ "\\xd5" DQ);
	test_quoting_of(is_tested, 0xd6, DQ "\\xd6" DQ);
	test_quoting_of(is_tested, 0xd7, DQ "\\xd7" DQ);
	test_quoting_of(is_tested, 0xd8, DQ "\\xd8" DQ);
	test_quoting_of(is_tested, 0xd9, DQ "\\xd9" DQ);
	test_quoting_of(is_tested, 0xda, DQ "\\xda" DQ);
	test_quoting_of(is_tested, 0xdb, DQ "\\xdb" DQ);
	test_quoting_of(is_tested, 0xdc, DQ "\\xdc" DQ);
	test_quoting_of(is_tested, 0xdd, DQ "\\xdd" DQ);
	test_quoting_of(is_tested, 0xde, DQ "\\xde" DQ);
	test_quoting_of(is_tested, 0xdf, DQ "\\xdf" DQ);
	// block 15 (8-bit): 0xe0 - 0xef
	test_quoting_of(is_tested, 0xe0, DQ "\\xe0" DQ);
	test_quoting_of(is_tested, 0xe1, DQ "\\xe1" DQ);
	test_quoting_of(is_tested, 0xe2, DQ "\\xe2" DQ);
	test_quoting_of(is_tested, 0xe3, DQ "\\xe3" DQ);
	test_quoting_of(is_tested, 0xe4, DQ "\\xe4" DQ);
	test_quoting_of(is_tested, 0xe5, DQ "\\xe5" DQ);
	test_quoting_of(is_tested, 0xe6, DQ "\\xe6" DQ);
	test_quoting_of(is_tested, 0xe7, DQ "\\xe7" DQ);
	test_quoting_of(is_tested, 0xe8, DQ "\\xe8" DQ);
	test_quoting_of(is_tested, 0xe9, DQ "\\xe9" DQ);
	test_quoting_of(is_tested, 0xea, DQ "\\xea" DQ);
	test_quoting_of(is_tested, 0xeb, DQ "\\xeb" DQ);
	test_quoting_of(is_tested, 0xec, DQ "\\xec" DQ);
	test_quoting_of(is_tested, 0xed, DQ "\\xed" DQ);
	test_quoting_of(is_tested, 0xee, DQ "\\xee" DQ);
	test_quoting_of(is_tested, 0xef, DQ "\\xef" DQ);
	// block 16 (8-bit): 0xf0 - 0xff
	test_quoting_of(is_tested, 0xf0, DQ "\\xf0" DQ);
	test_quoting_of(is_tested, 0xf1, DQ "\\xf1" DQ);
	test_quoting_of(is_tested, 0xf2, DQ "\\xf2" DQ);
	test_quoting_of(is_tested, 0xf3, DQ "\\xf3" DQ);
	test_quoting_of(is_tested, 0xf4, DQ "\\xf4" DQ);
	test_quoting_of(is_tested, 0xf5, DQ "\\xf5" DQ);
	test_quoting_of(is_tested, 0xf6, DQ "\\xf6" DQ);
	test_quoting_of(is_tested, 0xf7, DQ "\\xf7" DQ);
	test_quoting_of(is_tested, 0xf8, DQ "\\xf8" DQ);
	test_quoting_of(is_tested, 0xf9, DQ "\\xf9" DQ);
	test_quoting_of(is_tested, 0xfa, DQ "\\xfa" DQ);
	test_quoting_of(is_tested, 0xfb, DQ "\\xfb" DQ);
	test_quoting_of(is_tested, 0xfc, DQ "\\xfc" DQ);
	test_quoting_of(is_tested, 0xfd, DQ "\\xfd" DQ);
	test_quoting_of(is_tested, 0xfe, DQ "\\xfe" DQ);
	test_quoting_of(is_tested, 0xff, DQ "\\xff" DQ);

	// Ensure the search was exhaustive.
	for (int i = 0; i <= 0xff; ++i) {
		g_assert_true(is_tested[i]);
	}

	// Few extra tests (repeated) for specific things.

	// Smoke test
	sc_string_quote(buf, sizeof buf, "hello 123");
	g_assert_cmpstr(buf, ==, DQ "hello 123" DQ);

	// Whitespace
	sc_string_quote(buf, sizeof buf, "\n");
	g_assert_cmpstr(buf, ==, DQ "\\n" DQ);
	sc_string_quote(buf, sizeof buf, "\r");
	g_assert_cmpstr(buf, ==, DQ "\\r" DQ);
	sc_string_quote(buf, sizeof buf, "\t");
	g_assert_cmpstr(buf, ==, DQ "\\t" DQ);
	sc_string_quote(buf, sizeof buf, "\v");
	g_assert_cmpstr(buf, ==, DQ "\\v" DQ);

	// Escape character itself
	sc_string_quote(buf, sizeof buf, "\\");
	g_assert_cmpstr(buf, ==, DQ "\\\\" DQ);

	// Double quote character
	sc_string_quote(buf, sizeof buf, "\"");
	g_assert_cmpstr(buf, ==, DQ "\\\"" DQ);

#undef DQ
}

static void __attribute__ ((constructor)) init(void)
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
	g_test_add_func("/string-utils/sc_string_append/NULL_buf",
			test_sc_string_append__NULL_buf);
	g_test_add_func("/string-utils/sc_string_append/NULL_str",
			test_sc_string_append__NULL_str);
	g_test_add_func("/string-utils/sc_string_init/normal",
			test_sc_string_init__normal);
	g_test_add_func("/string-utils/sc_string_init/empty_buf",
			test_sc_string_init__empty_buf);
	g_test_add_func("/string-utils/sc_string_init/NULL_buf",
			test_sc_string_init__NULL_buf);
	g_test_add_func
	    ("/string-utils/sc_string_append_char__uninitialized_buf",
	     test_sc_string_append_char__uninitialized_buf);
	g_test_add_func("/string-utils/sc_string_append_char__NULL_buf",
			test_sc_string_append_char__NULL_buf);
	g_test_add_func("/string-utils/sc_string_append_char__overflow",
			test_sc_string_append_char__overflow);
	g_test_add_func("/string-utils/sc_string_append_char__invalid_zero",
			test_sc_string_append_char__invalid_zero);
	g_test_add_func("/string-utils/sc_string_append_char__normal",
			test_sc_string_append_char__normal);
	g_test_add_func
	    ("/string-utils/sc_string_append_char_pair__NULL_buf",
	     test_sc_string_append_char_pair__NULL_buf);
	g_test_add_func("/string-utils/sc_string_append_char_pair__overflow",
			test_sc_string_append_char_pair__overflow);
	g_test_add_func
	    ("/string-utils/sc_string_append_char_pair__invalid_zero_c1",
	     test_sc_string_append_char_pair__invalid_zero_c1);
	g_test_add_func
	    ("/string-utils/sc_string_append_char_pair__invalid_zero_c2",
	     test_sc_string_append_char_pair__invalid_zero_c2);
	g_test_add_func("/string-utils/sc_string_append_char_pair__normal",
			test_sc_string_append_char_pair__normal);
	g_test_add_func("/string-utils/sc_string_quote__NULL_buf",
			test_sc_string_quote_NULL_str);
	g_test_add_func
	    ("/string-utils/sc_string_append_char_pair__uninitialized_buf",
	     test_sc_string_append_char_pair__uninitialized_buf);
	g_test_add_func("/string-utils/sc_string_quote", test_sc_string_quote);
}
