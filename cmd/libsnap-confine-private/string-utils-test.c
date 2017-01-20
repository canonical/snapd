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
	g_assert_false(sc_endswith("", NULL));
	g_assert_false(sc_endswith(NULL, ""));
	g_assert_false(sc_endswith(NULL, NULL));
	g_assert_true(sc_endswith("", ""));
	g_assert_true(sc_endswith("foobar", "bar"));
	g_assert_true(sc_endswith("foobar", "ar"));
	g_assert_true(sc_endswith("foobar", "r"));
	g_assert_true(sc_endswith("foobar", ""));
	g_assert_false(sc_endswith("foobar", "quux"));
	g_assert_false(sc_endswith("", "bar"));
}

static void __attribute__ ((constructor)) init()
{
	g_test_add_func("/string-utils/sc_streq", test_sc_streq);
	g_test_add_func("/string-utils/sc_endswith", test_sc_endswith);
}
